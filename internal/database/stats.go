package database

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
	"zee-mirror/internal/domain"
)

func (db *DB) GetBotStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var totalTasks, completedTasks, failedTasks int
	var totalBandwidth int64

	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks").Scan(&totalTasks); err != nil {
		slog.Error("Database error in GetBotStats count", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE status = 'completed'").Scan(&completedTasks); err != nil {
		slog.Error("Database error in GetBotStats completed", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE status = 'failed'").Scan(&failedTasks); err != nil {
		slog.Error("Database error in GetBotStats failed", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_size), 0) FROM tasks WHERE status = 'completed'").Scan(&totalBandwidth); err != nil {
		slog.Error("Database error in GetBotStats bandwidth", "error", err)
	}

	stats["total_tasks"] = totalTasks
	stats["completed_tasks"] = completedTasks
	stats["failed_tasks"] = failedTasks
	stats["total_bandwidth"] = totalBandwidth

	return stats, nil
}

type UserStats = domain.UserStats

func (db *DB) GetUserStats(ctx context.Context, userID int64) (*UserStats, error) {
	stats := &UserStats{UserID: userID}

	if err := db.QueryRowContext(ctx, "SELECT COALESCE(username, '') FROM users WHERE id = $1", userID).Scan(&stats.Username); err != nil {
		slog.Error("Database error in GetUserStats username", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE user_id = $1", userID).Scan(&stats.TotalDownloads); err != nil {
		slog.Error("Database error in GetUserStats total", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND status = 'completed'", userID).Scan(&stats.SuccessfulTasks); err != nil {
		slog.Error("Database error in GetUserStats successful", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND status = 'failed'", userID).Scan(&stats.FailedTasks); err != nil {
		slog.Error("Database error in GetUserStats failed", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_size), 0) FROM tasks WHERE user_id = $1 AND status = 'completed'", userID).Scan(&stats.TotalBandwidth); err != nil {
		slog.Error("Database error in GetUserStats bandwidth", "error", err)
	}
	var lastActiveStr sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT MAX(created_at) FROM tasks WHERE user_id = $1", userID).Scan(&lastActiveStr); err != nil {
		slog.Error("Database error in GetUserStats last active", "error", err)
	}
	if lastActiveStr.Valid {
		stats.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActiveStr.String)
	}
	if stats.LastActive.IsZero() {
		stats.LastActive = time.Now()
	}

	return stats, nil
}

type DailyStats = domain.DailyStats

func todayRange() (string, string) {
	now := time.Now()
	start := now.Format("2006-01-02 00:00:00")
	end := now.AddDate(0, 0, 1).Format("2006-01-02 00:00:00")
	return start, end
}

func (db *DB) GetTodayStats(ctx context.Context) (*DailyStats, error) {
	stats := &DailyStats{Date: time.Now()}
	start, end := todayRange()

	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE created_at >= $1 AND created_at < $2", start, end).Scan(&stats.TotalTasks); err != nil {
		slog.Error("Database error in GetTodayStats total", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE created_at >= $1 AND created_at < $2 AND status = 'completed'", start, end).Scan(&stats.CompletedTasks); err != nil {
		slog.Error("Database error in GetTodayStats completed", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE created_at >= $1 AND created_at < $2 AND status = 'failed'", start, end).Scan(&stats.FailedTasks); err != nil {
		slog.Error("Database error in GetTodayStats failed", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_size), 0) FROM tasks WHERE created_at >= $1 AND created_at < $2 AND status = 'completed'", start, end).Scan(&stats.TotalBandwidth); err != nil {
		slog.Error("Database error in GetTodayStats", "error", err)
	}

	return stats, nil
}

func (db *DB) GetUserTodayStats(ctx context.Context, userID int64) (*DailyStats, error) {
	stats := &DailyStats{Date: time.Now()}
	start, end := todayRange()

	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND created_at >= $2 AND created_at < $3", userID, start, end).Scan(&stats.TotalTasks); err != nil {
		slog.Error("Database error in GetUserTodayStats total", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND created_at >= $2 AND created_at < $3 AND status = 'completed'", userID, start, end).Scan(&stats.CompletedTasks); err != nil {
		slog.Error("Database error in GetUserTodayStats completed", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND created_at >= $2 AND created_at < $3 AND status = 'failed'", userID, start, end).Scan(&stats.FailedTasks); err != nil {
		slog.Error("Database error in GetUserTodayStats failed", "error", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(SUM(total_size), 0) FROM tasks WHERE user_id = $1 AND created_at >= $2 AND created_at < $3 AND status = 'completed'", userID, start, end).Scan(&stats.TotalBandwidth); err != nil {
		slog.Error("Database error in GetUserTodayStats bandwidth", "error", err)
	}

	return stats, nil
}

func (db *DB) GetWeeklyStats(ctx context.Context) ([]DailyStats, error) {
	now := time.Now()
	start := now.AddDate(0, 0, -6).Format("2006-01-02")
	end := now.AddDate(0, 0, 1).Format("2006-01-02")

	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(DATE(created_at), 'unknown') as day,
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed,
			COALESCE(SUM(CASE WHEN status = 'completed' THEN total_size ELSE 0 END), 0) as bandwidth
		FROM tasks WHERE created_at >= $1 AND created_at < $2
		GROUP BY DATE(created_at) ORDER BY day
	`, start, end)
	if err != nil {
		slog.Error("Database error in GetWeeklyStats", "error", err)
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	resultMap := make(map[string]*DailyStats)
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		resultMap[dateStr] = &DailyStats{Date: d}
	}

	for rows.Next() {
		var day string
		var ds DailyStats
		if err := rows.Scan(&day, &ds.TotalTasks, &ds.CompletedTasks, &ds.FailedTasks, &ds.TotalBandwidth); err != nil {
			slog.Error("Database error scanning weekly stats row", "error", err)
			continue
		}
		if existing, ok := resultMap[day]; ok {
			existing.TotalTasks = ds.TotalTasks
			existing.CompletedTasks = ds.CompletedTasks
			existing.FailedTasks = ds.FailedTasks
			existing.TotalBandwidth = ds.TotalBandwidth
		}
	}

	var stats []DailyStats
	for i := 6; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		stats = append(stats, *resultMap[dateStr])
	}

	return stats, nil
}

func (db *DB) GetMonthlyStats(ctx context.Context) ([]DailyStats, error) {
	now := time.Now()
	start := now.AddDate(0, 0, -29).Format("2006-01-02")
	end := now.AddDate(0, 0, 1).Format("2006-01-02")

	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(DATE(created_at), 'unknown') as day,
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed,
			COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed,
			COALESCE(SUM(CASE WHEN status = 'completed' THEN total_size ELSE 0 END), 0) as bandwidth
		FROM tasks WHERE created_at >= $1 AND created_at < $2
		GROUP BY DATE(created_at) ORDER BY day
	`, start, end)
	if err != nil {
		slog.Error("Database error in GetMonthlyStats", "error", err)
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	resultMap := make(map[string]*DailyStats)
	for i := 29; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		resultMap[dateStr] = &DailyStats{Date: d}
	}

	for rows.Next() {
		var day string
		var ds DailyStats
		if err := rows.Scan(&day, &ds.TotalTasks, &ds.CompletedTasks, &ds.FailedTasks, &ds.TotalBandwidth); err != nil {
			slog.Error("Database error scanning monthly stats row", "error", err)
			continue
		}
		if existing, ok := resultMap[day]; ok {
			existing.TotalTasks = ds.TotalTasks
			existing.CompletedTasks = ds.CompletedTasks
			existing.FailedTasks = ds.FailedTasks
			existing.TotalBandwidth = ds.TotalBandwidth
		}
	}

	var stats []DailyStats
	for i := 29; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		dateStr := d.Format("2006-01-02")
		stats = append(stats, *resultMap[dateStr])
	}

	return stats, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
