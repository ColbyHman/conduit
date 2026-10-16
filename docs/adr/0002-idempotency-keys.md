# ADR 0002: Idempotency Keys for Job Submission

## Status
Accepted

## Context
Frontend apps may retry job submission on network errors. Without idempotency, retries create duplicate jobs, wasting LLM API costs and creating duplicate entries.

## Decision
Require **idempotency keys** on all job submission endpoints. Gateway enforces uniqueness via unique constraint on `(job_type, idempotency_key)`.

## Implementation
- Clients generate UUIDv4 per logical operation (e.g., per audio file upload)
- Gateway `POST /api/jobs/{type}` accepts `Idempotency-Key` header
- On duplicate key: return `200 OK` with existing job ID (not `409`)
- Unique index: `CREATE UNIQUE INDEX idx_jobs_idempotency ON jobs(job_type, idempotency_key) WHERE idempotency_key IS NOT NULL`

## Rationale
- **Client-controlled** — No server-side session/state needed
- **Exact-once semantics** — Duplicate submissions return the same job
- **Low overhead** — Single index, no extra tables
- **Audit trail** — Can trace which client request created which job

## Consequences
- Clients MUST generate stable keys per logical operation (not per HTTP attempt)
- Gateway validates key format (UUID) and length (max 64 chars)
- Workers don't need idempotency logic — they process each job once via row locking