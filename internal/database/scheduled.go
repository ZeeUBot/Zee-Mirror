package database

import (
	"context"
	"fmt"
	"zee-mirror/internal/domain"
)

func (db *DB) SaveScheduled(ctx context.Context, task domain.ScheduledTask) error {
	query := fmt.Sprintf(`
		INSERT INTO scheduled_tasks (id, task_type, url, file_name, chat_id, user_id, zip, unzip, password, quality, scheduled_at, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'pending', %s)
	`, db.nowExpr())

	_, err := db.ExecContext(ctx, query,
		task.ID, task.TaskType, task.URL, task.FileName, task.ChatID, task.UserID,
		boolToInt(task.Zip), boolToInt(task.Unzip), task.Password, task.Quality, task.ScheduledAt)
	return err
}

func (db *DB) GetPendingScheduled(ctx context.Context) ([]domain.ScheduledTask, error) {
	query := fmt.Sprintf(`
		SELECT id, task_type, url, file_name, chat_id, user_id, zip, unzip, password, quality, scheduled_at, status, task_id, created_at 
		FROM scheduled_tasks WHERE status='pending' AND scheduled_at <= %s
	`, db.nowExpr())

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []domain.ScheduledTask
	for rows.Next() {
		var t domain.ScheduledTask
		var zipInt, unzipInt int
		if err := rows.Scan(&t.ID, &t.TaskType, &t.URL, &t.FileName, &t.ChatID, &t.UserID, &zipInt, &unzipInt, &t.Password, &t.Quality, &t.ScheduledAt, &t.Status, &t.TaskID, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Zip = zipInt == 1
		t.Unzip = unzipInt == 1
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (db *DB) MarkScheduledDone(ctx context.Context, id, taskID string) error {
	_, err := db.ExecContext(ctx, "UPDATE scheduled_tasks SET status='done', task_id=$1 WHERE id=$2", taskID, id)
	return err
}

func (db *DB) DeleteScheduled(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, "DELETE FROM scheduled_tasks WHERE id=$1", id)
	return err
}
