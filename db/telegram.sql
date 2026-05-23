-- ═══════════════════════════════════════════════════════════════════════════
-- Telegram Bot Schema
-- Requires: update_updated_at() function from base schema
-- ═══════════════════════════════════════════════════════════════════════════

-- ── Telegram account linkage ──────────────────────────────────────────────
-- One user can link exactly one Telegram account.
-- Verification flow: web generates code → user sends to bot → bot confirms.
CREATE TABLE telegram_users (
    id                  SERIAL PRIMARY KEY,
    user_id             INT    UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    telegram_chat_id    BIGINT UNIQUE,                    -- NULL until verified
    telegram_username   VARCHAR(255),
    verification_code   VARCHAR(32),                      -- 6-digit code, cleared after use
    is_verified         BOOLEAN NOT NULL DEFAULT FALSE,
    last_reminded_at    TIMESTAMP NULL,                   -- Throttle for weekly credit reminders
    created_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at          TIMESTAMP NULL
);

-- ── Per-user credit ledger ────────────────────────────────────────────────
-- Free: 3 messages/day (resets at midnight UTC).
-- Premium: deducted from purchased_credits when free credits exhausted.
CREATE TABLE telegram_credits (
    id                    SERIAL PRIMARY KEY,
    user_id               INT UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    free_credits_used     INT  NOT NULL DEFAULT 0,
    free_reset_date       DATE NOT NULL DEFAULT CURRENT_DATE, -- last reset date
    purchased_credits     INT  NOT NULL DEFAULT 0,            -- bought via Solana SPL
    total_purchased_ever  INT  NOT NULL DEFAULT 0,            -- audit: lifetime purchased
    created_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── Per-monitor health state (Two-Packet state machine) ───────────────────
-- Tracks consecutive failures to implement the Two-Packet rule:
-- Only alert on 2nd consecutive failure; only send RESTORED if DOWN alert was sent.
CREATE TABLE monitor_health_state (
    monitor_id           UUID PRIMARY KEY REFERENCES monitors(id) ON DELETE CASCADE,
    is_down              BOOLEAN NOT NULL DEFAULT FALSE,
    consecutive_failures INT     NOT NULL DEFAULT 0,
    alert_sent           BOOLEAN NOT NULL DEFAULT FALSE,  -- TRUE = DOWN alert was dispatched
    last_alerted_at      TIMESTAMP NULL,
    updated_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── Per-monitor notification subscriptions ───────────────────────────────
-- Dashboard toggle: user enables/disables alerts for a specific monitor.
CREATE TABLE telegram_subscriptions (
    id                       SERIAL PRIMARY KEY,
    user_id                  INT  NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    monitor_id               UUID NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
    is_notifications_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at               TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, monitor_id)
);

-- ── Triggers ──────────────────────────────────────────────────────────────
CREATE TRIGGER trg_telegram_users_updated
    BEFORE UPDATE ON telegram_users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_telegram_credits_updated
    BEFORE UPDATE ON telegram_credits
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_telegram_subs_updated
    BEFORE UPDATE ON telegram_subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

CREATE TRIGGER trg_health_state_updated
    BEFORE UPDATE ON monitor_health_state
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- ── Indexes ───────────────────────────────────────────────────────────────
CREATE INDEX idx_telegram_users_chat_id ON telegram_users (telegram_chat_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_telegram_users_user_id ON telegram_users (user_id)          WHERE deleted_at IS NULL;
CREATE INDEX idx_telegram_subs_monitor  ON telegram_subscriptions (monitor_id);
CREATE INDEX idx_telegram_subs_user     ON telegram_subscriptions (user_id);
CREATE INDEX idx_health_state_down      ON monitor_health_state (is_down) WHERE is_down = TRUE;

-- ═══════════════════════════════════════════════════════════════════════════
-- Atomic credit deduction (thread-safe, single round-trip)
-- Returns: (success BOOL, credit_type TEXT)
--   credit_type: 'free' | 'purchased' | 'no_credits'
-- ═══════════════════════════════════════════════════════════════════════════
CREATE OR REPLACE FUNCTION deduct_telegram_credit(
    p_user_id INT
) RETURNS TABLE(success BOOLEAN, credit_type TEXT) AS $$
DECLARE
    v_free_used  INT;
    v_reset_date DATE;
    v_purchased  INT;
    v_today      DATE := CURRENT_DATE;
BEGIN
    -- Ensure row exists (idempotent upsert)
    INSERT INTO telegram_credits (user_id)
    VALUES (p_user_id)
    ON CONFLICT (user_id) DO NOTHING;

    -- Lock the row for this transaction
    SELECT free_credits_used, free_reset_date, purchased_credits
    INTO v_free_used, v_reset_date, v_purchased
    FROM telegram_credits
    WHERE user_id = p_user_id
    FOR UPDATE;

    -- Daily reset: if stored reset_date is before today, wipe free usage
    IF v_reset_date < v_today THEN
        UPDATE telegram_credits
        SET free_credits_used = 0,
            free_reset_date   = v_today
        WHERE user_id = p_user_id;
        v_free_used := 0;
    END IF;

    -- Tier 1: Free (3/day)
    IF v_free_used < 3 THEN
        UPDATE telegram_credits
        SET free_credits_used = free_credits_used + 1
        WHERE user_id = p_user_id;
        RETURN QUERY SELECT TRUE, 'free'::TEXT;
        RETURN;
    END IF;

    -- Tier 2: Purchased credits
    IF v_purchased > 0 THEN
        UPDATE telegram_credits
        SET purchased_credits = purchased_credits - 1
        WHERE user_id = p_user_id;
        RETURN QUERY SELECT TRUE, 'purchased'::TEXT;
        RETURN;
    END IF;

    -- No credits available
    RETURN QUERY SELECT FALSE, 'no_credits'::TEXT;
END;
$$ LANGUAGE plpgsql;

-- ═══════════════════════════════════════════════════════════════════════════
-- Add purchased credits (called after Solana SPL payment confirmed)
-- ═══════════════════════════════════════════════════════════════════════════
CREATE OR REPLACE FUNCTION add_purchased_credits(
    p_user_id INT,
    p_amount  INT
) RETURNS INT AS $$
DECLARE
    v_new_balance INT;
BEGIN
    INSERT INTO telegram_credits (user_id, purchased_credits, total_purchased_ever)
    VALUES (p_user_id, p_amount, p_amount)
    ON CONFLICT (user_id) DO UPDATE
        SET purchased_credits    = telegram_credits.purchased_credits + p_amount,
            total_purchased_ever = telegram_credits.total_purchased_ever + p_amount
    RETURNING purchased_credits INTO v_new_balance;

    RETURN v_new_balance;
END;
$$ LANGUAGE plpgsql;
