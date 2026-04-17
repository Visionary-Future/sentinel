ALTER TABLE investigations DROP COLUMN IF EXISTS reused_from;
ALTER TABLE investigations DROP COLUMN IF EXISTS human_cause;
ALTER TABLE investigations DROP COLUMN IF EXISTS feedback;
ALTER TABLE investigations DROP COLUMN IF EXISTS confidence;

DROP INDEX IF EXISTS investigations_alert_fingerprint_idx;
