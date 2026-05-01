# Conduit - Project Context

## Purpose

Conduit is a modular AI processing system built to handle multiple domains (Journal entries, Magic the Gathering card analysis) through a shared job queue architecture. The system uses PostgreSQL as a job queue with row-level locking for worker coordination.

## Core Concept

A job-based architecture where:
- Frontend applications send work to an AI Gateway API
- Jobs are stored in PostgreSQL with type classification
- Workers poll for jobs using `SELECT ... FOR UPDATE SKIP LOCKED`
- Workers process jobs using LLM APIs
- Results are stored in domain-specific tables

## Domains

### Journal Domain
- **Input**: Audio files (.m4a) containing journal entries
- **Flow**: Audio → Whisper transcription → LLM summarization/analysis → entries table
- **Future**: Will extend to support ideas and thoughts (different entry types)

### Magic the Gathering Domain
- **Input**: Card images
- **Flow**: Image → LLM analysis → magic_cards table
- **Output**: Card name, analysis, metadata

## Architecture Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Job Queue | PostgreSQL with row locking | Simple, no additional dependencies beyond DB |
| Worker Language | Golang | Performance, easy containerization, good for k3s deployment |
| DB Access | Workers write directly, Gateway reads/writes | Balance of simplicity and separation |
| Container Strategy | Minimal containers (5-6) | Reduce complexity while maintaining modularity |
| Configuration | YAML + Environment variables | Env vars override config, good for k3s secrets |
| LLM Integration | Provider-agnostic client | Allow swapping between OpenAI, Anthropic, local models |

## Local Development

The stack runs via Docker Compose with:
- PostgreSQL 16
- AI Gateway (Golang API)
- Worker (Golang, handles journal_analyze and mtg_analyze)
- Whisper Worker (Golang, handles journal_transcribe)
- Optional: pgAdmin for DB inspection

## Deployment Target

- **Container Orchestration**: k3s (lightweight Kubernetes)
- **Future**: Each domain can be scaled independently
- **Goal**: Simple deployment with kubectl apply or GitOps

## Project Structure

```
conduit/
├── context.md              # This file - project context
├── PLAN.md                 # Detailed architecture plan
├── docker-compose.yml      # Local development stack
├── k8s/                    # k3s manifests (future)
├── sql/                    # Database schemas
├── ai-gateway/             # API gateway service
├── worker/                 # Main worker service
└── whisper-worker/         # Transcription worker
```

## Environment Variables

Key variables needed:
- `DATABASE_URL` - PostgreSQL connection
- `LLM_API_KEY` - API key for LLM provider
- `LLM_ENDPOINT` - Optional custom LLM endpoint
- `LLM_MODEL` - Model to use (e.g., gpt-4)
- `WHISPER_URL` - Whisper receiver API address
- `WORKER_ID` - Unique worker identifier (auto-generated if not set)

## Status

🚧 **In Development** - Setting up local Docker Compose stack.
