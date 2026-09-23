// Command migrate creates or updates all database tables to match the
// current model definitions. It is safe to run repeatedly: GORM's
// AutoMigrate only adds missing tables/columns/indexes, it never drops or
// alters existing data.
//
// Usage:
//
//	go run ./cmd/migrate
package main

import (
	"log"

	"ridematch-backend/internal/config"
	"ridematch-backend/internal/database"
	"ridematch-backend/internal/models"
)

func main() {
	cfg := config.Load()

	db, err := database.NewMySQL(cfg)
	if err != nil {
		log.Fatalf("migrate: %v", err)
	}

	log.Println("migrate: running auto-migration...")

	err = db.AutoMigrate(
		&models.User{},
		&models.DriverProfile{},
		&models.OTPRequest{},
		&models.RefreshToken{},
		&models.Trip{},
		&models.TripOffer{},
		&models.TripRating{},
		&models.PaymentTransaction{},
	)
	if err != nil {
		log.Fatalf("migrate: auto-migration failed: %v", err)
	}

	log.Println("migrate: all tables are up to date")
}
