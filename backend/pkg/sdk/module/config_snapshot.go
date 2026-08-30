package module

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
)

// ErrConfigSecretUnreadable reports a stored secret in the target profile
// that cannot be decrypted. The mutation aborts rather than guessing whether
// the credential exists; a request that submits a replacement for that key
// is judged on the replacement and can repair the ciphertext in one PATCH.
var ErrConfigSecretUnreadable = errors.New("module: stored secret cannot be decrypted")

// buildValidationSnapshot assembles the snapshot for one target profile.
// values is the raw merged non-secret map that would be written;
// storedEncrypted is the target profile's ciphertext BEFORE this request;
// submittedSecrets is this request's plaintext secrets (nil for activation).
// Plaintext is consulted only to decide presence and never stored on the
// result. Pure apart from reading EnvVar fallbacks and decrypting stored
// ciphertext.
func buildValidationSnapshot(
	schema []ConfigField, env string,
	values, storedEncrypted, submittedSecrets map[string]string,
) (ConfigValidationSnapshot, error) {
	snap := ConfigValidationSnapshot{
		Environment:     env,
		Values:          mergeStringMaps(values, nil),
		EffectiveValues: effectiveValues(schema, values),
		SecretPresent:   map[string]bool{},
	}
	for _, key := range secretKeys(schema, values, storedEncrypted, submittedSecrets) {
		present, err := secretPresent(schema, values, key, storedEncrypted, submittedSecrets)
		if err != nil {
			return ConfigValidationSnapshot{}, err
		}
		snap.SecretPresent[key] = present
	}
	return snap, nil
}

// effectiveValues copies values and applies the fallback the runtime applies
// (config_unmarshal.go resolveValue / assignRecordList): a non-secret scalar
// field whose stored value is empty or absent takes EnvVar, then Default;
// every ROSTER element's non-secret sub-field takes the item Default (items
// have no EnvVar by construction). Keys the schema does not declare are
// copied verbatim; secrets never appear.
func effectiveValues(schema []ConfigField, values map[string]string) map[string]string {
	out := mergeStringMaps(values, nil)
	for _, f := range schema {
		switch f.Type {
		case FieldSecret:
			continue
		case FieldRecordList:
			for _, slug := range ParseRoster(values, f.Key) {
				for _, it := range f.Items {
					if it.Type == FieldSecret {
						continue
					}
					key := ItemKey(f.Key, slug, it.Key)
					if out[key] == "" && it.Default != "" {
						out[key] = it.Default
					}
				}
			}
		default:
			if out[f.Key] != "" {
				continue
			}
			if v := schemaFallbackValue(f); v != "" {
				out[f.Key] = v
			}
		}
	}
	return out
}

// schemaFallbackValue is the EnvVar-then-Default rule for one field — the
// same rule ModuleConfigService.schemaFallback and buildInitialConfig apply.
func schemaFallbackValue(f ConfigField) string {
	if f.EnvVar != "" {
		if v := os.Getenv(f.EnvVar); v != "" {
			return v
		}
	}
	return f.Default
}

// secretKeys is the union of every declared secret (scalar fields plus each
// roster element's secret sub-fields) and every key carrying a stored or
// submitted secret, sorted for deterministic output.
func secretKeys(schema []ConfigField, values, stored, submitted map[string]string) []string {
	set := map[string]bool{}
	for _, f := range schema {
		switch f.Type {
		case FieldSecret:
			set[f.Key] = true
		case FieldRecordList:
			for _, slug := range ParseRoster(values, f.Key) {
				for _, it := range f.Items {
					if it.Type == FieldSecret {
						set[ItemKey(f.Key, slug, it.Key)] = true
					}
				}
			}
		}
	}
	for k := range stored {
		set[k] = true
	}
	for k := range submitted {
		set[k] = true
	}
	return sortedKeys(set)
}

// secretPresent decides one key. Precedence: submitted → stored ciphertext →
// schema fallback — the order resolveValue uses at runtime, with the request
// layered on top. A submitted value wins even when empty (the request is
// clearing the key, and an empty secret is not presence), and it is consulted
// BEFORE the stored ciphertext so a corrupt secret can be replaced. A stored
// ciphertext that decrypts to "" is "" — like the runtime, it does NOT fall
// through to the schema default.
func secretPresent(schema []ConfigField, values map[string]string, key string, stored, submitted map[string]string) (bool, error) {
	if v, ok := submitted[key]; ok {
		if v != "" {
			return true, nil
		}
		return secretFallbackPresent(schema, values, key), nil
	}
	if enc, ok := stored[key]; ok && enc != "" {
		plain, err := decryptSecret(enc)
		if err != nil {
			return false, fmt.Errorf("%w: %q", ErrConfigSecretUnreadable, key)
		}
		return plain != "", nil
	}
	return secretFallbackPresent(schema, values, key), nil
}

// secretFallbackPresent mirrors the runtime's default for a secret with no
// stored value: a top-level secret's EnvVar/Default, or — for a key under a
// ROSTER element — the item's Default (an element outside the roster is never
// loaded, so its default counts for nothing).
func secretFallbackPresent(schema []ConfigField, values map[string]string, key string) bool {
	for _, f := range schema {
		switch f.Type {
		case FieldSecret:
			if f.Key == key {
				return schemaFallbackValue(f) != ""
			}
		case FieldRecordList:
			slug, sub, ok := SplitElementKey(f.Key, key)
			if !ok {
				continue
			}
			inRoster := false
			for _, r := range ParseRoster(values, f.Key) {
				if r == slug {
					inRoster = true
					break
				}
			}
			if !inRoster {
				return false
			}
			for _, it := range f.Items {
				if it.Key == sub && it.Type == FieldSecret {
					return it.Default != ""
				}
			}
			return false
		}
	}
	return false
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// candidate is one mutation's target as the service sees it before
// encryption or persistence. It carries what a snapshot needs; the snapshot
// itself is built only when the module asks for one, so a legacy module with
// an undecryptable stored secret stays editable.
type candidate struct {
	schema           []ConfigField
	env              string
	values           map[string]string // raw merged non-secret target
	storedEncrypted  map[string]string // target profile's ciphertext before this request
	submittedSecrets map[string]string // this request's plaintext secrets; nil on activation
	activation       bool
}

// validateCandidate runs the module's validation seam against exactly what
// would be written. Dispatch is source-compatible: a module implementing
// HasConfigSnapshotValidator is judged through it on every surface;
// otherwise a PATCH goes through HasConfigValidator and an activation
// through HasConfigActivationValidator, both with the raw merged map, as
// before. Modules unknown to this service, or without any seam, are
// accepted unchanged.
func (s *ModuleConfigService) validateCandidate(ctx context.Context, name string, c candidate) error {
	m, ok := s.knownModules[name]
	if !ok {
		return nil
	}
	if v, ok := m.(HasConfigSnapshotValidator); ok {
		snap, err := buildValidationSnapshot(c.schema, c.env, c.values, c.storedEncrypted, c.submittedSecrets)
		if err != nil {
			return err
		}
		return v.ValidateConfigSnapshot(ctx, snap)
	}
	values := c.values
	if values == nil {
		values = map[string]string{}
	}
	if c.activation {
		if v, ok := m.(HasConfigActivationValidator); ok {
			return v.ValidateConfigActivation(ctx, values)
		}
		return nil
	}
	if v, ok := m.(HasConfigValidator); ok {
		return v.ValidateConfig(ctx, values)
	}
	return nil
}
