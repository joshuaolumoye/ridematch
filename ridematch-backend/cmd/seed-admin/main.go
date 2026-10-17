// Command seed-admin creates or promotes an account to the "admin" role,
// so it can sign in to the admin dashboard through the same OTP flow
// every other account uses (POST /auth/otp/request + /auth/otp/verify) —
// there is no separate admin authentication system, a JWT's `role` claim
// already gates every /api/v1/admin/* route via middleware.RequireRole,
// and role is otherwise only ever set at account creation. This command
// is the one missing piece: a way to actually get the first admin
// account into the database.
//
// Usage:
//
//	go run ./cmd/seed-admin -identifier=08012345678 -name="Josh"
//	go run ./cmd/seed-admin -identifier=admin@ridematch.ng -name="Josh"
//
// If an account with that phone/email already exists, it's promoted to
// role=admin (and reactivated if it was suspended/banned) and left
// otherwise untouched. Otherwise a new admin account is created.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"

	"ridematch-backend/internal/config"
	"ridematch-backend/internal/database"
	"ridematch-backend/internal/models"
	"ridematch-backend/internal/repository"
	"ridematch-backend/internal/utils"
)

func main() {
	identifier := flag.String("identifier", "", "phone number or email address of the account to make an admin (required)")
	name := flag.String("name", "Admin", "display name to use if a new account is created")
	flag.Parse()

	if *identifier == "" {
		log.Fatal("seed-admin: -identifier is required (a phone number or email address)")
	}

	normalized, channel, err := utils.NormalizeIdentifier(*identifier)
	if err != nil {
		log.Fatalf("seed-admin: %v", err)
	}

	cfg := config.Load()
	db, err := database.NewMySQL(cfg)
	if err != nil {
		log.Fatalf("seed-admin: %v", err)
	}

	users := repository.NewUserRepository(db)
	ctx := context.Background()

	var existing *models.User
	if channel == utils.ChannelPhone {
		existing, err = users.FindByPhone(ctx, normalized)
	} else {
		existing, err = users.FindByEmail(ctx, normalized)
	}

	switch {
	case err == nil:
		existing.Role = models.RoleAdmin
		existing.Status = models.StatusActive
		if updateErr := users.Update(ctx, existing); updateErr != nil {
			log.Fatalf("seed-admin: failed to promote existing account: %v", updateErr)
		}
		fmt.Printf("seed-admin: promoted existing account %s (%s) to admin\n", existing.ID, normalized)

	case errors.Is(err, repository.ErrNotFound):
		user := &models.User{
			Name:   *name,
			Role:   models.RoleAdmin,
			Status: models.StatusActive,
		}
		if channel == utils.ChannelPhone {
			user.Phone = normalized
		} else {
			user.Email = normalized
		}
		if createErr := users.Create(ctx, user); createErr != nil {
			log.Fatalf("seed-admin: failed to create admin account: %v", createErr)
		}
		fmt.Printf("seed-admin: created new admin account %s (%s)\n", user.ID, normalized)

	default:
		log.Fatalf("seed-admin: failed to look up account: %v", err)
	}

	fmt.Println("seed-admin: this account can now sign in via the normal OTP flow (phone/email -> code) and will receive an admin-scoped access token.")
}
