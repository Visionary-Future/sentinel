CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "vector";

CREATE TYPE alert_source AS ENUM ('slack', 'outlook', 'webhook', 'aliyun', 'huaweicloud');
CREATE TYPE alert_severity AS ENUM ('critical', 'warning', 'info');
CREATE TYPE alert_status AS ENUM ('open', 'acked', 'resolved');

CREATE TABLE alerts (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    source           alert_source    NOT NULL,
    severity         alert_severity  NOT NULL DEFAULT 'warning',
    title            TEXT            NOT NULL,
    description      TEXT            NOT NULL DEFAULT '',
    service          TEXT            NOT NULL DEFAULT '',
    labels           JSONB           NOT NULL DEFAULT '{}',
    raw_payload      BYTEA,
    fingerprint      TEXT            NOT NULL,
    status           alert_status    NOT NULL DEFAULT 'open',
    correlation_id   UUID,
    -- Source-specific fields
    slack_channel_id TEXT            NOT NULL DEFAULT '',
    slack_message_ts TEXT            NOT NULL DEFAULT '',
    slack_thread_ts  TEXT            NOT NULL DEFAULT '',
    -- Vector embedding for semantic similarity search (populated async)
    embedding        vector(1536),
    -- Timestamps
    received_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    resolved_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Dedup: one row per fingerprint (most recent wins via ON CONFLICT DO NOTHING)
CREATE UNIQUE INDEX alerts_fingerprint_idx ON alerts (fingerprint);

-- Lookups
CREATE INDEX alerts_status_idx       ON alerts (status);
CREATE INDEX alerts_service_idx      ON alerts (service);
CREATE INDEX alerts_received_at_idx  ON alerts (received_at DESC);
CREATE INDEX alerts_source_idx       ON alerts (source);

-- Vector similarity search (IVFFlat, tune lists= for dataset size)
CREATE INDEX alerts_embedding_idx ON alerts
    USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

-- Auto-update updated_at
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER alerts_updated_at
    BEFORE UPDATE ON alerts
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
