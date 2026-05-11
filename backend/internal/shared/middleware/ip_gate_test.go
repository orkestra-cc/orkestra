package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newRequest(remote, xff, xri string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	if xri != "" {
		r.Header.Set("X-Real-IP", xri)
	}
	return r
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func runGate(t *testing.T, source IPNetSource, r *http.Request) (status int, body string) {
	t.Helper()
	gate := NewIPGate(source)
	rec := httptest.NewRecorder()
	gate.Middleware(okHandler()).ServeHTTP(rec, r)
	return rec.Code, rec.Body.String()
}

func TestIPGate_NoSource_FailsOpen(t *testing.T) {
	r := newRequest("198.51.100.5:1234", "", "")
	gate := NewIPGate(nil)
	rec := httptest.NewRecorder()
	gate.Middleware(okHandler()).ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("nil source should fail open, got %d", rec.Code)
	}
}

func TestIPGate_EmptyAllowlist_PermitsAny(t *testing.T) {
	src := func() ([]string, []string) { return nil, nil }
	status, _ := runGate(t, src, newRequest("198.51.100.5:1234", "", ""))
	if status != http.StatusOK {
		t.Fatalf("empty lists must permit, got %d", status)
	}
}

func TestIPGate_AllowlistMatch(t *testing.T) {
	src := func() ([]string, []string) { return []string{"10.0.0.0/8"}, nil }
	status, _ := runGate(t, src, newRequest("10.5.6.7:5555", "", ""))
	if status != http.StatusOK {
		t.Fatalf("matching allowlist must pass, got %d", status)
	}
}

func TestIPGate_AllowlistMiss_Denies(t *testing.T) {
	src := func() ([]string, []string) { return []string{"10.0.0.0/8"}, nil }
	status, body := runGate(t, src, newRequest("198.51.100.5:1234", "", ""))
	if status != http.StatusForbidden {
		t.Fatalf("non-matching allowlist must deny, got %d", status)
	}
	if got := body; got == "" || !contains(got, "ip_not_allowed") {
		t.Errorf("body must carry ip_not_allowed code, got %q", got)
	}
}

func TestIPGate_BlocklistWinsOverAllowlist(t *testing.T) {
	src := func() ([]string, []string) {
		return []string{"10.0.0.0/8"}, []string{"10.5.0.0/16"}
	}
	status, body := runGate(t, src, newRequest("10.5.6.7:5555", "", ""))
	if status != http.StatusForbidden {
		t.Fatalf("blocklist must win, got %d", status)
	}
	if !contains(body, "ip_blocked") {
		t.Errorf("body must carry ip_blocked code, got %q", body)
	}
}

func TestIPGate_BareIPNormalisedToHost(t *testing.T) {
	// A bare IP entry without /N should be treated as a /32 (or /128
	// for v6) so operators can drop a single host into the list.
	src := func() ([]string, []string) { return []string{"10.5.6.7"}, nil }
	status, _ := runGate(t, src, newRequest("10.5.6.7:5555", "", ""))
	if status != http.StatusOK {
		t.Fatalf("bare-IP allowlist match must pass, got %d", status)
	}
	status, _ = runGate(t, src, newRequest("10.5.6.8:5555", "", ""))
	if status != http.StatusForbidden {
		t.Fatalf("bare-IP allowlist must NOT match a different IP, got %d", status)
	}
}

func TestIPGate_PrefersXForwardedFor(t *testing.T) {
	src := func() ([]string, []string) { return []string{"10.0.0.0/8"}, nil }
	// RemoteAddr is outside, X-Forwarded-For first hop is inside.
	status, _ := runGate(t, src, newRequest("198.51.100.5:1234", "10.5.6.7, 198.51.100.5", ""))
	if status != http.StatusOK {
		t.Fatalf("XFF first hop must drive the gate, got %d", status)
	}
}

func TestIPGate_MalformedCIDRsIgnored(t *testing.T) {
	// Garbage entries shouldn't 500 the gate. They're dropped and the
	// remaining entries continue to gate normally.
	src := func() ([]string, []string) {
		return []string{"definitely-not-a-cidr", "10.0.0.0/8"}, []string{"???"}
	}
	status, _ := runGate(t, src, newRequest("10.5.6.7:5555", "", ""))
	if status != http.StatusOK {
		t.Fatalf("valid CIDR after malformed entry must still gate, got %d", status)
	}
}

func TestIPGate_CacheReusesParse(t *testing.T) {
	// The compile cache short-circuits when inputs haven't changed.
	// Verify by counting source calls vs underlying parses — proxy: a
	// repeated call returns the same slice header.
	src := func() ([]string, []string) { return []string{"10.0.0.0/8"}, []string{"203.0.113.0/24"} }
	gate := NewIPGate(src)
	allow1, block1 := gate.compile([]string{"10.0.0.0/8"}, []string{"203.0.113.0/24"})
	allow2, block2 := gate.compile([]string{"10.0.0.0/8"}, []string{"203.0.113.0/24"})
	if &allow1[0] != &allow2[0] {
		t.Errorf("expected cached allow slice reuse")
	}
	if &block1[0] != &block2[0] {
		t.Errorf("expected cached block slice reuse")
	}
	// Changing inputs must trigger a re-parse.
	allow3, _ := gate.compile([]string{"172.16.0.0/12"}, nil)
	if len(allow3) == 0 || allow3[0].String() == allow1[0].String() {
		t.Errorf("input change should re-parse to a different net")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	n := len(needle)
	for i := 0; i+n <= len(haystack); i++ {
		if haystack[i:i+n] == needle {
			return i
		}
	}
	return -1
}
