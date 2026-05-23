// Package handlers wires the marketing services to Huma request/
// response shapes. Each resource has its own file. Mutating routes
// require the `marketing.contact.write` or `marketing.contact.delete`
// permission, declared in marketing/module.go::Permissions(); the
// route bucket in marketing/routes.go applies the right gate per
// chi subgroup.
package handlers

import "time"

// PaginatedQuery is the common pagination input for list endpoints —
// limit defaults to 50 (cap 500 at the repository), cursor here is
// a simple offset/skip-style integer. Future revisions can swap for
// opaque cursors without changing the URL surface.
type PaginatedQuery struct {
	Limit int64 `query:"limit" doc:"Page size, defaults to 50, capped at 500"`
	Skip  int64 `query:"skip" doc:"Number of items to skip"`
}

// SuccessResponse is the empty-but-typed shape returned by delete
// endpoints. Huma needs a typed return so the OpenAPI dump can model
// it; an empty struct would also work but exposing Success makes the
// payload self-documenting.
type SuccessResponse struct {
	Body struct {
		Success bool `json:"success"`
	}
}

// ListMeta is the pagination envelope returned alongside list payloads.
type ListMeta struct {
	Limit int64 `json:"limit"`
	Skip  int64 `json:"skip"`
	Count int   `json:"count" doc:"Number of items in this page"`
}

// timestampedView captures the fields every read response carries.
// Composed into specific resource view types so each one stays
// explicit at the OpenAPI layer.
type timestampedView struct {
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
