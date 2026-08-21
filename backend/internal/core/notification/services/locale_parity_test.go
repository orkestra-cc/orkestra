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
