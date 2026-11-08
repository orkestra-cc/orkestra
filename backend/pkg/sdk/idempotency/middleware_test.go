package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

type fakeStore struct {
	mu        sync.Mutex
	recs      map[string]*Record
	abandoned int
	err       error
	forced    *Record
}

func newFake() *fakeStore { return &fakeStore{recs: map[string]*Record{}} }

func (f *fakeStore) Begin(_ context.Context, scope, key, fp string, now, lu, ea time.Time) (*Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	if f.forced != nil {
		return f.forced, nil
	}
	id := scope + "/" + key
	if r, ok := f.recs[id]; ok {
		cp := *r
		return &cp, nil
	}
	f.recs[id] = &Record{Scope: scope, Key: key, Fingerprint: fp, State: StateInProgress, LockedUntil: lu, ExpiresAt: ea}
	return nil, nil
}

func (f *fakeStore) Finish(_ context.Context, scope, key string, status int, ct, loc string, body []byte, stored bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recs[scope+"/"+key]
	r.State, r.Status, r.ContentType, r.Location, r.Body, r.BodyStored = StateDone, status, ct, loc, body, stored
	return nil
}

func (f *fakeStore) Abandon(_ context.Context, scope, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abandoned++
	delete(f.recs, scope+"/"+key)
	return nil
}

const goodKey = "key-12345678"

func do(t *testing.T, h http.Handler, user, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/things?x=1", strings.NewReader(body))
	if key != "" {
		req.Header.Set(HeaderKey, key)
	}
	ctx := req.Context()
	if user != "" {
		ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, user)
		ctx = context.WithValue(ctx, ctxauth.KeyTenantID, "t1")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req.WithContext(ctx))
	return rr
}

func counting(n *int, status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*n++
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Location", "/v1/things/1")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func code(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var p problem
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatalf("bad problem body %q: %v", rr.Body.String(), err)
	}
	if rr.Header().Get("Content-Type") != "application/problem+json" || p.Status != rr.Code || p.Title != http.StatusText(rr.Code) {
		t.Fatalf("bad envelope: %v %v", rr.Header(), p)
	}
	return p.Code
}

func TestNoKeyPassesThrough(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{})(counting(&n, 201, `{}`))
	do(t, h, "u1", "", "a")
	do(t, h, "u1", "", "a")
	if n != 2 {
		t.Fatalf("handler ran %d times", n)
	}
	// GET with a key is untouched, even unauthenticated.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderKey, "x")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if n != 3 {
		t.Fatal("GET not passed through")
	}
}

func TestRequiredKeyMissing(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{Required: true})(counting(&n, 201, `{}`))
	rr := do(t, h, "u1", "", "a")
	if rr.Code != 400 || code(t, rr) != CodeKeyRequired || n != 0 {
		t.Fatalf("got %d %s n=%d", rr.Code, rr.Body, n)
	}
}

func TestInvalidKey(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{})(counting(&n, 201, `{}`))
	for _, k := range []string{"short", strings.Repeat("a", 129), "bad key!!"} {
		rr := do(t, h, "u1", k, "a")
		if rr.Code != 400 || code(t, rr) != CodeInvalidKey {
			t.Fatalf("%q: got %d %s", k, rr.Code, rr.Body)
		}
	}
	if n != 0 {
		t.Fatal("handler ran")
	}
}

func TestFirstRequestExecutesAndStores(t *testing.T) {
	n := 0
	fs := newFake()
	h := Middleware(fs, Options{Scope: "operator"})(counting(&n, 201, `{"a":1}`))
	rr := do(t, h, "u1", goodKey, "body")
	if rr.Code != 201 || n != 1 || rr.Header().Get(HeaderReplayed) != "" {
		t.Fatalf("got %d n=%d", rr.Code, n)
	}
	r := fs.recs["operator|t1|u1/"+goodKey]
	if r == nil || r.State != StateDone || r.Status != 201 || string(r.Body) != `{"a":1}` || r.Location != "/v1/things/1" || !r.BodyStored {
		t.Fatalf("bad record %+v", r)
	}
}

func TestReplaySameFingerprint(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{})(counting(&n, 201, `{"a":1}`))
	first := do(t, h, "u1", goodKey, "body")
	second := do(t, h, "u1", goodKey, "body")
	if n != 1 {
		t.Fatalf("handler ran %d times", n)
	}
	if second.Code != first.Code || second.Body.String() != first.Body.String() ||
		second.Header().Get("Location") != "/v1/things/1" || second.Header().Get("Content-Type") != "application/json" ||
		second.Header().Get(HeaderReplayed) != "true" {
		t.Fatalf("bad replay %d %v %s", second.Code, second.Header(), second.Body)
	}
}

func TestReusedKeyDifferentBody(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{})(counting(&n, 201, `{}`))
	do(t, h, "u1", goodKey, "one")
	rr := do(t, h, "u1", goodKey, "two")
	if rr.Code != 422 || code(t, rr) != CodeKeyReused || n != 1 {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
}

func TestInProgress(t *testing.T) {
	fs := newFake()
	n := 0
	h := Middleware(fs, Options{})(counting(&n, 201, `{}`))
	do(t, h, "u1", goodKey, "x")
	// Force the stored record back to in_progress with the same fingerprint.
	for _, r := range fs.recs {
		r.State = StateInProgress
	}
	rr := do(t, h, "u1", goodKey, "x")
	if rr.Code != 409 || code(t, rr) != CodeInProgress || rr.Header().Get("Retry-After") != "1" || n != 1 {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
}

func TestServerErrorAbandons(t *testing.T) {
	fs := newFake()
	n := 0
	h := Middleware(fs, Options{})(counting(&n, 500, `{}`))
	do(t, h, "u1", goodKey, "x")
	if fs.abandoned != 1 || len(fs.recs) != 0 {
		t.Fatalf("abandoned=%d recs=%d", fs.abandoned, len(fs.recs))
	}
	do(t, h, "u1", goodKey, "x")
	if n != 2 {
		t.Fatalf("retry did not execute: %d", n)
	}
}

func TestEmptyResponseAbandons(t *testing.T) {
	fs := newFake()
	h := Middleware(fs, Options{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	do(t, h, "u1", goodKey, "x")
	if fs.abandoned != 1 {
		t.Fatal("not abandoned")
	}
}

func TestPanicAbandons(t *testing.T) {
	fs := newFake()
	h := Middleware(fs, Options{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic not re-raised")
			}
		}()
		do(t, h, "u1", goodKey, "x")
	}()
	if fs.abandoned != 1 || len(fs.recs) != 0 {
		t.Fatalf("abandoned=%d", fs.abandoned)
	}
}

func TestBodyTooLarge(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{MaxBody: 10})(counting(&n, 201, `{}`))
	rr := do(t, h, "u1", goodKey, strings.Repeat("a", 11))
	if rr.Code != 413 || code(t, rr) != CodeBodyTooLarge || n != 0 {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
	if rr := do(t, h, "u1", goodKey, strings.Repeat("a", 10)); rr.Code != 201 {
		t.Fatalf("boundary got %d", rr.Code)
	}
}

func TestHandlerSeesBody(t *testing.T) {
	var got string
	h := Middleware(newFake(), Options{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 16)
		k, _ := r.Body.Read(b)
		got = string(b[:k])
		w.WriteHeader(204)
	}))
	do(t, h, "u1", goodKey, "payload")
	if got != "payload" {
		t.Fatalf("handler read %q", got)
	}
}

func TestLargeResponseNotReplayed(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{})(counting(&n, 200, strings.Repeat("z", 300<<10)))
	first := do(t, h, "u1", goodKey, "x")
	if first.Body.Len() != 300<<10 {
		t.Fatal("first response truncated")
	}
	rr := do(t, h, "u1", goodKey, "x")
	if rr.Code != 409 || code(t, rr) != CodeReplayUnavailable || n != 1 {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String()[:80])
	}
}

func TestScopeIsolation(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{})(counting(&n, 201, `{}`))
	do(t, h, "u1", goodKey, "x")
	do(t, h, "u2", goodKey, "x")
	if n != 2 {
		t.Fatalf("n=%d", n)
	}
}

func TestStoreErrorFailsClosed(t *testing.T) {
	fs := newFake()
	fs.err = errors.New("mongo down")
	n := 0
	h := Middleware(fs, Options{})(counting(&n, 201, `{}`))
	rr := do(t, h, "u1", goodKey, "x")
	if rr.Code != 503 || code(t, rr) != CodeUnavailable || n != 0 {
		t.Fatalf("got %d %s n=%d", rr.Code, rr.Body, n)
	}
}

func TestUnauthenticatedWithKey(t *testing.T) {
	n := 0
	h := Middleware(newFake(), Options{})(counting(&n, 201, `{}`))
	rr := do(t, h, "", goodKey, "x")
	if rr.Code != 400 || code(t, rr) != CodeUnauthenticated || n != 0 {
		t.Fatalf("got %d %s", rr.Code, rr.Body)
	}
}
