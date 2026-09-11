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
// the requirement off actually accepts: the SDK has no module-level warning
// channel that could say it anywhere else in the admin UI. So this pins the
// wording that matters rather than merely the field's existence.
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

// The waiver is the one direction that cannot be allowed to happen by
// accident, so it is spelled explicitly and everything else keeps the
// requirement on. In particular "TRUE" and "on" — which the platform's
// ordinary GetConfigBool parsing resolves to false — must NOT waive.
func TestOneClickWaived_OnlyExplicitlyFalseValuesWaive(t *testing.T) {
	for _, v := range []string{"false", "FALSE", "False", " false ", "0", "no", "NO"} {
		if !oneClickWaived(v) {
			t.Errorf("%q is an operator saying off; it must waive", v)
		}
	}
	for _, v := range []string{"true", "TRUE", "True", "1", "yes", "on", "enabled", "treu", "", "   "} {
		if oneClickWaived(v) {
			t.Errorf("%q must leave the requirement in force, not waive it", v)
		}
	}
}

// The same rule through the save gate, which is where an operator actually
// meets it. Before the inversion a stored "TRUE" waived the requirement and
// this profile — marketing on a driver that cannot place the headers — was
// admitted, silently, with the compliance rule off.
func TestValidateConfig_UnrecognisedRequirementValuesStillRequireOneClick(t *testing.T) {
	for _, v := range []string{"TRUE", "True", "on", "enabled", "treu"} {
		m := NewModule()
		values := marketingOnMailUp()
		values["public_api_base_url"] = "https://api.example"
		values[requireOneClickKey] = v

		var ve *module.ConfigValidationError
		if err := m.ValidateConfig(context.Background(), values); !errors.As(err, &ve) ||
			ve.Code != errcode.NotificationSenderDriverNoOneClick {
			t.Errorf("%q: want sender_driver_no_one_click, got %v", v, err)
		}
		if err := m.ValidateConfigActivation(context.Background(), values); !errors.As(err, &ve) ||
			ve.Code != errcode.NotificationSenderDriverNoOneClick {
			t.Errorf("%q: activation: want sender_driver_no_one_click, got %v", v, err)
		}
	}
}

// And the waiver itself keeps working through every spelling of "off" the
// parser accepts, so the inversion did not trade one silent failure for a
// save an operator cannot make.
func TestValidateConfig_EveryFalseSpellingWaivesTheRequirement(t *testing.T) {
	for _, v := range []string{"false", "FALSE", "0", "no"} {
		m := NewModule()
		values := marketingOnMailUp()
		values["public_api_base_url"] = "https://api.example"
		values[requireOneClickKey] = v

		if err := m.ValidateConfig(context.Background(), values); err != nil {
			t.Errorf("%q turns the requirement off; the save must be admitted: %v", v, err)
		}
	}
}
