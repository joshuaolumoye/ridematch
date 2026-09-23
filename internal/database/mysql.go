// Package database wires up connections to MySQL (via GORM) and Redis.
package database

import (
	"fmt"
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ridematch-backend/internal/config"
)

// NewMySQL opens a GORM connection to MySQL and configures the underlying
// connection pool. It does not run migrations — see cmd/migrate.
func NewMySQL(cfg *config.Config) (*gorm.DB, error) {
	logLevel := logger.Warn
	if !cfg.IsProduction() {
		logLevel = logger.Info
	}

	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logLevel),
		DisableForeignKeyConstraintWhenMigrating: false,
	})
	if err != nil {
		return nil, fmt.Errorf("database: failed to connect to mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database: failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	log.Println("database: mysql connection established")
	return db, nil
}
