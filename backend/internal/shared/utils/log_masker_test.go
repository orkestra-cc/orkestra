package utils

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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

// --- regression tests for the review fix round ---

func maskedText(t *testing.T, m logMasker, in string) string {
	t.Helper()
	v, keep := maskOne(t, m, slog.String("detail", in))
	if !keep {
		t.Fatalf("detail %q dropped", in)
	}
	return v.(string)
}

func modeMasker(mode iface.IPAddressMode) logMasker {
	return logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = mode }), key: testHashKey}
}

func TestMaskText_IPv6Boundaries(t *testing.T) {
	trunc := modeMasker(iface.IPAddressTruncated)
	hashed := modeMasker(iface.IPAddressHashed)
	omitted := modeMasker(iface.IPAddressOmitted)
	h6 := hashed.hash("ip:", "2001:db8:1:2::1")
	cases := []struct {
		name string
		m    logMasker
		in   string
		want string
	}{
		{"trailing colon truncated", trunc, "lookup 2001:db8:1:2::1: no such host", "lookup 2001:db8:1::/48: no such host"},
		{"trailing colon hashed", hashed, "lookup 2001:db8:1:2::1: no such host", "lookup " + h6 + ": no such host"},
		{"trailing colon omitted", omitted, "lookup 2001:db8:1:2::1: no such host", "lookup [IP] no such host"},
		{"word colon prefix truncated", trunc, "ip:2001:db8:1:2::1", "ip:2001:db8:1::/48"},
		{"word colon prefix hashed", hashed, "ip:2001:db8:1:2::1", "ip:" + h6},
		{"word colon prefix omitted", omitted, "ip:2001:db8:1:2::1", "ip[IP]"},
		{"trailing full stop", trunc, "peer 2001:db8:1:2::1.", "peer 2001:db8:1::/48."},
		{"bracketed with port", trunc, "dial [2001:db8:1:2::1]:443", "dial [2001:db8:1::/48]:443"},
		{"loopback", trunc, "on ::1 only", "on ::/48 only"},
		{"clock time untouched", trunc, "at 10:20:30 and 10:20:30.123", "at 10:20:30 and 10:20:30.123"},
		{"ipv4 with port then colon", trunc, "dial tcp 203.0.113.7:51234: connection refused", "dial tcp 203.0.113.0/24:51234: connection refused"},
	}
	for _, c := range cases {
		if got := maskedText(t, c.m, c.in); got != c.want {
			t.Errorf("%s: %q -> %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestMaskText_IPv4MappedIPv6(t *testing.T) {
	in := "peer ::ffff:203.0.113.7 closed"
	h4 := modeMasker(iface.IPAddressHashed).hash("ip:", "203.0.113.7")
	cases := []struct {
		mode iface.IPAddressMode
		want string
	}{
		{iface.IPAddressTruncated, "peer 203.0.113.0/24 closed"},
		{iface.IPAddressHashed, "peer " + h4 + " closed"},
		{iface.IPAddressOmitted, "peer [IP] closed"},
		{iface.IPAddressFull, in},
	}
	for _, c := range cases {
		if got := maskedText(t, modeMasker(c.mode), in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.mode, got, c.want)
		}
	}
}

func TestMaskText_IPModesInFreeText(t *testing.T) {
	in := "from 203.0.113.7 ok"
	h4 := modeMasker(iface.IPAddressHashed).hash("ip:", "203.0.113.7")
	cases := []struct {
		name string
		m    logMasker
		want string
	}{
		{"hashed", modeMasker(iface.IPAddressHashed), "from " + h4 + " ok"},
		{"hashed without key", logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressHashed })}, "from [IP] ok"},
		{"omitted", modeMasker(iface.IPAddressOmitted), "from [IP] ok"},
		{"unknown mode fails closed", modeMasker(iface.IPAddressMode("bogus")), "from [IP] ok"},
	}
	for _, c := range cases {
		if got := maskedText(t, c.m, in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMaskText_IPv4Edges(t *testing.T) {
	m := modeMasker(iface.IPAddressTruncated)
	cases := []struct{ in, want string }{
		{"client_203.0.113.7", "client_203.0.113.0/24"},
		{"at 203.0.113.7.", "at 203.0.113.0/24."},
		{"ip=203.0.113.7,x", "ip=203.0.113.0/24,x"},
		{"203.000.113.007", "[IP]"},
		{"from 999.1.1.1 x", "from [IP] x"},
		{"build 1.2.3.4.5 done", "build [IP] done"},
		{"build 999.999.999.999.999 done", "build [IP] done"},
		{"v1.203.000.113.007", "v[IP]"},
		{"stamp 2026.09.29 and 2026.09.29.12", "stamp 2026.09.29 and 2026.09.29.12"},
	}
	for _, c := range cases {
		if got := maskedText(t, m, c.in); got != c.want {
			t.Errorf("%q -> %q, want %q", c.in, got, c.want)
		}
	}
	if got := maskedText(t, modeMasker(iface.IPAddressFull), "203.000.113.007"); got != "203.000.113.007" {
		t.Errorf("full mode changed the text: %q", got)
	}
}

func TestMaskText_IBANAndCodiceFiscaleForms(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	cases := []struct{ in, want string }{
		{"IT60 X054 2811 1010 0000 0123 456", "[IBAN]"},
		{"iban: IT60 X054 2811 1010 0000 0123 456, ok", "iban: [IBAN], ok"},
		{"it60x0542811101000000123456", "[IBAN]"},
		{"IT60X0542811101000000123456", "[IBAN]"},
		{"CF RSSMRA85T10A56NS", "CF [CF]"},
		{"CF rssmra85t10a562s", "CF [CF]"},
		{"CF RSSMRA85T10A562S", "CF [CF]"},
	}
	for _, c := range cases {
		if got := maskedText(t, m, c.in); got != c.want {
			t.Errorf("%q -> %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMaskAttr_UnknownModesFailClosed(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) {
		p.IPAddress = "bogus"
		p.UserAgent = "bogus"
		p.SubjectIDs = "bogus"
	}), key: testHashKey}
	for _, a := range []slog.Attr{slog.String("remote", "203.0.113.7"), slog.String("ua", "Mozilla/5.0"), slog.String("user_id", "3f2b")} {
		if v, keep := maskOne(t, m, a); keep {
			t.Errorf("%s kept as %v with an unknown mode", a.Key, v)
		}
	}
}

func TestMaskAttr_GroupKeyRules(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	out, keep := m.maskAttr(slog.Group("credentials", "user", "anna", "value", "hunter2"))
	if !keep || out.Value.Kind() != slog.KindString || out.Value.String() != "[REDACTED]" {
		t.Errorf("secret group = %v, want the string [REDACTED]", out.Value)
	}
	out, keep = m.maskAttr(slog.Group("address", "street", "Via Roma 1"))
	if !keep || out.Value.Kind() != slog.KindString || out.Value.String() != "[PII]" {
		t.Errorf("PII group = %v, want the string [PII]", out.Value)
	}
	out, _ = m.maskAttr(slog.Group("req", slog.Group("credentials", "x", "y"), slog.String("path", "/v1")))
	g := out.Value.Group()
	if g[0].Value.String() != "[REDACTED]" || g[1].Value.String() != "/v1" {
		t.Errorf("nested secret group not masked: %v", g)
	}
	out, _ = m.maskAttr(slog.Group("", slog.String("email", "a@b.it"), slog.String("path", "/v1")))
	g = out.Value.Group()
	if g[0].Value.String() != "[PII]" || g[1].Value.String() != "/v1" {
		t.Errorf("inline group not recursed: %v", g)
	}
}

func TestMaskAttr_SelfReferencingContainersTerminate(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	self := map[string]any{}
	self["self"] = self
	mv, _ := maskOne(t, m, slog.Any("payload", self))
	cur := mv.(map[string]any)
	depth := 0
	for {
		next := cur["self"]
		if s, ok := next.(string); ok {
			if s != "[REDACTED]" {
				t.Fatalf("depth cap value = %q, want [REDACTED]", s)
			}
			break
		}
		cur = next.(map[string]any)
		if depth++; depth > 40 {
			t.Fatal("recursion was not capped")
		}
	}
	loop := []any{nil}
	loop[0] = loop
	lv, _ := maskOne(t, m, slog.Any("list", loop))
	inner := lv.([]any)
	for i := 0; i < 40; i++ {
		if s, ok := inner[0].(string); ok {
			if s != "[REDACTED]" {
				t.Fatalf("slice depth cap value = %q", s)
			}
			return
		}
		inner = inner[0].([]any)
	}
	t.Fatal("slice recursion was not capped")
}

func TestMaskAttr_StringContainers(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}

	sv, _ := maskOne(t, m, slog.Any("emails", []string{"a@b.it", "plain"}))
	if got := sv.([]string); got[0] != "[EMAIL]" || got[1] != "plain" {
		t.Errorf("[]string = %v", got)
	}

	mv, _ := maskOne(t, m, slog.Any("form", map[string]string{"password": "x", "phone": "333", "note": "call a@b.it"}))
	ms := mv.(map[string]string)
	if ms["password"] != "[REDACTED]" || ms["phone"] != "[PII]" || ms["note"] != "call [EMAIL]" {
		t.Errorf("map[string]string = %v", ms)
	}

	hv, _ := maskOne(t, m, slog.Any("headers", http.Header{
		"Authorization": {"Bearer abc"},
		"X-Note":        {"from 203.0.113.7", "ok"},
		"Email":         {"a@b.it"},
	}))
	mh := hv.(http.Header)
	if mh["Authorization"][0] != "[REDACTED]" || mh["X-Note"][0] != "from 203.0.113.0/24" || mh["X-Note"][1] != "ok" || mh["Email"][0] != "[PII]" {
		t.Errorf("map[string][]string = %v", mh)
	}

	uv, _ := maskOne(t, m, slog.Any("query", url.Values{"token": {"t"}, "q": {"a@b.it"}}))
	mu := uv.(url.Values)
	if mu["token"][0] != "[REDACTED]" || mu["q"][0] != "[EMAIL]" {
		t.Errorf("url.Values = %v", mu)
	}
}

// --- fix round 2 ---

func TestMaskText_WordTailBeforeUncompressedIPv6(t *testing.T) {
	// A word ending in hex letters joins the run: the run is not exactly one
	// address, so every mode renders "[IP]" and no group can stay behind.
	full := "2001:db8:85a3:0:0:8a2e:370:7334"
	modes := []iface.IPAddressMode{iface.IPAddressTruncated, iface.IPAddressHashed, iface.IPAddressOmitted}
	cases := []struct{ in, want string }{
		{"src:" + full, "sr[IP]"},
		{"failed:2001:db8:85a3:1:2:8a2e:370:7334", "fail[IP]"},
		{"cafe:beef:2001:db8:85a3:1:2:8a2e:370:7334", "[IP]"},
		{"v1.2001:db8:85a3:0:0:8a2e:370:7334", "v[IP]"},
		{"2001:db8:85a3:1:2:8a2e:370:7334:8080", "[IP]"},
	}
	for _, mode := range modes {
		for _, c := range cases {
			got := maskedText(t, modeMasker(mode), c.in)
			if got != c.want {
				t.Errorf("%s: %q -> %q, want %q", mode, c.in, got, c.want)
			}
		}
	}
	// A lone non-hex word keeps its colon and the address is rendered exactly.
	h := modeMasker(iface.IPAddressHashed).hash("ip:", net.ParseIP(full).String())
	exact := []struct {
		mode iface.IPAddressMode
		want string
	}{
		{iface.IPAddressTruncated, "ip:2001:db8:85a3::/48"},
		{iface.IPAddressHashed, "ip:" + h},
		{iface.IPAddressOmitted, "ip[IP]"},
	}
	for _, c := range exact {
		if got := maskedText(t, modeMasker(c.mode), "ip:"+full); got != c.want {
			t.Errorf("%s: got %q, want %q", c.mode, got, c.want)
		}
	}
}

func TestMaskText_IPv4WithDottedNeighbours(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v1.203.0.113.7", "v[IP]"},
		{"release 1.203.0.113.7 ok", "release [IP] ok"},
		{"203.0.113.7.5", "[IP]"},
		{"9.9.203.0.113.7", "[IP]"},
		{"cafe203.0.113.7", "[IP]"},
	}
	for _, mode := range []iface.IPAddressMode{iface.IPAddressTruncated, iface.IPAddressHashed, iface.IPAddressOmitted} {
		for _, c := range cases {
			if got := maskedText(t, modeMasker(mode), c.in); got != c.want {
				t.Errorf("%s: %q -> %q, want %q", mode, c.in, got, c.want)
			}
		}
	}
}

func TestMaskText_LongRunFailsClosed(t *testing.T) {
	long := "ab:" + strings.Repeat("f", 112) + ":2001:db8:1:2::1"
	for _, mode := range []iface.IPAddressMode{iface.IPAddressTruncated, iface.IPAddressHashed, iface.IPAddressOmitted} {
		if got := maskedText(t, modeMasker(mode), "x "+long+" y"); got != "x [IP] y" {
			t.Errorf("%s: got %q, want %q", mode, got, "x [IP] y")
		}
	}
	if got := maskedText(t, modeMasker(iface.IPAddressFull), long); got != long {
		t.Errorf("full mode changed the text")
	}
}

func TestMaskText_IPv6AfterIPv4Octets(t *testing.T) {
	for _, mode := range []iface.IPAddressMode{iface.IPAddressTruncated, iface.IPAddressHashed, iface.IPAddressOmitted} {
		if got := maskedText(t, modeMasker(mode), "at 10.0.0.1:2001:db8:1:2::1 up"); got != "at [IP] up" {
			t.Errorf("%s: got %q, want %q", mode, got, "at [IP] up")
		}
	}
}

func TestMaskAttr_BranchingCycleIsBounded(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	self := map[string]any{}
	self["a"] = self
	self["b"] = self
	start := time.Now()
	out, keep := m.maskAttr(slog.Any("payload", self))
	if !keep || out.Value.String() != "[REDACTED]" {
		t.Fatalf("branching cycle = %v %v, want the whole value [REDACTED]", out, keep)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("branching cycle took %v", d)
	}
	// a node budget is per attribute: the next attribute is masked normally
	if v, _ := maskOne(t, m, slog.Any("ok", map[string]any{"a": "x@y.it"})); v.(map[string]any)["a"] != "[EMAIL]" {
		t.Errorf("budget leaked across attributes: %v", v)
	}
	big := make([]any, maxMaskNodes+5)
	for i := range big {
		big[i] = "v"
	}
	if bv, _ := maskOne(t, m, slog.Any("big", big)); bv != "[REDACTED]" {
		t.Errorf("a slice past the node budget must become [REDACTED], got %T", bv)
	}
}

func TestMaskAttr_GroupKeyRulesIPUserAgentSubject(t *testing.T) {
	child := []any{"addr", "203.0.113.7"}
	cases := []struct {
		name string
		mut  func(*iface.LogContentPolicy)
		attr slog.Attr
		keep bool
		want string // string value when kept as a string; "" = kept as a group
	}{
		{"ip group truncated", func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressTruncated }, slog.Group("client_ip", child...), true, "[REDACTED]"},
		{"ip group hashed", func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressHashed }, slog.Group("remote", child...), true, "[REDACTED]"},
		{"ip group omitted", func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted }, slog.Group("ip", child...), false, ""},
		{"ip group full recurses", func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull }, slog.Group("ip", child...), true, ""},
		{"ua group omitted", func(p *iface.LogContentPolicy) { p.UserAgent = iface.UserAgentOmitted }, slog.Group("user_agent", "raw", "Mozilla/5.0"), false, ""},
		{"ua group full recurses", nil, slog.Group("user_agent", "raw", "Mozilla/5.0"), true, ""},
		{"subject group omitted", func(p *iface.LogContentPolicy) { p.SubjectIDs = iface.SubjectIDOmitted }, slog.Group("user_id", "value", "3f2b"), false, ""},
		{"subject group hashed", func(p *iface.LogContentPolicy) { p.SubjectIDs = iface.SubjectIDHashed }, slog.Group("actorUserId", "value", "3f2b"), false, ""},
		{"subject group uuid recurses", nil, slog.Group("user_id", "value", "3f2b"), true, ""},
	}
	for _, c := range cases {
		m := logMasker{p: policy(c.mut), key: testHashKey}
		out, keep := m.maskAttr(c.attr)
		if keep != c.keep {
			t.Errorf("%s: keep = %v, want %v", c.name, keep, c.keep)
			continue
		}
		if !keep {
			continue
		}
		if c.want != "" {
			if out.Value.Kind() != slog.KindString || out.Value.String() != c.want {
				t.Errorf("%s: got %v, want string %q", c.name, out.Value, c.want)
			}
		} else if out.Value.Kind() != slog.KindGroup {
			t.Errorf("%s: got %v, want a recursed group", c.name, out.Value)
		}
	}
}

// --- fix round 3: one fail-closed IP rule, header/budget/group-depth safety ---

func TestMaskText_SingleIPRuleProbes(t *testing.T) {
	trunc := modeMasker(iface.IPAddressTruncated)
	omitted := modeMasker(iface.IPAddressOmitted)
	hashed := modeMasker(iface.IPAddressHashed)
	cases := []struct {
		name string
		m    logMasker
		in   string
		want string
	}{
		{"ipv4 with port", trunc, "203.0.113.7:51234", "203.0.113.0/24:51234"},
		{"ipv4 with port hashed", hashed, "203.0.113.7:51234", hashed.hash("ip:", "203.0.113.7") + ":51234"},
		{"ipv4 with port omitted", omitted, "203.0.113.7:51234", "[IP]"},
		{"ipv4 port and colon", trunc, "dial tcp 203.0.113.7:51234: connection refused", "dial tcp 203.0.113.0/24:51234: connection refused"},
		{"bracketed ipv6 with port", trunc, "[2001:db8::1]:443", "[2001:db8::/48]:443"},
		{"bracketed ipv6 with port omitted", omitted, "[2001:db8::1]:443", "[IP]:443"},
		{"bracketed with zone", trunc, "peer [fe80::1%eth0] up", "peer [fe80::/48] up"},
		{"zone without brackets", omitted, "peer fe80::1%eth0 up", "peer [IP] up"},
		{"timestamp", trunc, "10:20:30", "10:20:30"},
		{"timestamp with fraction", trunc, "at 10:20:30.123 done", "at 10:20:30.123 done"},
		{"cpp scope truncated", trunc, "std::string", "std::/48string"},
		{"cpp scope omitted", omitted, "std::string", "st[IP]string"},
		{"version five numbers", trunc, "1.2.3.4.5", "[IP]"},
		{"zero padded with prefix", omitted, "v1.203.000.113.007", "v[IP]"},
		{"two addresses", trunc, "1.2.3.4,5.6.7.8", "1.2.3.0/24,5.6.7.0/24"},
		{"two addresses glued", trunc, "1.2.3.4:5.6.7.8", "[IP]"},
		{"leading dot ipv4", trunc, "ip:203.0.113.7", "ip:203.0.113.0/24"},
		{"plain words untouched", trunc, "a.b.c.d and dead:beef", "a.b.c.d and dead:beef"},
	}
	for _, c := range cases {
		if got := maskedText(t, c.m, c.in); got != c.want {
			t.Errorf("%s: %q -> %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestMaskText_IPRuleIsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	bound := 2 * time.Second
	if raceEnabled {
		bound = 12 * time.Second
	}
	m := modeMasker(iface.IPAddressTruncated)
	inputs := map[string]string{
		"a::a::":       strings.Repeat("a::a::", (1<<20)/6),
		"five-digit":   strings.Repeat("12345:", (1<<20)/6),
		"dotted 4digs": strings.Repeat("1234.", (1<<20)/5),
		"colons":       strings.Repeat(":", 1<<20),
	}
	for name, in := range inputs {
		start := time.Now()
		got := m.safeText(in)
		if d := time.Since(start); d > bound {
			t.Errorf("%s: masking 1 MB took %v", name, d)
		}
		if name == "a::a::" && got != "[IP]" {
			t.Errorf("%s: got %.20q..., want [IP]", name, got)
		}
	}
}

func TestMaskAttr_HeaderBudgetExhaustionDoesNotPanic(t *testing.T) {
	var panics int
	SetMaskingPanicHook(func() { panics++ })
	t.Cleanup(func() { SetMaskingPanicHook(nil) })
	m := logMasker{p: policy(nil), key: testHashKey}
	// enough sibling values to spend the whole node budget before the header
	wide := make([]any, 0, maxMaskNodes)
	for i := 0; i < maxMaskNodes-2; i++ {
		wide = append(wide, 1)
	}
	wide = append(wide, http.Header{"X": {"a@b.it"}}, url.Values{"q": {"a@b.it"}})
	out, keep := m.maskAttr(slog.Any("wide", wide))
	if !keep {
		t.Fatal("dropped")
	}
	if panics != 0 {
		t.Fatalf("masking panicked %d times on budget exhaustion", panics)
	}
	if out.Value.String() != "[REDACTED]" {
		t.Errorf("got %v, want [REDACTED]", out.Value)
	}
}

type selfGroup struct{ n int }

func (g *selfGroup) LogValue() slog.Value {
	// children that are LogValuers themselves: a cycle that also branches
	return slog.GroupValue(slog.Any("a", g), slog.Any("b", g), slog.String("email", "a@b.it"))
}

func TestMaskAttr_SelfReferencingGroupTerminates(t *testing.T) {
	var panics int
	SetMaskingPanicHook(func() { panics++ })
	t.Cleanup(func() { SetMaskingPanicHook(nil) })
	m := logMasker{p: policy(nil), key: testHashKey}
	start := time.Now()
	out, keep := m.maskAttr(slog.Any("g", &selfGroup{}))
	if !keep || panics != 0 {
		t.Fatalf("keep=%v panics=%d", keep, panics)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("self-referencing group took %v", d)
	}
	if out.Value.Kind() != slog.KindGroup && out.Value.String() != "[REDACTED]" {
		t.Errorf("unexpected value %v", out.Value)
	}
}

func TestMaskAttr_GroupDepthCapRedacts(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	// a linear chain of 40 nested groups: capped at the depth limit
	a := slog.String("leaf", "x")
	for i := 0; i < 40; i++ {
		a = slog.Group("g", a)
	}
	out, _ := m.maskAttr(a)
	cur := out
	for i := 0; i < 50; i++ {
		if cur.Value.Kind() != slog.KindGroup {
			if cur.Value.String() != "[REDACTED]" {
				t.Fatalf("depth cap value = %v, want [REDACTED]", cur.Value)
			}
			return
		}
		cur = cur.Value.Group()[0]
	}
	t.Fatal("group nesting was not capped")
}
