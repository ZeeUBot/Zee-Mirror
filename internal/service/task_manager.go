package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"
	"zee-mirror/internal/config"
	"zee-mirror/internal/database"
	"zee-mirror/internal/domain"
	"zee-mirror/internal/downloader"
	"zee-mirror/internal/metrics"
	"zee-mirror/internal/queue"
	"zee-mirror/internal/recovery"
	"zee-mirror/internal/repository"
	"zee-mirror/internal/uploader"
	"zee-mirror/plugins/registry"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
)

type TaskStatus = domain.TaskStatus

const (
	StatusQueued      = domain.StatusQueued
	StatusDownloading = domain.StatusDownloading
	StatusFetching    = domain.StatusFetching
	StatusExtracting  = domain.StatusExtracting
	StatusZipping     = domain.StatusZipping
	StatusUploading   = domain.StatusUploading
	StatusCompleted   = domain.StatusCompleted
	StatusFailed      = domain.StatusFailed
	StatusCancelled   = domain.StatusCancelled
)

type TaskType = domain.TaskType

const (
	TypeMirror     = domain.TypeMirror
	TypeLeech      = domain.TypeLeech
	TypeYTDLP      = domain.TypeYTDLP
	TypeYTDLPLeech = domain.TypeYTDLPLeech
	TypeTorrent    = domain.TypeTorrent
	TypeClone      = domain.TypeClone
	TypeViking     = domain.TypeViking
	TypePlaylist   = domain.TypePlaylist
)

const (
	MarkdownV2       = "MarkdownV2"
	StatusHeaderText = "📊 *Status Task Aktif*\n\n"
	UnknownFile      = "unknown_file"
	UnlimitedStr     = "Unlimited"

	ModeLeech  = "leech"
	CmdRefresh = "refresh"
	CmdClose   = "close"
	CmdSystem  = "system"
	CmdHealth  = "health"
	CmdLogs    = "logs"

	IconFolder = "📁"
	IconFile   = "📄"
	IconOK     = "✅ OK"
	IconError  = "❌ ERROR"

	CleanupInterval = 30 * time.Minute
)

type Task struct {
	DB repository.TaskRepository
	domain.Task
}

type TaskSnapshot = domain.TaskSnapshot

type YTDLPSession = domain.YTDLPSession

type TorrentSession = domain.TorrentSession

type TorrentFile = domain.TorrentFile

type TaskManager struct {
	UserbotEngine        downloader.DownloadEngine
	DB                   repository.TaskRepository
	Aria2Engine          downloader.DownloadEngine
	YTDLPEngine          downloader.MediaDownloader
	LastTasksCount       map[int64]int
	ShutdownChan         chan struct{}
	Tasks                map[string]*Task
	Queue                *queue.PriorityQueue
	QueueSignal          chan struct{}
	RateLimiter          *queue.UserRateLimiter
	LastStatusMsg        map[int64]int
	StatusPages          map[int64]int
	YTDLPSessions        map[string]*YTDLPSession
	TorrentSessions      map[string]*TorrentSession
	LastDashUpdateAt     map[int64]time.Time
	LastDashProgressSum  map[int64]float64
	Bot                  *tgbotapi.BotAPI
	Config               *config.Config
	Auth                 Authorizer
	ProcessTaskFunc      func(*Task)
	RefreshDashboardFunc func(int64, bool)
	CheckpointManager    *recovery.CheckpointManager
	Scheduler            *Scheduler
	Semaphore            chan struct{}
	ConfigDir            string
	RcloneDest           string
	DownloadDir          string
	Wg                   sync.WaitGroup
	ActiveCount          int
	MaxConcurrent        int
	Mu                   sync.RWMutex
	StatusMu             sync.Mutex
	StopDuplicate        bool
}

func NewTaskManager(bot *tgbotapi.BotAPI, cfg *config.Config, processTaskFunc func(*Task), refreshDashboardFunc func(int64, bool), db repository.TaskRepository, sqlDB *sql.DB, auth Authorizer) *TaskManager {
	aria2Engine, _ := registry.CreateDownloadEngine("aria2", cfg)
	ytdlpEngine, _ := registry.CreateMediaDownloader("ytdlp", cfg)

	tm := &TaskManager{
		Tasks:                make(map[string]*Task),
		Queue:                queue.NewPriorityQueue(),
		QueueSignal:          make(chan struct{}, 1000),
		RateLimiter:          queue.NewUserRateLimiterWithDB(5, 10, sqlDB),
		MaxConcurrent:        cfg.MaxConcurrentDownloads,
		Semaphore:            make(chan struct{}, cfg.MaxConcurrentDownloads),
		DownloadDir:          cfg.DownloadDir,
		RcloneDest:           cfg.RcloneDest,
		ConfigDir:            cfg.ConfigDir,
		YTDLPSessions:        make(map[string]*YTDLPSession),
		TorrentSessions:      make(map[string]*TorrentSession),
		ShutdownChan:         make(chan struct{}),
		Bot:                  bot,
		DB:                   db,
		LastStatusMsg:        make(map[int64]int),
		StatusPages:          make(map[int64]int),
		ProcessTaskFunc:      processTaskFunc,
		RefreshDashboardFunc: refreshDashboardFunc,
		Aria2Engine:          aria2Engine,
		YTDLPEngine:          ytdlpEngine,
		UserbotEngine:        downloader.NewUserbotEngine(cfg),
		LastDashUpdateAt:     make(map[int64]time.Time),
		LastDashProgressSum:  make(map[int64]float64),
		LastTasksCount:       make(map[int64]int),
		Config:               cfg,
		Auth:                 auth,
	}

	if dbInstance, ok := db.(*database.DB); ok {
		tm.CheckpointManager = recovery.NewCheckpointManager(dbInstance)
	}

	if db != nil {
		ctx := context.Background()
		activeTasks, err := db.GetActive(ctx)
		if err == nil {
			for _, rt := range activeTasks {
				taskCtx, cancel := context.WithCancel(context.Background())
				task := &Task{
					Task: domain.Task{
						ID:             rt.ID,
						GID:            rt.GID,
						Type:           domain.TaskType(rt.Type),
						Status:         domain.TaskStatus(rt.Status),
						URL:            rt.URL,
						FileName:       rt.FileName,
						LocalPath:      rt.LocalPath,
						RemotePath:     rt.RemotePath,
						RemoteURL:      rt.RemoteURL,
						Dest:           rt.Dest,
						Dest2:          rt.Dest2,
						TotalSize:      rt.TotalSize,
						DownloadedSize: rt.DownloadedSize,
						UploadedSize:   rt.UploadedSize,
						ChatID:         rt.ChatID,
						UserID:         rt.UserID,
						CreatedAt:      rt.CreatedAt,
						Zip:            rt.Zip,
						Unzip:          rt.Unzip,
						Password:       rt.Password,
						Error:          rt.Error,
						Ctx:            taskCtx,
						CancelFunc:     cancel,
						RetryCount:     rt.RetryCount,
						MaxRetries:     cfg.MaxRetries,
					},
					DB: db,
				}
				if rt.CompletedAt.Valid {
					task.CompletedAt = rt.CompletedAt.Time
				}

				if tm.CheckpointManager != nil {
					if cp, err := tm.CheckpointManager.GetCheckpoint(task.ID); err == nil && cp != nil {
						task.DownloadedSize = cp.DownloadedBytes
						task.TotalSize = cp.TotalBytes
						task.Progress = cp.Progress
						slog.Info("Restored task from checkpoint", "taskID", task.ID, "progress", task.Progress)
					}
				}

				tm.Tasks[task.ID] = task
				tm.Queue.Enqueue(task, queue.PriorityLow)
				select {
				case tm.QueueSignal <- struct{}{}:
				default:
				}
			}
			slog.Info("Loaded active tasks from database", "count", len(activeTasks))
		}
	}

	for i := 0; i < cfg.MaxConcurrentDownloads; i++ {
		go tm.worker(i)
	}

	go tm.startAutoRefresh()
	go tm.startCleanup()
	go tm.startRateLimitPersist()

	tm.Scheduler = NewScheduler(db, tm)
	tm.Scheduler.Start(context.Background())

	return tm
}

func (tm *TaskManager) startAutoRefresh() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ShutdownChan:
			return
		case <-ticker.C:
			tm.Mu.RLock()
			hasActive := len(tm.LastStatusMsg) > 0
			tm.Mu.RUnlock()
			if hasActive {
				tm.refreshActiveDashboards()
			}
		}
	}
}

func (tm *TaskManager) refreshActiveDashboards() {
	tm.Mu.RLock()
	var chats []int64
	for chatID := range tm.LastStatusMsg {
		chats = append(chats, chatID)
	}
	tm.Mu.RUnlock()

	for _, chatID := range chats {
		if tm.RefreshDashboardFunc != nil {
			tm.RefreshDashboardFunc(chatID, false)
		}
	}
}

func (tm *TaskManager) startCleanup() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ShutdownChan:
			return
		case <-ticker.C:
			tm.cleanupTerminalTasks()
		}
	}
}

func (tm *TaskManager) cleanupTerminalTasks() {
	cutoff := time.Now().Add(-CleanupInterval)

	tm.Mu.Lock()
	var toRemove []string
	activeChats := make(map[int64]struct{})
	for id, task := range tm.Tasks {
		var isTerminal bool
		var completedAt time.Time
		var chatID int64
		task.Read(func() {
			isTerminal = isTerminalStatus(task.Status)
			completedAt = task.CompletedAt
			chatID = task.ChatID
		})
		if isTerminal && !completedAt.IsZero() && completedAt.Before(cutoff) {
			toRemove = append(toRemove, id)
			continue
		}
		activeChats[chatID] = struct{}{}
	}

	for _, id := range toRemove {
		delete(tm.Tasks, id)
	}
	tm.Mu.Unlock()

	tm.StatusMu.Lock()
	for chatID := range tm.LastStatusMsg {
		if _, ok := activeChats[chatID]; !ok {
			delete(tm.LastStatusMsg, chatID)
			delete(tm.StatusPages, chatID)
			delete(tm.LastDashUpdateAt, chatID)
			delete(tm.LastDashProgressSum, chatID)
			delete(tm.LastTasksCount, chatID)
		}
	}
	tm.StatusMu.Unlock()

	tm.Mu.Lock()
	for gid := range tm.YTDLPSessions {
		if _, exists := tm.Tasks[gid]; !exists {
			delete(tm.YTDLPSessions, gid)
		}
	}
	for infoHash := range tm.TorrentSessions {
		if _, exists := tm.Tasks[infoHash]; !exists {
			delete(tm.TorrentSessions, infoHash)
		}
	}
	tm.Mu.Unlock()

	if len(toRemove) > 0 {
		slog.Info("Cleaned up terminal tasks from memory", "count", len(toRemove))
	}
}

func (tm *TaskManager) startRateLimitPersist() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-tm.ShutdownChan:
			return
		case <-ticker.C:
			tm.RateLimiter.Persist()
		}
	}
}

// isPrivileged routes queue privilege through the Authorizer seam; bare
// managers in tests fall back to the env-based check with identical semantics.
func (tm *TaskManager) isPrivileged(userID int64) bool {
	if tm.Auth != nil {
		return tm.Auth.IsPrivileged(userID)
	}
	return isEnvPrivileged(tm.Config, userID)
}

// isTerminalStatus is the single seam for terminal-state checks shared by
// Task, BatchTask, and cleanup paths.
func isTerminalStatus(s TaskStatus) bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

func (tm *TaskManager) validateTaskConstraints(url, quality string, userID int64) error {
	if tm.StopDuplicate {
		tm.Mu.RLock()
		for _, t := range tm.Tasks {
			var isFinished, sameURL, sameQuality bool
			t.Read(func() {
				isFinished = t.Status == StatusCompleted || t.Status == StatusFailed || t.Status == StatusCancelled
				sameURL = t.URL == url
				sameQuality = t.Quality == quality
			})

			if !isFinished && sameURL && sameQuality {
				tm.Mu.RUnlock()
				return fmt.Errorf("%w: ID %s", domain.ErrDuplicateTask, t.ID)
			}
		}
		tm.Mu.RUnlock()

		if tm.DB != nil && !tm.isPrivileged(userID) {
			oldTask, errDB := tm.DB.GetCompletedTaskByURL(context.Background(), url, quality)
			if errDB == nil && oldTask != nil {
				return fmt.Errorf("%w: file already exists in cloud/database", domain.ErrDuplicateTask)
			}
		}
	}

	if !tm.RateLimiter.Allow(userID) {
		return fmt.Errorf("%w: limit 5 tasks/min exceeded", domain.ErrLimitExceeded)
	}

	return nil
}

func (tm *TaskManager) CreateTask(taskType TaskType, url, fileName string, chatID int64, msgID, replyID int, userID int64, zip, unzip bool, password, quality string, expectedTotalSize int64, subtitleLangs string, hardsub bool) (*Task, error) {
	if err := tm.validateTaskConstraints(url, quality, userID); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	task := &Task{
		Task: domain.Task{
			ID:             uuid.New().String()[:12],
			Type:           taskType,
			Status:         StatusQueued,
			URL:            url,
			FileName:       fileName,
			OrigFileName:   fileName,
			ChatID:         chatID,
			MessageID:      msgID,
			ReplyMessageID: replyID,
			UserID:         userID,
			Zip:            zip,
			Unzip:          unzip,
			Password:       password,
			Quality:        quality,
			CreatedAt:      time.Now().UTC(),
			Ctx:            ctx,
			CancelFunc:     cancel,
			TotalSize:      expectedTotalSize,
			MaxRetries:     tm.Config.MaxRetries,
			SubtitleLangs:  subtitleLangs,
			Hardsub:        hardsub,
		},
		DB: tm.DB,
	}

	tm.Mu.Lock()
	tm.Tasks[task.ID] = task
	tm.Mu.Unlock()

	_ = task.SaveToDB()

	priority := queue.PriorityNormal
	if tm.isPrivileged(userID) {
		priority = queue.PriorityHigh
	}

	tm.Queue.Enqueue(task, priority)
	select {
	case tm.QueueSignal <- struct{}{}:
	default:
	}

	return task, nil
}

func (tm *TaskManager) CreatePlaylistParentTask(title, url string, chatID int64, msgID int, userID int64, totalItems int) (*Task, error) {
	ctx, cancel := context.WithCancel(context.Background())

	task := &Task{
		Task: domain.Task{
			ID:            uuid.New().String()[:12],
			Type:          TypePlaylist,
			Status:        StatusDownloading,
			URL:           url,
			FileName:      title,
			ChatID:        chatID,
			MessageID:     msgID,
			UserID:        userID,
			CreatedAt:     time.Now().UTC(),
			Ctx:           ctx,
			CancelFunc:    cancel,
			PlaylistCount: totalItems,
			TotalSize:     0,
			MaxRetries:    tm.Config.MaxRetries,
		},
		DB: tm.DB,
	}

	tm.Mu.Lock()
	tm.Tasks[task.ID] = task
	tm.Mu.Unlock()

	_ = task.SaveToDB()

	return task, nil
}

func (tm *TaskManager) CreatePlaylistSubTask(parent *Task, url, fileName string, index, total int, taskType TaskType) (*Task, error) {
	ctx, cancel := context.WithCancel(parent.Ctx)

	task := &Task{
		Task: domain.Task{
			ID:            fmt.Sprintf("%s_%d", parent.ID, index),
			Type:          taskType,
			Status:        StatusQueued,
			URL:           url,
			FileName:      fileName,
			ChatID:        parent.ChatID,
			UserID:        parent.UserID,
			CreatedAt:     time.Now().UTC(),
			Ctx:           ctx,
			CancelFunc:    cancel,
			PlaylistIndex: index,
			PlaylistCount: total,
			MaxRetries:    tm.Config.MaxRetries,
		},
		DB: tm.DB,
	}

	tm.Mu.Lock()
	tm.Tasks[task.ID] = task
	tm.Mu.Unlock()

	_ = task.SaveToDB()

	tm.Queue.Enqueue(task, queue.PriorityLow)
	select {
	case tm.QueueSignal <- struct{}{}:
	default:
	}

	return task, nil
}

func (t *Task) SaveToDB() error {
	if t.DB == nil {
		return nil
	}

	var record domain.TaskRecord
	t.Read(func() {
		record = domain.TaskRecord{
			ID:             t.ID,
			GID:            t.GID,
			Type:           string(t.Type),
			Status:         string(t.Status),
			URL:            t.URL,
			FileName:       t.FileName,
			LocalPath:      t.LocalPath,
			RemotePath:     t.RemotePath,
			RemoteURL:      t.RemoteURL,
			Dest:           t.Dest,
			Dest2:          t.Dest2,
			TotalSize:      t.TotalSize,
			DownloadedSize: t.DownloadedSize,
			UploadedSize:   t.UploadedSize,
			ChatID:         t.ChatID,
			UserID:         t.UserID,
			CreatedAt:      t.CreatedAt,
			Zip:            t.Zip,
			Unzip:          t.Unzip,
			Password:       t.Password,
			Error:          t.Error,
			Quality:        t.Quality,
			RetryCount:     t.RetryCount,
		}

		if !t.CompletedAt.IsZero() {
			record.CompletedAt = struct {
				Time  time.Time
				Valid bool
			}{Time: t.CompletedAt, Valid: true}
		}
	})

	ctx := context.Background()
	err := t.DB.Save(ctx, record)
	if err != nil {
		slog.Error("Failed to save task to database", "taskID", t.ID, "status", t.Status, "error", err)
	}
	return err
}

func (tm *TaskManager) GetTask(taskID string) *Task {
	tm.Mu.RLock()
	defer tm.Mu.RUnlock()
	return tm.Tasks[taskID]
}

func (tm *TaskManager) GetTaskByGID(gid string) *Task {
	tm.Mu.RLock()
	defer tm.Mu.RUnlock()
	for _, task := range tm.Tasks {
		if task.GID == gid {
			return task
		}
	}
	return nil
}

func (tm *TaskManager) GetActiveTasks() []*Task {
	tm.Mu.RLock()
	defer tm.Mu.RUnlock()

	var active []*Task
	for _, task := range tm.Tasks {
		if task.Status != StatusCompleted && task.Status != StatusFailed && task.Status != StatusCancelled {
			active = append(active, task)
		}
	}

	sort.Slice(active, func(i, j int) bool {
		return active[i].CreatedAt.Before(active[j].CreatedAt)
	})

	return active
}

func (tm *TaskManager) CancelTask(taskID string) bool {
	tm.Mu.Lock()
	task, exists := tm.Tasks[taskID]
	if exists {
		if tm.CheckpointManager != nil {
			if err := tm.CheckpointManager.DeleteCheckpoint(task.ID); err != nil {
				slog.Warn("Failed to delete checkpoint", "taskID", task.ID, "error", err)
			}
		}
		delete(tm.Tasks, taskID)
	}
	tm.Mu.Unlock()

	if !exists {
		return false
	}

	var shouldCancel bool
	task.Update(func() {
		if task.Status != StatusCompleted && task.Status != StatusCancelled {
			task.Status = StatusCancelled
			task.CompletedAt = time.Now().UTC()
			if task.CancelFunc != nil {
				task.CancelFunc()
			}
			shouldCancel = true
		}
	})

	if !shouldCancel {
		return false
	}

	_ = task.SaveToDB()
	return true
}

func (t *Task) Cancel(status TaskStatus) bool {
	var shouldCancel bool
	t.Update(func() {
		if t.Status != StatusCompleted && t.Status != StatusFailed && t.Status != StatusCancelled {
			t.Status = status
			if t.CancelFunc != nil {
				t.CancelFunc()
			}
			shouldCancel = true
		}
	})

	if !shouldCancel {
		return false
	}

	_ = t.SaveToDB()
	return true
}

func (tm *TaskManager) CancelAllTasks() int {
	tm.Mu.RLock()
	var taskIDs []string
	for id := range tm.Tasks {
		taskIDs = append(taskIDs, id)
	}
	tm.Mu.RUnlock()

	cancelledCount := 0
	for _, id := range taskIDs {
		if tm.CancelTask(id) {
			cancelledCount++
		}
	}
	return cancelledCount
}

func (tm *TaskManager) worker(_ int) {
	for {
		select {
		case <-tm.ShutdownChan:
			return
		case <-tm.QueueSignal:
			item := tm.Queue.DequeueNonBlocking()
			if item == nil {
				continue
			}
			task, ok := item.(*Task)
			if !ok {
				continue
			}

			tm.Semaphore <- struct{}{}

			tm.Mu.Lock()
			tm.ActiveCount++
			tm.Mu.Unlock()

			tm.Wg.Add(1)
			if tm.ProcessTaskFunc != nil {
				tm.ProcessTaskFunc(task)
			}
			tm.Wg.Done()

			<-tm.Semaphore

			tm.Mu.Lock()
			tm.ActiveCount--
			tm.Mu.Unlock()
		}
	}
}

func (tm *TaskManager) IsShuttingDown() bool {
	select {
	case <-tm.ShutdownChan:
		return true
	default:
		return false
	}
}

func (t *Task) UpdateFromProgressUpdate(up downloader.ProgressUpdate) {
	t.Update(func() {
		if up.FileName != "" {
			t.FileName = up.FileName
		}
		if up.Downloaded != 0 {
			t.DownloadedSize = up.Downloaded
		}
		if up.Total != 0 {
			t.TotalSize = up.Total
		}
		if up.Speed != 0 {
			t.Speed = up.Speed
		}
		t.Progress = up.Progress
		if up.Connections != 0 {
			t.Connections = up.Connections
		}
		if up.ETA != 0 {
			t.ETA = up.ETA
		}
		if up.Error != "" {
			t.Error = up.Error
		}
		if up.Message != "" {
			t.ProcessingMessage = up.Message
			t.Speed = 0
			t.ETA = 0
		}
	})
}

func (t *Task) UpdateFromUploadProgress(up uploader.ProgressUpdate) {
	t.Update(func() {
		if up.UploadedSize > 0 {
			t.UploadedSize = up.UploadedSize
		}
		if up.TotalSize > 0 {
			t.TotalSize = up.TotalSize
		}
		if up.Progress > 0 {
			t.Progress = up.Progress
		}
		if up.Speed > 0 {
			t.Speed = up.Speed
		}
		if up.ETA > 0 {
			t.ETA = up.ETA
		}
		if up.Error != "" {
			t.Error = up.Error
		}
	})
}

func calculateSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func (t *Task) SetProgress(progress float64) {
	t.Update(func() {
		t.Progress = progress
	})
}

func (t *Task) CompleteTelegramUpload(msgID int, uploadedSize int64, fileID, filePath string) {
	t.Update(func() {
		t.ResultMessageID = msgID
		t.Progress = 100
		t.UploadedSize = uploadedSize
		t.RemotePath = "telegram"
		t.TelegramFileID = fileID
		t.TelegramFilePath = filePath
	})
}

func (t *Task) SetStatus(status TaskStatus) {
	t.Update(func() {
		if t.Status == StatusCancelled && status != StatusCancelled {
			return
		}
		oldStatus := t.Status
		t.Status = status
		if status == StatusDownloading {
			if t.Type == TypeYTDLP || t.Type == TypeMirror || t.Type == TypeLeech || t.Type == TypeTorrent {
				t.Connections = 16
			}
		}
		if status == StatusCompleted || status == StatusFailed || status == StatusCancelled {
			t.CompletedAt = time.Now().UTC()
			if oldStatus != status {
				metrics.TasksTotal.WithLabelValues(string(t.Type), string(status)).Inc()
			}
		}
	})
	_ = t.SaveToDB()
}

func (t *Task) SetError(err string) {
	t.Update(func() {
		// Cancel wins: a task cancelled mid-flight must not flip to failed
		// when its context tears down, mirroring the SetStatus guard.
		if t.Status == StatusCancelled {
			return
		}
		t.Error = err
		t.Status = StatusFailed
		t.CompletedAt = time.Now().UTC()
		metrics.TasksTotal.WithLabelValues(string(t.Type), "failed").Inc()
	})
	_ = t.SaveToDB()
}

func (t *Task) GetSnapshot() domain.TaskSnapshot {
	return t.Task.GetSnapshot()
}

func (s *BotService) HandleConfirmCallback(callback *tgbotapi.CallbackQuery, parts []string) {
	_, _ = s.Bot.Request(tgbotapi.NewCallback(callback.ID, ""))
	if len(parts) < 2 {
		return
	}

	action := parts[1]
	switch action {
	case "yes":
		text := "✅ *Dikonfirmasi*"
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
		editMsg.ParseMode = MarkdownV2
		_, _ = s.Bot.Send(editMsg)
	case "no":
		text := "❌ *Dibatalkan*"
		editMsg := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, text)
		editMsg.ParseMode = MarkdownV2
		_, _ = s.Bot.Send(editMsg)
	}
}
