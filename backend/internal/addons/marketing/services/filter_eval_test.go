package services

import (
	"testing"

	"github.com/orkestra-cc/orkestra-addon-marketing/models"
)

// TestEvaluatePersonFilterNilFilter — no filter means every person
// passes. Common case for profiles with no targeting.
func TestEvaluatePersonFilterNilFilter(t *testing.T) {
	person := &models.Person{FirstName: "Jane", Tags: []string{"any"}}
	if !EvaluatePersonFilter(nil, person) {
		t.Errorf("nil filter should accept every person")
	}
}

// TestEvaluatePersonFilterNilPerson — a stale snapshot reference must
// fail safely rather than NPE.
func TestEvaluatePersonFilterNilPerson(t *testing.T) {
	filter := &models.ProfileFilter{TagsInclude: []string{"hot"}}
	if EvaluatePersonFilter(filter, nil) {
		t.Errorf("nil person should not pass any non-nil filter")
	}
}

// TestEvaluatePersonFilterTagsInclude — at-least-one-of semantics.
func TestEvaluatePersonFilterTagsInclude(t *testing.T) {
	cases := []struct {
		name       string
		personTags []string
		include    []string
		want       bool
	}{
		{"single-match", []string{"hot"}, []string{"hot"}, true},
		{"one-of-many", []string{"warm", "hot"}, []string{"hot", "vip"}, true},
		{"none-match", []string{"cold"}, []string{"hot", "vip"}, false},
		{"empty-person", nil, []string{"hot"}, false},
		{"empty-include", []string{"hot"}, []string{}, true}, // no clause → no filtering
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &models.ProfileFilter{TagsInclude: c.include}
			p := &models.Person{Tags: c.personTags}
			if got := EvaluatePersonFilter(f, p); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestEvaluatePersonFilterTagsExclude — none-of semantics. Persons
// carrying any excluded tag are rejected even when TagsInclude would
// have accepted them.
func TestEvaluatePersonFilterTagsExclude(t *testing.T) {
	cases := []struct {
		name       string
		personTags []string
		exclude    []string
		want       bool
	}{
		{"clean", []string{"hot"}, []string{"do-not-contact"}, true},
		{"hit-one", []string{"hot", "do-not-contact"}, []string{"do-not-contact"}, false},
		{"hit-multi", []string{"opted-out"}, []string{"do-not-contact", "opted-out"}, false},
		{"empty-person", nil, []string{"x"}, true},
		{"empty-exclude", []string{"hot"}, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &models.ProfileFilter{TagsExclude: c.exclude}
			p := &models.Person{Tags: c.personTags}
			if got := EvaluatePersonFilter(f, p); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestEvaluatePersonFilterIncludeAndExclude — exclude wins. Person
// matching both an include and an exclude tag is rejected.
func TestEvaluatePersonFilterIncludeAndExclude(t *testing.T) {
	f := &models.ProfileFilter{
		TagsInclude: []string{"hot"},
		TagsExclude: []string{"do-not-contact"},
	}
	p := &models.Person{Tags: []string{"hot", "do-not-contact"}}
	if EvaluatePersonFilter(f, p) {
		t.Errorf("exclude must win over include")
	}
}

// TestEvaluatePersonFilterCustomFields — reuses the scoring matcher,
// so $in / $eq / $exists / implicit equality all work.
func TestEvaluatePersonFilterCustomFields(t *testing.T) {
	cases := []struct {
		name   string
		bag    map[string]any
		filter map[string]any
		want   bool
	}{
		{
			"implicit-eq-match",
			map[string]any{"industry": "tech"},
			map[string]any{"industry": "tech"},
			true,
		},
		{
			"implicit-eq-miss",
			map[string]any{"industry": "retail"},
			map[string]any{"industry": "tech"},
			false,
		},
		{
			"$in-hit",
			map[string]any{"industry": "tech"},
			map[string]any{"industry": map[string]any{"$in": []any{"manufacturing", "tech"}}},
			true,
		},
		{
			"$in-miss",
			map[string]any{"industry": "retail"},
			map[string]any{"industry": map[string]any{"$in": []any{"manufacturing", "tech"}}},
			false,
		},
		{
			"$exists-true-absent",
			map[string]any{},
			map[string]any{"industry": map[string]any{"$exists": true}},
			false,
		},
		{
			"$ne-different",
			map[string]any{"industry": "retail"},
			map[string]any{"industry": map[string]any{"$ne": "tech"}},
			true,
		},
		{
			"multi-key-all-match",
			map[string]any{"industry": "tech", "size": "enterprise"},
			map[string]any{
				"industry": map[string]any{"$in": []any{"tech"}},
				"size":     "enterprise",
			},
			true,
		},
		{
			"multi-key-one-miss",
			map[string]any{"industry": "tech", "size": "smb"},
			map[string]any{
				"industry": "tech",
				"size":     "enterprise",
			},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &models.ProfileFilter{CustomFieldFilters: c.filter}
			p := &models.Person{CustomFields: c.bag}
			if got := EvaluatePersonFilter(f, p); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// TestEvaluatePersonFilterAllClauses — combined include + exclude +
// customFields. Every clause must pass.
func TestEvaluatePersonFilterAllClauses(t *testing.T) {
	f := &models.ProfileFilter{
		TagsInclude:        []string{"lead"},
		TagsExclude:        []string{"do-not-contact"},
		CustomFieldFilters: map[string]any{"industry": "tech"},
	}
	cases := []struct {
		name string
		p    *models.Person
		want bool
	}{
		{
			"all-pass",
			&models.Person{
				Tags:         []string{"lead"},
				CustomFields: map[string]any{"industry": "tech"},
			},
			true,
		},
		{
			"include-fail",
			&models.Person{
				Tags:         []string{"customer"},
				CustomFields: map[string]any{"industry": "tech"},
			},
			false,
		},
		{
			"exclude-fail",
			&models.Person{
				Tags:         []string{"lead", "do-not-contact"},
				CustomFields: map[string]any{"industry": "tech"},
			},
			false,
		},
		{
			"customField-fail",
			&models.Person{
				Tags:         []string{"lead"},
				CustomFields: map[string]any{"industry": "retail"},
			},
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EvaluatePersonFilter(f, c.p); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
