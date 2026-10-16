# Conduit - Local Setup Guide

## Prerequisites Check

```bash
# Check Go is installed (need 1.21+)
go version

# Check Docker is running
docker version
docker compose version
```

---

## 1. SQL Schema (`/sql/` folder)

**What's here:** Database initialization scripts that PostgreSQL runs on first startup.

**Status:** ✅ Already created `sql/001_initial_schema.sql` with:
- `jobs` table (queue with retry/dead-letter support)
- `entries` table (journal entries, ideas, thoughts)
- `magic_cards` table (MTG card analyses)
- `job_events` table (audit trail for observability)
- Indexes for performance
- Triggers for `updated_at` timestamps

**Verify it works:**
```bash
docker compose up -d postgres
docker compose logs postgres | grep "001_initial_schema.sql"
```

---

## 2. AI Gateway (`/ai-gateway/` folder)

**Purpose:** HTTP API that receives jobs from frontend apps and inserts them into the `jobs` table with idempotency support.

### Structure
```
ai-gateway/
├── Dockerfile
├── go.mod / go.sum
├── config.yaml
├── cmd/main.go                 # Entry point
└── internal/
    ├── config/config.go        # Config loading (YAML + env)
    ├── handler/jobs.go         # HTTP handlers for job submission
    └── store/jobs.go           # Database operations
```

### Key Features
- **Idempotency keys** via `Idempotency-Key` header (prevents duplicate jobs)
- **Job types:** `journal_transcribe`, `journal_analyze`, `mtg_analyze`
- **Validation** per job type
- **Structured logging** with request correlation

### Run Locally
```bash
cd ai-gateway
go mod tidy
go run ./cmd/main.go
# Server starts on :8080
```

### Test It
```bash
curl http://localhost:8080/health

curl -X POST http://localhost:8080/api/jobs/journal/transcribe \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: test-key-123" \
  -d '{"audio_url": "https://example.com/audio.m4a"}'
```

---

## 3. Worker (`/worker/` folder)

**Purpose:** Polls `jobs` table, grabs jobs using PostgreSQL row locking (`FOR UPDATE SKIP LOCKED`), and processes them with LLM APIs. Handles `journal_analyze` and `mtg_analyze` job types.

### Structure
```
worker/
├── Dockerfile
├── go.mod / go.sum
├── config.yaml
├── cmd/main.go                          # Entry point + polling loop
├── prompts/
│   ├── journal_summary.txt              # Journal analysis prompt template
│   └── mtg_analysis.txt                 # MTG card analysis prompt template
└── internal/
    ├── config/config.go                 # Config loading
    ├── store/jobs.go                    # Job DB operations + entry/card creation
    ├── llm/
    │   ├── client.go                    # LLM interface + registry
    │   ├── openai.go                    # OpenAI/Anthropic/OpenAI-compatible
    │   └── ollama.go                    # Ollama local models
    └── handler/
        ├── journal_analyze.go           # Journal analysis handler
        └── mtg_analyze.go               # MTG analysis handler
```

### Key Features
- **Provider-agnostic LLM client** — swap OpenAI ↔ Anthropic ↔ Ollama via config
- **Prompt templates** loaded from `prompts/` directory
- **Exponential backoff retry** with jitter (configurable)
- **Dead letter queue** after max retries
- **Stale job cleanup** on startup
- **Structured logging** with job_id correlation

### Run Locally
```bash
cd worker
go mod tidy
# Set required env vars
export LLM_API_KEY=your_key
export LLM_PROVIDER=openai  # or ollama, anthropic
export LLM_MODEL=gpt-4o-mini
go run ./cmd/main.go
```

### Test It
```bash
# Submit a journal_analyze job via AI Gateway, then watch worker logs
docker compose logs -f worker
```

---

## 4. Whisper Worker (`/whisper-worker/` folder)

**Purpose:** Polls `jobs` table for `journal_transcribe` jobs, calls the Whisper API to transcribe `.m4a` audio files, and updates jobs with the transcription text.

### Structure
```
whisper-worker/
├── Dockerfile
├── go.mod / go.sum
├── config.yaml
├── cmd/main.go                          # Entry point + polling loop
└── internal/
    ├── config/config.go                 # Config loading
    ├── store/jobs.go                    # Job DB operations
    └── whisper/client.go                # Whisper API client
```

### Key Features
- **Multiple audio sources:** HTTP URL, local file path, base64 (planned)
- **Whisper API contract:** `POST /transcribe` with multipart form
- **Response format:** `verbose_json` for word-level timestamps
- **Retry policy** same as main worker
- **Host networking** via `host.docker.internal` for local GPU Whisper

### Run Locally
```bash
cd whisper-worker
go mod tidy
export WHISPER_URL=http://localhost:9000  # or your Whisper server
go run ./cmd/main.go
```

---

## 5. Running the Full Stack (Docker Compose)

### Prerequisites
1. **Start Whisper server on host** (for GPU acceleration):
   ```bash
   # Option A: whisper.cpp (Metal on Mac, CUDA on Linux)
   cd whisper.cpp
   ./server -m models/ggml-medium.bin -p 9000 --host 0.0.0.0

   # Option B: faster-whisper
   python -m faster_whisper.server --port 9000 --model medium
   ```

2. **Configure environment:**
   ```bash
   cp .env.example .env
   # Edit .env with your LLM_API_KEY
   ```

### Start Everything
```bash
docker compose up -d
```

### Check Status
```bash
docker compose ps
# Should show: postgres, ai-gateway, worker, whisper-worker, (pgadmin)

docker compose logs -f worker          # Watch worker logs
docker compose logs -f whisper-worker  # Watch transcription logs
```

### Test AI Gateway
```bash
curl http://localhost:8080/health

# Submit transcription job
curl -X POST http://localhost:8080/api/jobs/journal/transcribe \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"audio_url": "https://example.com/recording.m4a", "model": "medium"}'

# Check job status
curl http://localhost:8080/api/jobs/{job_id}
```

### Access pgAdmin (Optional)
```bash
docker compose --profile tools up -d pgadmin
# http://localhost:5050
# Email: admin@conduit.local
# Password: admin
# Add server: host=postgres, user=conduit, password=conduit_dev, db=conduit
```

---

## 6. Single-Machine Deployment (Host Whisper + Docker Conduit)

This is the recommended setup for local development:

```
┌─────────────────────────────────────────────────────────────┐
│ Host Machine                                                │
│  ┌──────────────┐    ┌──────────────────────────────────┐  │
│  │ Whisper      │    │ Docker Compose Stack             │  │
│  │ (native,     │◄───│  ┌─────────┐ ┌────────┐ ┌──────┐ │  │
│  │  Metal/GPU)  │    │  │ Postgres│ │Gateway │ │Worker│ │  │
│  │  :9000       │    │  │         │ │ :8080  │ │      │ │  │
│  └──────────────┘    │  └─────────┘ └────────┘ └──────┘ │  │
│                      │  ┌──────────────┐                  │  │
│                      │  │WhisperWorker │                  │  │
│                      │  └──────────────┘                  │  │
│                      └──────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

- Whisper runs **natively on host** (Metal GPU on Mac, CUDA on Linux)
- Docker containers reach it via `http://host.docker.internal:9000`
- No GPU passthrough needed — avoids Docker/Metal complexity
- `extra_hosts: host.docker.internal:host-gateway` in docker-compose handles Linux

---

## 7. Key Operational Features

### Retry Policy (ADR 0004)
- **Max retries:** 3 (configurable per job via `max_retries` column)
- **Backoff:** Exponential (30s, 60s, 120s) + ±25% jitter
- **Dead letter:** After max retries → `status = 'dead_letter'`
- **Manual replay:** Update job to `status = 'pending', attempt_count = 0`

### Idempotency (ADR 0002)
- Clients send `Idempotency-Key` header (UUID per logical operation)
- Gateway returns existing job on duplicate key (200 OK, not 409)
- Unique index on `(job_type, idempotency_key)`

### Observability (ADR 0003)
- Structured JSON logs via `zerolog`
- `job_id` propagated through all services
- Events logged: `received`, `claimed`, `processing_start`, `llm_request`, `llm_response`, `completed`, `failed`, `retry_scheduled`
- `job_events` table for persistent audit trail

### LLM Provider Abstraction (ADR 0005)
```yaml
llm:
  provider: "ollama"        # openai, anthropic, ollama, openai-compatible
  model: "llama3.1:8b"
  api_key: ""               # Not needed for Ollama
  endpoint: "http://host.docker.internal:11434"
```

---

## 8. Common Issues

| Issue | Solution |
|-------|----------|
| `connection refused` to Whisper | Ensure Whisper server runs on host `0.0.0.0:9000`, not `localhost` |
| `host.docker.internal` not resolving on Linux | Docker Compose includes `extra_hosts: host.docker.internal:host-gateway` |
| Jobs stuck in `processing` | Worker crashed — `CleanupStaleJobs` runs on startup (5min threshold) |
| Duplicate jobs | Use `Idempotency-Key` header; same key returns existing job |
| LLM rate limits | Worker retries with exponential backoff; increase `max_retries` for critical jobs |

---

## Next Steps (After Scaffolding)

### For AI Gateway:
1. Add authentication (API keys, JWT)
2. Rate limiting per client
3. Request validation middleware

### For Worker:
1. Add vision support for MTG (GPT-4o, GPT-4V)
2. Implement base64 image upload endpoint
3. Add prompt versioning/metrics

### For Whisper Worker:
1. Implement base64 audio decoding
2. Add support for multiple Whisper models per job
3. Streaming transcription for long audio

### For All:
1. Add metrics (Prometheus) + Grafana dashboards
2. Distributed tracing (OpenTelemetry → Jaeger)
3. k3s manifests in `/k8s/`