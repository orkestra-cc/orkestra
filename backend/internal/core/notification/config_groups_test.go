package notification

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestConfigGroups_DeclarationsValidate(t *testing.T) {
	m := &NotificationModule{}
	if err := module.ValidateConfigDeclarations(m.ConfigSchema(), m.ConfigGroups()); err != nil {
		t.Errorf("ValidateConfigDeclarations: %v", err)
	}
}

func TestConfigGroups_EveryFieldGrouped(t *testing.T) {
	m := &NotificationModule{}
	declared := map[string]bool{}
	for _, g := range m.ConfigGroups() {
		declared[g.Key] = true
	}
	for _, f := range m.ConfigSchema() {
		if !declared[f.Group] {
			t.Errorf("field %q references undeclared group %q", f.Key, f.Group)
		}
	}
}

func TestSMTPFields_GatedOnProvider(t *testing.T) {
	// The five SMTP connection fields are dead weight under the default noop
	// provider; they appear only once the provider is switched to smtp. The
	// provider selector itself is never gated — it is how you switch.
	gated := map[string]bool{
		"email.smtp.host": true, "email.smtp.port": true,
		"email.smtp.username": true, "email.smtp.password": true,
		"email.smtp.tls_mode": true,
	}
	seen := map[string]bool{}
	for _, f := range (&NotificationModule{}).ConfigSchema() {
		if gated[f.Key] {
			seen[f.Key] = true
			if len(f.DependsOn) != 1 ||
				f.DependsOn[0].Key != "email.provider" ||
				len(f.DependsOn[0].In) != 1 || f.DependsOn[0].In[0] != "smtp" {
				t.Errorf("field %q DependsOn = %+v, want [email.provider in [smtp]]", f.Key, f.DependsOn)
			}
		}
		if f.Key == "email.provider" && len(f.DependsOn) != 0 {
			t.Errorf("email.provider must not be gated, got DependsOn %+v", f.DependsOn)
		}
	}
	for k := range gated {
		if !seen[k] {
			t.Errorf("gated field %q not found in schema", k)
		}
	}
}

func TestEnumConversions(t *testing.T) {
	want := map[string][]string{
		"email.provider":      {"noop", "smtp"},
		"email.smtp.tls_mode": {"starttls", "tls", "none"},
	}
	for _, f := range (&NotificationModule{}).ConfigSchema() {
		opts, ok := want[f.Key]
		if !ok {
			continue
		}
		if f.Type != module.FieldEnum {
			t.Errorf("field %q Type = %v, want FieldEnum", f.Key, f.Type)
		}
		if len(f.Options) != len(opts) {
			t.Errorf("field %q Options = %v, want %v", f.Key, f.Options, opts)
			continue
		}
		for i := range opts {
			if f.Options[i] != opts[i] {
				t.Errorf("field %q Options[%d] = %q, want %q", f.Key, i, f.Options[i], opts[i])
			}
		}
	}
}
