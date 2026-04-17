CREATE TYPE investigation_status AS ENUM ('pending', 'running', 'completed', 'failed');

CREATE TABLE investigations (
    id           UUID                 PRIMARY KEY DEFAULT uuid_generate_v4(),
    alert_id     UUID                 NOT NULL REFERENCES alerts(id),
    runbook_id   UUID                 REFERENCES runbooks(id),
    status       investigation_status NOT NULL DEFAULT 'pending',
    -- Investigation result
    root_cause   TEXT,
    resolution   TEXT,
    summary      TEXT,
    -- Steps are stored as a JSONB array for simplicity at this stage
    steps        JSONB                NOT NULL DEFAULT '[]',
    -- LLM metadata
    llm_provider TEXT                 NOT NULL DEFAULT '',
    llm_model    TEXT                 NOT NULL DEFAULT '',
    token_usage  INTEGER              NOT NULL DEFAULT 0,
    -- Timing
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ          NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ          NOT NULL DEFAULT NOW()
);

CREATE INDEX investigations_alert_id_idx  ON investigations (alert_id);
CREATE INDEX investigations_status_idx    ON investigations (status);
CREATE INDEX investigations_created_at_idx ON investigations (created_at DESC);

CREATE TRIGGER investigations_updated_at
    BEFORE UPDATE ON investigations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
