package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"zee-mirror/internal/config"

	"github.com/getsentry/sentry-go"
	"gopkg.in/natefinch/lumberjack.v2"
)

func setupLogger(cfg *config.Config) {
	logPath := filepath.Join(cfg.ConfigDir, "zee-mirror.log")

	logFile := &lumberjack.Logger{
		Filename:   filepath.Clean(logPath),
		MaxSize:    10,
		MaxBackups: 5,
		MaxAge:     28,
		Compress:   true,
	}

	multi := io.MultiWriter(os.Stdout, logFile)

	var level slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		handler = slog.NewJSONHandler(multi, opts)
	} else {
		handler = slog.NewTextHandler(multi, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	banner := `
  ______ ______ ______     __  __ _____ _____  _____   ____  _____
 |___  /|  ____|  ____|   |  \/  |_   _|  __ \|  __ \ / __ \|  __ \
    / / | |__  | |__      | \  / | | | | |__) | |__) | |  | | |__) |
   / /  |  __| |  __|     | |\/| | | | |  _  /|  _  /| |  | |  _  /
  / /__ | |____| |____    | |  | |_| |_| | \ \| | \ \| |__| | | \ \
 /_____||______|______|   |_|  |_|_____|_|  \_\_|  \_\\____/|_|  \_\`

	_, _ = fmt.Fprintln(multi, banner)

	slog.Info("Logging to file enabled", "format", cfg.LogFormat)
}

func initSentry(cfg *config.Config) {
	if cfg.SentryDSN == "" {
		return
	}

	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.SentryDSN,
		Environment:      getEnvOrDefault("APP_ENV", "production"),
		Release:          getEnvOrDefault("APP_RELEASE", "unknown"),
		TracesSampleRate: 0.2,
	}); err != nil {
		slog.Warn("Failed to initialize Sentry", "error", err)
		return
	}

	slog.Info("Sentry error tracking initialized")
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
