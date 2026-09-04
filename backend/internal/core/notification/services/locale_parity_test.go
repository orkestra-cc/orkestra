package services

import (
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
)

func TestCoreTemplatesExistInEverySupportedLocale(t *testing.T) {
	byID := map[string]map[string]bool{}
	for _, def := range defaultTemplates {
		if byID[def.TemplateID] == nil {
			byID[def.TemplateID] = map[string]bool{}
		}
		byID[def.TemplateID][def.Locale] = true
	}
	for id, locales := range byID {
		for _, want := range models.SupportedLocales {
			if !locales[want] {
				t.Errorf("core template %s has no %s translation", id, want)
			}
		}
	}
}

// TestAuthMFAFactorAddedTemplateIsSeeded pins the specific template spec
// D13 requires. TestCoreTemplatesExistInEverySupportedLocale above only
// checks that whatever IS in defaultTemplates covers every supported
// locale, so it passes vacuously when a template is missing entirely.
// SupportedLocales is ["en"], which makes the "it" block a convention
// rather than a gated requirement — this asserts both, matching the six
// EN/IT pairs default_templates.go already carries by hand.
func TestAuthMFAFactorAddedTemplateIsSeeded(t *testing.T) {
	locales := map[string]bool{}
	for _, def := range defaultTemplates {
		if def.TemplateID == models.TemplateAuthMFAFactorAdded {
			locales[def.Locale] = true
		}
	}
	for _, want := range []string{"en", "it"} {
		if !locales[want] {
			t.Errorf("template %s has no %s block", models.TemplateAuthMFAFactorAdded, want)
		}
	}
}
