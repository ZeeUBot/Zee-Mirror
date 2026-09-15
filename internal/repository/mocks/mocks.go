package mocks

import (
	"context"
	"time"
	"zee-mirror/internal/domain"

	"github.com/stretchr/testify/mock"
)

type MockRepository struct {
	mock.Mock
}

func (m *MockRepository) Save(ctx context.Context, task domain.TaskRecord) error {
	args := m.Called(ctx, task)
	return args.Error(0)
}

func (m *MockRepository) GetTaskByID(ctx context.Context, id string) (*domain.TaskRecord, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TaskRecord), args.Error(1)
}

func (m *MockRepository) GetActive(ctx context.Context) ([]domain.TaskRecord, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.TaskRecord), args.Error(1)
}

func (m *MockRepository) GetRecoverable(ctx context.Context) ([]domain.TaskRecord, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.TaskRecord), args.Error(1)
}

func (m *MockRepository) UpdateStatus(ctx context.Context, id, status, err string) error {
	args := m.Called(ctx, id, status, err)
	return args.Error(0)
}
func (m *MockRepository) SetNotifyURLByUser(ctx context.Context, userID int64, url string) error {
	args := m.Called(ctx, userID, url)
	return args.Error(0)
}

func (m *MockRepository) DeleteOld(ctx context.Context, before string) (int, error) {
	args := m.Called(ctx, before)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) GetBotStats(ctx context.Context) (map[string]interface{}, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockRepository) GetUserStats(ctx context.Context, userID int64) (*domain.UserStats, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.UserStats), args.Error(1)
}

func (m *MockRepository) GetTodayStats(ctx context.Context) (*domain.DailyStats, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DailyStats), args.Error(1)
}

func (m *MockRepository) GetUserTodayStats(ctx context.Context, userID int64) (*domain.DailyStats, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.DailyStats), args.Error(1)
}

func (m *MockRepository) GetWeeklyStats(ctx context.Context) ([]domain.DailyStats, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.DailyStats), args.Error(1)
}

func (m *MockRepository) GetMonthlyStats(ctx context.Context) ([]domain.DailyStats, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.DailyStats), args.Error(1)
}

func (m *MockRepository) GetCompletedTaskByURL(ctx context.Context, url, quality string) (*domain.TaskRecord, error) {
	args := m.Called(ctx, url, quality)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.TaskRecord), args.Error(1)
}

func (m *MockRepository) GetByID(ctx context.Context, id int64) (*domain.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockRepository) Upsert(ctx context.Context, user domain.User) error {
	args := m.Called(ctx, user)
	return args.Error(0)
}

func (m *MockRepository) SetRole(ctx context.Context, id int64, role string) error {
	args := m.Called(ctx, id, role)
	return args.Error(0)
}

func (m *MockRepository) SetLimits(ctx context.Context, id int64, maxTasks int, maxBandwidth int64) error {
	args := m.Called(ctx, id, maxTasks, maxBandwidth)
	return args.Error(0)
}

func (m *MockRepository) SetExpiration(ctx context.Context, id int64, expiresAt time.Time) error {
	args := m.Called(ctx, id, expiresAt)
	return args.Error(0)
}

func (m *MockRepository) GetAll(ctx context.Context) ([]domain.User, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.User), args.Error(1)
}

func (m *MockRepository) GetCount(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockRepository) Delete(ctx context.Context, id int64) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}
func (m *MockRepository) GetUserByAPIKey(ctx context.Context, apiKey string) (*domain.User, error) {
	args := m.Called(ctx, apiKey)
	u, _ := args.Get(0).(*domain.User)
	return u, args.Error(1)
}
func (m *MockRepository) SetAPIKey(ctx context.Context, id int64, apiKey string) error {
	args := m.Called(ctx, id, apiKey)
	return args.Error(0)
}

func (m *MockRepository) Get(ctx context.Context, key string) (string, error) {
	args := m.Called(ctx, key)
	return args.String(0), args.Error(1)
}

func (m *MockRepository) Set(ctx context.Context, key, value string) error {
	args := m.Called(ctx, key, value)
	return args.Error(0)
}

func (m *MockRepository) SetLanguage(ctx context.Context, id int64, lang string) error {
	args := m.Called(ctx, id, lang)
	return args.Error(0)
}

func (m *MockRepository) UpdateMD5(ctx context.Context, id, md5 string) error {
	args := m.Called(ctx, id, md5)
	return args.Error(0)
}

func (m *MockRepository) ListTasks(ctx context.Context, filter domain.TaskFilter) ([]domain.TaskRecord, error) {
	args := m.Called(ctx, filter)
	return args.Get(0).([]domain.TaskRecord), args.Error(1)
}

func (m *MockRepository) SaveScheduled(ctx context.Context, task domain.ScheduledTask) error {
	args := m.Called(ctx, task)
	return args.Error(0)
}

func (m *MockRepository) GetPendingScheduled(ctx context.Context) ([]domain.ScheduledTask, error) {
	args := m.Called(ctx)
	return args.Get(0).([]domain.ScheduledTask), args.Error(1)
}

func (m *MockRepository) MarkScheduledDone(ctx context.Context, id, taskID string) error {
	args := m.Called(ctx, id, taskID)
	return args.Error(0)
}

func (m *MockRepository) DeleteScheduled(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockRepository) LogAudit(ctx context.Context, entry domain.AuditEntry) error {
	args := m.Called(ctx, entry)
	return args.Error(0)
}

func (m *MockRepository) ListAuditLogs(ctx context.Context, limit, offset int) ([]domain.AuditEntry, error) {
	args := m.Called(ctx, limit, offset)
	return args.Get(0).([]domain.AuditEntry), args.Error(1)
}

func (m *MockRepository) Ping(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
