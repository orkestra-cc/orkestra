package module

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

var laneSchema = []ConfigField{
	{Key: "flag", Type: FieldBool},
	{Key: "apiKey", Type: FieldSecret},
	{Key: "profiles", Type: FieldRecordList, Items: []ConfigItemField{
		{Key: "host", Type: FieldString},
		{Key: "password", Type: FieldSecret},
	}},
}

func TestValidateSubmittedKeys(t *testing.T) {
	cases := []struct {
		name    string
		values  map[string]string
		secrets map[string]string
		wantErr string // offending field, "" for accepted
	}{
		{"declared keys in their lanes", map[string]string{"flag": "true", "profiles.a.host": "h", "profiles.a.__label": "A"}, map[string]string{"apiKey": "k", "profiles.a.password": "p"}, ""},
		{"secret in the config lane", map[string]string{"apiKey": "leak"}, nil, "apiKey"},
		{"element secret in the config lane", map[string]string{"profiles.a.password": "leak"}, nil, "profiles.a.password"},
		{"non-secret in the secrets lane", nil, map[string]string{"flag": "true"}, "flag"},
		{"label in the secrets lane", nil, map[string]string{"profiles.a.__label": "A"}, "profiles.a.__label"},
		{"unknown scalar", map[string]string{"bogus": "x"}, nil, "bogus"},
		{"unknown sub-field", map[string]string{"profiles.a.port": "25"}, nil, "profiles.a.port"},
		{"roster key from a request", map[string]string{"profiles.__items": "a"}, nil, "profiles.__items"},
		{"unknown secret", nil, map[string]string{"nope": "x"}, "nope"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateSubmittedKeys(laneSchema, c.values, c.secrets)
			var typed *ConfigValidationError
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("unexpected: %v", err)
			case c.wantErr != "" && (!errors.As(err, &typed) || typed.Field != c.wantErr || typed.Code != CodeConfigKeyInvalid):
				t.Fatalf("err = %v, want ConfigValidationError on %q with %s", err, c.wantErr, CodeConfigKeyInvalid)
			}
		})
	}
	if err := validateSubmittedKeys(nil, map[string]string{"anything": "goes"}, map[string]string{"too": "x"}); err != nil {
		t.Errorf("a schema-less module must keep accepting anything: %v", err)
	}
}

// laneModule declares a record list so the roster and element rules can be
// exercised through the service, not just the helper.
type laneModule struct{ BaseModule }

func (laneModule) Name() string                { return "lane" }
func (laneModule) Init(*Dependencies) error    { return nil }
func (laneModule) ConfigSchema() []ConfigField { return laneSchema }

func newLaneService(t *testing.T) (*ModuleConfigService, *fakeConfigRepo) {
	t.Helper()
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["lane"] = &ModuleConfig{
		ModuleName: "lane", ActiveEnvironment: "production", ConfigSchema: laneSchema,
		ConfigValues: map[string]string{}, EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"profiles.__items": "a", "profiles.a.__label": "A"}, EncryptedValues: map[string]string{}},
		},
	}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{laneModule{}})
	return svc, repo
}

// The roster key is refused on every path — including the record-list one,
// where it used to be silently stripped — and a legitimate membership
// request still works.
func TestRecordListPath_RosterKeyIsRefusedNotStripped(t *testing.T) {
	svc, repo := newLaneService(t)
	ctx := context.Background()
	var typed *ConfigValidationError
	err := svc.UpdateEnvironmentConfigWithRecordLists(ctx, "lane", "production",
		map[string]string{"profiles.__items": "a,evil"}, nil, nil, nil)
	if !errors.As(err, &typed) || typed.Field != "profiles.__items" || typed.Code != CodeConfigKeyInvalid {
		t.Fatalf("record-list path with a roster key: err = %v, want 422 %s on profiles.__items", err, CodeConfigKeyInvalid)
	}
	if repo.casCalls != 0 || repo.docs["lane"].Environments["production"].ConfigValues["profiles.__items"] != "a" {
		t.Fatal("a refused roster key reached the repository")
	}
	if err := svc.UpdateConfig(ctx, "lane", map[string]string{"profiles.__items": "a,evil"}, nil); !errors.As(err, &typed) {
		t.Errorf("bare PATCH with a roster key: %v", err)
	}
	// Membership through the declared intent still works, label and all.
	err = svc.UpdateEnvironmentConfigWithRecordLists(ctx, "lane", "production",
		map[string]string{"profiles.b.__label": "B", "profiles.b.host": "h"}, map[string]string{"profiles.b.password": "p"},
		[]RecordListMutation{{Field: "profiles", Create: []string{"b"}}}, nil)
	if err != nil {
		t.Fatalf("legitimate create: %v", err)
	}
	if got := repo.docs["lane"].Environments["production"].ConfigValues["profiles.__items"]; got != "a,b" {
		t.Errorf("roster = %q, want a,b", got)
	}
}

// The live schema decides, never the stored snapshot: a field that became a
// secret in the binary is refused in the config lane even while the
// document still carries the old string declaration.
func TestUpdateConfig_LiveSchemaBeatsStaleStoredSchema(t *testing.T) {
	svc, repo := newLaneService(t)
	stale := append([]ConfigField(nil), laneSchema...)
	stale[1] = ConfigField{Key: "apiKey", Type: FieldString} // stored snapshot predates the change
	repo.docs["lane"].ConfigSchema = stale
	var typed *ConfigValidationError
	if err := svc.UpdateConfig(context.Background(), "lane", map[string]string{"apiKey": "leak"}, nil); !errors.As(err, &typed) || typed.Code != CodeConfigKeyInvalid {
		t.Fatalf("stale stored schema must not admit a live secret in the config lane: %v", err)
	}
	if _, ok := repo.docs["lane"].ConfigValues["apiKey"]; ok {
		t.Fatal("plaintext secret persisted")
	}
}

// The refusal happens before the validator, before encryption and before the
// write: nothing observes the misfiled secret.
func TestUpdateConfig_SecretInConfigLaneNeverReachesValidatorOrDocument(t *testing.T) {
	svc, repo := newInvService(t)
	err := svc.UpdateConfig(context.Background(), "inv", map[string]string{"providerSecret": "plaintext-leak"}, nil)
	var typed *ConfigValidationError
	if !errors.As(err, &typed) || typed.Code != CodeConfigKeyInvalid {
		t.Fatalf("err = %v, want %s", err, CodeConfigKeyInvalid)
	}
	if repo.docCasCalls != 0 {
		t.Error("a refused key reached the repository")
	}
	for _, m := range []map[string]string{repo.docs["inv"].ConfigValues, repo.docs["inv"].Environments["production"].ConfigValues} {
		if _, ok := m["providerSecret"]; ok {
			t.Error("plaintext secret persisted in ConfigValues")
		}
	}
	// Same rule on the named-environment path and the record-list path.
	if err := svc.UpdateEnvironmentConfig(context.Background(), "inv", "sandbox", nil, map[string]string{"password": "misfiled"}); !errors.As(err, &typed) {
		t.Errorf("env PATCH: %v", err)
	}
	if err := svc.UpdateEnvironmentConfigWithRecordLists(context.Background(), "inv", "sandbox", map[string]string{"bogus": "x"}, nil, nil, nil); !errors.As(err, &typed) {
		t.Errorf("record-list path: %v", err)
	}
	if repo.casCalls != 0 {
		t.Error("a refused key reached the record-list CAS")
	}
}

// A refused key persists nothing — not even the legacy-profile migration
// that UpdateConfig would otherwise run first.
func TestUpdateConfig_LaneRefusalDoesNotMigrate(t *testing.T) {
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["lane"] = &ModuleConfig{ModuleName: "lane", ConfigSchema: laneSchema, ConfigValues: map[string]string{"flag": "true"}, EncryptedValues: map[string]string{}}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{laneModule{}})
	var typed *ConfigValidationError
	if err := svc.UpdateConfig(context.Background(), "lane", map[string]string{"apiKey": "leak"}, nil); !errors.As(err, &typed) || typed.Code != CodeConfigKeyInvalid {
		t.Fatalf("err = %v, want %s", err, CodeConfigKeyInvalid)
	}
	doc := repo.docs["lane"]
	if len(doc.Environments) != 0 || doc.ConfigRevision != 0 {
		t.Fatalf("a refused request migrated the document: environments=%d revision=%d", len(doc.Environments), doc.ConfigRevision)
	}
}
