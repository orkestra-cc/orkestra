package module

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

var snapshotSchema = []ConfigField{
	{Key: "clientId", Type: FieldString, EnvVar: "SNAP_TEST_CLIENT_ID", Default: "default-id"},
	{Key: "redirect", Type: FieldString},
	{Key: "clientSecret", Type: FieldSecret, EnvVar: "SNAP_TEST_CLIENT_SECRET"},
	{Key: "otherSecret", Type: FieldSecret},
	{Key: "flag", Type: FieldBool, Default: "false"},
	{Key: "profiles", Type: FieldRecordList, Items: []ConfigItemField{
		{Key: "host", Type: FieldString, Default: "h-default"},
		{Key: "password", Type: FieldSecret},
		{Key: "token", Type: FieldSecret, Default: "tok-default"},
	}},
}

func TestBuildValidationSnapshot_RawVersusEffective(t *testing.T) {
	t.Setenv("SNAP_TEST_CLIENT_ID", "")
	values := map[string]string{"redirect": "", "profiles.__items": "a,b", "profiles.a.host": "h"}
	snap, err := buildValidationSnapshot(snapshotSchema, "sandbox", values, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Environment != "sandbox" {
		t.Errorf("Environment = %q", snap.Environment)
	}
	// Raw: absent vs explicit empty are distinguishable.
	if _, ok := snap.Values["clientId"]; ok {
		t.Error("raw Values must not invent an absent key")
	}
	if v, ok := snap.Values["redirect"]; !ok || v != "" {
		t.Error("raw Values must keep an explicit empty value")
	}
	// Effective: schema Default applied to the absent key, and to the bool.
	if snap.EffectiveValues["clientId"] != "default-id" || snap.EffectiveValues["flag"] != "false" {
		t.Errorf("EffectiveValues did not apply schema defaults: %v", snap.EffectiveValues)
	}
	if _, ok := snap.EffectiveValues["redirect"]; !ok || snap.EffectiveValues["redirect"] != "" {
		t.Error("a field with no fallback stays empty in EffectiveValues")
	}
	// Record-list element keys pass through verbatim; a roster element's
	// absent sub-field takes the item Default, exactly as assignRecordList
	// resolves it at runtime; an element outside the roster gets nothing.
	if snap.EffectiveValues["profiles.a.host"] != "h" || snap.EffectiveValues["profiles.b.host"] != "h-default" {
		t.Errorf("record-list effective values: %v", snap.EffectiveValues)
	}
	if _, ok := snap.Values["profiles.b.host"]; ok {
		t.Error("raw Values must not carry an item default")
	}
	if _, ok := snap.EffectiveValues["profiles.zzz.host"]; ok {
		t.Error("a non-roster element must not be resolved")
	}
	for _, m := range []map[string]string{snap.Values, snap.EffectiveValues} {
		if _, ok := m["clientSecret"]; ok {
			t.Error("a secret key leaked into a values map")
		}
	}
}

func TestBuildValidationSnapshot_EnvVarBeatsDefault(t *testing.T) {
	t.Setenv("SNAP_TEST_CLIENT_ID", "from-env")
	snap, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if snap.EffectiveValues["clientId"] != "from-env" {
		t.Errorf("EffectiveValues[clientId] = %q, want the EnvVar value", snap.EffectiveValues["clientId"])
	}
}

func TestBuildValidationSnapshot_SecretPresence(t *testing.T) {
	withEncryptionKey(t)
	t.Setenv("SNAP_TEST_CLIENT_SECRET", "")
	stored, _ := encryptSecret("stored-plaintext")
	storedEmpty, _ := encryptSecret("")
	cases := []struct {
		name      string
		stored    map[string]string
		submitted map[string]string
		env       string
		want      map[string]bool
	}{
		{"stored ciphertext is present", map[string]string{"clientSecret": stored}, nil, "", map[string]bool{"clientSecret": true, "otherSecret": false}},
		{"submitted non-empty is present without anything stored", nil, map[string]string{"otherSecret": "new"}, "", map[string]bool{"clientSecret": false, "otherSecret": true}},
		{"submitted empty is NOT presence", map[string]string{"otherSecret": stored}, map[string]string{"otherSecret": ""}, "", map[string]bool{"clientSecret": false, "otherSecret": false}},
		{"submitted empty falls back to the EnvVar", nil, map[string]string{"clientSecret": ""}, "env-secret", map[string]bool{"clientSecret": true, "otherSecret": false}},
		{"schema EnvVar alone is presence", nil, nil, "env-secret", map[string]bool{"clientSecret": true, "otherSecret": false}},
		{"stored empty ciphertext is absence", map[string]string{"clientSecret": storedEmpty}, nil, "", map[string]bool{"clientSecret": false, "otherSecret": false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("SNAP_TEST_CLIENT_SECRET", c.env)
			snap, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, c.stored, c.submitted)
			if err != nil {
				t.Fatal(err)
			}
			for k, want := range c.want {
				if snap.SecretPresent[k] != want {
					t.Errorf("SecretPresent[%s] = %v, want %v", k, snap.SecretPresent[k], want)
				}
			}
			// Plaintext never reaches the snapshot.
			for _, m := range []map[string]string{snap.Values, snap.EffectiveValues} {
				for _, v := range m {
					if strings.Contains(v, "plaintext") || v == "new" || v == "env-secret" {
						t.Errorf("secret material leaked into a values map: %q", v)
					}
				}
			}
		})
	}
}

func TestBuildValidationSnapshot_CorruptCiphertext(t *testing.T) {
	withEncryptionKey(t)
	corrupt := map[string]string{"clientSecret": "not-a-ciphertext"}
	_, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, corrupt, nil)
	if !errors.Is(err, ErrConfigSecretUnreadable) {
		t.Fatalf("corrupt stored secret: err = %v, want ErrConfigSecretUnreadable", err)
	}
	// A submitted replacement is judged BEFORE the stored ciphertext is
	// touched, so the operator can repair a corrupt secret in one PATCH.
	snap, err := buildValidationSnapshot(snapshotSchema, "production", map[string]string{}, corrupt, map[string]string{"clientSecret": "replacement"})
	if err != nil {
		t.Fatalf("a submitted replacement must repair a corrupt secret: %v", err)
	}
	if !snap.SecretPresent["clientSecret"] {
		t.Error("replacement not counted as present")
	}
}

func TestBuildValidationSnapshot_RecordListElementSecrets(t *testing.T) {
	withEncryptionKey(t)
	ct, _ := encryptSecret("x")
	values := map[string]string{"profiles.__items": "a,b", "profiles.a.host": "h"}
	stored := map[string]string{"profiles.a.password": ct}
	snap, err := buildValidationSnapshot(snapshotSchema, "production", values, stored, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.SecretPresent["profiles.a.password"] || snap.SecretPresent["profiles.b.password"] {
		t.Errorf("element secrets: %v", snap.SecretPresent)
	}
	// An item Default counts for a roster element (runtime resolves it) and
	// for nothing else.
	if !snap.SecretPresent["profiles.a.token"] || !snap.SecretPresent["profiles.b.token"] {
		t.Errorf("item secret defaults not applied to roster elements: %v", snap.SecretPresent)
	}
	if _, listed := snap.SecretPresent["profiles.zzz.token"]; listed {
		t.Error("a non-roster element's secret must not be listed")
	}
	stored["profiles.zzz.token"] = ct
	snap, err = buildValidationSnapshot(snapshotSchema, "production", values, stored, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.SecretPresent["profiles.zzz.token"] {
		t.Error("a stored ciphertext is presence even for a non-roster key (it is a stored value, listed by name)")
	}
}

// --- dispatch ---

type snapshotModule struct {
	BaseModule
	saw *ConfigValidationSnapshot
	err error
}

func (m *snapshotModule) Name() string             { return "snap" }
func (m *snapshotModule) Init(*Dependencies) error { return nil }
func (m *snapshotModule) ValidateConfigSnapshot(_ context.Context, s ConfigValidationSnapshot) error {
	m.saw = &s
	return m.err
}

// A snapshot module that ALSO implements the legacy hooks must be judged
// through the snapshot alone.
func (m *snapshotModule) ValidateConfig(context.Context, map[string]string) error {
	return errors.New("legacy PATCH hook must not be called on a snapshot module")
}
func (m *snapshotModule) ValidateConfigActivation(context.Context, map[string]string) error {
	return errors.New("legacy activation hook must not be called on a snapshot module")
}

func TestValidateCandidate_Dispatch(t *testing.T) {
	withEncryptionKey(t)
	ctx := context.Background()
	// activationValidatingModule embeds validatingModule, so both answer
	// Name() == "validating"; each gets its own service so the lookup is
	// unambiguous.
	sm := &snapshotModule{}
	vm := &validatingModule{}
	avm := &activationValidatingModule{}
	svcSnap := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svcSnap.RegisterKnownModules([]Module{sm})
	svcPatch := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svcPatch.RegisterKnownModules([]Module{vm})
	svcAct := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svcAct.RegisterKnownModules([]Module{avm})

	patch := candidate{schema: snapshotSchema, env: "sandbox", values: map[string]string{"strict": "bad", "mode": "bad"}}
	activation := patch
	activation.activation = true

	// Snapshot module: judged through the snapshot on PATCH and on
	// activation, sees the target env, and its legacy hooks are never called.
	if err := svcSnap.validateCandidate(ctx, "snap", patch); err != nil || sm.saw == nil || sm.saw.Environment != "sandbox" {
		t.Fatalf("snapshot dispatch on PATCH: err=%v saw=%+v", err, sm.saw)
	}
	sm.saw = nil
	if err := svcSnap.validateCandidate(ctx, "snap", activation); err != nil || sm.saw == nil {
		t.Fatalf("snapshot dispatch on activation: err=%v saw=%+v", err, sm.saw)
	}

	// Legacy PATCH hook still sees Values and still rejects on PATCH …
	var typed *ConfigValidationError
	if err := svcPatch.validateCandidate(ctx, "validating", patch); !errors.As(err, &typed) || typed.Field != "strict" {
		t.Errorf("legacy PATCH dispatch: %v", err)
	}
	// … and is NOT consulted on activation (legacy-recovery behaviour).
	if err := svcPatch.validateCandidate(ctx, "validating", activation); err != nil {
		t.Errorf("a module with only HasConfigValidator must activate unconditionally: %v", err)
	}

	// Legacy activation hook still runs on activation, with the raw map.
	if err := svcAct.validateCandidate(ctx, "validating", activation); !errors.As(err, &typed) || typed.Code != "x.mode_invalid" {
		t.Errorf("legacy activation dispatch: %v", err)
	}

	// Unknown module: accepted.
	if err := svcSnap.validateCandidate(ctx, "unknown", patch); err != nil {
		t.Errorf("unknown module: %v", err)
	}
}

// Decryption happens only for a module that asks for the snapshot: a legacy
// module with a corrupt stored secret must remain editable.
func TestValidateCandidate_LegacyModuleNeverDecrypts(t *testing.T) {
	withEncryptionKey(t)
	svc := NewModuleConfigService(newFakeConfigRepo(), fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{&validatingModule{}})
	c := candidate{schema: snapshotSchema, values: map[string]string{"strict": "good"}, storedEncrypted: map[string]string{"clientSecret": "corrupt"}}
	if err := svc.validateCandidate(context.Background(), "validating", c); err != nil {
		t.Fatalf("legacy module must not be blocked by undecryptable secrets: %v", err)
	}
}
