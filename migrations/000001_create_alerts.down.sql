DROP TRIGGER IF EXISTS alerts_updated_at ON alerts;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS alerts;
DROP TYPE IF EXISTS alert_status;
DROP TYPE IF EXISTS alert_severity;
DROP TYPE IF EXISTS alert_source;
