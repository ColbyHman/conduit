# Conduit

Conduit is a modular workflow engine that routes and processes asynchronous jobs through LLM-enhanced pipelines. It abstracts domain-specific logic, enabling services to connect, transform, and act on data through a unified interface.

## Overview

Conduit uses a job-based architecture with PostgreSQL as a distributed queue. Workers poll for jobs using row-level locking, process them with LLM APIs, and store results in domain-specific tables.

**Current Domains:**
- **Journal** - Audio transcription (Whisper) + LLM analysis of journal entries
- **Magic the Gathering** - LLM analysis of card images

## Architecture

```
Frontend Apps → AI Gateway → PostgreSQL (jobs table)
                                    ↓
                            Workers (PG Locking)
                                    ↓
                    ┌───────────────┼───────────────┐
                    ↓               ↓                ↓
              Whisper API    LLM (Journal)     LLM (MTG)
                    ↓               ↓                ↓
              Transcription    entries table    magic_cards table
```

## Services

| Service | Description |
|---------|-------------|
| **PostgreSQL** | Job queue + domain tables (jobs, entries, magic_cards, job_events) |
| **AI Gateway** | HTTP API for job ingestion from frontends (port 8080) |
| **Worker** | Processes `journal_analyze` and `mtg_analyze` jobs |
| **Whisper Worker** | Transcribes `.m4a` files via Whisper API |

## Project Structure

```
conduit/
├── README.md              # This file
├── context.md             # Project context and decisions
├── SETUP.md               # Step-by-step setup guide by service
├── PLAN.md                # Detailed architecture plan
├── docker-compose.yml     # Local development stack
├── .env.example           # Environment variable template
├── docs/
│   └── adr/               # Architecture Decision Records
├── sql/                   # Database schemas
│   └── 001_initial_schema.sql
├── ai-gateway/            # HTTP API service
├── worker/                # Main LLM worker
└── whisper-worker/        # Transcription worker
```

## Quick Start

### Prerequisites

- Go 1.21+
- Docker & Docker Compose
- PostgreSQL client (optional, for direct DB access)

### 1. Configure Environment

```bash
cp .env.example .env
# Edit .env with your API keys:
# - LLM_API_KEY (required for Worker)
# - WHISPER_URL (required for Whisper Worker, default: http://host.docker.internal:9000)
```

### 2. Start Whisper Server (Separate Process)

The Whisper Worker calls an external Whisper API. Run Whisper natively on your host for GPU acceleration:

**Option A: whisper.cpp server (recommended for Mac Metal / Linux CUDA)**
```bash
# Install whisper.cpp
git clone https://github.com/ggerganov/whisper.cpp
cd whisper.cpp
make
# Download model (e.g., medium)
./models/download-ggml-model.sh medium
# Start server on port 9000
./server -m models/ggml-medium.bin -p 9000 --host 0.0.0.0
```

**Option B: faster-whisper (Python)**
```bash
pip install faster-whisper
python -m faster_whisper.server --port 9000 --model medium
```

**Option C: OpenAI Whisper API (cloud)**
```bash
# Set WHISPER_URL=https://api.openai.com/v1 in .env
# Requires OPENAI_API_KEY
```

### 3. Start Conduit Stack

```bash
docker compose up -d
```

### 4. Verify Services

```bash
# Check all containers running
docker compose ps

# View logs
docker compose logs -f

# Test AI Gateway health
curl http://localhost:8080/health

# Access pgAdmin (optional)
# http://localhost:5050 (admin@conduit.local / admin)
```

## Submitting Jobs

### Journal Transcription
```bash
curl -X POST http://localhost:8080/api/jobs/journal/transcribe \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "audio_url": "https://example.com/audio.m4a",
    "model": "medium",
    "response_format": "verbose_json"
  }'
```

### Journal Analysis (after transcription)
```bash
curl -X POST http://localhost:8080/api/jobs/journal/analyze \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "transcription_text": "Your transcribed text here...",
    "segments": [...],
    "language": "en",
    "duration": 45.2,
    "entry_type": "journal"
  }'
```

### MTG Card Analysis
```bash
curl -X POST http://localhost:8080/api/jobs/mtg/analyze \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "image_url": "https://example.com/card.jpg"
  }'
```

### Check Job Status
```bash
curl http://localhost:8080/api/jobs/{job_id}
```

## Local Development (Without Docker)

### AI Gateway
```bash
cd ai-gateway
go mod tidy
go run ./cmd/main.go
# Runs on :8080
```

### Worker
```bash
cd worker
go mod tidy
go run ./cmd/main.go
# Polls for journal_analyze and mtg_analyze jobs
```

### Whisper Worker
```bash
cd whisper-worker
go mod tidy
go run ./cmd/main.go
# Polls for journal_transcribe jobs, calls WHISPER_URL
```

## Architecture Decision Records (ADRs)

See `docs/adr/` for detailed design decisions:

- [0001: PostgreSQL as Job Queue](docs/adr/0001-postgresql-as-job-queue.md)
- [0002: Idempotency Keys for Job Submission](docs/adr/0002-idempotency-keys.md)
- [0003: Structured Logging and Job Observability](docs/adr/0003-structured-logging.md)
- [0004: Retry Policy and Dead Letter Handling](docs/adr/0004-retry-policy.md)
- [0005: Worker Architecture and LLM Provider Abstraction](docs/adr/0005-worker-llm-abstraction.md)
- [0006: Whisper Integration and Single-Machine Deployment](docs/adr/0006-whisper-integration.md)

## Configuration

All services use `config.yaml` with environment variable overrides. Key variables:

| Variable | Service | Description |
|----------|---------|-------------|
| `DATABASE_URL` | All | PostgreSQL connection string |
| `LLM_API_KEY` | Worker | API key for LLM provider |
| `LLM_PROVIDER` | Worker | `openai`, `anthropic`, `ollama`, `openai-compatible` |
| `LLM_MODEL` | Worker | Model name (e.g., `gpt-4o-mini`) |
| `WHISPER_URL` | Whisper Worker | Whisper API endpoint |
| `WHISPER_MODEL` | Whisper Worker | Whisper model size |
| `WORKER_ID` | Workers | Unique worker identifier |
| `LOG_LEVEL` | All | `debug`, `info`, `warn`, `error` |

## Deployment

Target: **k3s** (lightweight Kubernetes)

Future k8s manifests will live in `/k8s/`.

## Status

🚧 **In Development** - Initial implementation complete with:
- ✅ PostgreSQL job queue with row locking
- ✅ AI Gateway with idempotent job submission
- ✅ Worker with LLM provider abstraction (OpenAI, Anthropic, Ollama)
- ✅ Whisper Worker with external Whisper API integration
- ✅ Retry policy with exponential backoff + jitter
- ✅ Dead letter queue for failed jobs
- ✅ Structured logging with job correlation
- ✅ Docker Compose for local development
- ✅ ADR documentation

## License

[Your License Here]