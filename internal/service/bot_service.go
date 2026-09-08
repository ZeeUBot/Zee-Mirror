package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"zee-mirror/internal/config"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/repository"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"

	"zee-mirror/internal/uploader"
)

type BotService struct {
	SettingsRepo   repository.SettingsRepository
	DB             repository.FullRepository
	UserRepo       repository.UserRepository
	Settings       *Settings
	Bot            *tgbotapi.BotAPI
	Auth           *AuthService
	Media          *MediaService
	BatchManager   *BatchManager
	TaskManager    *TaskManager
	Config         *config.Config
	Notifications  *NotificationService
	RcloneUploader *uploader.RcloneUploader
	SQLDB          *sql.DB
	pathCacheClean chan struct{}
	PathCache      sync.Map
}

func (s *BotService) StorePath(path string) string {
	id := uuid.New().String()[:12]
	s.PathCache.Store(id, path)
	return id
}

func (s *BotService) startPathCacheCleanup() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.pathCacheClean:
			return
		case <-ticker.C:
			s.PathCache.Range(func(key, _ any) bool {
				s.PathCache.Delete(key)
				return true
			})
		}
	}
}

func (s *BotService) GetPath(id string) (string, bool) {
	val, ok := s.PathCache.Load(id)
	if !ok {
		return "", false
	}
	return val.(string), true
}

func NewBotService(bot *tgbotapi.BotAPI, cfg *config.Config, db repository.FullRepository, sqlDB *sql.DB) *BotService {
	authService := NewAuthService(cfg, db, db)
	mediaService := NewMediaService(cfg)

	s := &BotService{
		Bot:            bot,
		Auth:           authService,
		Media:          mediaService,
		Config:         cfg,
		DB:             db,
		UserRepo:       db,
		SettingsRepo:   db,
		Settings:       NewSettings(),
		BatchManager:   NewBatchManager(),
		RcloneUploader: uploader.NewRcloneUploader(cfg),
		SQLDB:          sqlDB,
		pathCacheClean: make(chan struct{}),
	}

	go s.startPathCacheCleanup()

	ctx := context.Background()
	if val, err := db.Get(ctx, "auto_delete_messages"); err == nil {
		s.Settings.AutoDeleteMessages = (val == "true")
	}
	if val, err := db.Get(ctx, "default_mode"); err == nil {
		s.Settings.DefaultMode = val
	}
	if val, err := db.Get(ctx, "ytdlp_quality"); err == nil {
		s.Settings.YTDLPQuality = val
	}

	processFunc := func(task *Task) {
		s.processTask(task)
	}

	refreshFunc := func(chatID int64, forceNew bool) {
		s.UpdateSharedDashboard(chatID, forceNew)
	}

	tm := NewTaskManager(bot, cfg, processFunc, refreshFunc, db, sqlDB, authService)
	tm.StopDuplicate = cfg.StopDuplicate
	s.TaskManager = tm

	var alertChannelID int64
	if alertCh, err := db.Get(ctx, "alert_channel_id"); err == nil {
		_, _ = fmt.Sscanf(alertCh, "%d", &alertChannelID)
	}
	s.Notifications = NewNotificationService(bot, alertChannelID, cfg.OwnerID)

	_ = db.Upsert(ctx, domain.User{
		ID:                cfg.OwnerID,
		Username:          "Owner",
		Role:              "owner",
		CreatedAt:         time.Now(),
		MaxDailyTasks:     -1,
		MaxDailyBandwidth: -1,
		IsActive:          true,
	})

	for _, id := range cfg.AuthorizedUsers {
		_ = db.Upsert(ctx, domain.User{
			ID:                id,
			Username:          "Authorized User",
			Role:              "authorized",
			CreatedAt:         time.Now(),
			MaxDailyTasks:     cfg.DefaultMaxDailyTasks,
			MaxDailyBandwidth: cfg.DefaultMaxDailyBandwidth,
			IsActive:          true,
		})
	}

	go s.startDiskCleanupWorker()
	go s.startDatabaseCleanupWorker()
	s.StartResourceMonitor()

	return s
}

func (s *BotService) Shutdown() {
	if s.TaskManager != nil {
		slog.Info("Shutting down TaskManager, cancelling active tasks...")

		activeTasks := s.TaskManager.GetActiveTasks()
		for _, task := range activeTasks {
			if task.CancelFunc != nil {
				task.CancelFunc()
			}
		}

		close(s.TaskManager.ShutdownChan)
		s.TaskManager.Wg.Wait()
		slog.Info("All tasks stopped.")
	}

	if s.DB != nil {
		if closer, ok := s.DB.(interface{ Close() error }); ok {
			slog.Info("Closing database connection...")
			_ = closer.Close()
		}
	}
}

func (s *BotService) AutoDeleteMessage(chatID int64, messageID int, delay time.Duration) {
	if s.Settings == nil {
		return
	}

	s.Settings.Mu.RLock()
	active := s.Settings.AutoDeleteMessages
	s.Settings.Mu.RUnlock()

	if !active || messageID == 0 {
		return
	}

	go func() {
		if delay > 0 {
			time.Sleep(delay)
		}
		deleteCmd := tgbotapi.NewDeleteMessage(chatID, messageID)
		_, _ = s.Bot.Request(deleteCmd)
	}()
}

func (s *BotService) HandleAutoDelete(task *Task) {
	if s.Settings == nil {
		return
	}

	s.Settings.Mu.RLock()
	active := s.Settings.AutoDeleteMessages
	s.Settings.Mu.RUnlock()

	if !active {
		return
	}

	if task.MessageID != 0 {
		go func() {
			deleteCmd := tgbotapi.NewDeleteMessage(task.ChatID, task.MessageID)
			_, _ = s.Bot.Request(deleteCmd)
		}()
	}

	if task.ReplyMessageID != 0 {
		go func() {
			deleteCmd := tgbotapi.NewDeleteMessage(task.ChatID, task.ReplyMessageID)
			_, _ = s.Bot.Request(deleteCmd)
		}()
	}
}

func (s *BotService) AutoDeleteCommandAndReply(message *tgbotapi.Message) {
	s.AutoDeleteMessage(message.Chat.ID, message.MessageID, 0)
	if message.ReplyToMessage != nil && !IsMediaMessage(message.ReplyToMessage) {
		s.AutoDeleteMessage(message.Chat.ID, message.ReplyToMessage.MessageID, 0)
	}
}
func (s *BotService) HandleCreateTaskError(chatID int64, messageID int, err error) {
	slog.Info("HandleCreateTaskError called", "chatID", chatID, "error", err)

	text := ""
	if errors.Is(err, domain.ErrDuplicateTask) {
		text = fmt.Sprintf("⚠️ *Download Dibatalkan*\n\n%s", utils.EscapeMarkdownV2(err.Error()))
	} else {
		text = fmt.Sprintf("❌ *Gagal membuat task:*\n%s", utils.EscapeMarkdownV2(err.Error()))
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = MarkdownV2
	msg.ReplyParameters.MessageID = messageID

	_, sendErr := s.Bot.Send(msg)
	if sendErr != nil {
		slog.Warn("Failed to send error with reply, trying without reply", "error", sendErr)
		msg.ReplyParameters.MessageID = 0
		_, sendErr = s.Bot.Send(msg)
		if sendErr != nil {
			slog.Error("Failed to send error message completely", "error", sendErr)
			return
		}
	}
}
func (s *BotService) GetFileWithFallback(fileID string) (tgbotapi.File, bool, error) {
	type result struct {
		err  error
		file tgbotapi.File
	}

	type offResult struct {
		err  error
		file tgbotapi.File
	}

	done := make(chan result, 1)
	go func() {
		tgFile, err := s.Bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
		done <- result{file: tgFile, err: err}
	}()

	select {
	case <-time.After(60 * time.Second):
		slog.Warn("GetFile timed out from local TG API", "fileID", fileID)
	case res := <-done:
		if res.err == nil {
			return res.file, false, nil
		}
		if s.Config.TelegramAPI == "" {
			return res.file, false, res.err
		}
		slog.Warn("Failed to get file from local TG API, retrying with official API...", "error", res.err, "fileID", fileID)
	}

	offDone := make(chan offResult, 1)
	go func() {
		offBot, offErr := tgbotapi.NewBotAPI(s.Bot.Token)
		if offErr != nil {
			offDone <- offResult{err: offErr}
			return
		}
		offFile, offErr := offBot.GetFile(tgbotapi.FileConfig{FileID: fileID})
		offDone <- offResult{file: offFile, err: offErr}
	}()

	select {
	case <-time.After(60 * time.Second):
		slog.Warn("GetFile timed out from official TG API", "fileID", fileID)
		return tgbotapi.File{}, false, fmt.Errorf("timeout resolving Telegram file: %s", fileID)
	case offRes := <-offDone:
		if offRes.err != nil {
			return tgbotapi.File{}, false, offRes.err
		}
		slog.Info("Successfully retrieved file info from official API", "fileID", fileID, "path", offRes.file.FilePath)
		return offRes.file, true, nil
	}
}
func (s *BotService) GetUserLanguage(userID int64) string {
	user, err := s.DB.GetByID(context.Background(), userID)
	if err != nil || user.Language == "" {
		return "id"
	}
	return user.Language
}

func (s *BotService) SyncUser(user *tgbotapi.User) {
	if user == nil {
		return
	}

	ctx := context.Background()
	existing, err := s.DB.GetByID(ctx, user.ID)

	role := "user"
	if s.Auth.IsPrivileged(user.ID) {
		if user.ID == s.Config.OwnerID {
			role = "owner"
		} else {
			role = "authorized"
		}
	}

	lang := "id"
	maxTasks := s.Config.DefaultMaxDailyTasks
	maxBandwidth := s.Config.DefaultMaxDailyBandwidth
	createdAt := time.Now()

	if err == nil && existing != nil {
		lang = existing.Language
		if existing.Role == "owner" || existing.Role == "authorized" {
			role = existing.Role
		}
		maxTasks = existing.MaxDailyTasks
		maxBandwidth = existing.MaxDailyBandwidth
		createdAt = existing.CreatedAt
	}

	_ = s.DB.Upsert(ctx, domain.User{
		ID:                user.ID,
		Username:          user.UserName,
		Role:              role,
		Language:          lang,
		CreatedAt:         createdAt,
		MaxDailyTasks:     maxTasks,
		MaxDailyBandwidth: maxBandwidth,
		IsActive:          true,
	})
}
