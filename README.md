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
| **PostgreSQL** | Job queue + domain tables (3 tables) |
| **AI Gateway** | HTTP API for job ingestion from frontends |
| **Worker** | Processes `journal_analyze` and `mtg_analyze` jobs |
| **Whisper Worker** | Transcribes .m4a files via Whisper API |

## Project Structure

```
conduit/
├── README.md              # This file
├── context.md             # Project context and decisions
├── SETUP.md               # Step-by-step setup guide by service
├── PLAN.md                # Detailed architecture plan
├── docker-compose.yml     # Local development stack
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
- PostgreSQL client (optional)

### Local Development

1. **Initialize Go modules** (in each service folder):
   ```bash
   cd ai-gateway && go mod init github.com/conduit/ai-gateway
   cd ../worker && go mod init github.com/conduit/worker
   cd ../whisper-worker && go mod init github.com/conduit/whisper-worker
   ```

2. **Start the stack**:
   ```bash
   docker compose up -d
   ```

3. **Check status**:
   ```bash
   docker compose ps
   docker compose logs -f
   ```

### Environment Variables

Create a `.env` file in the root (or export these):
```bash
LLM_API_KEY=your_llm_api_key
LLM_ENDPOINT=https://api.openai.com/v1  # optional
LLM_MODEL=gpt-4
WHISPER_URL=http://localhost:9000
```

## Documentation

- **[Context](context.md)** - Project context, architecture decisions, and status
- **[Setup Guide](SETUP.md)** - Step-by-step setup instructions by service folder
- **[Architecture Plan](PLAN.md)** - Detailed system design and data flows

## Deployment

Target: **k3s** (lightweight Kubernetes)

Future k8s manifests will live in `/k8s/`.

## Status

🚧 **In Development** - Setting up local Docker Compose stack and scaffolding Go services.

## License

[Your License Here]
