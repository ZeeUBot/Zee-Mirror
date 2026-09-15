package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"zee-mirror/internal/repository"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib registration
)

type DB struct {
	*sql.DB
	driver string
}

var _ repository.TaskRepository = (*DB)(nil)
var _ repository.UserRepository = (*DB)(nil)
var _ repository.SettingsRepository = (*DB)(nil)
var _ repository.ScheduledTaskRepository = (*DB)(nil)
var _ repository.AuditRepository = (*DB)(nil)

func NewDB(driverName, configDir, dsn, migrationsDir string) (*DB, error) {
	switch driverName {
	case "postgres":
		return newPostgresDB(dsn, migrationsDir)
	default:
		return newSQLiteDB(configDir, dsn, migrationsDir)
	}
}

func newSQLiteDB(configDir, dsn, migrationsDir string) (*DB, error) {
	dbPath := dsn
	if dbPath == "" {
		dbPath = filepath.Join(configDir, "zee-mirror.db")
	}

	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	instance := &DB{DB: db, driver: "sqlite"}
	if err := instance.RunMigrations(migrationsDir); err != nil {
		slog.Error("Database migration failed", "error", err)
		return nil, err
	}

	return instance, nil
}

func newPostgresDB(dsn, migrationsDir string) (*DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is required for postgres driver")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	instance := &DB{DB: db, driver: "postgres"}
	if err := instance.RunMigrations(migrationsDir); err != nil {
		slog.Error("Database migration failed", "error", err)
		return nil, err
	}

	return instance, nil
}

func (db *DB) nowExpr() string {
	if db.driver == "postgres" {
		return "NOW()"
	}
	return "datetime('now')"
}

func (db *DB) nowMinusExpr(duration string) string {
	if db.driver == "postgres" {
		return fmt.Sprintf("NOW() - INTERVAL '%s'", duration)
	}
	return fmt.Sprintf("datetime('now', '-%s')", duration)
}

func (db *DB) RunMigrations(migrationsDir string) error {
	var driver database.Driver
	var err error

	switch db.driver {
	case "postgres":
		driver, err = postgres.WithInstance(db.DB, &postgres.Config{})
		if err != nil {
			return fmt.Errorf("failed to create postgres migrate driver: %w", err)
		}
	default:
		driver, err = sqlite.WithInstance(db.DB, &sqlite.Config{})
		if err != nil {
			return fmt.Errorf("failed to create sqlite migrate driver: %w", err)
		}
	}

	migrateDir := migrationsDir
	if db.driver == "postgres" {
		migrateDir = filepath.Join(migrationsDir, "postgres")
		absMigrateDir, absErr := filepath.Abs(migrateDir)
		if absErr == nil {
			migrateDir = strings.ReplaceAll(absMigrateDir, "\\", "/")
		}
		if _, statErr := os.Stat(migrateDir); os.IsNotExist(statErr) {
			return fmt.Errorf("postgres migrations directory not found: %s", migrateDir)
		}
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://"+migrateDir,
		db.driver, driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		if strings.Contains(err.Error(), "Dirty database version 1") {
			slog.Warn("Database is dirty at version 1, attempting to force version 1 and retry...")
			if errForce := m.Force(1); errForce != nil {
				return fmt.Errorf("failed to force migration: %w", errForce)
			}
			if errRetry := m.Up(); errRetry != nil && errRetry != migrate.ErrNoChange {
				return fmt.Errorf("migration failed after force: %w", errRetry)
			}
			slog.Info("Database migration fixed and completed successfully")
			return nil
		}
		return err
	}

	slog.Info("Database migrations completed successfully")
	return nil
}

func (db *DB) Ping(ctx context.Context) error {
	return db.PingContext(ctx)
}
