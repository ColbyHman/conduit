# ADR 0007: Scheduled / Recurring Jobs

## Status
Proposed

## Context
Every job today is triggered by an external caller hitting the AI Gateway
(a journal upload, an MTG card photo). The finance domain (auto-researcher
triage) needs a job that fires on its own on a timer — periodically
re-ingest news/filings per holding and re-run analysis — with no human
submitting it each time. There's no scheduling primitive in Conduit yet.
This is deliberately generic: finance is the first consumer, not the
only one (Journal/MTG could use it later for digests, cleanup, etc).

## Decision
**New `schedules` table + a new minimal `scheduler` binary**, following
the existing per-service container shape (worker, whisper-worker).

### Schema (`sql/002_schedules.sql`)
```sql
CREATE TABLE schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,          -- e.g. 'finance_ingest_aapl'
    job_type VARCHAR(50) NOT NULL,              -- job_type to enqueue, e.g. 'finance_ingest'
    payload_template JSONB NOT NULL,            -- merged verbatim into the created job's payload
    interval_seconds INT NOT NULL,              -- simple interval, not cron (see Consequences)
    enabled BOOLEAN NOT NULL DEFAULT true,
    next_run_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_run_at TIMESTAMP WITH TIME ZONE,
    last_job_id UUID REFERENCES jobs(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
CREATE INDEX idx_schedules_due ON schedules(next_run_at) WHERE enabled = true;
```

### Scheduler behavior
- New `scheduler/` service, same shape as `worker/` (own `cmd/main.go`,
  own Dockerfile, polls on a ticker — default 30s).
- Each tick: `SELECT ... FOR UPDATE SKIP LOCKED` on rows where
  `enabled = true AND next_run_at <= NOW()`, same locking pattern the
  job queue already uses, so two scheduler replicas never double-fire.
- For each due row: insert a new row into `jobs` with
  `job_type = schedules.job_type`, `payload = schedules.payload_template`
  (a dynamic finance schedule can template in the ticker, e.g.
  `{"ticker": "AAPL"}`), set `schedules.last_run_at = NOW()`,
  `last_job_id = <new job id>`, `next_run_at = NOW() + interval_seconds`.
- Scheduler never executes job logic itself — it only enqueues. Existing
  workers pick the job up through the normal poll loop. This keeps the
  scheduler dumb and reuses all existing retry/dead-letter machinery
  (ADR 0004) for free.
- `idempotency_key` on the created job = `schedule_id + next_run_at`
  (truncated to the interval boundary) so a scheduler crash-and-restart
  mid-tick can't double-enqueue the same fire.

## Rationale
- **Interval, not cron syntax** — every known use case right now
  ("every N hours") is expressible as a plain interval. Cron expression
  parsing is a solved but non-zero-effort problem; not worth it before
  the first scheduled job type (finance) even has a second domain
  wanting cron-specific behavior (e.g. "9am weekdays only"). Add cron
  parsing later behind the same `schedules` table (new nullable
  `cron_expression` column, `interval_seconds` becomes optional) without
  a breaking change.
- **Separate `scheduler` binary**, not a goroutine bolted onto
  `ai-gateway` — matches the existing "one small container per
  responsibility" decision (context.md) and means the scheduler can
  restart/redeploy independently of the API.
- **Enqueue-only, never execute** — avoids a second code path for job
  execution; the scheduler is trivial to reason about and test.

## Consequences
- Manual "refresh now" (a demo button, or any ad-hoc user action) stays
  a direct `POST` to the gateway that inserts straight into `jobs` —
  schedules are only for the *unattended* recurring path, the gateway
  keeps its existing direct-enqueue behavior unchanged.
- No cron semantics (day-of-week, time-of-day) in this pass. If the
  finance domain later wants "only during market hours," that's a
  follow-up ADR, not blocking this one.
- Admin visibility (list schedules, pause/resume, see last run) needs
  at least one `ai-gateway` endpoint; included in scope here since it's
  near-zero extra work (`GET/PATCH /schedules`).
