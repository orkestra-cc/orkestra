package models

import "time"

// NavOverride is a persisted ordering override for one parent's children.
// One document per parent — parentKey is either:
//   - an item's ItemKey (when reordering nested children), or
//   - a synthetic root key of the form "__root.<realm>.<slug(section)>"
//     (when reordering top-level items inside one (realm, section) bucket)
//
// The synthetic-key form is generated server-side and surfaced in the
// admin response so the frontend never has to compute it.
//
// OrderedChildren is a list of ItemKey values; items present in the list
// take the listed position, items missing from the list keep their
// declared order and append after the overridden block. Unknown ItemKeys
// are dropped on read with a slog.Warn — modules renaming/removing items
// auto-heal stale overrides without an admin migration.
type NavOverride struct {
	ParentKey       string    `bson:"_id" json:"parentKey"`
	OrderedChildren []string  `bson:"orderedChildren" json:"orderedChildren"`
	UpdatedAt       time.Time `bson:"updatedAt" json:"updatedAt"`
	UpdatedBy       string    `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
}
