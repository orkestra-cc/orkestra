package module

import (
	"context"
	"fmt"
)

// HasConfigValidator lets a module reject config values at the PATCH
// boundary, before encryption or persistence. It is OPTIONAL: modules
// that omit it retain the pre-ADR-0017 behaviour, in which UpdateConfig
// persists whatever it is given.
//
// The seam exists because ConfigField's Min/Max are *int and cannot
// express a bound on a duration, and because teaching UpdateConfig to
// interpret every schema constraint generically is a broader contract
// change that deserves its own ADR. Policy therefore stays inside the
// module that owns it, and pkg/sdk/module needs no import of, or
// name-based special case for, any particular module. ADR-0017 D6.
//
// mergedValues is the module's stored non-secret configuration with the
// PATCH overlaid — not just the submitted keys — so a cross-field rule
// added later cannot be bypassed by PATCHing one half of a pair.
// Secrets are never passed: a validator must not see decrypted secret
// material to do its job.
//
// Return a *ConfigValidationError to produce a 422 naming the offending
// field. Any other error propagates as an ordinary failure.
type HasConfigValidator interface {
	ValidateConfig(ctx context.Context, mergedValues map[string]string) error
}

// ConfigValidationError reports one rejected config field. The admin API
// maps it to 422 Unprocessable Entity; the message is operator-facing, so
// it must describe the accepted range and must never quote internal state.
type ConfigValidationError struct {
	Field   string
	Message string
}

func (e *ConfigValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}
