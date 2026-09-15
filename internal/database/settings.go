package database

import (
	"context"
	"time"
)

func (db *DB) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&value)
	return value, err
}

func (db *DB) Set(ctx context.Context, key, value string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, time.Now())
	return err
}
