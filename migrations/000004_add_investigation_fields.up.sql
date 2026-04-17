-- Add new investigation status value
ALTER TYPE investigation_status ADD VALUE IF NOT EXISTS 'reused';

-- Add new columns to investigations
ALTER TABLE investigations ADD COLUMN IF NOT EXISTS confidence   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE investigations ADD COLUMN IF NOT EXISTS feedback     TEXT NOT NULL DEFAULT '';
ALTER TABLE investigations ADD COLUMN IF NOT EXISTS human_cause  TEXT NOT NULL DEFAULT '';
ALTER TABLE investigations ADD COLUMN IF NOT EXISTS reused_from  UUID REFERENCES investigations(id);

-- Index for result caching lookups (alert fingerprint → recent completed investigation)
CREATE INDEX IF NOT EXISTS investigations_alert_fingerprint_idx
    ON investigations (alert_id, status, completed_at DESC);
