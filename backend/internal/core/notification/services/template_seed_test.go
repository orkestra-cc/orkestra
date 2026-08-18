package services

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// Module-declared templates are seeded exactly like the built-in ones,
// including the insert-if-absent rule: an operator who edited a template
// must not have it overwritten on the next boot.
func TestSeedModuleTemplatesInsertsAndPreserves(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	specs := []module.NotificationTemplateSpec{{
		TemplateID: "subscriptions.renewal.ok", Locale: "en",
		Subject: "Renewed", BodyText: "text", BodyHTML: "<p>html</p>",
	}}

	if err := svc.SeedModuleTemplates(context.Background(), specs); err != nil {
		t.Fatalf("SeedModuleTemplates: %v", err)
	}
	got := repo.docs[tplKey("subscriptions.renewal.ok", "en")]
	if got == nil {
		t.Fatal("template not seeded")
	}
	if !got.IsSystem {
		t.Error("IsSystem = false, want true — module defaults are system templates")
	}

	// Operator edits the subject, then the app restarts.
	got.Subject = "Edited by operator"
	if err := svc.SeedModuleTemplates(context.Background(), specs); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	if repo.docs[tplKey("subscriptions.renewal.ok", "en")].Subject != "Edited by operator" {
		t.Error("re-seeding overwrote an operator edit")
	}
}
