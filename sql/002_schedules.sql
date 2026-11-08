-- Conduit ADR 0007: Scheduled / Recurring Jobs
-- New schedules table + scheduler enqueue-only primitive.

CREATE TABLE IF NOT EXISTS schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,          -- e.g. 'finance_ingest_aapl'
    job_type VARCHAR(50) NOT NULL,              -- job_type to enqueue, e.g. 'finance_ingest'
    payload_template JSONB NOT NULL,            -- merged verbatim into the created job's payload
    interval_seconds INT NOT NULL,              -- simple interval, not cron (see ADR 0007 Consequences)
    enabled BOOLEAN NOT NULL DEFAULT true,
    next_run_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_run_at TIMESTAMP WITH TIME ZONE,
    last_job_id UUID REFERENCES jobs(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_schedules_due ON schedules(next_run_at) WHERE enabled = true;

CREATE TRIGGER update_schedules_updated_at BEFORE UPDATE ON schedules
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
