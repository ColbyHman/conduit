-- Conduit initial database schema
-- Run this manually or let PostgreSQL execute it on startup (via docker-entrypoint-initdb.d)

-- 1. Jobs table: Worker job queue with row-level locking support
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(), -- Unique job ID
    job_type VARCHAR(50) NOT NULL, -- 'journal_transcribe', 'journal_analyze', 'mtg_analyze'
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- 'pending', 'processing', 'completed', 'failed'
    payload JSONB NOT NULL, -- Input data (audio URL, transcription text, image URL, etc.)
    result JSONB, -- Output from LLM/Whisper
    error TEXT, -- Error message if job fails
    locked_at TIMESTAMP WITH TIME ZONE, -- When a worker locked this job
    locked_by VARCHAR(255), -- Worker ID that locked the job
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE
);

-- Indexes to speed up job queries
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_job_type ON jobs(job_type);
CREATE INDEX IF NOT EXISTS idx_jobs_locked_at ON jobs(locked_at);

-- 2. Entries table: Stores journal entries, ideas, thoughts
CREATE TABLE IF NOT EXISTS entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_type VARCHAR(20) NOT NULL, -- 'journal', 'idea', 'thought'
    title VARCHAR(255), -- Optional title for the entry
    content TEXT NOT NULL, -- Original content (transcription text, etc.)
    summary TEXT, -- LLM-generated summary
    analysis JSONB, -- LLM analysis results
    source_reference VARCHAR(255), -- Link to original source (audio file path, etc.)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_entries_type ON entries(entry_type);
CREATE INDEX IF NOT EXISTS idx_entries_created_at ON entries(created_at DESC);

-- 3. Magic cards table: Stores MTG card analysis results
CREATE TABLE IF NOT EXISTS magic_cards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    image_url VARCHAR(500), -- Original image URL if applicable
    image_path VARCHAR(500), -- Local stored image path if applicable
    card_name VARCHAR(255), -- Extracted card name from LLM
    analysis JSONB, -- LLM analysis results
    raw_response TEXT, -- Full LLM response for debugging
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_magic_cards_created_at ON magic_cards(created_at DESC);
