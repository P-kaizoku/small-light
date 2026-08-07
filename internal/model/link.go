package model

import (
	"time"

	"github.com/google/uuid"
)

type Link struct {
	ID          uuid.UUID  `json:"id"`
	OwnerID     uuid.UUID  `json:"owner_id"`
	OriginalURL string     `json:"original_url"`
	ShortCode   string     `json:"short_code"`
	ClickCount  int        `json:"click_count"`
	ExpiresAt   time.Time  `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"-"`
}
