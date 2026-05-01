# Conduit - Local Setup Guide (by Service)

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

**Status:** ✅ Already created `sql/001_initial_schema.sql`

**Verify it works:**
```bash
docker compose up -d postgres
docker compose logs postgres | grep "001_initial_schema.sql"
```

---

## 2. AI Gateway (`/ai-gateway/` folder)

**Purpose:** HTTP API that receives jobs from frontend apps and inserts them into the `jobs` table.

### Step 1: Initialize Go module
```bash
cd ai-gateway
go mod init github.com/conduit/ai-gateway
```

### Step 2: Create `main.go`
```go
package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "AI Gateway running")
	})

	fmt.Println("Starting AI Gateway on :8080")
	http.ListenAndServe(":8080", nil)
}
```

### Step 3: Create `Dockerfile`
```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o ai-gateway .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/ai-gateway .
EXPOSE 8080
CMD ["./ai-gateway"]
```

### Step 4: Test it
```bash
go run main.go
# In another terminal: curl http://localhost:8080/health
```

---

## 3. Worker (`/worker/` folder)

**Purpose:** Polls `jobs` table, grabs jobs with PG locking, sends to LLM for processing. Handles `journal_analyze` and `mtg_analyze` job types.

### Step 1: Initialize Go module
```bash
cd ../worker
go mod init github.com/conduit/worker
```

### Step 2: Create `main.go`
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Worker starting...")

	// TODO: Connect to DB, start polling for jobs

	for {
		fmt.Println("Polling for jobs...")
		time.Sleep(5 * time.Second)
	}
}
```

### Step 3: Create `Dockerfile`
```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o worker .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/worker .
CMD ["./worker"]
```

### Step 4: Test it
```bash
go run main.go
# Should print "Polling for jobs..." every 5 seconds
```

---

## 4. Whisper Worker (`/whisper-worker/` folder)

**Purpose:** Polls `jobs` table for `journal_transcribe` jobs, calls Whisper API with .m4a files, gets transcription.

### Step 1: Initialize Go module
```bash
cd ../whisper-worker
go mod init github.com/conduit/whisper-worker
```

### Step 2: Create `main.go`
```go
package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Whisper Worker starting...")

	// TODO: Connect to DB, poll for journal_transcribe jobs
	// TODO: Call Whisper API at WHISPER_URL

	for {
		fmt.Println("Polling for transcription jobs...")
		time.Sleep(5 * time.Second)
	}
}
```

### Step 3: Create `Dockerfile`
```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o whisper-worker .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/whisper-worker .
CMD ["./whisper-worker"]
```

### Step 4: Test it
```bash
go run main.go
```

---

## 5. Running the Full Stack

### Start everything:
```bash
cd /Users/colby/repos/conduit
docker compose up -d
```

### Check status:
```bash
docker compose ps
docker compose logs -f worker  # Watch worker logs
```

### Test AI Gateway:
```bash
curl http://localhost:8080/health
```

### Access pgAdmin (optional):
- URL: http://localhost:5050
- Email: admin@conduit.local
- Password: admin
- Add server: host=postgres, user=conduit, password=conduit_dev, db=conduit

---

## Next Steps (After Scaffolding)

### For AI Gateway:
1. Add PostgreSQL driver: `go get github.com/jackc/pgx/v5`
2. Create `handlers/` folder for HTTP route handlers
3. Create `store/` folder for database operations

### For Worker:
1. Add PostgreSQL driver: `go get github.com/jackc/pgx/v5`
2. Create `config/` folder for YAML/env config
3. Create `handlers/` folder for job type handlers
4. Create `llm/` folder for LLM API client

### For Whisper Worker:
1. Add PostgreSQL driver
2. Create `whisper/` folder for Whisper API client

---

## Quick Reference: What Go Packages You'll Need

| Package | Purpose | Install Command |
|---------|---------|-----------------|
| `github.com/jackc/pgx/v5` | PostgreSQL driver | `go get github.com/jackc/pgx/v5` |
| `github.com/gin-gonic/gin` | HTTP framework (optional) | `go get github.com/gin-gonic/gin` |
| `gopkg.in/yaml.v3` | YAML config parsing | `go get gopkg.in/yaml.v3` |
| `github.com/joho/godotenv` | .env file support (optional) | `go get github.com/joho/godotenv` |

Start with just `pgx` for database access. Add others as needed.
