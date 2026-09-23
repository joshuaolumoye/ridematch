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
	"fmt"
	"log"

	"gorm.io/gorm"

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

	// users.phone used to be a required, unique column (phone-only auth).
	// It's now optional (email sign-up is supported too, and Phone/Email
	// are plain Go strings rather than *string — see the comment on
	// models.User for why uniqueness is enforced in AuthService instead
	// of the database). AutoMigrate never drops an existing index on its
	// own, so the old unique constraint has to be dropped explicitly
	// first, or a second email-only account would fail to insert. This is
	// a no-op after the first run.
	if err := dropIndexIfExists(db, "users", "idx_users_phone"); err != nil {
		log.Fatalf("migrate: failed to drop legacy unique index on users.phone: %v", err)
	}
	// Same OTP requests table used to key off a "phone" column; it's now
	// "identifier" (phone or email). AutoMigrate adds the new column
	// alongside the old one rather than renaming it, so drop the old one
	// once it's no longer referenced by any code path. Safe: OTP requests
	// are short-lived, already-expired rows with no lasting value.
	if err := dropColumnIfExists(db, "otp_requests", "phone"); err != nil {
		log.Fatalf("migrate: failed to drop legacy otp_requests.phone column: %v", err)
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

func dropIndexIfExists(db *gorm.DB, table, index string) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?`,
		table, index,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	log.Printf("migrate: dropping legacy index %s on %s...", index, table)
	return db.Exec(fmt.Sprintf("ALTER TABLE `%s` DROP INDEX `%s`", table, index)).Error
}

func dropColumnIfExists(db *gorm.DB, table, column string) error {
	var count int64
	if err := db.Raw(
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		table, column,
	).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	log.Printf("migrate: dropping legacy column %s.%s...", table, column)
	return db.Exec(fmt.Sprintf("ALTER TABLE `%s` DROP COLUMN `%s`", table, column)).Error
}
