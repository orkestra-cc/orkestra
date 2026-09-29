package utils

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
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

func BenchmarkPolicyHandler_TenAttrs(b *testing.B) {
	p := iface.DefaultLogContentPolicy()
	p.ScanFreeText = false
	h := NewPolicyHandler(slog.NewJSONHandler(io.Discard, nil), NewStaticLogPolicyResolver(p), testHashKey)
	logger := slog.New(h).With(slog.String("service", "orkestra-backend"), slog.String("version", "1"), slog.String("environment", "production"))
	attrs := []any{
		slog.String("method", "GET"), slog.String("path", "/v1/x"), slog.Int("status", 200),
		slog.Int64("duration_ms", 3), slog.Int("bytes", 120), slog.String("remote", "203.0.113.7:1"),
		slog.String("ua", "Mozilla/5.0"), slog.String("request_id", "r-1"), slog.String("tenant_id", "t"),
		slog.String("user_id", "u"),
	}
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("http_request", attrs...)
	}
}
