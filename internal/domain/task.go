package domain

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

type TaskStatus string

const (
	StatusQueued      TaskStatus = "queued"
	StatusDownloading TaskStatus = "downloading"
	StatusFetching    TaskStatus = "fetching"
	StatusExtracting  TaskStatus = "extracting"
	StatusZipping     TaskStatus = "zipping"
	StatusUploading   TaskStatus = "uploading"
	StatusCompleted   TaskStatus = "completed"
	StatusFailed      TaskStatus = "failed"
	StatusCancelled   TaskStatus = "cancelled"
	StatusExpired     TaskStatus = "expired"
)

type TaskType string

const (
	TypeMirror     TaskType = "mirror"
	TypeLeech      TaskType = "leech"
	TypeYTDLP      TaskType = "ytdlp"
	TypeYTDLPLeech TaskType = "ytdlp_leech"
	TypeTorrent    TaskType = "torrent"
	TypeClone      TaskType = "clone"
	TypeViking     TaskType = "viking"
	TypePlaylist   TaskType = "playlist"
)

const (
	MaxTelegramUploadSize int64 = 2 * 1024 * 1024 * 1024
)

const (
	BtnTextCloudLink = "☁️ Cloud Link"
	BtnTextIndexURL  = "🌐 Index URL"
)

type Task struct {
	CompletedAt       time.Time
	CreatedAt         time.Time
	StartedAt         time.Time
	Ctx               context.Context
	CancelFunc        context.CancelFunc
	Type              TaskType
	Status            TaskStatus
	ID                string
	GID               string
	URL               string
	FileName          string
	LocalPath         string
	RemotePath        string
	Dest              string
	Dest2             string
	RemoteURL         string
	TelegramFileID    string
	TelegramFilePath  string
	NotifyURL         string
	Error             string
	Password          string
	Quality           string
	MD5               string
	OrigFileName      string
	SubtitleLangs     string
	ProcessingMessage string
	TotalSize         int64
	Connections       int
	ETA               time.Duration
	DownloadedSize    int64
	UploadedSize      int64
	Speed             int64
	UserID            int64
	ChatID            int64
	Progress          float64
	PlaylistIndex     int
	MessageID         int
	ResultMessageID   int
	ReplyMessageID    int
	RetryCount        int
	MaxRetries        int
	PlaylistCount     int
	Mu                sync.RWMutex
	Zip               bool
	Unzip             bool
	Hardsub           bool
}

type TaskRecord struct {
	CreatedAt   time.Time
	CompletedAt sql.NullTime
	ID          string
	GID         string
	Type        string
	Status      string
	URL         string
	FileName    string
	LocalPath   string
	RemotePath  string
	Dest        string
	Dest2       string
	RemoteURL   string
	NotifyURL   string
	// #nosec G117 -- archive password must never leave the server via API JSON
	Password       string `json:"-"`
	Error          string
	Quality        string
	MD5            string
	TotalSize      int64
	DownloadedSize int64
	UploadedSize   int64
	ChatID         int64
	UserID         int64
	RetryCount     int
	Zip            bool
	Unzip          bool
}

type UserStats struct {
	LastActive      time.Time
	Username        string
	UserID          int64
	TotalBandwidth  int64
	TotalDownloads  int
	TotalUploads    int
	SuccessfulTasks int
	FailedTasks     int
}

type DailyStats struct {
	Date           time.Time
	TotalTasks     int
	CompletedTasks int
	FailedTasks    int
	TotalBandwidth int64
	AverageSpeed   int64
	PeakConcurrent int
}

type TaskSnapshot struct {
	CreatedAt        time.Time  `json:"createdAt"`
	StartedAt        time.Time  `json:"startedAt"`
	CompletedAt      time.Time  `json:"completedAt"`
	Status           TaskStatus `json:"status"`
	ID               string     `json:"id"`
	GID              string     `json:"gid"`
	URL              string     `json:"url"`
	FileName         string     `json:"fileName"`
	LocalPath        string     `json:"localPath"`
	RemotePath       string     `json:"remotePath"`
	Dest             string     `json:"dest,omitempty"`
	Dest2            string     `json:"dest2,omitempty"`
	RemoteURL        string     `json:"remoteURL"`
	TelegramFileID   string     `json:"telegramFileID,omitempty"`
	TelegramFilePath string     `json:"telegramFilePath,omitempty"`
	NotifyURL        string     `json:"-"`
	Error            string     `json:"error"`
	// #nosec G117 -- archive password must never leave the server via API/websocket JSON
	Password          string        `json:"-"`
	Quality           string        `json:"quality"`
	MD5               string        `json:"md5,omitempty"`
	OrigFileName      string        `json:"origFileName"`
	SubtitleLangs     string        `json:"subtitleLangs,omitempty"`
	ProcessingMessage string        `json:"processingMessage,omitempty"`
	Type              TaskType      `json:"type"`
	DownloadedSize    int64         `json:"downloadedSize"`
	MessageID         int           `json:"messageID"`
	TotalSize         int64         `json:"totalSize"`
	UploadedSize      int64         `json:"uploadedSize"`
	Speed             int64         `json:"speed"`
	ChatID            int64         `json:"chatID"`
	UserID            int64         `json:"userID"`
	Progress          float64       `json:"progress"`
	Connections       int           `json:"connections"`
	ETA               time.Duration `json:"eta"`
	ResultMessageID   int           `json:"resultMessageID"`
	ReplyMessageID    int           `json:"replyMessageID"`
	RetryCount        int           `json:"retryCount"`
	MaxRetries        int           `json:"maxRetries"`
	PlaylistCount     int           `json:"playlistCount,omitempty"`
	PlaylistIndex     int           `json:"playlistIndex,omitempty"`
	Zip               bool          `json:"zip"`
	Unzip             bool          `json:"unzip"`
	Hardsub           bool          `json:"hardsub,omitempty"`
}

func (t *Task) GetSnapshot() TaskSnapshot {
	t.Mu.RLock()
	defer t.Mu.RUnlock()
	return TaskSnapshot{
		ID:                t.ID,
		GID:               t.GID,
		Type:              t.Type,
		Status:            t.Status,
		URL:               t.URL,
		FileName:          t.FileName,
		LocalPath:         t.LocalPath,
		RemotePath:        t.RemotePath,
		Dest:              t.Dest,
		Dest2:             t.Dest2,
		RemoteURL:         t.RemoteURL,
		TelegramFileID:    t.TelegramFileID,
		TelegramFilePath:  t.TelegramFilePath,
		NotifyURL:         t.NotifyURL,
		TotalSize:         t.TotalSize,
		DownloadedSize:    t.DownloadedSize,
		UploadedSize:      t.UploadedSize,
		Speed:             t.Speed,
		Connections:       t.Connections,
		ETA:               t.ETA,
		Progress:          t.Progress,
		Error:             t.Error,
		ChatID:            t.ChatID,
		MessageID:         t.MessageID,
		UserID:            t.UserID,
		CreatedAt:         t.CreatedAt,
		StartedAt:         t.StartedAt,
		CompletedAt:       t.CompletedAt,
		Zip:               t.Zip,
		Unzip:             t.Unzip,
		Password:          t.Password,
		Quality:           t.Quality,
		MD5:               t.MD5,
		OrigFileName:      t.OrigFileName,
		ResultMessageID:   t.ResultMessageID,
		ReplyMessageID:    t.ReplyMessageID,
		RetryCount:        t.RetryCount,
		MaxRetries:        t.MaxRetries,
		PlaylistCount:     t.PlaylistCount,
		PlaylistIndex:     t.PlaylistIndex,
		SubtitleLangs:     t.SubtitleLangs,
		Hardsub:           t.Hardsub,
		ProcessingMessage: t.ProcessingMessage,
	}
}

func (t *Task) Update(fn func()) {
	t.Mu.Lock()
	defer t.Mu.Unlock()
	fn()
}

func (t *Task) Read(fn func()) {
	t.Mu.RLock()
	defer t.Mu.RUnlock()
	fn()
}

type TaskFilter struct {
	Status string
	UserID int64
	Limit  int
	Offset int
}

type TaskCheckpoint struct {
	LastUpdate      time.Time
	TaskID          string
	DownloadedBytes int64
	TotalBytes      int64
	Progress        float64
}
