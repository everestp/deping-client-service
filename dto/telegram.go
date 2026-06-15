package dto

import "time"

// ── Telegram Account Linking ───────────────────────────────────────────────

// LinkTelegramRequest is sent from the web frontend when a user wants to
// connect their Telegram account. The backend generates a verification_code
// and stores it; the user then sends /verify <code> to the bot.
type LinkTelegramRequest struct {
	TelegramUsername string `json:"telegram_username" validate:"required"`
}

type LinkTelegramResponse struct {
	VerificationCode string `json:"verification_code"`
	BotUsername      string `json:"bot_username"` // e.g. "@YourMonitorBot" for deep-link
	Message          string `json:"message"`
}

// ── Credit Management ─────────────────────────────────────────────────────

type TelegramCreditStatus struct {
	FreeCreditsUsed  int       `json:"free_credits_used"`
	FreeCreditsLeft  int       `json:"free_credits_left"`   // 3 - free_credits_used (if same day)
	FreeResetDate    time.Time `json:"free_reset_date"`
	PurchasedCredits int       `json:"purchased_credits"`
	TotalCreditsLeft int       `json:"total_credits_left"`  // free_left + purchased
}

type AddCreditsRequest struct {
	Amount       int    `json:"expected_amount"`        // Credits to add
	TxSignature  string `json:"signature"`  // Solana tx for audit
}

// ── Monitor Notification Toggle ───────────────────────────────────────────

type ToggleNotificationRequest struct {
	IsNotificationsEnabled bool `json:"is_notifications_enabled"`
}

type SubscriptionResponse struct {
	MonitorID              string `json:"monitor_id"`
	IsNotificationsEnabled bool   `json:"is_notifications_enabled"`
}

// ── Internal Alert Event ──────────────────────────────────────────────────
// Used internally between the consumer and alert service — never serialised to HTTP.

type AlertEvent struct {
	MonitorID   string
	TargetURL   string
	OwnerUserID int
	IsDown      bool    // true = DOWN alert, false = RESTORED alert
	LatencyMs   int
	StatusCode  int
	ErrorKind   string
	GeoRegion   string
	Timestamp   time.Time
}

// ── Ping Summary (used in /monitor bot command) ───────────────────────────

type MonitorSummary struct {
	ID          string  `json:"id"`
	TargetURL   string  `json:"target_url"`
	IsActive    bool    `json:"is_active"`
	IsDown      bool    `json:"is_down"`
	UptimePct   float64 `json:"uptime_pct_24h"`
	AvgLatency  float64 `json:"avg_latency_ms"`
}

// RecentPingRow is a condensed view of ping_logs for bot display.
type RecentPingRow struct {
	Timestamp  time.Time `db:"timestamp"`
	Success    bool      `db:"success"`
	LatencyMs  int       `db:"latency_ms"`
	StatusCode int       `db:"status_code"`
	GeoRegion  string    `db:"geo_region"`
	ErrorKind  string    `db:"error_kind"`
	DnsUs      int64     `db:"dns_us"`
	TcpUs      int64     `db:"tcp_us"`
	TlsUs      int64     `db:"tls_us"`
	TtfbUs     int64     `db:"ttfb_us"`
	TotalUs    int64     `db:"total_us"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}



type PingResultItem struct {
	JobID       string `json:"job_id"`       // Composite identifier: monitor_id:runner_pubkey:timestamp
	BatchID     string `json:"batch_id"`     // Unique ID grouping jobs in a miner evaluation batch
	NodeID      string `json:"node_id"`      // The public identification string of the runner
	TargetURL   string `json:"target_url"`   // The URL target requested by the monitor
	Success     bool   `json:"success"`      // Flag showing whether response yielded a 2xx HTTP code
	StatusCode  int    `json:"status_code"`  // Raw HTTP response status; 0 if network request failed completely

	// ── Phase latencies in microseconds (us) ───────────────────────────────────
	DnsUs       int64  `json:"dns_us"`       // DNS resolution duration
	TcpUs       int64  `json:"tcp_us"`       // TCP connection handshaking duration
	TlsUs       int64  `json:"tls_us"`       // TLS handshake duration (0 for plain HTTP targets)
	TtfbUs      int64  `json:"ttfb_us"`      // Time to First Byte (TTFB) duration
	TotalUs     int64  `json:"total_us"`     // Total raw network transaction duration
	LatencyMs   int    `json:"latency_ms"`   // Computed field for backwards compatibility with legacy UI APIs

	// ── Error envelope (empty strings on success) ──────────────────────────────
	ErrorKind   string `json:"error_kind"`   // Stable uppercase error tag (e.g., TIMEOUT, DNS_FAILURE)
	ErrorMsg    string `json:"error_msg"`    // Human-readable message detailing why the check broke

	// ── Metadata ──────────────────────────────────────────────────────────────
	MonitorID   string `json:"monitor_id"`   // Legacy matching attribute (populated via split fallback)
	GeoRegion   string `json:"geo_region"`   // Geographic regional cluster mapping context
	TimestampMs int64  `json:"timestamp_ms"` // Unix epoch milliseconds when the probe was dispatched
	Latitude  float64 `json:"latitude"`
    Longitude float64 `json:"longitude"`
}


// ── Auth ───────────────────────────────────────────────────────────────────

type RegisterRequest struct {
	Email        string `json:"email"`
	Password     string `json:"password"`
	WalletPubkey string `json:"wallet_pubkey"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token string   `json:"token"`
	User  UserInfo `json:"user"`
}

type UserInfo struct {
	ID           int    `json:"id"`
	Email        string `json:"email"`
	WalletPubkey string `json:"wallet_pubkey"`
}


type SubmitResultsRequest struct {
	RunnerPubkey string           `json:"runner_pubkey"`
	Signature    string           `json:"signature"` // Ed25519 signature tracking payload data validation
	Results      []PingResultItem `json:"results"`
}
