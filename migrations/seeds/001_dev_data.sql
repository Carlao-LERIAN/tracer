-- ============================================
-- Development Seed Data
-- WARNING: For development/testing only - NOT for production
-- Note: MVP uses a single migration (000001_initial_schema)
-- ============================================

BEGIN;

-- ============================================
-- Sample Rules
-- ============================================

-- Rule 1: High-value transaction blocking
INSERT INTO rules (
    id, name, description, expression, action,
    scopes, status
) VALUES (
    '10000000-0000-0000-0000-000000000001',
    'block-high-value',
    'Block transactions above $10,000 (1000000 cents)',
    'amount > 1000000',
    'DENY',
    '[]'::jsonb,
    'ACTIVE'
);

-- Rule 2: Allow small transactions
INSERT INTO rules (
    id, name, description, expression, action,
    scopes, status
) VALUES (
    '10000000-0000-0000-0000-000000000002',
    'allow-small-transactions',
    'Auto-approve transactions below $1,000 (100000 cents)',
    'amount < 100000',
    'ALLOW',
    '[]'::jsonb,
    'ACTIVE'
);

-- Rule 3: Weekend review (DRAFT for testing)
INSERT INTO rules (
    id, name, description, expression, action,
    scopes, status
) VALUES (
    '10000000-0000-0000-0000-000000000003',
    'weekend-review',
    'Flag weekend transactions for review',
    'timestamp.getDayOfWeek() == 0 || timestamp.getDayOfWeek() == 6',
    'REVIEW',
    '[]'::jsonb,
    'DRAFT'
);

-- ============================================
-- Sample Limits (max_amount in decimal)
-- ============================================

-- Limit 1: Daily spending limit ($50,000.00)
INSERT INTO limits (
    id, name, description, limit_type, max_amount, currency,
    scopes, status, reset_at
) VALUES (
    '20000000-0000-0000-0000-000000000001',
    'daily-account-limit',
    'Daily spending limit per account',
    'DAILY',
    50000,
    'USD',
    '[{"transactionType": "CARD"}]'::jsonb,
    'ACTIVE',
    (CURRENT_DATE + INTERVAL '1 day')::TIMESTAMP WITH TIME ZONE
);

-- Limit 2: Monthly portfolio limit ($1,000,000.00)
INSERT INTO limits (
    id, name, description, limit_type, max_amount, currency,
    scopes, status, reset_at
) VALUES (
    '20000000-0000-0000-0000-000000000002',
    'monthly-portfolio-limit',
    'Monthly spending limit per portfolio',
    'MONTHLY',
    1000000,
    'USD',
    '[{"portfolioId": "80000000-0000-0000-0000-000000000001"}]'::jsonb,
    'ACTIVE',
    (DATE_TRUNC('month', CURRENT_DATE) + INTERVAL '1 month')::TIMESTAMP WITH TIME ZONE
);

-- ============================================
-- Sample Usage Counters (current_usage in decimal)
-- ============================================

-- Counter for daily limit ($15,000.00 used)
INSERT INTO usage_counters (
    id, limit_id, scope_key, period_key, current_usage
) VALUES (
    '30000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    'transactionType:CARD',
    TO_CHAR(CURRENT_DATE, 'YYYY-MM-DD'),
    15000
);

-- Counter for monthly limit ($250,000.00 used)
INSERT INTO usage_counters (
    id, limit_id, scope_key, period_key, current_usage
) VALUES (
    '30000000-0000-0000-0000-000000000002',
    '20000000-0000-0000-0000-000000000002',
    'portfolioId:80000000-0000-0000-0000-000000000001',
    TO_CHAR(CURRENT_DATE, 'YYYY-MM'),
    250000
);

-- ============================================
-- Sample Transaction Validation
-- ============================================

INSERT INTO transaction_validations (
    id,
    request_id,
    transaction_type,
    sub_type,
    amount,
    currency,
    transaction_timestamp,
    account,
    segment,
    portfolio,
    merchant,
    metadata,
    decision,
    reason,
    matched_rule_ids,
    evaluated_rule_ids,
    limit_usage_details,
    processing_time_ms
) VALUES (
    '40000000-0000-0000-0000-000000000001',
    'a0000000-0000-0000-0000-000000000001',
    'CARD',
    NULL,
    1500000,
    'USD',
    NOW(),
    '{"id": "20000000-0000-0000-0000-000000000001"}'::jsonb,
    NULL,
    NULL,
    NULL,
    '{}'::jsonb,
    'DENY',
    'Amount exceeds $10,000 threshold',
    ARRAY['10000000-0000-0000-0000-000000000001'::UUID],
    ARRAY['10000000-0000-0000-0000-000000000001'::UUID, '10000000-0000-0000-0000-000000000002'::UUID],
    '[]'::jsonb,
    23
);

COMMIT;

-- ============================================
-- Verification Queries
-- ============================================

SELECT 'Rules' AS entity, COUNT(*) AS count FROM rules
UNION ALL
SELECT 'Limits', COUNT(*) FROM limits
UNION ALL
SELECT 'UsageCounters', COUNT(*) FROM usage_counters
UNION ALL
SELECT 'TransactionValidations', COUNT(*) FROM transaction_validations;
