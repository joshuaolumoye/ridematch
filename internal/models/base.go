// Package models defines the GORM entities that make up the persistence
// layer. Every table uses a UUID primary key (rather than an auto-increment
// int) so IDs are safe to expose in API responses and never leak sequence
// information.
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base is embedded by every model to provide a UUID primary key and
// standard timestamp/soft-delete columns.
type Base struct {
	ID        string         `gorm:"type:char(36);primaryKey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate assigns a new UUIDv4 to the primary key if one hasn't
// already been set by the caller.
func (b *Base) BeforeCreate(tx *gorm.DB) error {
	if b.ID == "" {
		b.ID = uuid.NewString()
	}
	return nil
}
