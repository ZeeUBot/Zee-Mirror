package repository

import (
	"context"
	"time"
	"zee-mirror/internal/domain"
)

type TaskRepository interface {
	Save(ctx context.Context, task domain.TaskRecord) error
	GetTaskByID(ctx context.Context, id string) (*domain.TaskRecord, error)
	GetActive(ctx context.Context) ([]domain.TaskRecord, error)
	GetRecoverable(ctx context.Context) ([]domain.TaskRecord, error)
	UpdateStatus(ctx context.Context, id, status, err string) error
	SetNotifyURLByUser(ctx context.Context, userID int64, url string) error
	UpdateMD5(ctx context.Context, id, md5 string) error
	ListTasks(ctx context.Context, filter domain.TaskFilter) ([]domain.TaskRecord, error)
	DeleteOld(ctx context.Context, before string) (int, error)

	GetBotStats(ctx context.Context) (map[string]interface{}, error)
	GetUserStats(ctx context.Context, userID int64) (*domain.UserStats, error)
	GetTodayStats(ctx context.Context) (*domain.DailyStats, error)
	GetUserTodayStats(ctx context.Context, userID int64) (*domain.DailyStats, error)
	GetWeeklyStats(ctx context.Context) ([]domain.DailyStats, error)
	GetMonthlyStats(ctx context.Context) ([]domain.DailyStats, error)
	GetCompletedTaskByURL(ctx context.Context, url, quality string) (*domain.TaskRecord, error)

	SaveScheduled(ctx context.Context, task domain.ScheduledTask) error
	GetPendingScheduled(ctx context.Context) ([]domain.ScheduledTask, error)
	MarkScheduledDone(ctx context.Context, id, taskID string) error
	DeleteScheduled(ctx context.Context, id string) error
}

type UserRepository interface {
	GetByID(ctx context.Context, id int64) (*domain.User, error)
	Upsert(ctx context.Context, user domain.User) error
	SetRole(ctx context.Context, id int64, role string) error
	SetLimits(ctx context.Context, id int64, maxTasks int, maxBandwidth int64) error
	SetExpiration(ctx context.Context, id int64, expiresAt time.Time) error
	SetLanguage(ctx context.Context, id int64, lang string) error
	GetAll(ctx context.Context) ([]domain.User, error)
	GetUserByAPIKey(ctx context.Context, apiKey string) (*domain.User, error)
	SetAPIKey(ctx context.Context, id int64, apiKey string) error
	GetCount(ctx context.Context) (int, error)
	Delete(ctx context.Context, id int64) error
}

type SettingsRepository interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

type ScheduledTaskRepository interface {
	SaveScheduled(ctx context.Context, task domain.ScheduledTask) error
	GetPendingScheduled(ctx context.Context) ([]domain.ScheduledTask, error)
	MarkScheduledDone(ctx context.Context, id, taskID string) error
	DeleteScheduled(ctx context.Context, id string) error
}

type AuditRepository interface {
	LogAudit(ctx context.Context, entry domain.AuditEntry) error
	ListAuditLogs(ctx context.Context, limit, offset int) ([]domain.AuditEntry, error)
}

type FullRepository interface {
	TaskRepository
	UserRepository
	SettingsRepository
	AuditRepository
	Ping(ctx context.Context) error
}
