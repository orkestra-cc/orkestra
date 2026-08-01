package middleware

// Device identity must not be derivable from request headers.
//
// The device id used to be MD5(User-Agent | IP | Accept-Language |
// Accept-Encoding | Accept) whenever no X-Device-ID header was present —
// i.e. for every browser. Every one of those inputs is chosen by the
// caller, so "which device is this" was a value an attacker could
// reproduce at will. That id is what the session documents, the refresh
// rows, the risk scorer's new-device detection, and the device-trust
// grants are all keyed on.
//
// It is now a server-minted random value carried in an HttpOnly cookie:
// unguessable, not reproducible from headers, and not settable by the
// caller. The header-derived value survives only as a *fingerprint* —
// a risk signal, never an identity.

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/orkestra/backend/internal/shared/errors"
	"github.com/orkestra/backend/internal/shared/types"
)

func deviceMW() *DeviceMiddleware {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewDeviceMiddleware(errors.NewManager(logger, true))
}

// runDevice drives the middleware and returns the extracted info plus the
// response recorder so cookie emission can be asserted.
func runDevice(r *http.Request) (*types.DeviceInfo, *httptest.ResponseRecorder) {
	var captured *types.DeviceInfo
	rec := httptest.NewRecorder()
	deviceMW().ExtractDeviceInfo(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		captured = types.GetDeviceInfoFromContext(req.Context())
	})).ServeHTTP(rec, r)
	return captured, rec
}

func browserRequest() *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0.0.0")
	r.Header.Set("Accept-Language", "en-GB,en;q=0.9")
	r.Header.Set("Accept-Encoding", "gzip, deflate, br")
	r.Header.Set("Accept", "text/html")
	return r
}

func cookieValue(rec *httptest.ResponseRecorder, name string) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestDeviceID_NotDerivableFromHeaders(t *testing.T) {
	// Two requests with byte-identical headers and the same source IP —
	// an attacker replaying a victim's exact browser signature — must
	// still be told apart.
	first, _ := runDevice(browserRequest())
	second, _ := runDevice(browserRequest())

	if first.DeviceID == "" || second.DeviceID == "" {
		t.Fatal("a device id must always be assigned")
	}
	if first.DeviceID == second.DeviceID {
		t.Errorf("identical headers produced the same device id %q — the id is still header-derived", first.DeviceID)
	}
}

func TestDeviceID_MintedIntoHttpOnlyCookie(t *testing.T) {
	info, rec := runDevice(browserRequest())

	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == DeviceIDCookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatalf("middleware must mint a %s cookie so the device is recognised next time", DeviceIDCookieName)
	}
	if found.Value != info.DeviceID {
		t.Errorf("cookie value %q != device id %q", found.Value, info.DeviceID)
	}
	if !found.HttpOnly {
		t.Error("the device id cookie must be HttpOnly — script access turns it back into a stealable identifier")
	}
}

func TestDeviceID_ReusesCookieOnReturn(t *testing.T) {
	_, rec := runDevice(browserRequest())
	minted := cookieValue(rec, DeviceIDCookieName)

	back := browserRequest()
	back.AddCookie(&http.Cookie{Name: DeviceIDCookieName, Value: minted})
	info, rec2 := runDevice(back)

	if info.DeviceID != minted {
		t.Errorf("a returning device must keep its id: got %q want %q", info.DeviceID, minted)
	}
	if cookieValue(rec2, DeviceIDCookieName) != "" {
		t.Error("no need to re-mint the cookie when the browser already presented one")
	}
}

func TestDeviceID_NativeAppHeaderWins(t *testing.T) {
	r := browserRequest()
	r.Header.Set("X-Device-ID", "flutter-installation-id")

	info, rec := runDevice(r)

	if info.DeviceID != "flutter-installation-id" {
		t.Errorf("X-Device-ID must be honoured for native apps, got %q", info.DeviceID)
	}
	if cookieValue(rec, DeviceIDCookieName) != "" {
		t.Error("a native app supplying its own id needs no cookie")
	}
}

func TestDeviceID_QueryParameterIsIgnored(t *testing.T) {
	// device_id used to be readable from the query string "for OAuth
	// flows", which let anyone hand the server a device identity via a
	// link. The cookie survives the OAuth redirect on its own.
	r := httptest.NewRequest(http.MethodGet, "/?device_id=attacker-chosen", nil)
	r.RemoteAddr = "203.0.113.9:1234"

	info, _ := runDevice(r)

	if info.DeviceID == "attacker-chosen" {
		t.Error("device_id must not be readable from the query string")
	}
}

func TestFingerprint_StaysAStableHeaderSignal(t *testing.T) {
	// The fingerprint is a risk signal, not an identity: it SHOULD be
	// reproducible from headers, which is exactly why it must never be
	// the thing a trust decision is keyed on.
	first, _ := runDevice(browserRequest())
	second, _ := runDevice(browserRequest())

	if first.Fingerprint == "" {
		t.Fatal("fingerprint must still be computed")
	}
	if first.Fingerprint != second.Fingerprint {
		t.Error("the fingerprint should be stable for the same browser signature")
	}
	if len(first.Fingerprint) != 64 {
		t.Errorf("expected a SHA-256 hex digest (64 chars), got %d chars — MD5 is not acceptable for a security-adjacent identifier", len(first.Fingerprint))
	}
	if first.Fingerprint == first.DeviceID {
		t.Error("the device id must not be the header fingerprint")
	}
}
