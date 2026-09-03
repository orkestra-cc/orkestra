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
	// Code is an optional stable error code (e.g. "tenant.single_mode_conflict").
	// When set, the admin API responds with the {status,title,detail,code}
	// envelope shared with internal/shared/errcode instead of the legacy
	// text-only Huma 422 — same wire shape on every mutation surface without
	// the SDK importing internal packages.
	Code string
}

func (e *ConfigValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// HasConfigActivationValidator lets a module veto switching the active
// environment profile. It is OPTIONAL and separate from HasConfigValidator:
// PATCH-time validation sees a merged single profile, while activation must
// judge the complete target profile as a whole before SetActiveEnvironment
// writes either the active profile name or the legacy top-level values.
// Modules that omit it preserve the pre-existing validation-free activation
// (legacy-recovery behaviour). targetValues is the target profile's
// non-secret map; secrets are never passed. Activation failure leaves the
// previously active environment unchanged.
type HasConfigActivationValidator interface {
	ValidateConfigActivation(ctx context.Context, targetValues map[string]string) error
}

// ConfigValidationSnapshot is what a HasConfigSnapshotValidator judges: the
// exact profile that would become effective after the mutation, on
// whichever of the three mutation surfaces produced it (active-config
// PATCH, named-environment PATCH, activation).
//
//   - Values is the raw merged target profile. Presence and explicit
//     emptiness are preserved, so a strict boolean or duration rule can
//     tell "absent" from "cleared".
//   - EffectiveValues applies the same fallback the runtime GetValue
//     applies — empty → schema EnvVar → schema Default — so a rule about
//     what the module will actually run with reads this map, and validation
//     agrees with runtime even when a credential is supplied by the
//     deployment environment rather than stored config.
//   - SecretPresent reports, per secret key, whether a NON-EMPTY value would
//     be in force after the write: a secret submitted in this request first,
//     else the target profile's OWN stored ciphertext (decrypted only inside
//     ConfigService, only to test emptiness), else the schema
//     EnvVar/Default. Names and booleans only: no plaintext crosses the
//     validator boundary, and no other profile's secrets are ever consulted
//     — an inactive environment is judged from its own secrets, never the
//     active environment's.
type ConfigValidationSnapshot struct {
	Environment     string
	Values          map[string]string
	EffectiveValues map[string]string
	SecretPresent   map[string]bool
}

// HasConfigSnapshotValidator is the OPTIONAL successor to HasConfigValidator
// and HasConfigActivationValidator: one policy function that sees the
// complete target snapshot on all three mutation surfaces, so a cross-field
// rule that depends on secret presence (an OAuth provider being "fully
// configured") cannot be bypassed by editing the other half of the pair on
// a different surface. A module that implements it is judged through it
// everywhere and its older hooks are NOT called; a module that omits it
// keeps the two older seams exactly as they were. Return a
// *ConfigValidationError for a 422 naming the field; any other error
// propagates as an ordinary failure.
type HasConfigSnapshotValidator interface {
	ValidateConfigSnapshot(ctx context.Context, snapshot ConfigValidationSnapshot) error
}
