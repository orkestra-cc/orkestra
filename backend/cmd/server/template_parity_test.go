package main

import (
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	sdkmodule "github.com/orkestra/backend/pkg/sdk/module"
)

// Runs over the real catalog, so an addon that ships a template in one
// language only fails the build here rather than failing sends in
// production.
func TestModuleTemplatesExistInEverySupportedLocale(t *testing.T) {
	byID := map[string]map[string]bool{}
	for _, factory := range optionalModules {
		for _, spec := range sdkmodule.NotificationTemplatesOf(factory()) {
			if byID[spec.TemplateID] == nil {
				byID[spec.TemplateID] = map[string]bool{}
			}
			byID[spec.TemplateID][spec.Locale] = true
		}
	}
	for id, locales := range byID {
		for _, want := range models.SupportedLocales {
			if !locales[want] {
				t.Errorf("module template %s has no %s translation", id, want)
			}
		}
	}
}
