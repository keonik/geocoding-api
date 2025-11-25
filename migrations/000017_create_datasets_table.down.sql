-- Rollback Migration 17: Drop datasets table
DROP INDEX IF EXISTS idx_datasets_uploaded_at;
DROP INDEX IF EXISTS idx_datasets_uploaded_by;
DROP INDEX IF EXISTS idx_datasets_status;
DROP INDEX IF EXISTS idx_datasets_county;
DROP INDEX IF EXISTS idx_datasets_state;
DROP TABLE IF EXISTS datasets;
