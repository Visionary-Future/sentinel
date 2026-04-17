CREATE TABLE runbooks (
    id          UUID        PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    content     TEXT        NOT NULL,  -- Raw Markdown
    -- Parsed trigger conditions (stored as JSONB for fast matching)
    triggers    JSONB       NOT NULL DEFAULT '[]',
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX runbooks_enabled_idx ON runbooks (enabled);

CREATE TRIGGER runbooks_updated_at
    BEFORE UPDATE ON runbooks
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
