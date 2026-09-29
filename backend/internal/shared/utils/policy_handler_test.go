package utils

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// mapResolver returns byTenant[tenant], falling back to byTenant[""].
type mapResolver map[string]*iface.LogContentPolicy

func (m mapResolver) LogContentFor(tenant string) *iface.LogContentPolicy {
	if p, ok := m[tenant]; ok {
		return p
	}
	return m[""]
}

func newPolicyLogger(t *testing.T, r LogPolicyResolver) (*slog.Logger, *PolicyHandler, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	h := NewPolicyHandler(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}), r, testHashKey)
	return slog.New(h), h, buf
}

func TestPolicyHandler_TenantFromContext(t *testing.T) {
	strict := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	loose := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	logger, _, buf := newPolicyLogger(t, mapResolver{"": strict, "t-loose": loose})

	ctx := context.WithValue(context.Background(), ctxauth.KeyTenantID, "t-loose")
	logger.InfoContext(ctx, "x", slog.String("ip", "203.0.113.7"))
	if line := parseLines(t, buf.Bytes())[0]; line["ip"] != "203.0.113.7" {
		t.Fatalf("tenant policy not applied: %v", line)
	}
}

func TestPolicyHandler_TenantFromAnnotations(t *testing.T) {
	strict := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	loose := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	logger, _, buf := newPolicyLogger(t, mapResolver{"": strict, "t-loose": loose})

	ctx, ann := ctxauth.WithRequestAnnotations(context.Background())
	ann.SetPrincipal("t-loose", "", "", "")
	logger.InfoContext(ctx, "http_request", slog.String("remote", "203.0.113.7:1234"))
	if line := parseLines(t, buf.Bytes())[0]; line["remote"] != "203.0.113.7:1234" {
		t.Fatalf("annotated tenant policy not applied: %v", line)
	}
}

// Review Focus 2: a context-less line while a stricter tenant policy is
// active must get the strictest policy, not the platform one.
func TestPolicyHandler_NoTenantUsesStrictest(t *testing.T) {
	strictest := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	platform := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	logger, _, buf := newPolicyLogger(t, mapResolver{"": strictest, "t-platform": platform})

	logger.Info("no ctx", slog.String("ip", "203.0.113.7"))
	if _, ok := parseLines(t, buf.Bytes())[0]["ip"]; ok {
		t.Fatal("context-less line kept the IP: it must use the strictest policy")
	}
}

func TestPolicyHandler_WithAttrsMaskedAtWriteTimeAfterSwap(t *testing.T) {
	full := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	omitted := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	logger, h, buf := newPolicyLogger(t, mapResolver{"": full})
	child := logger.With(slog.String("ip", "203.0.113.7")).WithGroup("g").With(slog.String("email", "a@b.it"))

	child.Info("before")
	h.SetResolver(mapResolver{"": omitted})
	child.Info("after")

	lines := parseLines(t, buf.Bytes())
	if lines[0]["ip"] != "203.0.113.7" {
		t.Fatalf("before swap: %v", lines[0])
	}
	if _, ok := lines[1]["ip"]; ok {
		t.Fatalf("after swap the With attr must follow the new policy: %v", lines[1])
	}
	if g := lines[1]["g"].(map[string]any); g["email"] != "[PII]" {
		t.Fatalf("grouped With attr not masked: %v", lines[1])
	}
}

func TestPolicyHandler_MessageScanned(t *testing.T) {
	logger, _, buf := newPolicyLogger(t, NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy()))
	logger.Info("login failed for anna@example.com")
	if msg := parseLines(t, buf.Bytes())[0]["msg"]; msg != "login failed for [EMAIL]" {
		t.Fatalf("msg = %v", msg)
	}
}

func TestSetupLogger_InstallsPolicyHandler(t *testing.T) {
	t.Setenv("ENV", "production")
	logger := SetupLogger()
	h := globalPolicyBox.Load()
	if h == nil {
		t.Fatal("SetupLogger did not register the policy handler box")
	}
	_ = logger
}

func TestLogHashKeyFromEnv(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", "")
	if LogHashKeyFromEnv() != nil {
		t.Fatal("missing key must yield nil")
	}
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", "zz")
	if LogHashKeyFromEnv() != nil {
		t.Fatal("non-hex key must yield nil")
	}
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", hex.EncodeToString(testHashKey))
	k1 := LogHashKeyFromEnv()
	if len(k1) != 32 || bytes.Equal(k1, testHashKey) {
		t.Fatal("key must be a 32-byte HKDF derivation, not the raw secret")
	}
	if !bytes.Equal(k1, LogHashKeyFromEnv()) {
		t.Fatal("derivation must be deterministic")
	}
}

// A WithGroup whose name is claimed by a group key rule must behave like an
// inline slog.Group with that name: everything under it collapses into the
// single masked outcome (or is dropped), for the record's own attributes and
// for later With attributes alike.
func TestPolicyHandler_WithGroupFollowsGroupKeyRules(t *testing.T) {
	cases := []struct {
		name string
		key  string
		mut  func(*iface.LogContentPolicy)
	}{
		{"secret", "password", nil},
		{"secret-nested-word", "refresh_token", nil},
		{"ip-omitted", "ip", func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted }},
		{"ip-truncated", "ip", func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressTruncated }},
		{"ip-full", "ip", func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull }},
		{"ua-omitted", "user_agent", func(p *iface.LogContentPolicy) { p.UserAgent = iface.UserAgentOmitted }},
		{"subject-omitted", "user_id", func(p *iface.LogContentPolicy) { p.SubjectIDs = iface.SubjectIDOmitted }},
		{"pii-key", "email", nil},
	}
	strip := func(m map[string]any) map[string]any { delete(m, "time"); return m }
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := NewStaticLogPolicyResolver(*policy(c.mut))
			inline, _, ibuf := newPolicyLogger(t, r)
			inline.Info("a", slog.Group(c.key, slog.String("value", "hunter2")))
			viaWith, _, wbuf := newPolicyLogger(t, r)
			viaWith.WithGroup(c.key).Info("a", slog.String("value", "hunter2"))
			laterWith, _, lbuf := newPolicyLogger(t, r)
			laterWith.WithGroup(c.key).With(slog.String("value", "hunter2")).Info("a")

			want := strip(parseLines(t, ibuf.Bytes())[0])
			for name, buf := range map[string]*bytes.Buffer{"WithGroup+record attr": wbuf, "WithGroup+With attr": lbuf} {
				if strings.Contains(buf.String(), "hunter2") && !strings.Contains(ibuf.String(), "hunter2") {
					t.Fatalf("%s leaked the value: %s", name, buf.String())
				}
				if got := strip(parseLines(t, buf.Bytes())[0]); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s = %v, inline slog.Group = %v", name, got, want)
				}
			}
		})
	}
}

func TestPolicyHandler_WithGroupNestedSealed(t *testing.T) {
	r := NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy())
	strip := func(m map[string]any) map[string]any { delete(m, "time"); return m }
	inline, _, ibuf := newPolicyLogger(t, r)
	inline.Info("a", slog.Group("safe", slog.Group("password", slog.String("value", "hunter2")), slog.String("ok", "1")))
	nested, _, nbuf := newPolicyLogger(t, r)
	nested.WithGroup("safe").WithGroup("password").Info("a", slog.String("value", "hunter2"))

	got := strip(parseLines(t, nbuf.Bytes())[0])
	if strings.Contains(nbuf.String(), "hunter2") {
		t.Fatalf("nested sealed group leaked: %v", got)
	}
	safe, ok := got["safe"].(map[string]any)
	if !ok || safe["password"] != "[REDACTED]" {
		t.Fatalf("WithGroup(safe).WithGroup(password) = %v", got)
	}
	// Same shape as the inline group, minus the sibling "ok" attribute.
	want := strip(parseLines(t, ibuf.Bytes())[0])
	if want["safe"].(map[string]any)["password"] != "[REDACTED]" {
		t.Fatalf("inline reference = %v", want)
	}
}

// A sealed group with nothing logged under it emits nothing, and a later
// non-sealed sibling group is unaffected.
func TestPolicyHandler_WithGroupSealedEmptyAndDeeper(t *testing.T) {
	r := NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy())
	logger, _, buf := newPolicyLogger(t, r)
	logger.WithGroup("password").Info("empty")
	logger.WithGroup("password").WithGroup("inner").With(slog.String("k", "hunter2")).Info("deep", slog.String("v", "hunter2"))
	lines := parseLines(t, buf.Bytes())
	if _, ok := lines[0]["password"]; ok {
		t.Fatalf("sealed group with no content must not be emitted: %v", lines[0])
	}
	if strings.Contains(buf.String(), "hunter2") || lines[1]["password"] != "[REDACTED]" {
		t.Fatalf("groups below a sealed group must collapse: %v", lines[1])
	}
}

// capturingHandler records every attribute it is given (handler-level and
// record-level) as a flat key=value list, and every message.
type capturingHandler struct {
	st    *captureState
	attrs []slog.Attr
}

type captureState struct {
	mu   sync.Mutex
	msgs []string
	kv   []string
}

func (c *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (c *capturingHandler) WithAttrs(a []slog.Attr) slog.Handler {
	return &capturingHandler{st: c.st, attrs: append(append([]slog.Attr{}, c.attrs...), a...)}
}
func (c *capturingHandler) WithGroup(string) slog.Handler { return c }
func (c *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	c.st.mu.Lock()
	defer c.st.mu.Unlock()
	c.st.msgs = append(c.st.msgs, r.Message)
	add := func(a slog.Attr) bool { c.st.kv = append(c.st.kv, a.Key+"="+a.Value.String()); return true }
	for _, a := range c.attrs {
		add(a)
	}
	r.Attrs(add)
	return nil
}

// Chain position (spec §2.3): the fan-out members (stdout, OTLP) receive the
// already-masked record, and the level gate sits in front of the mask.
func TestSetupLogger_FanoutReceivesMaskedRecords(t *testing.T) {
	prevBox, prevPM := globalPolicyBox.Load(), globalPerModule.Load()
	t.Cleanup(func() { globalPolicyBox.Store(prevBox); globalPerModule.Store(prevPM) })
	t.Setenv("ENV", "production")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", "")

	st := &captureState{}
	logger := SetupLogger(&capturingHandler{st: st})
	logger.Info("login failed for anna@example.com",
		slog.String("password", "hunter2"),
		slog.String("remote", "203.0.113.7:4711"),
		slog.String("note", "mail bob@example.org"))
	logger.Debug("below the level gate anna@example.com", slog.String("password", "hunter2"))

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.msgs) != 1 {
		t.Fatalf("extra handler got %d records, want 1 (debug must be gated): %v", len(st.msgs), st.msgs)
	}
	if st.msgs[0] != "login failed for [EMAIL]" {
		t.Fatalf("message not masked before the fan-out: %q", st.msgs[0])
	}
	all := strings.Join(st.kv, "\n")
	for _, leak := range []string{"hunter2", "203.0.113.7", "bob@example.org", "anna@example.com"} {
		if strings.Contains(all, leak) || strings.Contains(st.msgs[0], leak) {
			t.Fatalf("extra handler saw %q unmasked:\n%s", leak, all)
		}
	}
	for _, want := range []string{"password=[REDACTED]", "remote=203.0.113.0/24", "note=mail [EMAIL]"} {
		if !strings.Contains(all, want) {
			t.Fatalf("extra handler missing %q:\n%s", want, all)
		}
	}
}

// benchAttrs is the 10-attribute record shared by the two benchmarks below;
// the spec budget (§2.3) is the difference between them.
func benchAttrs() []any {
	return []any{
		slog.String("method", "GET"), slog.String("path", "/v1/x"), slog.Int("status", 200),
		slog.Int64("duration_ms", 3), slog.Int("bytes", 120), slog.String("remote", "203.0.113.7:1"),
		slog.String("ua", "Mozilla/5.0"), slog.String("request_id", "r-1"), slog.String("tenant_id", "t"),
		slog.String("user_id", "u"),
	}
}

func benchWith(l *slog.Logger) *slog.Logger {
	return l.With(slog.String("service", "orkestra-backend"), slog.String("version", "1"), slog.String("environment", "production"))
}

// BenchmarkPlainJSON_TenAttrs is the baseline: the same record through the
// bare slog JSON handler.
func BenchmarkPlainJSON_TenAttrs(b *testing.B) {
	logger := benchWith(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	attrs := benchAttrs()
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("http_request", attrs...)
	}
}

func BenchmarkPolicyHandler_TenAttrs(b *testing.B) {
	p := iface.DefaultLogContentPolicy()
	p.ScanFreeText = false
	h := NewPolicyHandler(slog.NewJSONHandler(io.Discard, nil), NewStaticLogPolicyResolver(p), testHashKey)
	logger := benchWith(slog.New(h))
	attrs := benchAttrs()
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("http_request", attrs...)
	}
}
