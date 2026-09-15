package database

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"zee-mirror/internal/domain"
)

func (db *DB) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	u := &domain.User{ID: id}
	var expiresAt sql.NullTime

	err := db.QueryRowContext(ctx, `
		SELECT username, role, language, max_daily_tasks, max_daily_bandwidth, expires_at, created_at 
		FROM users WHERE id = $1
	`, id).Scan(&u.Username, &u.Role, &u.Language, &u.MaxDailyTasks, &u.MaxDailyBandwidth, &expiresAt, &u.CreatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	u.ExpiresAt = expiresAt

	u.IsActive = true
	if u.ExpiresAt.Valid && u.ExpiresAt.Time.Before(time.Now()) {
		u.IsActive = false
	}

	return u, nil
}

func (db *DB) Upsert(ctx context.Context, u domain.User) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, username, role, language, created_at, max_daily_tasks, max_daily_bandwidth, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			role = CASE WHEN users.role = 'owner' THEN 'owner' ELSE excluded.role END,
			language = excluded.language,
			max_daily_tasks = excluded.max_daily_tasks,
			max_daily_bandwidth = excluded.max_daily_bandwidth,
			expires_at = excluded.expires_at
	`, u.ID, u.Username, u.Role, u.Language, u.CreatedAt, u.MaxDailyTasks, u.MaxDailyBandwidth, u.ExpiresAt)
	return err
}

func (db *DB) SetRole(ctx context.Context, id int64, role string) error {
	_, err := db.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", role, id)
	return err
}

func (db *DB) SetLimits(ctx context.Context, id int64, maxTasks int, maxBandwidth int64) error {
	_, err := db.ExecContext(ctx, "UPDATE users SET max_daily_tasks = $1, max_daily_bandwidth = $2 WHERE id = $3", maxTasks, maxBandwidth, id)
	return err
}

func (db *DB) SetExpiration(ctx context.Context, id int64, expiresAt time.Time) error {
	_, err := db.ExecContext(ctx, "UPDATE users SET expires_at = $1 WHERE id = $2", expiresAt, id)
	return err
}

func (db *DB) SetLanguage(ctx context.Context, id int64, lang string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO users (id, language, created_at, role, max_daily_tasks, max_daily_bandwidth)
		VALUES ($1, $2, $3, 'user', 0, 0)
		ON CONFLICT(id) DO UPDATE SET language = excluded.language
	`, id, lang, time.Now())
	return err
}

func (db *DB) GetAll(ctx context.Context) ([]domain.User, error) {
	rows, err := db.QueryContext(ctx, "SELECT id, username, role, language, max_daily_tasks, max_daily_bandwidth, expires_at, created_at, COALESCE(api_key, '') FROM users")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var users []domain.User
	for rows.Next() {
		var u domain.User
		var expiresAt sql.NullTime
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.Language, &u.MaxDailyTasks, &u.MaxDailyBandwidth, &expiresAt, &u.CreatedAt, &u.APIKey); err != nil {
			return nil, err
		}
		u.ExpiresAt = expiresAt
		u.IsActive = true
		if u.ExpiresAt.Valid && u.ExpiresAt.Time.Before(time.Now()) {
			u.IsActive = false
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *DB) GetCount(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (db *DB) GetUserByAPIKey(ctx context.Context, apiKey string) (*domain.User, error) {
	if apiKey == "" {
		return nil, domain.ErrNotFound
	}
	u := &domain.User{}
	var expiresAt sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT id, username, role, language, max_daily_tasks, max_daily_bandwidth, expires_at, created_at 
		FROM users WHERE api_key = $1
	`, apiKey).Scan(&u.ID, &u.Username, &u.Role, &u.Language, &u.MaxDailyTasks, &u.MaxDailyBandwidth, &expiresAt, &u.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	u.ExpiresAt = expiresAt
	u.IsActive = !u.ExpiresAt.Valid || u.ExpiresAt.Time.After(time.Now())
	return u, nil
}

func (db *DB) SetAPIKey(ctx context.Context, id int64, apiKey string) error {
	_, err := db.ExecContext(ctx, "UPDATE users SET api_key = $1 WHERE id = $2", apiKey, id)
	return err
}

func (db *DB) Delete(ctx context.Context, id int64) error {
	_, err := db.ExecContext(ctx, "DELETE FROM users WHERE id = $1", id)
	return err
}
