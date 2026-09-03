package module

import (
	"context"
	"fmt"
)

// ActiveConfigView is ONE consistent read of a required module's active
// profile: the non-secret values (schema-secret keys stripped), every stored
// secret already decrypted, and the document revision they were read at.
//
// It exists because a decision that spans several keys — "is this OAuth
// provider enabled, structurally complete, and what secret do I build it
// from?" — must not be assembled from N independent reads, each of which
// could observe a different document, and because a stored secret that no
// longer decrypts is a DOCUMENT-level outage, not a per-key "fall back to
// the env var" (which is what GetSecret does). The view is immutable and
// safe to share for the duration of one request.
type ActiveConfigView struct {
	module   string
	schema   []ConfigField
	values   map[string]string
	secrets  map[string]string
	revision int64
}

// NewActiveConfigView builds a view from already-resolved maps. Exported for
// tests and for a fork's fakes; production views come from
// ModuleConfigService.ActiveConfigRequiredModule. secrets are plaintext
// stored values keyed by schema key (an entry present with "" means "stored
// and decrypts to empty"); values are stripped of every schema-secret key by
// the constructor, so a legacy plaintext under a secret key can never be
// read back as a value. Both maps are copied.
func NewActiveConfigView(module string, schema []ConfigField, values, secrets map[string]string, revision int64) *ActiveConfigView {
	v := &ActiveConfigView{
		module:   module,
		schema:   schema,
		values:   nonSecretValues(schema, values),
		secrets:  make(map[string]string, len(secrets)),
		revision: revision,
	}
	if v.values == nil {
		v.values = map[string]string{}
	}
	for k, s := range secrets {
		v.secrets[k] = s
	}
	return v
}

// Module returns the module name the view was read for.
func (v *ActiveConfigView) Module() string { return v.module }

// Revision returns the document's configRevision at read time.
func (v *ActiveConfigView) Revision() int64 { return v.revision }

// Raw reports the stored non-secret value and whether the key is present —
// the GetRawValue contract: ("", false) is absent, (v, true) is present and
// v may legitimately be "". A schema-secret key is never present here.
func (v *ActiveConfigView) Raw(key string) (string, bool) {
	s, ok := v.values[key]
	return s, ok
}

// Effective is the GetValue rule for a non-secret key: a present non-empty
// stored value, else the schema's EnvVar-then-Default fallback, else "".
// A schema-secret key always answers "" — secrets are read via Secret.
func (v *ActiveConfigView) Effective(key string) string {
	if s, ok := v.values[key]; ok && s != "" {
		return s
	}
	for _, f := range v.schema {
		if f.Key != key {
			continue
		}
		if f.Type == FieldSecret {
			return ""
		}
		return schemaFallbackValue(f)
	}
	return ""
}

// Secret is the GetSecret rule: a stored secret (even one that decrypts to
// ""), else the schema's EnvVar-then-Default fallback, else "". The
// constructor's callers store only NON-EMPTY ciphertexts, so — exactly like
// GetSecret — a key whose ciphertext was cleared to "" falls back.
func (v *ActiveConfigView) Secret(key string) string {
	if s, ok := v.secrets[key]; ok {
		return s
	}
	for _, f := range v.schema {
		if f.Key == key && f.Type == FieldSecret {
			return schemaFallbackValue(f)
		}
	}
	return ""
}

// SecretPresent reports whether Secret(key) is non-empty — the "presence"
// the structural predicates consume. The value itself never leaves the view
// except through Secret.
func (v *ActiveConfigView) SecretPresent(key string) bool { return v.Secret(key) != "" }

// ActiveConfigRequiredModule reads a module's document ONCE and returns a
// consistent ActiveConfigView of its active profile. Like
// GetRawValueRequiredModule it treats a missing document as the ERROR
// outcome (ErrRequiredConfigMissing) and never calls GetConfig's lazy-seed
// path. Every non-empty stored ciphertext in the active profile is decrypted
// up front: one that cannot be decrypted fails the whole read with
// ErrConfigSecretUnreadable naming the key, because a caller governing
// credentials must never build a provider from an env-var fallback while the
// operator believes the stored secret is in force. Fallbacks (EnvVar/Default)
// and secret-ness come from the LIVE schema of the registered module, not
// the stored copy, so a schema that gained a key after the document was
// written still answers correctly.
func (s *ModuleConfigService) ActiveConfigRequiredModule(ctx context.Context, name string) (*ActiveConfigView, error) {
	doc, err := s.repo.FindByName(ctx, name)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("%w: %q", ErrRequiredConfigMissing, name)
	}
	secrets := map[string]string{}
	for key, enc := range doc.ActiveEncryptedValues() {
		if enc == "" {
			continue
		}
		plain, err := decryptSecret(enc)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrConfigSecretUnreadable, key)
		}
		secrets[key] = plain
	}
	return NewActiveConfigView(name, s.schemaFor(name, doc), doc.ActiveConfigValues(), secrets, doc.ConfigRevision), nil
}
