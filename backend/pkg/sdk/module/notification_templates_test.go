package module

import "testing"

type tmplModule struct{ BaseModule }

func (tmplModule) Name() string             { return "tmpl" }
func (tmplModule) Category() ModuleCategory { return CategoryToggleable }
func (tmplModule) Init(*Dependencies) error { return nil }
func (tmplModule) NotificationTemplates() []NotificationTemplateSpec {
	return []NotificationTemplateSpec{{
		TemplateID: "tmpl.hello", Locale: "en", Subject: "Hello",
		BodyText: "hi", BodyHTML: "<p>hi</p>",
	}}
}

type plainModule struct{ BaseModule }

func (plainModule) Name() string             { return "plain" }
func (plainModule) Category() ModuleCategory { return CategoryToggleable }
func (plainModule) Init(*Dependencies) error { return nil }

// A module that declares templates has them collected; one that does not
// returns nil rather than panicking — the accessor is how the registry
// stays ignorant of which modules opted in.
func TestNotificationTemplatesOf(t *testing.T) {
	got := NotificationTemplatesOf(tmplModule{})
	if len(got) != 1 || got[0].TemplateID != "tmpl.hello" {
		t.Fatalf("NotificationTemplatesOf = %+v, want one tmpl.hello spec", got)
	}
	if got := NotificationTemplatesOf(plainModule{}); got != nil {
		t.Errorf("NotificationTemplatesOf(plain) = %+v, want nil", got)
	}
}
