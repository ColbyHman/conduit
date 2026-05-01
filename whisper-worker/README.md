# Whisper Worker

Polls the `jobs` table for `journal_transcribe` jobs, calls the Whisper API to transcribe .m4a audio files, and updates jobs with the transcription text.

## What belongs in this folder:

```
whisper-worker/
├── Dockerfile          # Build instructions for Docker
├── go.mod              # Go module file
├── go.sum              # Dependency lock file
├── main.go             # Entry point - starts job polling loop
├── internal/
│   ├── store/          # Database operations
│   │   ├── db.go       # PostgreSQL connection
│   │   └── jobs.go     # Job locking, updating
│   └── whisper/        # Whisper API client
│       └── client.go   # Calls Whisper receiver API
└── config.yaml         # Optional config
```

## Setup Commands:

```bash
# Initialize Go module
go mod init github.com/conduit/whisper-worker

# Add dependencies
go get github.com/jackc/pgx/v5

# Run locally
go run main.go
```

## Environment Variables:
- `DATABASE_URL` - PostgreSQL connection string
- `WORKER_ID` - Unique worker identifier
- `WHISPER_URL` - Whisper receiver API address (e.g., http://localhost:9000)
