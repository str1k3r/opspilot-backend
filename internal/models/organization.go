package models

import "time"

type Organization struct {
	ID        string    `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	Slug      string    `db:"slug" json:"slug"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

type CreateOrganizationInput struct {
	Name string `json:"name" validate:"required,min=2,max=255"`
	Slug string `json:"slug" validate:"required,min=2,max=63,slug"`
}
