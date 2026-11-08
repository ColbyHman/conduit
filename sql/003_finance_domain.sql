-- Conduit ADR 0015: Live Finance Domain + Dynamic Holdings

-- Dynamic holdings list (replaces fixed frontend fixture list)
CREATE TABLE IF NOT EXISTS finance_holdings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker VARCHAR(20) NOT NULL,
    asset_type VARCHAR(20) NOT NULL CHECK (asset_type IN ('equity','etf','treasury','index')),
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- A ticker may be soft-deleted and re-added; keep history, avoid dup active rows.
CREATE UNIQUE INDEX IF NOT EXISTS idx_finance_holdings_active_ticker
    ON finance_holdings(ticker) WHERE active = true;
CREATE INDEX IF NOT EXISTS idx_finance_holdings_ticker ON finance_holdings(ticker);

CREATE TRIGGER update_finance_holdings_updated_at BEFORE UPDATE ON finance_holdings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Raw ingested documents (SEC EDGAR filings, optional Finnhub news)
CREATE TABLE IF NOT EXISTS finance_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker VARCHAR(20) NOT NULL,
    source VARCHAR(20) NOT NULL CHECK (source IN ('sec_edgar','finnhub_news')),
    doc_type VARCHAR(50),               -- '10-K', '10-Q', '8-K', 'Form 4', 'news', etc.
    url VARCHAR(1000),
    title VARCHAR(500),
    published_at TIMESTAMP WITH TIME ZONE,
    raw_text TEXT,
    fetched_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    job_id UUID REFERENCES jobs(id)
);

CREATE INDEX IF NOT EXISTS idx_finance_documents_ticker ON finance_documents(ticker);
CREATE INDEX IF NOT EXISTS idx_finance_documents_fetched_at ON finance_documents(fetched_at DESC);
-- Avoid re-storing the same filing/article twice across ingest runs.
CREATE UNIQUE INDEX IF NOT EXISTS idx_finance_documents_dedupe ON finance_documents(ticker, source, url);

-- One row per finance_analyze run
CREATE TABLE IF NOT EXISTS finance_analyses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker VARCHAR(20) NOT NULL,
    summary TEXT NOT NULL,
    event_type VARCHAR(50),             -- 'earnings','insider_activity','macro_sector','rumor_opinion','no_activity', etc.
    sentiment VARCHAR(20),              -- 'positive','negative','neutral','mixed'
    materiality INT,                    -- 0-100
    citations JSONB NOT NULL DEFAULT '[]', -- [{document_id, url, excerpt}]
    job_id UUID REFERENCES jobs(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_finance_analyses_ticker ON finance_analyses(ticker);
CREATE INDEX IF NOT EXISTS idx_finance_analyses_created_at ON finance_analyses(created_at DESC);
