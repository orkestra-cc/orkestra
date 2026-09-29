package utils

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

var testHashKey = []byte("0123456789abcdef0123456789abcdef")

func policy(mut func(*iface.LogContentPolicy)) *iface.LogContentPolicy {
	p := iface.DefaultLogContentPolicy()
	if mut != nil {
		mut(&p)
	}
	return &p
}

func maskOne(t *testing.T, m logMasker, a slog.Attr) (any, bool) {
	t.Helper()
	out, keep := m.maskAttr(a)
	if !keep {
		return nil, false
	}
	return out.Value.Any(), true
}

func TestMaskAttr_SecretsAlwaysMasked(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.PIIKeys = nil; p.ScanFreeText = false }), key: testHashKey}
	for _, k := range []string{"password", "refresh_token", "Authorization", "api_key"} {
		if v, _ := maskOne(t, m, slog.String(k, "s3cr3t")); v != "[REDACTED]" {
			t.Errorf("%s = %v, want [REDACTED]", k, v)
		}
	}
}

func TestMaskAttr_IPModes(t *testing.T) {
	cases := []struct {
		mode iface.IPAddressMode
		in   string
		want string // "" with drop=true means the attribute is removed
		drop bool
	}{
		{iface.IPAddressFull, "203.0.113.7:51234", "203.0.113.7:51234", false},
		{iface.IPAddressTruncated, "203.0.113.7:51234", "203.0.113.0/24", false},
		{iface.IPAddressTruncated, "[2001:db8:1:2::1]:443", "2001:db8:1::/48", false},
		{iface.IPAddressTruncated, "not-an-ip", "[REDACTED]", false},
		{iface.IPAddressOmitted, "203.0.113.7", "", true},
		{iface.IPAddressTruncated, "", "", false},
	}
	for _, c := range cases {
		m := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = c.mode }), key: testHashKey}
		v, keep := maskOne(t, m, slog.String("remote", c.in))
		if c.drop {
			if keep {
				t.Errorf("%s %q: kept %v, want dropped", c.mode, c.in, v)
			}
			continue
		}
		if v != c.want {
			t.Errorf("%s %q = %v, want %q", c.mode, c.in, v, c.want)
		}
	}
}

func TestMaskAttr_IPHashedStripsPortAndIsStable(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressHashed }), key: testHashKey}
	a, _ := maskOne(t, m, slog.String("client_ip", "203.0.113.7:1111"))
	b, _ := maskOne(t, m, slog.String("ip", "203.0.113.7"))
	if a != b || !strings.HasPrefix(a.(string), "h:") || len(a.(string)) != 18 {
		t.Fatalf("hashed IP not stable or malformed: %v vs %v", a, b)
	}
	noKey := logMasker{p: m.p}
	if _, keep := maskOne(t, noKey, slog.String("ip", "203.0.113.7")); keep {
		t.Fatal("hashed mode without a key must drop the value")
	}
}

func TestMaskAttr_UserAgentAndSubjects(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) {
		p.UserAgent = iface.UserAgentOmitted
		p.SubjectIDs = iface.SubjectIDHashed
	}), key: testHashKey}
	if _, keep := maskOne(t, m, slog.String("ua", "Mozilla/5.0")); keep {
		t.Error("ua kept with userAgent=omitted")
	}
	v, _ := maskOne(t, m, slog.String("user_id", "3f2b"))
	if !strings.HasPrefix(v.(string), "h:") {
		t.Errorf("user_id = %v, want hashed", v)
	}
	ip, _ := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressHashed }), key: testHashKey}.maskIP("3f2b")
	if ip == v {
		t.Error("ip and subject hashes of the same value must differ (domain separation)")
	}
	m.p.SubjectIDs = iface.SubjectIDOmitted
	if _, keep := maskOne(t, m, slog.String("actorUserId", "3f2b")); keep {
		t.Error("actorUserId kept with subjectIds=omitted")
	}
}

func TestMaskAttr_PIIKeysExactMatch(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	if v, _ := maskOne(t, m, slog.String("first_name", "Anna")); v != "[PII]" {
		t.Errorf("first_name = %v, want [PII]", v)
	}
	if v, _ := maskOne(t, m, slog.String("filename", "report.pdf")); v != "report.pdf" {
		t.Errorf("filename = %v, must not be masked (exact match only)", v)
	}
}

func TestMaskAttr_FreeText(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	in := "sent to anna.rossi@example.com from 203.0.113.7, IBAN IT60X0542811101000000123456, CF RSSMRA85T10A562S"
	v, _ := maskOne(t, m, slog.String("detail", in))
	got := v.(string)
	for _, leak := range []string{"anna.rossi", "203.0.113.7", "IT60X0542811101000000123456", "RSSMRA85T10A562S"} {
		if strings.Contains(got, leak) {
			t.Errorf("free text leaked %q: %s", leak, got)
		}
	}
	for _, mark := range []string{"[EMAIL]", "203.0.113.0/24", "[IBAN]", "[CF]"} {
		if !strings.Contains(got, mark) {
			t.Errorf("free text missing %q: %s", mark, got)
		}
	}
	off := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.ScanFreeText = false })}
	if v, _ := maskOne(t, off, slog.String("detail", in)); v != in {
		t.Errorf("scanFreeText=false changed the value: %v", v)
	}
}

func TestMaskAttr_IPv6InFreeTextTruncatedOnce(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	v, _ := maskOne(t, m, slog.String("detail", "peer 2001:db8:1:2::1 closed"))
	if v != "peer 2001:db8:1::/48 closed" {
		t.Fatalf("got %v", v)
	}
}

func TestMaskAttr_GroupsMapsErrors(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	out, _ := m.maskAttr(slog.Group("req", slog.String("email", "a@b.it"), slog.String("path", "/v1")))
	g := out.Value.Group()
	if g[0].Value.String() != "[PII]" || g[1].Value.String() != "/v1" {
		t.Errorf("group not masked: %v", g)
	}
	mv, _ := maskOne(t, m, slog.Any("payload", map[string]any{"token": "x", "nested": map[string]any{"phone": "333"}, "list": []any{"a@b.it"}}))
	mm := mv.(map[string]any)
	if mm["token"] != "[REDACTED]" || mm["nested"].(map[string]any)["phone"] != "[PII]" || mm["list"].([]any)[0] != "[EMAIL]" {
		t.Errorf("map not masked: %v", mm)
	}
	ev, _ := maskOne(t, m, slog.Any("error", errors.New("user a@b.it not found")))
	if ev != "user [EMAIL] not found" {
		t.Errorf("error = %v", ev)
	}
}

func TestMaskAttr_RecoversFromPanic(t *testing.T) {
	var panics int
	SetMaskingPanicHook(func() { panics++ })
	t.Cleanup(func() { SetMaskingPanicHook(nil) })
	m := logMasker{} // nil policy: any policy read panics
	out, keep := m.maskAttr(slog.String("email", "a@b.it"))
	if !keep || out.Value.String() != maskErrValue {
		t.Fatalf("panic not recovered into %s: %v %v", maskErrValue, out, keep)
	}
	if got := m.safeText("x"); got != maskErrValue {
		t.Fatalf("safeText = %q", got)
	}
	if panics != 2 {
		t.Fatalf("panic hook called %d times, want 2", panics)
	}
}
