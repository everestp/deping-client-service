package dto

type CreateMonitorRequest struct {
    TargetURL       string `json:"target_url"`
    IntervalSeconds int    `json:"interval_seconds"`
}

type MonitorResponse struct {
    ID                  string  `json:"id"`
    TargetURL           string  `json:"target_url"`
    IntervalSeconds     int     `json:"interval_seconds"`
    CreditBalanceChecks int64   `json:"credit_balance_checks"`
    TotalSpentTokens    float64 `json:"total_spent_tokens"`
    IsActive            bool    `json:"is_active"`
}

type MonitorStatsResponse struct {
	MonitorID    string  `json:"monitor_id"`
	CheckInterval int     `json:"check_interval"`
    UptimePct24h float64 `json:"uptime_pct_24h"`
    UptimePct7d  float64 `json:"uptime_pct_7d"`
    RecentPings  any     `json:"recent_pings"`
}

// 🚀 ADD THIS TO RESOLVE THE THREE UNDEFINED ERRORS:
type DashboardOverviewResponse struct {
    TotalMonitors      int     `json:"total_monitors"`
    ActiveMonitors     int     `json:"active_monitors"`
    GlobalAvgLatencyMs float64 `json:"global_avg_latency_ms"`
    TotalSpentTokens   float64 `json:"total_spent_tokens"`
    WalletConnected    bool    `json:"wallet_connected"`
    RunnerNodesCount   int     `json:"runner_nodes_count"`
}
