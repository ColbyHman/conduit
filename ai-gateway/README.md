# AI Gateway

Receives jobs from frontend apps and inserts them into the `jobs` PostgreSQL table.

## What belongs in this folder:

```
ai-gateway/
├── Dockerfile          # Build instructions for Docker
├── go.mod              # Go module file (go mod init)
├── go.sum              # Dependency lock file (auto-generated)
├── main.go             # Entry point - starts HTTP server
├── internal/
│   ├── handler/        # HTTP route handlers
│   │   ├── journal.go # POST /api/jobs/journal
│   │   └── mtg.go     # POST /api/jobs/mtg
│   └── store/          # Database operations
│       └── jobs.go     # Insert job into jobs table
└── config.yaml         # Optional config (env vars override)
```

## Setup Commands:

```bash
# Initialize Go module
go mod init github.com/conduit/ai-gateway

# Add PostgreSQL driver
go get github.com/jackc/pgx/v5

# Run locally
go run main.go
```

## Environment Variables:
- `DATABASE_URL` - PostgreSQL connection string
- `PORT` - HTTP server port (default 8080)
