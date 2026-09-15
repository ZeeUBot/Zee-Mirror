package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"zee-mirror/internal/domain"
)

func (db *DB) GetRecoverable(ctx context.Context) ([]TaskRecord, error) {
	query := fmt.Sprintf(`
		SELECT `+allTaskColumns+` FROM tasks 
		WHERE status IN ('downloading', 'uploading', 'queued', 'processing')
		AND created_at > %s
	`, db.nowMinusExpr("24 hours"))

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []TaskRecord
	for rows.Next() {
		var t TaskRecord
		err := rows.Scan(
			&t.ID, &t.GID, &t.Type, &t.Status, &t.URL, &t.FileName, &t.LocalPath, &t.RemotePath, &t.RemoteURL,
			&t.TotalSize, &t.DownloadedSize, &t.UploadedSize, &t.ChatID, &t.UserID, &t.CreatedAt,
			&t.CompletedAt, &t.Zip, &t.Unzip, &t.Password, &t.Error, &t.RetryCount, &t.Quality, &t.MD5, &t.NotifyURL,
		)
		if err != nil {
			continue
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (db *DB) SaveCheckpoint(ctx context.Context, cp domain.TaskCheckpoint) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO task_checkpoints (task_id, downloaded_bytes, total_bytes, progress, last_update)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(task_id) DO UPDATE SET
			downloaded_bytes = excluded.downloaded_bytes,
			total_bytes = excluded.total_bytes,
			progress = excluded.progress,
			last_update = excluded.last_update
	`, cp.TaskID, cp.DownloadedBytes, cp.TotalBytes, cp.Progress, cp.LastUpdate.Unix())
	return err
}

func (db *DB) GetCheckpoint(ctx context.Context, taskID string) (*domain.TaskCheckpoint, error) {
	var cp domain.TaskCheckpoint
	var lastUpdate int64
	err := db.QueryRowContext(ctx, `
		SELECT task_id, downloaded_bytes, total_bytes, progress, last_update
		FROM task_checkpoints WHERE task_id = $1
	`, taskID).Scan(&cp.TaskID, &cp.DownloadedBytes, &cp.TotalBytes, &cp.Progress, &lastUpdate)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	cp.LastUpdate = time.Unix(lastUpdate, 0)
	return &cp, nil
}

func (db *DB) DeleteCheckpoint(ctx context.Context, taskID string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM task_checkpoints WHERE task_id = $1", taskID)
	return err
}
