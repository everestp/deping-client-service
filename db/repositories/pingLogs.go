package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)
type PingLog struct {
	ID           int64
	MonitorID    string
	RunnerPubkey string
	DnsUs        uint64
	TcpUs        uint64
	TlsUs        uint64
	TtfbUs       uint64
	TotalUs      uint64
	LatencyMs    int
	StatusCode   int
	Success      bool
	ErrorKind    string
	GeoRegion    string
	Latitude  float64
    Longitude float64
	Timestamp    time.Time
}


type PingLogRepository interface {

	FindByMonitor(ctx context.Context, monitorID string, limit int) ([]*PingLog, error)
	UptimePercentage(ctx context.Context, monitorID string, since time.Time) (float64, error)
	AvgLatencyUs(ctx context.Context, monitorID string, since time.Time) (uint64, error)
}


type postgresPingLogRepo struct {
	db *sql.DB
}
func NewPingLogRepository(db *sql.DB) PingLogRepository {
	return &postgresPingLogRepo{db: db}
}
func (r *postgresPingLogRepo) FindByMonitor(ctx context.Context, monitorID string, limit int) ([]*PingLog, error) {
    const q = `
        SELECT id, monitor_id, runner_pubkey,
               dns_us, tcp_us, tls_us, ttfb_us, total_us,
               latency_ms, status_code, success, error_kind,
               geo_region, timestamp, latitude, longitude
        FROM ping_logs
        WHERE monitor_id = $1
        ORDER BY timestamp DESC
        LIMIT $2`

    rows, err := r.db.QueryContext(ctx, q, monitorID, limit)
    if err != nil {
        return nil, fmt.Errorf("postgresPingLogRepo.FindByMonitor: %w", err)
    }
    defer rows.Close()

    var result []*PingLog
    for rows.Next() {
        l := &PingLog{}
        if err := rows.Scan(
            &l.ID, &l.MonitorID, &l.RunnerPubkey,
            &l.DnsUs, &l.TcpUs, &l.TlsUs, &l.TtfbUs, &l.TotalUs,
            &l.LatencyMs, &l.StatusCode, &l.Success, &l.ErrorKind,
            &l.GeoRegion, &l.Timestamp, &l.Latitude, &l.Longitude,
        ); err != nil {
            return nil, err
        }
        result = append(result, l)
    }
    return result, rows.Err()
}

func (r *postgresPingLogRepo) UptimePercentage(ctx context.Context, monitorID string, since time.Time) (float64, error) {
	const q = `
		SELECT COALESCE(
		  100.0 * SUM(CASE WHEN success THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0),
		0) FROM ping_logs WHERE monitor_id = $1 AND timestamp >= $2`
	var pct float64
	if err := r.db.QueryRowContext(ctx, q, monitorID, since).Scan(&pct); err != nil {
		return 0, fmt.Errorf("postgresPingLogRepo.UptimePercentage: %w", err)
	}
	return pct, nil
}

func (r *postgresPingLogRepo) AvgLatencyUs(ctx context.Context, monitorID string, since time.Time) (uint64, error) {
	const q = `
		SELECT COALESCE(AVG(total_us)::bigint, 0)
		FROM ping_logs WHERE monitor_id = $1 AND timestamp >= $2 AND success = TRUE`
	var avg uint64
	if err := r.db.QueryRowContext(ctx, q, monitorID, since).Scan(&avg); err != nil {
		return 0, fmt.Errorf("pingLogRepo.AvgLatencyUs: %w", err)
	}
	return avg, nil
}




