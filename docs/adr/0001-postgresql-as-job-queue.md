# ADR 0001: PostgreSQL as Job Queue

## Status
Accepted

## Context
Conduit needs a reliable, durable job queue for asynchronous AI processing (transcription, LLM analysis). Options considered:
- Redis + Bull/BullMQ
- RabbitMQ
- PostgreSQL with `SELECT FOR UPDATE SKIP LOCKED`
- Kafka

## Decision
Use **PostgreSQL with row-level locking (`SELECT FOR UPDATE SKIP LOCKED`)** as the job queue.

## Rationale
- **Zero additional infrastructure** — Postgres is already required for domain tables (entries, magic_cards)
- **ACID guarantees** — Job state transitions are transactional; no dual-write problems
- **Operational simplicity** — One database to backup, monitor, secure
- **`SKIP LOCKED` is battle-tested** — Used by GitHub, Shopify, and others for high-throughput queues
- **Visibility** — Jobs are queryable with standard SQL; easy to debug, replay, inspect
- **k3s friendly** — Single StatefulSet for Postgres; no extra operators

## Trade-offs
- **Latency** — Polling interval adds ~100-500ms vs push-based queues (mitigated by short poll intervals)
- **Throughput ceiling** — ~10k jobs/sec on modest hardware; sufficient for current domains
- **No built-in delayed execution** — Implemented via `next_retry_at` column + poll filter

## Consequences
- Workers poll with `WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= NOW())`
- Job claiming uses single UPDATE with `RETURNING` to atomically claim + fetch
- Schema includes `attempt_count`, `max_retries`, `next_retry_at` for retry logic
- Idempotency keys prevent duplicate processing on gateway retries