package services

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizePatterns(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"auth.*", "", "crm.*"}, []string{"auth.*", "crm.*"}}, // an empty entry never becomes a match-everything pattern
		{[]string{" Auth.* ", "auth.*", "AUTH.*"}, []string{"auth.*"}}, // trim + lowercase + within-profile dedup
		{[]string{"*", "auth.x", "*"}, []string{"*", "auth.x"}},        // order kept, first wins
		{nil, []string{}},
		{[]string{"  "}, []string{}},
	}
	for _, c := range cases {
		if got := NormalizePatterns(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizePatterns(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	valid := []string{"*", "auth.verify_email", "auth.*", "auth.oauth.*", "marketing", "crm-2.x_1.*", "crm/campaign", "vendor:event.*", "événement.*"}
	invalid := []string{"", ".", ".*", "auth*", "auth.*.google", "*.auth", "a..b", "auth.", "auth.**", "*auth", "au*th.x", "a,b", "a,b.*"}
	for _, p := range valid {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range invalid {
		if err := ValidatePattern(p); !errors.Is(err, ErrPatternInvalid) {
			t.Errorf("ValidatePattern(%q) = %v, want ErrPatternInvalid", p, err)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, category string
		matched           bool
		literal           int
	}{
		{"*", "anything", true, 0},
		{"*", "", true, 0},
		{"auth.x", "auth.x", true, 6},
		{"auth.x", "auth.x.y", false, 6},
		{"auth.*", "auth.x", true, 5},
		{"auth.*", "auth.oauth.google", true, 5}, // any depth
		{"auth.*", "auth", false, 5},             // never the bare token
		{"auth.*", "auth.", false, 5},            // at least one further character
		{"auth.*", "authx.y", false, 5},
		{"auth.oauth.*", "auth.oauth.google", true, 11},
		{"marketing", "marketing", true, 9},
		{"marketing.*", "marketing", false, 10}, // a category with no dot is never captured by a prefix rule
	}
	for _, c := range cases {
		m, lit := MatchPattern(c.pattern, c.category)
		if m != c.matched || lit != c.literal {
			t.Errorf("MatchPattern(%q, %q) = (%v, %d), want (%v, %d)", c.pattern, c.category, m, lit, c.matched, c.literal)
		}
	}
}
