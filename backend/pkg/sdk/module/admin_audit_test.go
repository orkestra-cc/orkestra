package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

type recordingSink struct {
	events []iface.AuditEvent
	panics bool
}

func (r *recordingSink) Emit(_ context.Context, ev iface.AuditEvent) {
	if r.panics {
		panic("sink exploded")
	}
	r.events = append(r.events, ev)
}

// auditDemoModule is a toggleable module with a scalar, a secret and a
// record list, whose Start can be made to fail.
type auditDemoModule struct {
	BaseModule
	startErr  error
	hotReload bool
}

func (m *auditDemoModule) Name() string             { return "demo" }
func (m *auditDemoModule) Category() ModuleCategory { return CategoryToggleable }
func (m *auditDemoModule) Init(*Dependencies) error { return nil }
func (m *auditDemoModule) Start(context.Context) error {
	return m.startErr
}
func (m *auditDemoModule) HotReloadConfig() bool { return m.hotReload }
func (m *auditDemoModule) ConfigSchema() []ConfigField {
	return []ConfigField{
		{Key: "flag", Type: FieldBool, Default: "false"},
		{Key: "apiKey", Type: FieldSecret},
		{Key: "email.profiles", Type: FieldRecordList, Items: []ConfigItemField{
			{Key: "host", Type: FieldString}, {Key: "password", Type: FieldSecret},
		}},
	}
}
func (m *auditDemoModule) ValidateConfig(_ context.Context, v map[string]string) error {
	if v["flag"] == "bad" {
		return &ConfigValidationError{Field: "flag", Message: "no", Code: "demo.flag_invalid"}
	}
	return nil
}

func newAuditHandler(t *testing.T, mod *auditDemoModule) (*ModuleAdminHandler, *fakeConfigRepo, *recordingSink) {
	t.Helper()
	withEncryptionKey(t)
	repo := newFakeConfigRepo()
	repo.docs["demo"] = &ModuleConfig{
		ModuleName: "demo", Category: CategoryToggleable, Enabled: false, ActiveEnvironment: "production",
		ConfigSchema:    mod.ConfigSchema(),
		ConfigValues:    map[string]string{"flag": "false"},
		EncryptedValues: map[string]string{},
		Environments: map[string]EnvironmentConfig{
			"production": {ConfigValues: map[string]string{"flag": "false"}, EncryptedValues: map[string]string{}},
			"sandbox":    {ConfigValues: map[string]string{}, EncryptedValues: map[string]string{}},
		},
	}
	svc := NewModuleConfigService(repo, fakeRedisClient{}, slog.Default())
	svc.RegisterKnownModules([]Module{mod})
	reg := NewModuleRegistry(slog.Default())
	reg.Register(mod)
	h := NewModuleAdminHandler(svc, reg)
	sink := &recordingSink{}
	h.SetAuditSink(sink)
	h.SetActorResolver(func(context.Context) AdminActor {
		return AdminActor{UserID: "u-1", TenantID: "t-1", TenantKind: "internal", IP: "203.0.113.9", UserAgent: "UA/1", RequestID: "req-42"}
	})
	return h, repo, sink
}

func patchConfig(cfg, secrets map[string]string) *UpdateModuleInput {
	in := &UpdateModuleInput{Name: "demo"}
	in.Body.Config, in.Body.Secrets = cfg, secrets
	return in
}

func TestAudit_ConfigUpdateSuccessCarriesNamesNeverValues(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	_, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, map[string]string{"apiKey": "hunter2-value"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}
	ev := sink.events[0]
	if ev.Action != ActionModuleConfigUpdated || ev.Outcome != "success" || ev.ResourceType != "module" || ev.ResourceID != "demo" {
		t.Errorf("event shape: %+v", ev)
	}
	if ev.ActorUserID != "u-1" || ev.TenantID != "t-1" || ev.TenantKind != "internal" || ev.IPAddress != "203.0.113.9" || ev.UserAgent != "UA/1" || ev.ActorType != "user" {
		t.Errorf("actor: %+v", ev)
	}
	if ev.ActorEmail != "" {
		t.Error("actor email must never be recorded")
	}
	if !reflect.DeepEqual(ev.Metadata["keys"], []string{"flag"}) || !reflect.DeepEqual(ev.Metadata["secretKeys"], []string{"apiKey"}) {
		t.Errorf("metadata keys: %v", ev.Metadata)
	}
	if ev.Metadata["env"] != "production" || ev.Metadata["requestId"] != "req-42" {
		t.Errorf("metadata env/requestId: %v", ev.Metadata)
	}
	if _, ok := ev.Metadata["code"]; ok {
		t.Error("a success carries no code")
	}
	dump := fmt.Sprintf("%v", ev)
	if strings.Contains(dump, "hunter2") || strings.Contains(dump, "true") {
		t.Errorf("a value reached the audit event: %s", dump)
	}
}

func TestAudit_ValidationAndStaleFailuresCarryCodes(t *testing.T) {
	h, repo, sink := newAuditHandler(t, &auditDemoModule{})
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "bad"}, nil)); err == nil {
		t.Fatal("expected 422")
	}
	repo.docCasFailures = 1
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil)); err == nil {
		t.Fatal("expected 409")
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Outcome != "failure" || sink.events[0].Metadata["code"] != "demo.flag_invalid" {
		t.Errorf("422 event: %+v", sink.events[0])
	}
	if sink.events[1].Outcome != "failure" || sink.events[1].Metadata["code"] != CodeConfigRevisionStale {
		t.Errorf("409 event: %+v", sink.events[1])
	}
}

func TestAudit_RecordListKeysCollapseAndUnknownKeysAreCounted(t *testing.T) {
	schema := (&auditDemoModule{}).ConfigSchema()
	keys, unknown := auditKeyNames(schema, map[string]string{
		"email.profiles.acme.host":    "h",
		"email.profiles.acme.__label": "Acme",
		"email.profiles.other.host":   "h2",
		"email.profiles.__items":      "acme,other",
		"flag":                        "true",
		"totally-made-up<script>":     "x",
	}, false)
	want := []string{"email.profiles", "email.profiles.__label", "email.profiles.host", "flag"}
	if !reflect.DeepEqual(keys, want) || unknown != 1 {
		t.Errorf("keys=%v unknown=%d, want %v / 1", keys, unknown, want)
	}
	// A key filed in the wrong block is counted, never reported under the
	// name it borrows: a secret in the config block, a value in secrets.
	if keys, unknown := auditKeyNames(schema, map[string]string{"apiKey": "x", "email.profiles.acme.password": "p"}, false); len(keys) != 0 || unknown != 2 {
		t.Errorf("secrets in the config block: keys=%v unknown=%d, want [] / 2", keys, unknown)
	}
	keys, unknown = auditKeyNames(schema, map[string]string{"apiKey": "x", "email.profiles.acme.password": "p", "flag": "misfiled", "email.profiles.__items": "a"}, true)
	if !reflect.DeepEqual(keys, []string{"apiKey", "email.profiles.password"}) || unknown != 2 {
		t.Errorf("secrets block: keys=%v unknown=%d, want [apiKey email.profiles.password] / 2", keys, unknown)
	}
	big := map[string]string{}
	for i := 0; i < 100; i++ {
		big[fmt.Sprintf("email.profiles.p%03d.host", i)] = "x"
	}
	keys, _ = auditKeyNames(schema, big, false)
	if len(keys) != 1 {
		t.Errorf("100 element keys collapse to one schema name, got %v", keys)
	}
	if keys, _ := auditKeyNames(nil, map[string]string{}, false); len(keys) != 0 || keys == nil {
		t.Errorf("empty submission must yield an empty (non-nil) list, got %v", keys)
	}
	summary, fields, unknown := auditRecordLists(schema, []RecordListMutation{
		{Field: "email.profiles", Create: []string{"a", "b"}, Remove: []string{"c"}},
		{Field: "not-declared", Remove: []string{"x"}},
	})
	if len(summary) != 1 || summary[0]["created"] != 2 || summary[0]["removed"] != 1 || !reflect.DeepEqual(fields, []string{"email.profiles"}) || unknown != 1 {
		t.Errorf("record-list summary: %v fields=%v unknown=%d", summary, fields, unknown)
	}
	// Counts only: the summary row carries exactly field/created/removed,
	// so no slug can be in it.
	if len(summary[0]) != 3 {
		t.Errorf("summary row must carry field/created/removed only, got %v", summary[0])
	}
	// One row per FIELD. A duplicate intent is refused by the service, but
	// the failure is audited too — and two rows for one field would read as
	// two changes that never happened.
	dup := make([]RecordListMutation, 0, 70)
	for i := 0; i < 70; i++ {
		dup = append(dup, RecordListMutation{Field: "email.profiles", Create: []string{"a"}})
	}
	summary, fields, unknown = auditRecordLists(schema, dup)
	if len(summary) != 1 || !reflect.DeepEqual(fields, []string{"email.profiles"}) || unknown != 0 {
		t.Errorf("duplicate fields must collapse to one row: summary=%v fields=%v unknown=%d", summary, fields, unknown)
	}
	// And the lists are bounded like every other key list on the event.
	wide := make([]ConfigField, 0, 70)
	many := make([]RecordListMutation, 0, 70)
	for i := 0; i < 70; i++ {
		key := fmt.Sprintf("list%02d", i)
		wide = append(wide, ConfigField{Key: key, Type: FieldRecordList})
		many = append(many, RecordListMutation{Field: key, Create: []string{"a"}})
	}
	summary, fields, _ = auditRecordLists(wide, many)
	if len(summary) != auditMaxKeys || len(fields) != auditMaxKeys {
		t.Errorf("summary=%d fields=%d, want both capped at %d", len(summary), len(fields), auditMaxKeys)
	}
}

// A membership-only removal touches no value key; the event still names the
// field and carries the counts.
func TestAudit_MembershipOnlyRemovalIsVisible(t *testing.T) {
	h, repo, sink := newAuditHandler(t, &auditDemoModule{})
	prod := repo.docs["demo"].Environments["production"]
	prod.ConfigValues["email.profiles.__items"] = "acme"
	prod.ConfigValues["email.profiles.acme.host"] = "h"
	repo.docs["demo"].Environments["production"] = prod
	rev := int64(0)
	in := &UpdateEnvironmentInput{Name: "demo", Env: "production"}
	in.Body.RecordLists = []RecordListMutationDTO{{Field: "email.profiles", Remove: []string{"acme"}}}
	in.Body.Revision = &rev
	if _, err := h.UpdateEnvironment(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	ev := sink.events[len(sink.events)-1]
	if !reflect.DeepEqual(ev.Metadata["keys"], []string{"email.profiles"}) {
		t.Errorf("keys = %v, want the record-list field", ev.Metadata["keys"])
	}
	lists, _ := ev.Metadata["recordLists"].([]map[string]any)
	if len(lists) != 1 || lists[0]["removed"] != 1 {
		t.Errorf("recordLists = %v", ev.Metadata["recordLists"])
	}
	if strings.Contains(fmt.Sprint(ev.Metadata), "acme") {
		t.Error("a slug reached the audit event")
	}
}

// G7: a mutation that reaches the handler is audited even when it cannot be
// dispatched — the document read fails, or the module does not exist.
func TestAudit_AbortBeforeDispatchStillEmits(t *testing.T) {
	h, repo, sink := newAuditHandler(t, &auditDemoModule{})
	enabled := true
	in := patchConfig(map[string]string{"flag": "true"}, nil)
	in.Body.Enabled = &enabled
	repo.findErr = errors.New("mongo down")
	if _, err := h.UpdateModule(context.Background(), in); err == nil {
		t.Fatal("expected the read failure to surface")
	}
	repo.findErr = nil
	if len(sink.events) != 2 || sink.events[0].Action != ActionModuleConfigUpdated || sink.events[1].Action != ActionModuleEnabled {
		t.Fatalf("read failure must audit both intended halves as failures, got %+v", sink.events)
	}
	for _, ev := range sink.events {
		if ev.Outcome != "failure" {
			t.Errorf("outcome = %q, want failure", ev.Outcome)
		}
	}
	sink.events = nil
	unknown := patchConfig(map[string]string{"flag": "true"}, nil)
	unknown.Name = "no-such-module"
	if _, err := h.UpdateModule(context.Background(), unknown); err == nil {
		t.Fatal("expected 404")
	}
	if len(sink.events) != 1 || sink.events[0].ResourceID != "no-such-module" || sink.events[0].Outcome != "failure" {
		t.Errorf("404 must still be audited, got %+v", sink.events)
	}
}

func TestAudit_UserAgentIsBounded(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	h.SetActorResolver(func(context.Context) AdminActor {
		return AdminActor{UserID: "u-1", UserAgent: strings.Repeat("x", 1000)}
	})
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil)); err != nil {
		t.Fatal(err)
	}
	if got := len(sink.events[0].UserAgent); got != auditMaxUserAgent {
		t.Errorf("UserAgent length = %d, want %d", got, auditMaxUserAgent)
	}
}

func TestAudit_ConfigFailureNeverStartsTheModule(t *testing.T) {
	mod := &auditDemoModule{}
	h, _, sink := newAuditHandler(t, mod)
	enabled := true
	in := patchConfig(map[string]string{"flag": "bad"}, nil)
	in.Body.Enabled = &enabled
	if _, err := h.UpdateModule(context.Background(), in); err == nil {
		t.Fatal("expected 422")
	}
	if h.registry.IsStarted("demo") {
		t.Fatal("a rejected config change still started the module")
	}
	if len(sink.events) != 1 || sink.events[0].Action != ActionModuleConfigUpdated {
		t.Errorf("only the config failure is audited, got %+v", sink.events)
	}
}

func TestAudit_ConfigSucceedsThenLifecycleFails_BothReported(t *testing.T) {
	mod := &auditDemoModule{startErr: errors.New("boom")}
	h, repo, sink := newAuditHandler(t, mod)
	enabled := true
	in := patchConfig(map[string]string{"flag": "true"}, nil)
	in.Body.Enabled = &enabled
	if _, err := h.UpdateModule(context.Background(), in); err == nil {
		t.Fatal("expected the start failure to surface")
	}
	if repo.docs["demo"].ConfigValues["flag"] != "true" {
		t.Error("config must remain changed when the later lifecycle step fails")
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Action != ActionModuleConfigUpdated || sink.events[0].Outcome != "success" {
		t.Errorf("config event: %+v", sink.events[0])
	}
	if sink.events[1].Action != ActionModuleEnabled || sink.events[1].Outcome != "failure" {
		t.Errorf("enable event: %+v", sink.events[1])
	}
}

func TestAudit_EnableAndDisableAreSeparateEvents(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	on, off := true, false
	in := &UpdateModuleInput{Name: "demo"}
	in.Body.Enabled = &on
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.Body.Enabled = &off
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 || sink.events[0].Action != ActionModuleEnabled || sink.events[1].Action != ActionModuleDisabled {
		t.Errorf("events: %+v", sink.events)
	}
	if sink.events[0].Outcome != "success" || sink.events[1].Outcome != "success" {
		t.Errorf("outcomes: %+v", sink.events)
	}
}

func TestAudit_EnvironmentAndActivationSurfaces(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	env := &UpdateEnvironmentInput{Name: "demo", Env: "sandbox"}
	env.Body.Config = map[string]string{"flag": "true"}
	if _, err := h.UpdateEnvironment(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	act := &SetActiveEnvironmentInput{Name: "demo"}
	act.Body.Environment = "sandbox"
	if _, err := h.SetActiveEnvironment(context.Background(), act); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want 2", len(sink.events))
	}
	if sink.events[0].Action != ActionModuleConfigUpdated || sink.events[0].Metadata["env"] != "sandbox" {
		t.Errorf("env PATCH event: %+v", sink.events[0])
	}
	if sink.events[1].Action != ActionModuleEnvironmentActivated || sink.events[1].Metadata["env"] != "sandbox" || sink.events[1].Outcome != "success" {
		t.Errorf("activation event: %+v", sink.events[1])
	}
}

func TestAudit_NilSinkAndResolverTolerated_PanickingSinkContained(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	h.SetAuditSink(nil)
	h.SetActorResolver(nil)
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil)); err != nil {
		t.Fatalf("nil sink/resolver: %v", err)
	}
	sink.panics = true
	h.SetAuditSink(sink)
	out, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "false"}, nil))
	if err != nil || out == nil || out.Body.ConfigValues["flag"] != "false" {
		t.Fatalf("a failing sink changed the HTTP result: out=%v err=%v", out, err)
	}
}

// The recover() covers the WHOLE of emitAudit, not just the Emit call: the
// host's actor resolver and a fork module's ConfigSchema() are foreign code
// running after the mutation has already committed, so a panic there must
// not turn a successful write into a 500.
func TestAudit_PanickingActorResolverContained(t *testing.T) {
	h, _, sink := newAuditHandler(t, &auditDemoModule{})
	h.SetActorResolver(func(context.Context) AdminActor { panic("resolver exploded") })
	out, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil))
	if err != nil {
		t.Fatalf("a panicking resolver changed the HTTP result: %v", err)
	}
	if out == nil || out.Body.ConfigValues["flag"] != "true" {
		t.Fatalf("the write did not land: out=%v", out)
	}
	if len(sink.events) != 0 {
		t.Errorf("the aborted event still reached the sink: %+v", sink.events)
	}
}

// A combined PATCH writes the config and then starts the module. The config
// half persisted needsRestart=true because this module reads its config only
// at Init, and StartModule does not re-run Init — so the runtime start must
// not clear the hint it did not satisfy.
func TestUpdateModule_CombinedPatchKeepsNeedsRestartForAColdModule(t *testing.T) {
	h, repo, _ := newAuditHandler(t, &auditDemoModule{})
	enabled := true
	in := patchConfig(map[string]string{"flag": "true"}, nil)
	in.Body.Enabled = &enabled
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if !repo.docs["demo"].NeedsRestart {
		t.Error("a config change on a module without hot reload must keep needsRestart through the enable")
	}
	if repo.clearRestartCalls != 0 {
		t.Errorf("clearRestartCalls = %d, want 0", repo.clearRestartCalls)
	}
}

// An enable with no config half has nothing to re-read: the module starts on
// the config it already has, and the hint UpdateEnabled sets is cleared.
func TestUpdateModule_EnableOnlyClearsNeedsRestart(t *testing.T) {
	h, repo, _ := newAuditHandler(t, &auditDemoModule{})
	enabled := true
	in := &UpdateModuleInput{Name: "demo"}
	in.Body.Enabled = &enabled
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if repo.docs["demo"].NeedsRestart {
		t.Error("an enable-only PATCH must clear needsRestart")
	}
	if repo.clearRestartCalls != 1 {
		t.Errorf("clearRestartCalls = %d, want 1", repo.clearRestartCalls)
	}
}

// A hot-reloadable module re-reads its config at request time, so the
// combined PATCH owes no restart.
func TestUpdateModule_CombinedPatchClearsNeedsRestartWhenHotReloadable(t *testing.T) {
	h, repo, _ := newAuditHandler(t, &auditDemoModule{hotReload: true})
	enabled := true
	in := patchConfig(map[string]string{"flag": "true"}, nil)
	in.Body.Enabled = &enabled
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if repo.docs["demo"].NeedsRestart {
		t.Error("a hot-reloadable module must not keep needsRestart after a combined PATCH")
	}
	if repo.clearRestartCalls != 1 {
		t.Errorf("clearRestartCalls = %d, want 1", repo.clearRestartCalls)
	}
}

// The same rule on the way down: a config change followed by a disable
// leaves the hint standing for a cold module.
func TestUpdateModule_CombinedPatchKeepsNeedsRestartOnDisable(t *testing.T) {
	h, repo, _ := newAuditHandler(t, &auditDemoModule{})
	repo.docs["demo"].Enabled = true
	disabled := false
	in := patchConfig(map[string]string{"flag": "true"}, nil)
	in.Body.Enabled = &disabled
	if _, err := h.UpdateModule(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if !repo.docs["demo"].NeedsRestart {
		t.Error("a config change on a module without hot reload must keep needsRestart through the disable")
	}
	if repo.clearRestartCalls != 0 {
		t.Errorf("clearRestartCalls = %d, want 0", repo.clearRestartCalls)
	}
}

// The handler's pre-read and the service's own read are two reads: an
// activation landing between them moves the target profile. The event must
// name the profile the write ACTUALLY landed in, not the one the handler saw
// a moment earlier — an audit trail that files a production change under
// sandbox is worse than none.
func TestAudit_EnvIsTheProfileTheWriteTargeted(t *testing.T) {
	h, repo, sink := newAuditHandler(t, &auditDemoModule{})
	repo.beforeFind = func(call int) {
		// Call 1 is the handler's pre-read (production); the other operator's
		// activation lands before the service's own read on call 2.
		if call == 2 {
			repo.docs["demo"].ActiveEnvironment = "sandbox"
		}
	}
	if _, err := h.UpdateModule(context.Background(), patchConfig(map[string]string{"flag": "true"}, nil)); err != nil {
		t.Fatal(err)
	}
	doc := repo.docs["demo"]
	if doc.Environments["sandbox"].ConfigValues["flag"] != "true" {
		t.Errorf("the write did not land in sandbox: %v", doc.Environments["sandbox"].ConfigValues)
	}
	if doc.Environments["production"].ConfigValues["flag"] != "false" {
		t.Errorf("production was written: %v", doc.Environments["production"].ConfigValues)
	}
	if len(sink.events) != 1 || sink.events[0].Metadata["env"] != "sandbox" {
		t.Errorf("event env = %v, want sandbox", sink.events[0].Metadata["env"])
	}
}
