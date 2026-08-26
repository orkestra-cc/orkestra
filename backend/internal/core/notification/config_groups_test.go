package notification

import (
	"testing"

	"github.com/orkestra/backend/internal/core/notification/services"
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

func TestSMTPHost_RequiredWhenVisible(t *testing.T) {
	// A host is the one SMTP setting a real send cannot do without, so the
	// admin UI must flag it once the provider reveals it (required-when-
	// visible). The other four stay optional: unauthenticated relays are
	// legitimate and port/tls_mode carry defaults.
	for _, f := range (&NotificationModule{}).ConfigSchema() {
		switch f.Key {
		case "email.smtp.host":
			if !f.Required {
				t.Errorf("email.smtp.host must be Required (required-when-visible)")
			}
		case "email.smtp.port", "email.smtp.username", "email.smtp.password", "email.smtp.tls_mode":
			if f.Required {
				t.Errorf("field %q must not be Required", f.Key)
			}
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

// TestSenderItems_SchemaDriverAgreement: for every driver, every sub-field
// the schema marks Required AND visible under that provider must be one the
// driver requires (secret or not — a required secret is a console-side
// hint, D5); and every field the driver requires must be visible under that
// provider and either Required or defaulted. A noop profile with
// nothing but a slug and a label must be savable.
func TestSenderItems_SchemaDriverAgreement(t *testing.T) {
	items := services.SenderItems()
	visible := func(it module.ConfigItemField, provider string) bool {
		if len(it.DependsOn) == 0 {
			return true
		}
		for _, c := range it.DependsOn {
			if c.Key != services.SubProvider {
				t.Fatalf("sub-field %q depends on %q; only provider is expected", it.Key, c.Key)
			}
			for _, v := range c.In {
				if v == provider {
					return true
				}
			}
		}
		return false
	}
	for _, d := range services.CoreDrivers(nil) {
		required := map[string]bool{} // every requirement, secrets included
		for _, r := range d.Requires() {
			required[r.Key] = true
		}
		for _, it := range items {
			if it.Key == services.SubProvider {
				continue // the selector itself is required and never gated
			}
			schemaRequired := it.Required && visible(it, d.Name())
			if schemaRequired && !required[it.Key] {
				t.Errorf("driver %s: schema requires %q but the driver does not — the console would block Save on a field the driver never reads", d.Name(), it.Key)
			}
			if required[it.Key] && !visible(it, d.Name()) {
				t.Errorf("driver %s: requires %q but the schema hides it under that provider", d.Name(), it.Key)
			}
			if required[it.Key] && !it.Required && it.Default == "" {
				t.Errorf("driver %s: requires %q but the schema neither marks it required nor defaults it", d.Name(), it.Key)
			}
		}
	}
	// noop: nothing visible is required.
	for _, it := range items {
		if it.Key != services.SubProvider && it.Required && visible(it, "noop") {
			t.Errorf("a noop profile must be savable with nothing but a slug and a label; %q is required and visible", it.Key)
		}
	}
}

func TestSendersField_Declared(t *testing.T) {
	for _, f := range (&NotificationModule{}).ConfigSchema() {
		if f.Key == services.SendersField {
			if f.Type != module.FieldRecordList || f.Group != "senders" || len(f.Items) == 0 {
				t.Fatalf("email.senders must be a recordList in group senders with items: %+v", f)
			}
			return
		}
	}
	t.Fatal("email.senders not declared")
}
