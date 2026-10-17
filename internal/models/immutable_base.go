package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ImmutableBase is Base without soft delete. Money-related rows (wallets,
// ledger rows, escrow holds, withdrawals, appeals) must never silently
// disappear from queries, and several carry unique indexes that a
// soft-deleted ghost row would otherwise permanently occupy. They are
// never deleted at all; their lifecycle is a status column.
type ImmutableBase struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeCreate assigns a UUID when the caller did not set one.
func (b *ImmutableBase) BeforeCreate(_ *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}
