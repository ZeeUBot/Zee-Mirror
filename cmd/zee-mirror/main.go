package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	"zee-mirror/handlers/download"
	"zee-mirror/handlers/search"
	"zee-mirror/internal/api"
	"zee-mirror/internal/config"
	"zee-mirror/internal/database"
	"zee-mirror/internal/metrics"
	"zee-mirror/internal/router"
	"zee-mirror/internal/service"
	"zee-mirror/internal/userbot"
	"zee-mirror/pkg/utils"
	"zee-mirror/plugins/torrent"

	"github.com/getsentry/sentry-go"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "modernc.org/sqlite"

	_ "zee-mirror/plugins/drive"
	_ "zee-mirror/plugins/mega"
	_ "zee-mirror/plugins/telegram"
	_ "zee-mirror/plugins/ytdlp"
)

func main() {
	cfg := config.LoadConfig()

	if err := os.MkdirAll(cfg.ConfigDir, 0750); err != nil {
		slog.Error("Failed to create config directory", "dir", cfg.ConfigDir, "error", err)
	}
	setupLogger(cfg)
	initSentry(cfg)

	slog.Info("Starting Zee-Mirror Bot...")

	if err := cfg.Validate(); err != nil {
		slog.Error("Invalid configuration", "error", err)
		os.Exit(1)
	}

	db, err := database.NewDB(cfg.DBDriver, cfg.ConfigDir, cfg.DatabaseURL, "migrations")
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}

	bots, err := service.InitBots(cfg.BotTokens, cfg.TelegramAPI)
	if err != nil || len(bots) == 0 {
		slog.Error("Failed to initialize any bot instances", "error", err)
		os.Exit(1)
	}

	primaryBot := bots[0].Bot
	slog.Info("Authorized on account", "username", primaryBot.Self.UserName, "totalBots", len(bots))

	ub := userbot.GetInstance(cfg)
	if err := ub.Start(); err != nil {
		slog.Warn("Userbot failed to start", "error", err)
	}

	aria2Daemon := torrent.NewAria2Daemon(cfg.ConfigDir)
	if err := aria2Daemon.Start(); err != nil {
		slog.Error("Failed to start aria2 daemon", "error", err)
	}
	defer aria2Daemon.Stop()

	botSvc := service.NewBotService(primaryBot, cfg, db, db.DB)
	go search.StartSearchSessionCleanup(botSvc.TaskManager.ShutdownChan)

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			slog.Info("Received SIGHUP, reloading config...")
			newCfg := config.Reload()
			botSvc.TaskManager.Mu.Lock()
			botSvc.TaskManager.Config = newCfg
			botSvc.TaskManager.MaxConcurrent = newCfg.MaxConcurrentDownloads
			botSvc.TaskManager.StopDuplicate = newCfg.StopDuplicate
			botSvc.TaskManager.Mu.Unlock()
			slog.Info("TaskManager config updated")
		}
	}()

	apiServer := api.NewServer(botSvc, cfg.DashboardPort)

	r := router.NewRouter(botSvc)
	setupRoutes(r)
	registerBotMenu(primaryBot, r)

	r.RegisterMagnetHandler(func(s *service.BotService, m *tgbotapi.Message) {
		text := m.Text
		if text == "" {
			text = m.Caption
		}
		download.HandleTorrent(s, m, text)
	})

	apiServer.SetRouter(r)
	apiServer.Start()

	go func() {
		time.Sleep(2 * time.Second)
		recovery := service.NewTaskRecovery(db, botSvc.TaskManager)
		if err := recovery.RecoverIncompleteTasks(); err != nil {
			slog.Warn("Failed to auto-recover tasks", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(cfg.DownloadDir); err == nil {
					size, _ := utils.CalculateDirSize(cfg.DownloadDir)
					metrics.StorageUsage.WithLabelValues(cfg.DownloadDir).Set(float64(size))
					slog.Debug("Storage usage metric updated", "size", size)
				}
			}
		}
	}()

	if cfg.UseWebhook && cfg.WebhookURL != "" {
		slog.Info("🌐 Starting in WEBHOOK mode", "webhook_url", cfg.WebhookURL)

		if err := apiServer.SetupWebhook(); err != nil {
			slog.Error("Failed to setup webhook, falling back to polling", "error", err)
			for _, bi := range bots {
				startPolling(ctx, bi.Bot, botSvc, r)
			}
		} else {
			slog.Info("✅ Webhook mode active. Waiting for updates from Telegram...")
			<-ctx.Done()
			slog.Info("Shutting down gracefully (webhook mode)...")
			if err := apiServer.RemoveWebhook(); err != nil {
				slog.Warn("Failed to remove webhook on shutdown", "error", err)
			}
		}
	} else {
		slog.Info("📡 Starting in LONG POLLING mode", "botCount", len(bots))
		_ = apiServer.RemoveWebhook()
		for _, bi := range bots {
			startPolling(ctx, bi.Bot, botSvc, r)
		}
	}

	slog.Info("Initiating global shutdown sequence...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := apiServer.Stop(shutdownCtx); err != nil {
		slog.Warn("API Server shutdown error", "error", err)
	}

	botSvc.Shutdown()
	ub.Stop()
	aria2Daemon.Stop()

	sentry.Flush(5 * time.Second)
	slog.Info("Zee-Mirror Bot has been gracefully shut down.")
}
