# 🤖 UptimeMonitor — Telegram Notification System

A production-grade Telegram bot integration for real-time uptime alerting, built on Go + PostgreSQL + RabbitMQ. Features a Two-Packet state machine for false-alarm prevention, a credit-gated notification system with Solana SPL token payments, and a full interactive bot experience.

---

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Database Schema](#database-schema)
- [Environment Variables](#environment-variables)
- [Quick Start](#quick-start)
- [Linking Flow](#linking-flow)
- [Notification Pipeline](#notification-pipeline)
- [Credit System](#credit-system)
- [Bot Commands Reference](#bot-commands-reference)
- [HTTP API Reference](#http-api-reference)
- [File Structure](#file-structure)
- [Dependency Graph](#dependency-graph)
- [Extending the System](#extending-the-system)

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                         INBOUND DATA FLOW                           │
│                                                                     │
│  Rust Miner                                                         │
│  (ping runner) ──► RabbitMQ ──► ConsumerService ──► AlertService   │
│                   processing_queue                      │           │
│                                                         │           │
│                              Two-Packet State Machine   │           │
│                         ┌───────────────────────────┐  │           │
│                         │  monitor_health_state      │◄─┘           │
│                         │  consecutive_failures      │              │
│                         │  is_down | alert_sent      │              │
│                         └──────────┬────────────────┘              │
│                                    │ fire alert?                    │
│                                    ▼                                │
│                         Credit Gate (deduct_telegram_credit)        │
│                                    │ ok?                            │
│                                    ▼                                │
│                         Bot.SendMessage() ──► Telegram API          │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                         WEB DASHBOARD FLOW                          │
│                                                                     │
│  Next.js Frontend                                                   │
│  TelegramPage.tsx ──► POST /api/telegram/link                       │
│                       (generate verification code)                  │
│                              │                                      │
│                              ▼                                      │
│                       User opens Telegram                           │
│                       sends: /verify <code>                         │
│                              │                                      │
│                              ▼                                      │
│                       Bot.handleVerify() ──► VerifyTelegramUser()   │
│                       (chat_id stored, is_verified = true)          │
└─────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility |
|---|---|
| `ConsumerService` | Reads from RabbitMQ, parses `SubmitResultsRequest`, delegates per-result |
| `AlertService` | Owns the Two-Packet state machine; credit gating; dispatches alert messages |
| `TelegramService` | Account linking lifecycle; credit CRUD; subscription toggles |
| `TelegramRepository` | All database I/O for telegram-related tables; bot query helpers |
| `Bot` (telebot.v3) | Telegram bot command handlers; implements `MessageSender` interface |
| `TelegramController` | HTTP handlers for the web dashboard → backend integration |

---

## Database Schema

Four new tables are added by `db/telegram.sql`. Run this **after** the base schema.

### `telegram_users`

Links a platform user to their Telegram account.

| Column | Type | Notes |
|---|---|---|
| `user_id` | `INT` | FK → `users.id`, unique |
| `telegram_chat_id` | `BIGINT` | NULL until verified |
| `telegram_username` | `VARCHAR` | For display only |
| `verification_code` | `VARCHAR(32)` | Cleared after use |
| `is_verified` | `BOOLEAN` | True after bot `/verify` |
| `last_reminded_at` | `TIMESTAMP` | Throttle for weekly credit reminders |

### `telegram_credits`

Per-user notification credit ledger.

| Column | Type | Notes |
|---|---|---|
| `user_id` | `INT` | FK → `users.id`, unique |
| `free_credits_used` | `INT` | Resets daily via `free_reset_date` |
| `free_reset_date` | `DATE` | Compared to `CURRENT_DATE` on every deduction |
| `purchased_credits` | `INT` | Deducted after free tier exhausted |
| `total_purchased_ever` | `INT` | Audit field |

**Daily reset is lazy** — no cron job needed. `deduct_telegram_credit()` checks `free_reset_date < CURRENT_DATE` and resets inline under a `FOR UPDATE` lock.

### `monitor_health_state`

The Two-Packet state machine store. One row per monitored URL.

| Column | Type | Notes |
|---|---|---|
| `monitor_id` | `UUID` | PK, FK → `monitors.id` |
| `is_down` | `BOOLEAN` | Current state |
| `consecutive_failures` | `INT` | Incremented on failure, reset on success |
| `alert_sent` | `BOOLEAN` | True if a DOWN alert was dispatched |
| `last_alerted_at` | `TIMESTAMP` | Last DOWN alert timestamp |

### `telegram_subscriptions`

Per-user per-monitor notification toggle (dashboard control).

| Column | Type | Notes |
|---|---|---|
| `user_id` | `INT` | FK → `users.id` |
| `monitor_id` | `UUID` | FK → `monitors.id` |
| `is_notifications_enabled` | `BOOLEAN` | Default TRUE |

> If no row exists for a (user, monitor) pair, alerts default to **enabled**.

### Atomic SQL Functions

```sql
-- Thread-safe credit deduction (FOR UPDATE lock)
SELECT success, credit_type FROM deduct_telegram_credit($user_id);
-- credit_type: 'free' | 'purchased' | 'no_credits'

-- Add purchased credits (called after Solana payment)
SELECT add_purchased_credits($user_id, $amount);
```

---

## Environment Variables

```bash
# Required
DATABASE_URL=postgres://user:pass@localhost:5432/uptime?sslmode=disable
JWT_SECRET=your-very-long-random-secret-here
RABBITMQ_URL=amqp://guest:guest@localhost:5672/
TELEGRAM_BOT_TOKEN=123456789:AABBcc...     # From @BotFather

# Optional (with defaults)
HTTP_PORT=8080
RABBITMQ_QUEUE=processing_queue
TELEGRAM_BOT_USERNAME=YourMonitorBot       # Without @
```

---

## Quick Start

### 1. Create your Telegram bot

```
1. Open Telegram → search @BotFather
2. Send /newbot
3. Choose name: "UptimeMonitor"
4. Choose username: "YourMonitorBot"
5. Copy the token → TELEGRAM_BOT_TOKEN
```

### 2. Apply the database schema

```bash
# Base schema first (if not already applied)
psql $DATABASE_URL -f db/schema.sql

# Telegram tables
psql $DATABASE_URL -f db/telegram.sql
```

### 3. Set environment variables

```bash
cp .env.example .env
# Fill in DATABASE_URL, JWT_SECRET, RABBITMQ_URL, TELEGRAM_BOT_TOKEN
```

### 4. Run

```bash
go run main.go
```

The application starts three concurrent services:
- HTTP server on `$HTTP_PORT`
- Telegram bot long-polling
- RabbitMQ consumer

---

## Linking Flow

This is the account verification handshake between the web dashboard and the Telegram bot.

```
Web Browser                Backend               Telegram Bot
     │                        │                        │
     │  POST /api/telegram/   │                        │
     │  link                  │                        │
     │  { username: "@alice" }│                        │
     │──────────────────────►│                        │
     │                        │ Generate 8-char code   │
     │                        │ Store in telegram_users│
     │◄──────────────────────│                        │
     │  { code: "a3f9b2c1",  │                        │
     │    bot: "@YourBot" }   │                        │
     │                        │                        │
     │  User opens Telegram   │                        │
     │  sends: /verify a3f9b2c1                        │
     │──────────────────────────────────────────────►│
     │                        │  VerifyLink(code,      │
     │                        │  chatID, username)     │
     │                        │◄──────────────────────│
     │                        │  UPDATE telegram_users │
     │                        │  SET is_verified=TRUE  │
     │                        │  SET chat_id=<id>      │
     │                        │──────────────────────►│
     │                        │                  Send welcome msg
     │                        │                        │
```

**Security properties:**
- Code is cryptographically random (4 random bytes → 8 hex chars)
- Code is single-use: cleared from the database after successful verification
- Code is invalidated on re-link: calling `InitiateLink` again overwrites the old code

---

## Notification Pipeline

### Two-Packet Rule

A site is only declared DOWN after **two consecutive failed checks**. This eliminates transient network hiccups from producing false alarms.

```
Check 1 fails  →  consecutive_failures = 1  →  NO ALERT (still healthy)
Check 2 fails  →  consecutive_failures = 2  →  🔴 DOWN ALERT FIRES
Check 3 fails  →  consecutive_failures = 3  →  (already down, no repeat)
Check 4 succeeds → consecutive_failures = 0  →  ✅ RESTORED ALERT FIRES
                   (only because alert_sent = true)
```

**RESTORED is only sent if we previously sent a DOWN alert.** This prevents spurious "all clear" messages for monitors that were never alerting.

### Notification Toggle

Before every alert, `IsNotificationEnabled(monitorID)` is checked. Users can toggle this per-monitor from the dashboard or with the inline keyboard button in the `/monitor` bot command.

### Credit Gate

```
AlertService.dispatchAlert()
  │
  ├─ IsNotificationEnabled? → no → return (silent)
  │
  ├─ GetMonitorOwnerChatID() → not linked? → return (silent)
  │
  ├─ DeductCredit()
  │    ├─ free_credits_used < 3 today? → deduct free → ✅ proceed
  │    ├─ purchased_credits > 0?       → deduct paid  → ✅ proceed
  │    └─ no credits                   → weekly reminder check
  │                                          └─ last_reminded_at > 7 days? → send reminder
  │
  └─ FormatAlertMessage() → SendMessage() → Telegram API
```

### Alert Message Format

**DOWN alert:**
```
🔴 SERVICE DOWN

🌐 URL: example.com
🌍 Region: us-east
📋 Status: 503
🔖 Error: CONNECTION_REFUSED
🕐 Detected: 2025-06-01 14:23:01 UTC

Your monitor has failed 2 consecutive checks. We'll notify you when it recovers.
```

**RESTORED alert:**
```
✅ SERVICE RESTORED

🌐 URL: example.com
⚡ Latency: 142ms
🌍 Region: us-east
🕐 Recovered: 2025-06-01 14:28:55 UTC

Your monitor is back online and responding normally.
```

---

## Credit System

### Tiers

| Tier | Amount | Source |
|---|---|---|
| **Free** | 3 messages/day | Auto-reset at midnight UTC |
| **Purchased** | Unlimited | Buy with Solana SPL tokens |

### Daily Reset Logic

The reset is **lazy** (no scheduled job). `deduct_telegram_credit()` checks:

```sql
IF v_reset_date < CURRENT_DATE THEN
    SET free_credits_used = 0, free_reset_date = CURRENT_DATE
END IF
```

This runs inside a `FOR UPDATE` transaction, so concurrent requests are safe.

### Purchasing Credits

When a Solana SPL transaction is confirmed on the frontend:

```
Frontend detects tx confirmed
    ↓
POST /api/telegram/credits/add
{ amount: 100, tx_signature: "5Kj..." }
    ↓
add_purchased_credits(user_id, 100)
Audit logged to solana_sync_events
    ↓
New balance returned to frontend
```

### Credit Reminder

When a user has `0` credits and an alert would fire, instead of silently dropping it, we check `last_reminded_at`. If it's `NULL` or older than 7 days, we send one reminder message to the user. This prevents weekly spam while still nudging them to top up.

---

## Bot Commands Reference

### Public (no account required)

| Command | Description |
|---|---|
| `/start` | Welcome message + step-by-step linking guide |
| `/help` | Full command reference with feature explanations |
| `/verify <code>` | Link Telegram account using code from dashboard |

### Authenticated (requires linked account)

| Command | Description |
|---|---|
| `/monitor` | List all monitors with 🟢/🔴/⏸ status, uptime %, avg latency. Each entry has a button to see ping details |
| `/status` | Quick overview: total / up / down / paused count |
| `/credits` | Current free and purchased credit balance with reset time |

### Inline Keyboard Actions

| Button | Trigger | Description |
|---|---|---|
| `Show monitor details` | Tap monitor in `/monitor` | Shows last 5 pings with phase breakdown (DNS/TCP/TLS/TTFB/Total) |
| `Toggle Alerts` | Tap inside monitor detail view | Enables or disables notifications for that specific monitor |

---

## HTTP API Reference

All authenticated endpoints require `Authorization: Bearer <jwt>`.

### Account Linking

```
POST /api/telegram/link
Authorization: Bearer <jwt>
Content-Type: application/json

{ "telegram_username": "@alice" }

→ 200 {
    "verification_code": "a3f9b2c1",
    "bot_username": "@YourMonitorBot",
    "message": "Open Telegram and send /verify a3f9b2c1 to @YourMonitorBot"
  }
```

### Credits

```
GET /api/telegram/credits
Authorization: Bearer <jwt>

→ 200 {
    "free_credits_used": 1,
    "free_credits_left": 2,
    "free_reset_date": "2025-06-02T00:00:00Z",
    "purchased_credits": 87,
    "total_credits_left": 89
  }

POST /api/telegram/credits/add
Authorization: Bearer <jwt>
Content-Type: application/json

{ "amount": 100, "tx_signature": "5KjHG..." }

→ 200 { "message": "Credits added successfully", "purchased_credits": 187 }
```

### Notification Toggle (per-monitor)

```
PUT /api/monitors/{monitor_id}/notifications
Authorization: Bearer <jwt>
Content-Type: application/json

{ "is_notifications_enabled": false }

→ 200 { "monitor_id": "...", "is_notifications_enabled": false }

GET /api/monitors/{monitor_id}/notifications
Authorization: Bearer <jwt>

→ 200 { "monitor_id": "...", "is_notifications_enabled": true }
```

### Auth

```
POST /api/auth/register
{ "email": "...", "password": "...", "wallet_pubkey": "..." }
→ 201 { "token": "...", "user": { "id": 1, "email": "...", "wallet_pubkey": "..." } }

POST /api/auth/login
{ "email": "...", "password": "..." }
→ 200 { "token": "...", "user": { ... } }

GET /api/auth/me
Authorization: Bearer <jwt>
→ 200 { "id": 1, "email": "...", "wallet_pubkey": "..." }
```

---

## File Structure

```
.
├── main.go                              # Entrypoint
├── go.mod
├── app/
│   └── application.go                  # DI root — wires all services
├── config/
│   └── env/
│       └── env.go                      # Environment variable loading
├── controllers/
│   ├── user.go                         # Register, Login, Me
│   └── telegram.go                     # Link, Credits, Toggle notifications
├── db/
│   ├── schema.sql                      # Base schema (existing)
│   ├── telegram.sql                    # ← NEW: telegram tables + SQL functions
│   └── repositories/
│       ├── storage.go                  # Root container for all repos
│       ├── users.go                    # UserRepository interface + impl
│       └── telegram_repo.go            # ← NEW: TelegramRepository interface + impl
├── dto/
│   ├── dto.go                          # Existing DTOs
│   └── telegram.go                     # ← NEW: Telegram-specific DTOs
├── services/
│   ├── user_service.go                 # Auth, JWT generation/validation
│   ├── alert_service.go                # ← NEW: Two-Packet machine + notifications
│   ├── consumer_service.go             # ← NEW: RabbitMQ consumer
│   └── telegram_service.go             # ← NEW: Linking, credits, subscriptions
├── bot/
│   └── bot.go                          # ← NEW: telebot.v3 bot + all commands
└── router/
    ├── router.go                       # All HTTP routes
    └── middleware.go                   # JWT middleware
```

---

## Dependency Graph

```
main.go
  └── app.New()
        ├── env.Load()                      config
        ├── sql.Open()                      database
        ├── amqp.Dial()                     rabbitmq
        ├── repositories.NewStorage(db)
        │     ├── NewUserRepository(db)
        │     └── NewTelegramRepository(db)
        │
        ├── services.NewUserService(storage.Users, jwtSecret)
        ├── services.NewTelegramService(storage.Telegram, botUsername)
        │
        ├── bot.NewBot(token, telegramSvc, nil, storage.Telegram)
        │     └── implements MessageSender interface
        │
        ├── services.NewAlertService(storage.Telegram, bot, log)
        │     └── bot injected as MessageSender (no import of bot package)
        │
        ├── bot.SetAlertService(alertSvc)   circular dep resolution
        │
        ├── services.NewConsumerService(amqpConn, alertSvc, log)
        │
        ├── controllers.NewUserController(userSvc)
        ├── controllers.NewTelegramController(telegramSvc)
        └── router.SetupRouter(...)
```

**Interfaces used for loose coupling:**

| Interface | Implemented by | Used by |
|---|---|---|
| `MessageSender` | `bot.Bot` | `AlertService` |
| `AlertService` | `alertService` (services pkg) | `ConsumerService`, `Bot` |
| `TelegramService` | `telegramService` | `TelegramController`, `Bot` |
| `TelegramRepository` | `postgressTelegramRepo` | All services |
| `UserService` | `userService` | `UserController`, `JWTMiddleware` |

---

## Extending the System

### Add a new bot command

```go
// In bot/bot.go registerHandlers()
b.tele.Handle("/mycommand", b.withAuth(b.handleMyCommand))

func (b *Bot) handleMyCommand(c tele.Context) error {
    ctx := context.Background()
    tu, _ := b.telegramRepo.GetTelegramUserByChatID(ctx, c.Sender().ID)
    // ... your logic
    return c.Send("Response", tele.ModeMarkdownV2)
}
```

### Add a new alert type (e.g. high latency warning)

```go
// In services/alert_service.go ProcessPingResult()
if result.Success && result.LatencyMs > HighLatencyThresholdMs {
    alertEvent = &dto.AlertEvent{
        MonitorID:  result.MonitorID,
        TargetURL:  result.TargetURL,
        IsDown:     false,
        LatencyMs:  result.LatencyMs,
        // add a new field: IsSlowWarning: true
    }
    shouldAlert = true
}
```

### Add credit top-up webhook (Solana)

After confirming a Solana transaction on-chain, call:

```go
newBalance, err := telegramService.AddPurchasedCredits(ctx, userID, amount, txSignature)
```

The `tx_signature` is stored in `solana_sync_events` for audit.

---

## Notes

- **telebot.v3** is used for the bot (`gopkg.in/telebot.v3`). It's idiomatic, actively maintained, and supports MarkdownV2, inline keyboards, and middleware.
- **MarkdownV2** is used for all bot messages. Special characters (`-`, `.`, `!`, etc.) must be escaped with `\`. The `escapeMarkdownV2()` helper in `bot/bot.go` handles this.
- **Manual ACK** is used in the RabbitMQ consumer (`autoAck: false`). Messages are only acknowledged after successful processing. On transient DB errors they are re-queued once.
- **Go 1.22+** is required for `http.ServeMux` path parameters (`{monitor_id}`).
