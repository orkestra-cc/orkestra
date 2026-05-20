package iface

import (
	"reflect"
	"strings"
	"testing"
)

// TestNewUserDefaultsLanguage locks in that fresh users carry the
// platform default language tag — the SPA reads /me on bootstrap and
// expects a non-empty BCP-47 value to drive react-i18next.
func TestNewUserDefaultsLanguage(t *testing.T) {
	t.Parallel()

	u := NewUser()
	if u.Language != DefaultLanguage {
		t.Errorf("NewUser().Language = %q, want %q", u.Language, DefaultLanguage)
	}
	if DefaultLanguage != "en" {
		t.Errorf("DefaultLanguage = %q, want \"en\" — i18n plan calls English the platform default", DefaultLanguage)
	}
}

// TestUpdateUserInputLanguageAllowlist guards the oneof validate tag
// on UpdateUserInput.Language. The list is the contract for which
// languages the SPA ships translations for — drifting silently would
// leave the field documented as allowlisted while accepting anything.
// Extend both this test and the SPA's locale files in lockstep when
// adding a new locale.
func TestUpdateUserInputLanguageAllowlist(t *testing.T) {
	t.Parallel()

	field, ok := reflect.TypeOf(UpdateUserInput{}).FieldByName("Language")
	if !ok {
		t.Fatal("UpdateUserInput.Language missing — i18n plan Phase 1 contract")
	}
	tag := field.Tag.Get("validate")
	if !strings.Contains(tag, "omitempty") {
		t.Errorf("validate tag missing omitempty: %q", tag)
	}
	if !strings.Contains(tag, "oneof=en it") {
		t.Errorf("validate tag missing oneof=en it: %q — add locales to the allowlist when extending", tag)
	}
}

// TestToResponseIncludesLanguage guards the user/CLAUDE.md invariant
// that every new field on User must be reflected in UserManagementResponse.
// A forgotten mapping would silently strip the language from /me
// responses without a test failure on the auth handler.
func TestToResponseIncludesLanguage(t *testing.T) {
	t.Parallel()

	u := NewUser()
	u.Language = "it"
	resp := u.ToResponse()
	if resp.Language != "it" {
		t.Errorf("ToResponse().Language = %q, want %q", resp.Language, "it")
	}
}
