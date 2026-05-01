# Worker

Polls the `jobs` table, grabs jobs using PostgreSQL row locking, and processes them with LLM APIs. Handles `journal_analyze` and `mtg_analyze` job types.

## What belongs in this folder:

```
worker/
├── Dockerfile              # Build instructions for Docker
├── go.mod                  # Go module file
├── go.sum                  # Dependency lock file
├── main.go                 # Entry point - starts job polling loop
├── config.yaml             # YAML config (env vars override)
├── internal/
│   ├── config/             # Config loading (YAML + env vars)
│   │   └── config.go
│   ├── store/              # Database operations
│   │   ├── db.go           # PostgreSQL connection
│   │   └── jobs.go         # Job locking, updating, creating
│   ├── handler/            # Job type handlers
│   │   ├── journal_analyze.go  # LLM summarization of transcriptions
│   │   └── mtg_analyze.go      # LLM analysis of MTG card images
│   └── llm/                # LLM API client
│       └── client.go       # Generic LLM client (OpenAI, Anthropic, etc.)
└── prompts/                # LLM prompt templates
    ├── journal_summary.txt
    └── mtg_analysis.txt
```

## Setup Commands:

```bash
# Initialize Go module
go mod init github.com/conduit/worker

# Add dependencies
go get github.com/jackc/pgx/v5
go get gopkg.in/yaml.v3

# Run locally
go run main.go
```

## Environment Variables:
- `DATABASE_URL` - PostgreSQL connection string
- `WORKER_ID` - Unique worker identifier (auto-generated if not set)
- `LLM_API_KEY` - API key for LLM provider
- `LLM_ENDPOINT` - Optional custom LLM endpoint
- `LLM_MODEL` - Model to use (e.g., gpt-4)
