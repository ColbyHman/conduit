# ADR 0004: Retry Policy and Dead Letter Handling

## Status
Accepted

## Context
Jobs can fail for transient reasons (LLM rate limits, network blips, Whisper timeout) or permanent reasons (bad input, model degradation). Need automatic retry with backoff and a dead-letter path for permanent failures.

## Decision
**Exponential backoff with jitter**, max 3 attempts by default, configurable per job type. Permanent failures move to `status = 'dead_letter'` with error preserved.

## Retry Parameters
| Parameter | Default | Configurable Via |
|-----------|---------|------------------|
| `max_retries` | 3 | `jobs.max_retries` column, env `WORKER_MAX_RETRIES` |
| `base_delay` | 30s | env `WORKER_BASE_RETRY_DELAY` |
| `max_delay` | 10m | env `WORKER_MAX_RETRY_DELAY` |
| `jitter` | ±25% | hardcoded |

## Algorithm
```
next_delay = min(base_delay * 2^attempt + jitter, max_delay)
next_retry_at = NOW() + next_delay
```
- Attempt 0 (first failure): ~30s ±25%
- Attempt 1: ~60s ±25%
- Attempt 2: ~120s ±25%
- Attempt 3: → `dead_letter`

## Dead Letter
- `status = 'dead_letter'` (not `failed`)
- `error` column contains last error
- `completed_at` set to failure time
- Separate index: `idx_jobs_dead_letter` for admin queries
- Manual replay: admin updates `status = 'pending', attempt_count = 0, next_retry_at = NULL`

## Non-Retriable Errors (Fast Path to Dead Letter)
- `context deadline exceeded` (worker shutdown)
- `invalid payload` (JSON unmarshal fails)
- `idempotency key conflict` (shouldn't reach worker)
- LLM `400 Bad Request` (prompt too long, invalid params)

## Rationale
- **Exponential backoff** — Handles rate limits and transient outages
- **Jitter** — Prevents thundering herd when many jobs fail simultaneously
- **Dead letter ≠ failed** — Distinguishes "needs human review" from "will retry"
- **Per-job max_retries** — Allows critical jobs (e.g., paid user) more attempts

## Consequences
- Workers MUST check `attempt_count >= max_retries` before processing
- `next_retry_at` index enables efficient "jobs ready to retry" query
- Admin UI needed for dead letter inspection/replay (future)