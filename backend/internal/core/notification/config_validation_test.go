package notification

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestNotificationModuleImplementsConfigValidatorHooks(t *testing.T) {
	var m module.Module = NewModule()
	if _, ok := m.(module.HasConfigValidator); !ok {
		t.Fatal("notification must implement HasConfigValidator")
	}
	if _, ok := m.(module.HasConfigActivationValidator); !ok {
		t.Fatal("notification must implement HasConfigActivationValidator")
	}
}

// Both hooks must share one policy function: a map broken in the third
// state must be rejected at PATCH time AND must not be promotable.
func TestNotificationConfigValidation_BothHooksAgree(t *testing.T) {
	broken := map[string]string{
		module.RosterKey(services.SendersField):                            "a",
		module.ItemKey(services.SendersField, "a", services.SubProvider):   "noop",
		module.ItemKey(services.SendersField, "a", services.SubCategories): "auth.*",
	}
	legacy := map[string]string{"app.name": "Orkestra", "email.provider": "smtp"}
	m := NewModule()
	hooks := []struct {
		name string
		call func(map[string]string) error
	}{
		{"ValidateConfig", func(v map[string]string) error { return m.ValidateConfig(context.Background(), v) }},
		{"ValidateConfigActivation", func(v map[string]string) error { return m.ValidateConfigActivation(context.Background(), v) }},
	}
	for _, h := range hooks {
		if err := h.call(legacy); err != nil {
			t.Errorf("%s: legacy map must pass, got %v", h.name, err)
		}
		var ve *module.ConfigValidationError
		if err := h.call(broken); !errors.As(err, &ve) || ve.Code != errcode.NotificationSenderNoDefault {
			t.Errorf("%s: want sender_no_default, got %v", h.name, err)
		}
	}
	// A zero-value module (as the declaration tests build it) must still validate.
	if err := (&NotificationModule{}).ValidateConfig(context.Background(), broken); err == nil {
		t.Error("zero-value module must build its registry lazily and still reject")
	}
}

// --- one-click unsubscribe (require_one_click_unsubscribe) -----------------

// The field's Description is the only place an operator is told what turning
// the requirement off actually accepts. There is no module-level warning
// channel in the SDK to say it anywhere else (see the task report), so this
// pins the wording that matters rather than merely the field's existence.
func TestConfigSchema_RequireOneClickUnsubscribeIsOnByDefaultAndSaysWhatOffMeans(t *testing.T) {
	var f *module.ConfigField
	for i, c := range (&NotificationModule{}).ConfigSchema() {
		if c.Key == requireOneClickKey {
			f = &(&NotificationModule{}).ConfigSchema()[i]
			break
		}
	}
	if f == nil {
		t.Fatalf("ConfigSchema must declare %q", requireOneClickKey)
	}
	if f.Type != module.FieldBool || f.Default != "true" {
		t.Fatalf("field must be a bool defaulting to true, got type %q default %q", f.Type, f.Default)
	}
	if f.EnvVar == "" || f.Group != "delivery" {
		t.Fatalf("field must live in the delivery group with an env var, got group %q env %q", f.Group, f.EnvVar)
	}
	for _, want := range []string{"without", "one-click"} {
		if !strings.Contains(strings.ToLower(f.Description), want) {
			t.Fatalf("the description must spell out what turning this off accepts; %q is missing from %q", want, f.Description)
		}
	}
}

// marketingOnMailUp is one complete, selectable marketing profile on the one
// core driver that cannot place the headers on the wire.
func marketingOnMailUp() map[string]string {
	return map[string]string{
		module.RosterKey(services.SendersField):                                "mkt",
		module.ItemKey(services.SendersField, "mkt", services.SubProvider):     "mailup",
		module.ItemKey(services.SendersField, "mkt", services.SubAllowedTypes): "marketing",
		module.ItemKey(services.SendersField, "mkt", services.SubFromAddress):  "f@x",
		module.ItemKey(services.SendersField, "mkt", services.SubMailUpUser):   "s1_2",
	}
}

// An absent require_one_click_unsubscribe is the schema default (true), not
// "off": a document written before this field existed must be judged by the
// requirement, not slip past it.
func TestValidateConfig_AbsentRequirementReadsAsRequired(t *testing.T) {
	m := NewModule()
	values := marketingOnMailUp()
	values["public_api_base_url"] = "https://api.example"

	var ve *module.ConfigValidationError
	if err := m.ValidateConfig(context.Background(), values); !errors.As(err, &ve) ||
		ve.Code != errcode.NotificationSenderDriverNoOneClick {
		t.Fatalf("want sender_driver_no_one_click, got %v", err)
	}
	// Activation applies the same policy.
	if err := m.ValidateConfigActivation(context.Background(), values); !errors.As(err, &ve) ||
		ve.Code != errcode.NotificationSenderDriverNoOneClick {
		t.Fatalf("activation: want sender_driver_no_one_click, got %v", err)
	}
}

func TestValidateConfig_ExplicitlyWaivedRequirementAdmits(t *testing.T) {
	m := NewModule()
	values := marketingOnMailUp()
	values["public_api_base_url"] = "https://api.example"
	values[requireOneClickKey] = "false"

	if err := m.ValidateConfig(context.Background(), values); err != nil {
		t.Fatalf("an operator who turned the requirement off takes the consequence knowingly: %v", err)
	}
}
