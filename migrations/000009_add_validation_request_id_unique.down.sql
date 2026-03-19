-- ============================================
-- Migration: 000009_add_validation_request_id_unique (ROLLBACK)
-- Description: Remove UNIQUE constraint on transaction_validations(request_id)
-- Date: 2026-03-18
-- ============================================

-- Remove the UNIQUE index on request_id
DROP INDEX IF EXISTS idx_transaction_validations_request_id_unique;
