// Package database wires up connections to SQL databases (via GORM) and Redis.
package database

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ridematch-backend/internal/config"
)

// NewDB opens the configured SQL database and configures the connection pool.
// It does not run migrations — see cmd/migrate.
func NewDB(cfg *config.Config) (*gorm.DB, error) {
	logLevel := logger.Warn
	if !cfg.IsProduction() {
		logLevel = logger.Info
	}

	gormConfig := &gorm.Config{
		Logger:                                   logger.Default.LogMode(logLevel),
		DisableForeignKeyConstraintWhenMigrating: false,
		TranslateError:                           true,
	}

	var db *gorm.DB
	var err error
	switch cfg.DBDriver {
	case "mysql":
		db, err = gorm.Open(mysql.Open(cfg.DSN()), gormConfig)
	case "postgres", "postgresql":
		db, err = gorm.Open(postgres.Open(cfg.PostgresDSN()), gormConfig)
	default:
		return nil, fmt.Errorf("database: unsupported DB_DRIVER %q (expected mysql or postgres)", cfg.DBDriver)
	}
	if err != nil {
		return nil, fmt.Errorf("database: failed to connect to %s: %w", cfg.DBDriver, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database: failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	log.Printf("database: %s connection established", cfg.DBDriver)
	return db, nil
}
