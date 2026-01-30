package models

import "time"

type User struct {
	ID           string    `json:"id" db:"id"`
	OrgID        string    `json:"org_id" db:"org_id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}
