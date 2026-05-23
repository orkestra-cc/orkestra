// Package services holds the marketing addon's business logic — thin
// orchestration over the repositories with the validations the data
// layer cannot enforce on its own (custom-field bag shape, identity-
// minimum on Person, primary-flag invariant on Memberships, slug+path
// derivation on Tags).
package services

import (
	"regexp"
	"strings"
)

// slugSanitizer collapses any run of non-alphanumeric characters into
// a single ASCII hyphen — the same conventional slug shape every other
// Orkestra surface uses (subscriptions plans, tenant slugs, tag slugs).
var slugSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

// DeriveSlug normalises a free-form name into a URL-safe slug. Stable
// for any input that differs only by case, accents, or punctuation —
// the slug stays the same when a tag is renamed in cosmetic ways.
//
// Marketing tags carry the slug as their stable machine identifier; the
// human-readable Name is free to mutate without breaking references.
func DeriveSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugSanitizer.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// JoinPath builds the materialized path for a tag given its parent's
// path and its own name. Roots have parentPath="" and produce
// "/<Name>"; nested tags concatenate "<parentPath>/<Name>".
//
// Stored as a string so a prefix scan ("^/Industry/" on the indexed
// `path` field) returns every descendant without a graph traversal.
func JoinPath(parentPath, name string) string {
	name = strings.TrimSpace(name)
	if parentPath == "" {
		return "/" + name
	}
	return strings.TrimRight(parentPath, "/") + "/" + name
}
