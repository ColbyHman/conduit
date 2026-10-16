# ADR 0003: Structured Logging and Job Observability

## Status
Accepted

## Context
With multiple workers polling the same queue, debugging requires correlating events across services: gateway → queue → worker → LLM → database.

## Decision
Emit **structured JSON logs** with a consistent `job_id` field at every state transition. Use a shared logging library (zerolog) across all services.

## Log Events
| Event | Fields |
|-------|--------|
| `job_received` | `job_id`, `job_type`, `client_ip`, `idempotency_key` |
| `job_claimed` | `job_id`, `worker_id`, `attempt` |
| `job_processing_start` | `job_id`, `worker_id`, `handler` |
| `llm_request` | `job_id`, `model`, `prompt_tokens` (estimated) |
| `llm_response` | `job_id`, `model`, `completion_tokens`, `duration_ms`, `cached` |
| `job_completed` | `job_id`, `worker_id`, `duration_ms`, `result_size` |
| `job_failed` | `job_id`, `worker_id`, `attempt`, `error`, `will_retry` |
| `job_retry_scheduled` | `job_id`, `next_attempt`, `next_retry_at` |

## Implementation
- All services use `zerolog` with `Context` propagation
- `job_id` added to log context at claim time, flows through handler
- Log level: `info` for transitions, `debug` for LLM request/response, `error` for failures
- Docker Compose: `logging: driver: json-file` with `max-size: 10m`, `max-file: 3`
- Future: ship to Loki/Grafana via promtail

## Rationale
- **Correlation** — Single `job_id` links gateway log to worker log to DB row
- **No tracing infra yet** — Structured logs are 80% of value at 5% of cost
- **zerolog** — Zero-allocation, fast, standard library compatible
- **JSON** — Machine-parseable for log aggregation

## Consequences
- All services import shared `pkg/log` (to be created)
- Workers log at claim, not at poll (reduces noise)
- Failed jobs log `will_retry` boolean for alerting on permanent failures