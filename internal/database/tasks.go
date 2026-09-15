package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
	"zee-mirror/internal/domain"
)

type TaskRecord = domain.TaskRecord

const taskColumns = "id, gid, type, status, url, file_name, local_path, remote_path, remote_url, total_size, downloaded_size, uploaded_size, chat_id, user_id, created_at, completed_at, zip, unzip, password, error, retries, quality, md5, notify_url"

func (db *DB) ListTasks(ctx context.Context, filter domain.TaskFilter) ([]TaskRecord, error) {
	query := "SELECT " + taskColumns + " FROM tasks WHERE 1=1"
	var args []interface{}
	paramIdx := 1

	if filter.UserID > 0 {
		query += fmt.Sprintf(" AND user_id = $%d", paramIdx)
		args = append(args, filter.UserID)
		paramIdx++
	}
	if filter.Status != "" {
		query += fmt.Sprintf(" AND status = $%d", paramIdx)
		args = append(args, filter.Status)
		paramIdx++
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", paramIdx)
		args = append(args, filter.Limit)
		paramIdx++
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", paramIdx)
		args = append(args, filter.Offset)
	}

	rows, err := db.QueryContext(ctx, query, args...)
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
			slog.Error("Error scanning task in ListTasks", "error", err)
			continue
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (db *DB) GetCompletedTaskByURL(ctx context.Context, url, quality string) (*TaskRecord, error) {
	tr := &TaskRecord{}
	err := db.QueryRowContext(ctx, `
		SELECT `+taskColumns+` FROM tasks WHERE url = $1 AND quality = $2 AND status = 'completed'
		ORDER BY created_at DESC LIMIT 1
	`, url, quality).Scan(
		&tr.ID, &tr.GID, &tr.Type, &tr.Status, &tr.URL, &tr.FileName, &tr.LocalPath, &tr.RemotePath, &tr.RemoteURL,
		&tr.TotalSize, &tr.DownloadedSize, &tr.UploadedSize, &tr.ChatID, &tr.UserID,
		&tr.CreatedAt, &tr.CompletedAt, &tr.Zip, &tr.Unzip, &tr.Password, &tr.Error, &tr.RetryCount, &tr.Quality, &tr.MD5, &tr.NotifyURL,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return tr, nil
}

func (db *DB) Save(ctx context.Context, t TaskRecord) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO tasks (
			id, gid, type, status, url, file_name, local_path, remote_path, remote_url,
			total_size, downloaded_size, uploaded_size, chat_id, user_id, created_at,
			completed_at, zip, unzip, password, error, retries, quality, notify_url
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
		ON CONFLICT(id) DO UPDATE SET
			gid = excluded.gid,
			status = excluded.status,
			file_name = excluded.file_name,
			local_path = excluded.local_path,
			remote_path = excluded.remote_path,
			remote_url = excluded.remote_url,
			total_size = excluded.total_size,
			downloaded_size = excluded.downloaded_size,
			uploaded_size = excluded.uploaded_size,
			completed_at = excluded.completed_at,
			error = excluded.error,
			retries = excluded.retries,
			quality = excluded.quality
	`, t.ID, t.GID, t.Type, t.Status, t.URL, t.FileName, t.LocalPath, t.RemotePath, t.RemoteURL,
		t.TotalSize, t.DownloadedSize, t.UploadedSize, t.ChatID, t.UserID, t.CreatedAt,
		t.CompletedAt, t.Zip, t.Unzip, t.Password, t.Error, t.RetryCount, t.Quality, t.NotifyURL)
	return err
}

const allTaskColumns = "id, gid, type, status, url, file_name, local_path, remote_path, remote_url, total_size, downloaded_size, uploaded_size, chat_id, user_id, created_at, completed_at, zip, unzip, password, error, retries, quality, md5, notify_url"

func (db *DB) GetActive(ctx context.Context) ([]TaskRecord, error) {
	rows, err := db.QueryContext(ctx, "SELECT "+allTaskColumns+" FROM tasks WHERE status NOT IN ('completed', 'failed', 'cancelled')")
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
			slog.Error("Error scanning task", "error", err)
			continue
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func (db *DB) GetTaskByID(ctx context.Context, id string) (*TaskRecord, error) {
	tr := &TaskRecord{}
	err := db.QueryRowContext(ctx, `
		SELECT `+taskColumns+` FROM tasks WHERE id = $1
	`, id).Scan(
		&tr.ID, &tr.GID, &tr.Type, &tr.Status, &tr.URL, &tr.FileName, &tr.LocalPath, &tr.RemotePath, &tr.RemoteURL,
		&tr.TotalSize, &tr.DownloadedSize, &tr.UploadedSize, &tr.ChatID, &tr.UserID,
		&tr.CreatedAt, &tr.CompletedAt, &tr.Zip, &tr.Unzip, &tr.Password, &tr.Error, &tr.RetryCount, &tr.Quality, &tr.MD5, &tr.NotifyURL,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return tr, nil
}

func (db *DB) UpdateStatus(ctx context.Context, taskID, status, errorMsg string) error {
	_, err := db.ExecContext(ctx, "UPDATE tasks SET status = $1, error = $2 WHERE id = $3", status, errorMsg, taskID)
	return err
}

func (db *DB) UpdateMD5(ctx context.Context, id, md5 string) error {
	_, err := db.ExecContext(ctx, "UPDATE tasks SET md5=$1 WHERE id=$2", md5, id)
	return err
}
func (db *DB) SetNotifyURLByUser(ctx context.Context, userID int64, url string) error {
	_, err := db.ExecContext(ctx, "UPDATE tasks SET notify_url=$1 WHERE user_id=$2 AND status NOT IN ('completed','failed','cancelled')", url, userID)
	return err
}

func (db *DB) DeleteOld(ctx context.Context, before string) (int, error) {
	result, err := db.ExecContext(ctx, "DELETE FROM tasks WHERE status IN ('completed', 'failed', 'cancelled') AND created_at < $1", before)
	if err != nil {
		return 0, err
	}
	affected, _ := result.RowsAffected()
	return int(affected), nil
}

func (db *DB) GetRecentLogs(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, type, status, file_name, created_at, error 
		FROM tasks 
		ORDER BY created_at DESC 
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var logs []map[string]interface{}
	for rows.Next() {
		var id, taskType, status, fileName, errorStr string
		var createdAt time.Time
		if err := rows.Scan(&id, &taskType, &status, &fileName, &createdAt, &errorStr); err != nil {
			continue
		}

		level := "info"
		message := fmt.Sprintf("[%s] %s - %s", taskType, fileName, status)
		switch status {
		case "failed":
			level = "error"
			message = fmt.Sprintf("[%s] %s - %s: %s", taskType, fileName, status, errorStr)
		case "completed":
			level = "success"
		}

		logs = append(logs, map[string]interface{}{
			"level":     level,
			"message":   message,
			"timestamp": createdAt,
		})
	}
	return logs, nil
}
