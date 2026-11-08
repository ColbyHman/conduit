-- Conduit ADR 0015: default holdings for the live demo — AAPL + NVDA.
-- Inserts both the holding row and its ADR 0007 schedule row
-- (finance_ingest every 6h = 21600s), idempotently.

INSERT INTO finance_holdings (ticker, asset_type, active)
VALUES ('AAPL', 'equity', true), ('NVDA', 'equity', true)
ON CONFLICT (ticker) WHERE active = true DO NOTHING;

INSERT INTO schedules (name, job_type, payload_template, interval_seconds, enabled)
VALUES
    ('finance_ingest_aapl', 'finance_ingest', '{"ticker": "AAPL"}'::jsonb, 21600, true),
    ('finance_ingest_nvda', 'finance_ingest', '{"ticker": "NVDA"}'::jsonb, 21600, true)
ON CONFLICT (name) DO NOTHING;
