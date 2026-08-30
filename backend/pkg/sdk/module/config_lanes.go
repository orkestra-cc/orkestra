package module

// CodeConfigKeyInvalid is the stable 422 code for a submitted key the
// module's schema does not accept in that lane. SDK-owned, like
// CodeConfigRevisionStale.
const CodeConfigKeyInvalid = "module.config_key_invalid"

// validateSubmittedKeys enforces the lane rule the admin API's two request
// blocks imply: a key in `config` must be a declared non-secret field, a
// declared record-list field's label key, or one of its non-secret sub-field
// keys; a key in `secrets` must be a declared secret field or a declared
// secret sub-field key. Anything else — undeclared, the SDK-owned roster, or
// a key filed in the other lane — is refused BEFORE validation, encryption or
// persistence. This is what keeps a secret submitted in the config block out
// of the non-secret validation snapshot and out of ConfigValues in plaintext.
//
// A module that declares no schema has nothing to classify against and keeps
// accepting anything (pre-existing behaviour; every in-tree module declares
// one). Keys are checked in sorted order so the reported field is
// deterministic.
func validateSubmittedKeys(schema []ConfigField, values, secrets map[string]string) error {
	if len(schema) == 0 {
		return nil
	}
	for _, key := range sortedKeys(keySet(values)) {
		if !keyAllowedInLane(schema, key, false) {
			return &ConfigValidationError{
				Field: key, Code: CodeConfigKeyInvalid,
				Message: "is not a non-secret field of this module (send secrets in `secrets`; unknown keys are refused)",
			}
		}
	}
	for _, key := range sortedKeys(keySet(secrets)) {
		if !keyAllowedInLane(schema, key, true) {
			return &ConfigValidationError{
				Field: key, Code: CodeConfigKeyInvalid,
				Message: "is not a secret field of this module (send non-secret values in `config`; unknown keys are refused)",
			}
		}
	}
	return nil
}

// keyAllowedInLane reports whether key may appear in the secrets (true) or
// config (false) lane. The roster key (`<field>.__items`) is never accepted
// from a request: it is SDK-owned, and on the record-list path this check
// runs BEFORE the roster strip, so a submitted roster key is refused rather
// than silently dropped.
func keyAllowedInLane(schema []ConfigField, key string, secret bool) bool {
	for _, f := range schema {
		if f.Type != FieldRecordList {
			if f.Key == key {
				return (f.Type == FieldSecret) == secret
			}
			continue
		}
		_, sub, ok := SplitElementKey(f.Key, key)
		if !ok {
			continue
		}
		if sub == labelSuffix {
			return !secret
		}
		for _, it := range f.Items {
			if it.Key == sub {
				return (it.Type == FieldSecret) == secret
			}
		}
		return false
	}
	return false
}

func keySet(m map[string]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}
