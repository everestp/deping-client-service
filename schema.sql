-- 1. Drop the existing table
DROP TABLE IF EXISTS processed_transactions;

-- 2. Create the clean table
CREATE TABLE processed_transactions (
    signature TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    amount BIGINT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    timestamp_ms BIGINT
);