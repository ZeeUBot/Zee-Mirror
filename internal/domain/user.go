package domain

import (
	"database/sql"
	"time"
)

type User struct {
	ExpiresAt         sql.NullTime `json:"expiresAt"`
	CreatedAt         time.Time    `json:"createdAt"`
	Username          string       `json:"username"`
	Role              string       `json:"role"`
	Language          string       `json:"language"`
	APIKey            string       `json:"apiKey,omitempty"`
	ID                int64        `json:"id"`
	MaxDailyBandwidth int64        `json:"maxDailyBandwidth"`
	MaxDailyTasks     int          `json:"maxDailyTasks"`
	IsActive          bool         `json:"isActive"`
}
