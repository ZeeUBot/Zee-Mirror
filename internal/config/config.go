package config

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"zee-mirror/internal/crypto"
	"zee-mirror/pkg/utils"

	"github.com/joho/godotenv"
)

type Config struct {
	TelegramAPI              string
	IndexURL                 string
	RcloneLogLevel           string
	RcloneDest               string
	DownloadDir              string
	ConfigDir                string
	DashboardToken           string
	LogLevel                 string
	LogFormat                string
	AppEnv                   string
	SentryDSN                string
	BotToken                 string
	DatabaseURL              string
	DashboardURL             string
	AppHash                  string
	RcloneTransfers          string
	Aria2RPCURL              string
	Aria2RPCSecret           string
	WebhookURL               string
	VikingUserHash           string
	UserSessionString        string
	WebhookSecret            string
	RclonePacerBurst         string
	RclonePacerMinSleep      string
	RcloneBufferSize         string
	RcloneDestFallbacks      []string
	RcloneDriveChunkSize     string
	RcloneCheckers           string
	DBDriver                 string
	encryptionKeyHex         string
	BotTokens                []string
	AuthorizedUsers          []int64
	EncryptionKey            []byte
	AutoCleanupDays          int
	DefaultMaxDailyTasks     int
	MaxConcurrentDownloads   int
	MaxRetries               int
	AppID                    int
	OwnerID                  int64
	DefaultMaxDailyBandwidth int64
	DashboardPort            int
	SmartAutoOrganization    bool
	StopDuplicate            bool
	UseWebhook               bool
}

func LoadConfig() *Config {
	cfg := &Config{
		BotToken:                 os.Getenv("BOT_TOKEN"),
		TelegramAPI:              os.Getenv("TELEGRAM_API"),
		RcloneDest:               os.Getenv("RCLONE_DEST"),
		DownloadDir:              getEnv("DOWNLOAD_DIR", "/app/downloads"),
		ConfigDir:                getEnv("CONFIG_DIR", "/app/config"),
		MaxConcurrentDownloads:   getEnvInt("MAX_CONCURRENT_DOWNLOADS", 3),
		DashboardToken:           getEnv("WEB_DASHBOARD_TOKEN", "zee-mirror-secret"),
		DashboardPort:            getEnvInt("DASHBOARD_PORT_INTERNAL", 8080),
		LogLevel:                 getEnv("LOG_LEVEL", "info"),
		LogFormat:                getEnv("LOG_FORMAT", "text"),
		AppEnv:                   getEnv("APP_ENV", ""),
		SentryDSN:                os.Getenv("SENTRY_DSN"),
		SmartAutoOrganization:    getEnvBool("SMART_AUTO_ORGANIZATION", false),
		IndexURL:                 os.Getenv("INDEX_URL"),
		DashboardURL:             getEnv("WEB_DASHBOARD_URL", "127.0.0.1"),
		DefaultMaxDailyTasks:     getEnvInt("DEFAULT_MAX_DAILY_TASKS", -1),
		DefaultMaxDailyBandwidth: utils.ParseBytesString(getEnv("DEFAULT_MAX_DAILY_BANDWIDTH", "-1")),
		StopDuplicate:            getEnvBool("STOP_DUPLICATE", false),
		MaxRetries:               getEnvInt("MAX_RETRIES", 3),
		VikingUserHash:           os.Getenv("VIKING_USER_HASH"),
		AppID:                    getEnvInt("APP_ID", 0),
		AppHash:                  os.Getenv("APP_HASH"),
		UserSessionString:        os.Getenv("USER_SESSION_STRING"),
		Aria2RPCURL:              getEnv("ARIA2_RPC_URL", "http://localhost:6800/jsonrpc"),
		Aria2RPCSecret:           os.Getenv("ARIA2_RPC_SECRET"),
		WebhookURL:               os.Getenv("WEBHOOK_URL"),
		WebhookSecret:            getEnv("WEBHOOK_SECRET", ""),
		UseWebhook:               getEnvBool("USE_WEBHOOK", false),
		RcloneTransfers:          getEnv("RCLONE_TRANSFERS", "10"),
		RcloneCheckers:           getEnv("RCLONE_CHECKERS", "20"),
		RcloneDriveChunkSize:     getEnv("RCLONE_DRIVE_CHUNK_SIZE", "256M"),
		RcloneBufferSize:         getEnv("RCLONE_BUFFER_SIZE", "128M"),
		RcloneDestFallbacks:      splitCSV(os.Getenv("RCLONE_DEST_FALLBACKS")),
		RclonePacerMinSleep:      getEnv("RCLONE_PACER_MIN_SLEEP", "10ms"),
		RclonePacerBurst:         getEnv("RCLONE_PACER_BURST", "200"),
		RcloneLogLevel:           getEnv("RCLONE_LOG_LEVEL", "NOTICE"),
		AutoCleanupDays:          getEnvInt("AUTO_CLEANUP_DAYS", 30),
		DBDriver:                 getEnv("DB_DRIVER", "sqlite"),
		DatabaseURL:              os.Getenv("DATABASE_URL"),
		encryptionKeyHex:         os.Getenv("ENCRYPTION_KEY"),
	}

	if keyHex := cfg.encryptionKeyHex; keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err == nil && len(key) == 32 {
			cfg.EncryptionKey = key
			cfg.UserSessionString = decryptConfigValue(cfg.UserSessionString, key)
			cfg.BotToken = decryptConfigValue(cfg.BotToken, key)
			cfg.DashboardToken = decryptConfigValue(cfg.DashboardToken, key)
			slog.Info("Encryption key loaded, sensitive values decrypted")
		} else {
			slog.Warn("Invalid ENCRYPTION_KEY: must be 32-byte hex", "length", len(key))
		}
	}

	if ownerIDStr := os.Getenv("OWNER_ID"); ownerIDStr != "" {
		if id, err := strconv.ParseInt(ownerIDStr, 10, 64); err == nil {
			cfg.OwnerID = id
		}
	}

	if authUsers := os.Getenv("AUTHORIZED_USERS"); authUsers != "" {
		for _, idStr := range strings.Split(authUsers, ",") {
			idStr = strings.TrimSpace(idStr)
			if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
				cfg.AuthorizedUsers = append(cfg.AuthorizedUsers, id)
			}
		}
	}

	if botTokens := os.Getenv("BOT_TOKENS"); botTokens != "" {
		for _, t := range strings.Split(botTokens, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				cfg.BotTokens = append(cfg.BotTokens, decryptConfigValue(t, cfg.EncryptionKey))
			}
		}
	} else if cfg.BotToken != "" {
		cfg.BotTokens = []string{cfg.BotToken}
	}

	return cfg
}

var currentConfig atomic.Value

func init() {
	_ = godotenv.Load()
	currentConfig.Store(LoadConfig())
}

func Reload() *Config {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to load .env file", "error", err)
	}
	newCfg := LoadConfig()
	if newCfg.BotToken == "" {
		slog.Error("Reloaded config has empty BOT_TOKEN, keeping previous config")
		return currentConfig.Load().(*Config)
	}
	currentConfig.Store(newCfg)
	slog.Info("Config reloaded successfully",
		"maxConcurrent", newCfg.MaxConcurrentDownloads,
		"stopDuplicate", newCfg.StopDuplicate,
		"logLevel", newCfg.LogLevel,
	)
	return newCfg
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	str := getEnv(key, "")
	if n, err := strconv.Atoi(str); err == nil {
		return n
	}
	return fallback
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnvBool(key string, fallback bool) bool {
	str := getEnv(key, "")
	if str == "" {
		return fallback
	}
	b, err := strconv.ParseBool(str)
	if err != nil {
		return fallback
	}
	return b
}

func decryptConfigValue(val string, key []byte) string {
	if !strings.HasPrefix(val, "enc:") {
		return val
	}
	decoded, err := crypto.Decrypt(strings.TrimPrefix(val, "enc:"), key)
	if err != nil {
		slog.Warn("Failed to decrypt config value", "error", err)
		return val
	}
	return string(decoded)
}

func (c *Config) Validate() error {
	if len(c.BotTokens) == 0 {
		return fmt.Errorf("BOT_TOKEN or BOT_TOKENS must be set")
	}
	if c.OwnerID == 0 {
		return fmt.Errorf("OWNER_ID must be set")
	}
	if strings.EqualFold(c.AppEnv, "production") &&
		(c.DashboardToken == "" || c.DashboardToken == "zee-mirror-secret") {
		return fmt.Errorf("WEB_DASHBOARD_TOKEN must be set to a secure value when APP_ENV=production (empty or default token is not allowed)")
	}
	if c.UseWebhook && c.WebhookSecret == "" {
		return fmt.Errorf("WEBHOOK_SECRET must be set when USE_WEBHOOK=true (webhook endpoint is unauthenticated without it)")
	}
	if c.RcloneDest == "" {
		slog.Warn("RCLONE_DEST not set, using default: gdrive:/MirrorBot")
		c.RcloneDest = "gdrive:/MirrorBot"
	}
	return nil
}
