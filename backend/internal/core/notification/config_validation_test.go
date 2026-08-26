package notification

import (
	"context"
	"errors"
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
