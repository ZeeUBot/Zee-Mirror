package service_test

import (
	"testing"

	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/errors"
	"zee-mirror/internal/repository/mocks"
	"zee-mirror/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestIsAuthorized(t *testing.T) {
	mockRepo := new(mocks.MockRepository)
	cfg := &config.Config{OwnerID: 12345}

	t.Run("Owner", func(t *testing.T) {
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)
		assert.True(t, svc.IsAuthorized(12345))
	})

	t.Run("Admin User", func(t *testing.T) {
		mockRepo.On("GetByID", mock.Anything, int64(67890)).Return(&domain.User{
			ID:       67890,
			Role:     service.RoleAdmin,
			IsActive: true,
		}, nil).Once()

		svc := service.NewAuthService(&config.Config{}, mockRepo, mockRepo)
		assert.True(t, svc.IsAuthorized(67890))
	})

	t.Run("Authorized User", func(t *testing.T) {
		mockRepo.On("GetByID", mock.Anything, int64(11111)).Return(&domain.User{
			ID:       11111,
			Role:     service.RoleAuthorized,
			IsActive: true,
		}, nil).Once()

		svc := service.NewAuthService(&config.Config{}, mockRepo, mockRepo)
		assert.True(t, svc.IsAuthorized(11111))
	})

	t.Run("Inactive User", func(t *testing.T) {
		mockRepo.On("GetByID", mock.Anything, int64(22222)).Return(&domain.User{
			ID:       22222,
			Role:     service.RoleAuthorized,
			IsActive: false,
		}, nil).Once()

		svc := service.NewAuthService(&config.Config{}, mockRepo, mockRepo)
		assert.False(t, svc.IsAuthorized(22222))
	})

	t.Run("User Not Found", func(t *testing.T) {
		mockRepo.On("GetByID", mock.Anything, int64(33333)).Return(nil, errors.ErrUserNotFound).Once()

		svc := service.NewAuthService(&config.Config{}, mockRepo, mockRepo)
		assert.False(t, svc.IsAuthorized(33333))
	})
}

func TestIsPrivileged(t *testing.T) {
	mockRepo := new(mocks.MockRepository)
	svc := service.NewAuthService(&config.Config{OwnerID: 1, AuthorizedUsers: []int64{111, 222}}, mockRepo, mockRepo)

	assert.True(t, svc.IsPrivileged(1))
	assert.True(t, svc.IsPrivileged(222))
	assert.False(t, svc.IsPrivileged(999))
	assert.False(t, service.NewAuthService(nil, mockRepo, mockRepo).IsPrivileged(1))
}

func TestCheckQuota(t *testing.T) {
	cfg := &config.Config{OwnerID: 12345}

	t.Run("Owner", func(t *testing.T) {
		mockRepo := new(mocks.MockRepository)
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)

		assert.NoError(t, svc.CheckQuota(12345))
		mockRepo.AssertExpectations(t)
	})

	t.Run("User Not Found", func(t *testing.T) {
		mockRepo := new(mocks.MockRepository)
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)

		mockRepo.On("GetByID", mock.Anything, int64(999)).Return(nil, errors.ErrUserNotFound).Once()

		err := svc.CheckQuota(999)
		assert.Error(t, err)
		assert.ErrorIs(t, err, errors.ErrUserNotFound)
		mockRepo.AssertExpectations(t)
	})

	t.Run("Inactive User", func(t *testing.T) {
		mockRepo := new(mocks.MockRepository)
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)

		mockRepo.On("GetByID", mock.Anything, int64(888)).Return(&domain.User{
			ID:       888,
			IsActive: false,
		}, nil).Once()

		err := svc.CheckQuota(888)
		assert.Error(t, err)
		assert.ErrorIs(t, err, errors.ErrAccountInactive)
		mockRepo.AssertExpectations(t)
	})

	t.Run("Unlimited Quota", func(t *testing.T) {
		mockRepo := new(mocks.MockRepository)
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)

		mockRepo.On("GetByID", mock.Anything, int64(777)).Return(&domain.User{
			ID:                777,
			IsActive:          true,
			MaxDailyTasks:     -1,
			MaxDailyBandwidth: -1,
		}, nil).Once()

		assert.NoError(t, svc.CheckQuota(777))
		mockRepo.AssertExpectations(t)
	})

	t.Run("Task Limit Exceeded", func(t *testing.T) {
		mockRepo := new(mocks.MockRepository)
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)

		mockRepo.On("GetByID", mock.Anything, int64(666)).Return(&domain.User{
			ID:                666,
			IsActive:          true,
			MaxDailyTasks:     5,
			MaxDailyBandwidth: -1,
		}, nil).Once()

		mockRepo.On("GetUserTodayStats", mock.Anything, int64(666)).Return(&domain.DailyStats{
			TotalTasks: 5,
		}, nil).Once()

		err := svc.CheckQuota(666)
		assert.Error(t, err)
		assert.ErrorIs(t, err, errors.ErrQuotaExceeded)
		assert.Contains(t, err.Error(), "(5/5)")
		mockRepo.AssertExpectations(t)
	})

	t.Run("Bandwidth Limit Exceeded", func(t *testing.T) {
		mockRepo := new(mocks.MockRepository)
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)

		mockRepo.On("GetByID", mock.Anything, int64(555)).Return(&domain.User{
			ID:                555,
			IsActive:          true,
			MaxDailyTasks:     -1,
			MaxDailyBandwidth: 1000,
		}, nil).Once()

		mockRepo.On("GetUserTodayStats", mock.Anything, int64(555)).Return(&domain.DailyStats{
			TotalTasks:     1,
			TotalBandwidth: 1000,
		}, nil).Once()

		err := svc.CheckQuota(555)
		assert.Error(t, err)
		assert.ErrorIs(t, err, errors.ErrQuotaExceeded)
		assert.Contains(t, err.Error(), "1000 B/1000 B")
		mockRepo.AssertExpectations(t)
	})

	t.Run("Within Limits", func(t *testing.T) {
		mockRepo := new(mocks.MockRepository)
		svc := service.NewAuthService(cfg, mockRepo, mockRepo)

		mockRepo.On("GetByID", mock.Anything, int64(444)).Return(&domain.User{
			ID:                444,
			IsActive:          true,
			MaxDailyTasks:     5,
			MaxDailyBandwidth: 1000,
		}, nil).Once()

		mockRepo.On("GetUserTodayStats", mock.Anything, int64(444)).Return(&domain.DailyStats{
			TotalTasks:     4,
			TotalBandwidth: 500,
		}, nil).Once()

		assert.NoError(t, svc.CheckQuota(444))
		mockRepo.AssertExpectations(t)
	})
}
