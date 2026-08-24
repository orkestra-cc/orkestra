package module

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// UnmarshalModule decodes the active-environment configuration of moduleName
// into v, which must be a non-nil pointer to a struct. Resolution order per
// field matches GetValue/GetSecret: active-environment value → schema default
// → schema EnvVar.
//
// Field-to-key mapping:
//   - Explicit `module:"keyName"` tag on the struct field wins.
//   - Otherwise the struct field name is lowercased-first-rune (PascalCase →
//     camelCase). E.g. APIKey → aPIKey; declare a tag if your schema uses a
//     different convention (most schemas do).
//   - Unexported fields and fields tagged `module:"-"` are ignored.
//   - Fields whose mapped key has no entry in the schema are left at their Go
//     zero value (no error). This matches the silent-default behaviour of the
//     existing GetValue helpers.
//
// FieldSecret values are decrypted via decryptSecret (AES-256-GCM,
// same path as GetSecret). A decryption failure surfaces as an error.
//
// Type compatibility between Go struct field and schema field type:
//
//	schema FieldString     → string
//	schema FieldBool       → bool
//	schema FieldInt        → int, int32, int64
//	schema FieldDuration   → time.Duration
//	schema FieldSecret     → string
//	schema FieldEnum       → string (value validated against Options)
//	schema FieldStringList → []string (comma-separated; empty string → nil)
//
// Any other pairing returns an error naming the offending field.
func (s *ModuleConfigService) UnmarshalModule(ctx context.Context, moduleName string, v any) error {
	doc, err := s.repo.FindByName(ctx, moduleName)
	if err != nil {
		return fmt.Errorf("UnmarshalModule: load %q: %w", moduleName, err)
	}
	return unmarshalFromDoc(moduleName, doc, v)
}

// unmarshalFromDoc is the pure, repo-independent core of UnmarshalModule.
// Exposed at package level so tests can exercise the reflection / coercion
// logic without a MongoDB fixture.
func unmarshalFromDoc(moduleName string, doc *ModuleConfig, v any) error {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("UnmarshalModule: v must be a non-nil pointer, got %T", v)
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("UnmarshalModule: v must point to a struct, got pointer to %s", elem.Kind())
	}

	var (
		schema    []ConfigField
		values    map[string]string
		encrypted map[string]string
	)
	if doc != nil {
		schema = doc.ConfigSchema
		values = doc.ActiveConfigValues()
		encrypted = doc.ActiveEncryptedValues()
	}
	return unmarshalInto(moduleName, schema, values, encrypted, elem)
}

// UnmarshalConfig decodes a schema plus its raw value maps into v, which must
// be a non-nil pointer to a struct. It is the repo-independent half of
// UnmarshalModule — useful to a module that already holds a config snapshot,
// and to tests, which need the coercion logic without a MongoDB fixture.
func UnmarshalConfig(schema []ConfigField, values, encrypted map[string]string, v any) error {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("UnmarshalConfig: v must be a non-nil pointer, got %T", v)
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return fmt.Errorf("UnmarshalConfig: v must point to a struct, got pointer to %s", elem.Kind())
	}
	return unmarshalInto("", schema, values, encrypted, elem)
}

func unmarshalInto(moduleName string, schema []ConfigField, values, encrypted map[string]string, elem reflect.Value) error {
	schemaByKey := make(map[string]ConfigField, len(schema))
	for _, f := range schema {
		schemaByKey[f.Key] = f
	}

	t := elem.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		key := schemaKeyForField(sf)
		if key == "" {
			continue
		}
		field, ok := schemaByKey[key]
		if !ok {
			// No schema entry — leave the Go zero value.
			continue
		}
		if field.Type == FieldRecordList {
			if err := assignRecordList(elem.Field(i), field, values, encrypted); err != nil {
				return fmt.Errorf("UnmarshalModule %q field %q: %w", moduleName, sf.Name, err)
			}
			continue
		}
		raw, err := resolveValue(field, values, encrypted)
		if err != nil {
			return fmt.Errorf("UnmarshalModule %q field %q: %w", moduleName, sf.Name, err)
		}
		if err := assignField(elem.Field(i), sf, field, raw); err != nil {
			return fmt.Errorf("UnmarshalModule %q field %q: %w", moduleName, sf.Name, err)
		}
	}
	return nil
}

// assignRecordList decodes one record list into a []T, one element per roster
// entry and in roster order. Inside T, `module:"slug"` receives the element's
// immutable key segment and `module:"label"` its display label; every other
// tag names a declared sub-field and goes through the same resolve/coerce path
// a top-level field does, against the composed key.
//
// Element sub-fields carry no EnvVar by construction (ConfigItemField has no
// such field), so resolution is stored value → sub-field Default. Nothing is
// read from the process environment.
func assignRecordList(target reflect.Value, field ConfigField, values, encrypted map[string]string) error {
	if target.Kind() != reflect.Slice {
		return fmt.Errorf("schema type recordList requires a slice field, got %s", target.Kind())
	}
	elemType := target.Type().Elem()
	if elemType.Kind() != reflect.Struct {
		return fmt.Errorf("schema type recordList requires a slice of structs, got []%s", elemType.Kind())
	}

	itemByKey := make(map[string]ConfigItemField, len(field.Items))
	for _, it := range field.Items {
		itemByKey[it.Key] = it
	}

	roster := ParseRoster(values, field.Key)
	out := reflect.MakeSlice(target.Type(), 0, len(roster))
	for _, slug := range roster {
		elem := reflect.New(elemType).Elem()
		for i := 0; i < elemType.NumField(); i++ {
			sf := elemType.Field(i)
			if !sf.IsExported() {
				continue
			}
			key := schemaKeyForField(sf)
			if key == "" {
				continue
			}
			switch key {
			case "slug":
				if sf.Type.Kind() != reflect.String {
					return fmt.Errorf("element field %q tagged \"slug\" must be a string, got %s", sf.Name, sf.Type.Kind())
				}
				elem.Field(i).SetString(slug)
				continue
			case "label":
				if sf.Type.Kind() != reflect.String {
					return fmt.Errorf("element field %q tagged \"label\" must be a string, got %s", sf.Name, sf.Type.Kind())
				}
				elem.Field(i).SetString(values[LabelKey(field.Key, slug)])
				continue
			}
			item, ok := itemByKey[key]
			if !ok {
				// No declared sub-field — leave the Go zero value, matching
				// the top-level behaviour for an unknown key.
				continue
			}
			sub := itemAsField(item, ItemKey(field.Key, slug, key))
			raw, err := resolveValue(sub, values, encrypted)
			if err != nil {
				return fmt.Errorf("element %q field %q: %w", slug, sf.Name, err)
			}
			if err := assignField(elem.Field(i), sf, sub, raw); err != nil {
				return fmt.Errorf("element %q field %q: %w", slug, sf.Name, err)
			}
		}
		out = reflect.Append(out, elem)
	}
	target.Set(out)
	return nil
}

// itemAsField projects one sub-field onto the composed key it is stored under,
// so resolveValue and assignField — which know only ConfigField — can be
// reused unchanged. EnvVar is left empty on purpose: an element has no env
// source, and inventing an indexed convention is a contract this design
// deliberately does not take on.
func itemAsField(item ConfigItemField, key string) ConfigField {
	return ConfigField{
		Key:      key,
		Label:    item.Label,
		Type:     item.Type,
		Required: item.Required,
		Default:  item.Default,
		Options:  item.Options,
		Min:      item.Min,
		Max:      item.Max,
		Pattern:  item.Pattern,
	}
}

// schemaKeyForField returns the schema key the struct field maps to. An
// explicit `module:"..."` tag wins; a `module:"-"` tag skips the field.
// Otherwise the field name's first rune is lowercased and the rest preserved
// (PascalCase → camelCase). Anonymous (embedded) fields without an explicit
// tag are skipped — embedding is rare in config structs and treating each
// embedded field would create surprising key collisions.
func schemaKeyForField(sf reflect.StructField) string {
	if tag, ok := sf.Tag.Lookup("module"); ok {
		if tag == "-" {
			return ""
		}
		// Allow `module:"key,omitempty"` style tags by trimming options.
		if comma := strings.IndexByte(tag, ','); comma >= 0 {
			tag = tag[:comma]
		}
		if tag != "" {
			return tag
		}
	}
	if sf.Anonymous {
		return ""
	}
	name := sf.Name
	if name == "" {
		return ""
	}
	runes := []rune(name)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// resolveValue applies the GetValue/GetSecret precedence to a single field:
// active-env value (decrypted for secrets) → schema default → env var.
// Returns the raw string ready for type coercion.
func resolveValue(field ConfigField, values, encrypted map[string]string) (string, error) {
	if field.Type == FieldSecret {
		if enc, ok := encrypted[field.Key]; ok && enc != "" {
			plain, err := decryptSecret(enc)
			if err != nil {
				return "", fmt.Errorf("decrypt secret: %w", err)
			}
			return plain, nil
		}
	} else {
		if v, ok := values[field.Key]; ok && v != "" {
			return v, nil
		}
	}
	if field.EnvVar != "" {
		if v := os.Getenv(field.EnvVar); v != "" {
			return v, nil
		}
	}
	return field.Default, nil
}

// assignField writes raw into target, coercing based on the schema field type
// and the struct field's Go kind. Returns an error if the pairing is
// unsupported or the raw value cannot be parsed.
func assignField(target reflect.Value, sf reflect.StructField, field ConfigField, raw string) error {
	if !target.CanSet() {
		return fmt.Errorf("cannot set field")
	}

	switch field.Type {
	case FieldString, FieldSecret:
		if target.Kind() != reflect.String {
			return fmt.Errorf("schema type %s requires string field, got %s", field.Type, target.Kind())
		}
		target.SetString(raw)

	case FieldEnum:
		if target.Kind() != reflect.String {
			return fmt.Errorf("schema type %s requires string field, got %s", field.Type, target.Kind())
		}
		if raw != "" && len(field.Options) > 0 {
			ok := false
			for _, opt := range field.Options {
				if opt == raw {
					ok = true
					break
				}
			}
			if !ok {
				return fmt.Errorf("value %q not in enum options %v", raw, field.Options)
			}
		}
		target.SetString(raw)

	case FieldBool:
		if target.Kind() != reflect.Bool {
			return fmt.Errorf("schema type bool requires bool field, got %s", target.Kind())
		}
		target.SetBool(parseBool(raw))

	case FieldInt:
		switch target.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if raw == "" {
				target.SetInt(0)
				return nil
			}
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return fmt.Errorf("parse int %q: %w", raw, err)
			}
			if target.OverflowInt(n) {
				return fmt.Errorf("int value %d overflows %s", n, target.Kind())
			}
			target.SetInt(n)
		default:
			return fmt.Errorf("schema type int requires int field, got %s", target.Kind())
		}

	case FieldDuration:
		if target.Type() != reflect.TypeOf(time.Duration(0)) {
			return fmt.Errorf("schema type duration requires time.Duration field, got %s", target.Type())
		}
		if raw == "" {
			target.Set(reflect.ValueOf(time.Duration(0)))
			return nil
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("parse duration %q: %w", raw, err)
		}
		target.Set(reflect.ValueOf(d))

	case FieldStringList:
		if target.Kind() != reflect.Slice || target.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("schema type stringList requires []string field, got %s", target.Type())
		}
		if raw == "" {
			target.Set(reflect.Zero(target.Type()))
			return nil
		}
		parts := strings.Split(raw, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		target.Set(reflect.ValueOf(out))

	default:
		return fmt.Errorf("unknown schema field type %q", field.Type)
	}
	return nil
}

// parseBool matches the existing GetConfigBool semantics from Dependencies.
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
