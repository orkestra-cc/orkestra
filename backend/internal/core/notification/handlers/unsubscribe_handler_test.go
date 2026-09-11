package handlers

// RFC 8058 one-click unsubscribe: the POST endpoint mail providers call,
// and the pre-existing GET brought onto the same crash-safe sequence
// (services.UnsubscribeService.Consume). These tests drive the real HTTP
// layer — a chi.Mux carrying a Huma API built the same way module.go's
// RegisterRoutes does — because the property under test (identical bytes
// for every token state) is a property of the wire response, not of any
// one Go value inside the handler.
//
// The token store and opt-out store are small in-memory fakes; Consume's
// own state machine (claim ordering, pending flags, fail-closed on an
// unwired seam, …) is exhaustively covered in the services package and is
// not re-tested here.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/internal/core/notification/services"
)

// ---------------------------------------------------------------------------
// In-memory token store and opt-out store
// ---------------------------------------------------------------------------

// testHashToken mirrors the services package's unexported hashToken exactly
// (sha256, hex-encoded) so fixtures seeded here are found by the real
// UnsubscribeService the handler runs against.
func testHashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// unsubTestRepo is a minimal repository.UnsubscribeRepository: enough to
// exercise Consume's GetByHash + ClaimToken sequence for a handful of
// pre-seeded token states, keyed by hash like the real repository.
type unsubTestRepo struct {
	docs map[string]*models.UnsubscribeTokenDoc
}

func (r *unsubTestRepo) Create(context.Context, *models.UnsubscribeTokenDoc) error { return nil }

func (r *unsubTestRepo) GetByHash(_ context.Context, hash string) (*models.UnsubscribeTokenDoc, error) {
	doc, ok := r.docs[hash]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *doc
	return &cp, nil
}

func (r *unsubTestRepo) MarkUsed(context.Context, string) error { return nil }

// ClaimToken mirrors the real repository's filter: a doc already used or
// past its expiry does not match, and the caller gets (nil, nil) rather than
// an error.
func (r *unsubTestRepo) ClaimToken(_ context.Context, hash string, now time.Time, hasUser bool) (*models.UnsubscribeTokenDoc, error) {
	doc, ok := r.docs[hash]
	if !ok || doc.UsedAt != nil || now.After(doc.ExpiresAt) {
		return nil, nil
	}
	doc.UsedAt = &now
	doc.SinkPending = true
	if hasUser {
		doc.PrefPending = true
	}
	cp := *doc
	return &cp, nil
}

func (r *unsubTestRepo) ClearSinkPending(_ context.Context, hash string) error {
	if doc, ok := r.docs[hash]; ok {
		doc.SinkPending = false
	}
	return nil
}

func (r *unsubTestRepo) ClearPrefPending(_ context.Context, hash string) error {
	if doc, ok := r.docs[hash]; ok {
		doc.PrefPending = false
	}
	return nil
}

func (r *unsubTestRepo) ListPending(context.Context, time.Time, int) ([]models.UnsubscribeTokenDoc, error) {
	return nil, nil
}

func (r *unsubTestRepo) RecordFailedAttempt(context.Context, string, time.Time) error { return nil }

func (r *unsubTestRepo) MarkDeadLettered(context.Context, string, time.Time) error { return nil }

// unsubTestOptouts is a minimal repository.MarketingOptoutRepository that
// records what it was asked to record, so a test can prove the handler
// actually reached Consume rather than a stub that always answers 200.
type unsubTestOptouts struct {
	mu      sync.Mutex
	err     error
	written map[string]models.MarketingOptoutDoc
}

func (o *unsubTestOptouts) Upsert(_ context.Context, doc models.MarketingOptoutDoc) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return o.err
	}
	if o.written == nil {
		o.written = map[string]models.MarketingOptoutDoc{}
	}
	o.written[normalizeTestAddress(doc.Address)] = doc
	return nil
}

func (o *unsubTestOptouts) IsOptedOut(_ context.Context, address, _ string) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.written[normalizeTestAddress(address)]
	return ok, nil
}

func (o *unsubTestOptouts) has(address string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.written[normalizeTestAddress(address)]
	return ok
}

func normalizeTestAddress(a string) string { return strings.ToLower(strings.TrimSpace(a)) }

// ---------------------------------------------------------------------------
// HTTP harness
// ---------------------------------------------------------------------------

// unsubTestAPI drives the real Huma-on-chi wiring RegisterPublicRoutes
// produces, the same way module.go mounts it at boot — there is no
// humatest usage anywhere else in this repository, so this harness follows
// the pattern the package's own route-mounting tests already use
// (internal/shared/setup/routes_mount_test.go): a chi.Mux carrying a Huma
// API built with humachi.New, driven with httptest.
type unsubTestAPI struct {
	mux *chi.Mux
	api huma.API
}

func (a unsubTestAPI) Get(path string) *httptest.ResponseRecorder {
	return a.do(http.MethodGet, path, "", nil)
}

// defaultProviderBody is what a real RFC 8058 one-click POST always
// carries (§3.2). Post sends it by default so a plain api.Post(path) call
// exercises the shape an actual mail provider's request has — a POST is
// never truly bodyless in production, and Huma's own RawBody wiring
// requires a non-empty body once a raw-body field is declared, so this is
// both the realistic case and the one the framework allows.
var defaultProviderBody = []byte("List-Unsubscribe=One-Click")

func (a unsubTestAPI) Post(path string) *httptest.ResponseRecorder {
	return a.do(http.MethodPost, path, "application/x-www-form-urlencoded", defaultProviderBody)
}

func (a unsubTestAPI) PostBody(path, contentType string, body []byte) *httptest.ResponseRecorder {
	return a.do(http.MethodPost, path, contentType, body)
}

func (a unsubTestAPI) do(method, path, contentType string, body []byte) *httptest.ResponseRecorder {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	return rec
}

// newUnsubscribeTestAPI seeds a repository with one valid, unused,
// unexpired token per tokens[raw]=address entry, plus two fixed states the
// tests below exercise regardless of what tokens carries: "already-used" (an
// already-used token) and "expired" (an expired one). "unknown" is
// deliberately never seeded — an unknown token has no address to record an
// opt-out against.
func newUnsubscribeTestAPI(t *testing.T, tokens map[string]string) (unsubTestAPI, *unsubTestOptouts) {
	t.Helper()

	now := time.Now()
	docs := map[string]*models.UnsubscribeTokenDoc{}
	seed := func(raw, address string, usedAt *time.Time, expiresAt time.Time) {
		docs[testHashToken(raw)] = &models.UnsubscribeTokenDoc{
			UUID:      "uuid-" + raw,
			TokenHash: testHashToken(raw),
			Address:   address,
			Category:  "marketing",
			UsedAt:    usedAt,
			ExpiresAt: expiresAt,
		}
	}
	for raw, addr := range tokens {
		seed(raw, addr, nil, now.Add(24*time.Hour))
	}
	used := now.Add(-time.Hour)
	seed("already-used", "used@example.test", &used, now.Add(24*time.Hour))
	seed("expired", "expired@example.test", nil, now.Add(-time.Hour))

	repo := &unsubTestRepo{docs: docs}
	optouts := &unsubTestOptouts{}
	unsubSvc := services.NewUnsubscribeService(repo,
		services.WithOptouts(optouts),
		services.WithUnsubscribeLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
	)
	svc := newHandlerTestSvc(unsubSvc)
	handler := NewNotificationHandler(svc)

	mux := chi.NewRouter()
	api := humachi.New(mux, huma.DefaultConfig("unsubscribe-test", "1.0.0"))
	handler.RegisterPublicRoutes(api)

	return unsubTestAPI{mux: mux, api: api}, optouts
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestUnsubscribePost_ResponseIsIdenticalForEveryTokenState is the security
// property this whole endpoint exists to provide: a valid, an unknown, an
// already-used and an expired token must all answer with the same status
// and byte-identical body, so nobody holding a list of addresses can use
// this endpoint to learn which of them are registered here.
func TestUnsubscribePost_ResponseIsIdenticalForEveryTokenState(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})

	var bodies []string
	for _, tok := range []string{"valid", "unknown", "already-used", "expired"} {
		resp := api.Post("/v1/notifications/unsubscribe?token=" + tok)
		if resp.Code != http.StatusOK {
			t.Fatalf("token %q: expected 200, got %d", tok, resp.Code)
		}
		bodies = append(bodies, resp.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("responses must be indistinguishable:\n%q\n%q", bodies[0], bodies[i])
		}
	}
}

// TestUnsubscribeGet_ResponseIsIdenticalForEveryTokenState is the same
// property for the pre-existing GET, now that it runs the same sequence.
func TestUnsubscribeGet_ResponseIsIdenticalForEveryTokenState(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})

	var bodies []string
	for _, tok := range []string{"valid", "unknown", "already-used", "expired"} {
		resp := api.Get("/v1/notifications/unsubscribe?token=" + tok)
		if resp.Code != http.StatusOK {
			t.Fatalf("token %q: expected 200, got %d", tok, resp.Code)
		}
		bodies = append(bodies, resp.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("responses must be indistinguishable:\n%q\n%q", bodies[0], bodies[i])
		}
	}
}

// TestUnsubscribeGetAndPost_ProduceTheSameBody proves the GET the existing
// footer link points at and the new POST run the same sequence, not two
// sequences that happen to look alike.
func TestUnsubscribeGetAndPost_ProduceTheSameBody(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{
		"valid-a": "ada@example.test",
		"valid-b": "bob@example.test",
	})
	getResp := api.Get("/v1/notifications/unsubscribe?token=valid-a")
	postResp := api.Post("/v1/notifications/unsubscribe?token=valid-b")
	if getResp.Code != http.StatusOK || postResp.Code != http.StatusOK {
		t.Fatalf("GET = %d, POST = %d, want both 200", getResp.Code, postResp.Code)
	}
	if getResp.Body.String() != postResp.Body.String() {
		t.Fatalf("GET and POST must run the same sequence and answer identically:\nGET:  %q\nPOST: %q", getResp.Body.String(), postResp.Body.String())
	}
}

// TestUnsubscribePost_QueryToken_RecordsOptout is the case a browser or a
// provider following a plain link hits: the token travels in the query
// string.
func TestUnsubscribePost_QueryToken_RecordsOptout(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.Post("/v1/notifications/unsubscribe?token=valid")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("the opt-out for the token's address was not recorded")
	}
}

// TestUnsubscribePost_JSONBodyToken_RecordsOptout: no query token, so the
// handler must fall back to a JSON body carrying {"token": "..."}.
func TestUnsubscribePost_JSONBodyToken_RecordsOptout(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.PostBody("/v1/notifications/unsubscribe", "application/json", []byte(`{"token":"valid"}`))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("the opt-out for the token carried in the JSON body was not recorded")
	}
}

// TestUnsubscribePost_FormURLEncodedProviderBody_TokenFromQuery is the
// RFC 8058 §3.2 shape: the token lives in the URL the provider was handed,
// and the body just carries the fixed "List-Unsubscribe=One-Click" marker
// the endpoint must not try to parse.
func TestUnsubscribePost_FormURLEncodedProviderBody_TokenFromQuery(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.PostBody("/v1/notifications/unsubscribe?token=valid", "application/x-www-form-urlencoded", []byte("List-Unsubscribe=One-Click"))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("the opt-out was not recorded for a provider's form-urlencoded body")
	}
}

// TestUnsubscribePost_MultipartProviderBody_TokenFromQuery: some providers
// send multipart/form-data instead. Same requirement: the body is opaque,
// only the query token matters.
func TestUnsubscribePost_MultipartProviderBody_TokenFromQuery(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("List-Unsubscribe", "One-Click"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.PostBody("/v1/notifications/unsubscribe?token=valid", w.FormDataContentType(), buf.Bytes())
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("the opt-out was not recorded for a provider's multipart body")
	}
}

// TestUnsubscribeGet_RecordsOptout pins the GET's rewritten behavior: it
// must reach the same Consume sequence as POST, not a stub.
func TestUnsubscribeGet_RecordsOptout(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.Get("/v1/notifications/unsubscribe?token=valid")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("GET must record the opt-out through the same Consume sequence as POST")
	}
}

// assertNoInternalDetailLeaked fails the test if body — a response reaching
// an anonymous caller — contains the seeded token's internal UUID or the
// address it was issued for. This is what pins finding 1 of the fix-round-1
// review: a public 500 must not echo Consume's error text, which can carry
// exactly these two things (see unsubscribe_service.go's Consume doc
// comment and consumeUnsubscribe's).
func assertNoInternalDetailLeaked(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, "uuid-valid") {
		t.Fatalf("response body leaks the token's internal UUID: %q", body)
	}
	if strings.Contains(body, "ada@example.test") {
		t.Fatalf("response body leaks the recipient's address: %q", body)
	}
}

// TestUnsubscribePost_OptoutWriteFailureIsNotFlattenedToSuccess: Consume
// returns an error only when the durable opt-out could not be written. That
// is a real failure — the recipient's click did not do anything — and the
// handler must not paper over it with the generic 200 it uses for every
// token state. It also must not repeat Consume's error text: this route is
// public and unauthenticated, and that text can carry the token's internal
// UUID or the recipient's address (see unsubscribe_service.go's Consume,
// and the scrubbing it does NOT apply to the token UUID it logs).
func TestUnsubscribePost_OptoutWriteFailureIsNotFlattenedToSuccess(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	optouts.err = errors.New("boom: failed on document for ada@example.test (uuid-valid)")
	resp := api.Post("/v1/notifications/unsubscribe?token=valid")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (a database failure recording the opt-out must not be reported as a successful unsubscribe): %s", resp.Code, resp.Body.String())
	}
	assertNoInternalDetailLeaked(t, resp.Body.String())
}

// TestUnsubscribeGet_OptoutWriteFailureIsNotFlattenedToSuccess is the same
// requirement for the rewritten GET.
func TestUnsubscribeGet_OptoutWriteFailureIsNotFlattenedToSuccess(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	optouts.err = errors.New("boom: failed on document for ada@example.test (uuid-valid)")
	resp := api.Get("/v1/notifications/unsubscribe?token=valid")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (a database failure recording the opt-out must not be reported as a successful unsubscribe): %s", resp.Code, resp.Body.String())
	}
	assertNoInternalDetailLeaked(t, resp.Body.String())
}

// TestUnsubscribeRoutes_ArePublic pins both operations as unauthenticated:
// a mail provider following List-Unsubscribe carries no session and no
// bearer token.
func TestUnsubscribeRoutes_ArePublic(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, nil)
	item := api.api.OpenAPI().Paths["/v1/notifications/unsubscribe"]
	if item == nil {
		t.Fatal("route /v1/notifications/unsubscribe was not registered")
	}
	if item.Get == nil || len(item.Get.Security) != 0 {
		t.Fatalf("GET must be public (no Security), got %+v", item.Get.Security)
	}
	if item.Post == nil || len(item.Post.Security) != 0 {
		t.Fatalf("POST must be public (no Security), got %+v", item.Post.Security)
	}
	if item.Post.OperationID != "notifications-unsubscribe-post" {
		t.Fatalf("POST operation id = %q, want notifications-unsubscribe-post", item.Post.OperationID)
	}
}

// ---------------------------------------------------------------------------
// Edge cases named in the brief and in fix-round-1 review
// ---------------------------------------------------------------------------

// TestUnsubscribePost_EmptyBody_StillAccepted pins finding 2 of fix-round-1
// review: declaring a RawBody field makes Huma require a non-empty body by
// default, which would silently break a provider, proxy or link-scanner
// that sends Content-Length: 0 with a perfectly valid ?token=. Before the
// fix this returned 400 "request body is required"; RegisterPublicRoutes
// now turns RequestBody.Required back off after registering the operation.
func TestUnsubscribePost_EmptyBody_StillAccepted(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.PostBody("/v1/notifications/unsubscribe?token=valid", "", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a bodyless POST with a valid token (%s)", resp.Code, resp.Body.String())
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("a bodyless POST with a valid token must still record the opt-out")
	}
}

// TestUnsubscribePost_OversizedBody_Rejected pins finding 3: the operation
// now declares MaxBodyBytes: 4096, so a body past that limit must be
// rejected rather than buffered up to the server-wide default.
func TestUnsubscribePost_OversizedBody_Rejected(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	oversized := bytes.Repeat([]byte("x"), 5000)
	resp := api.PostBody("/v1/notifications/unsubscribe?token=valid", "application/x-www-form-urlencoded", oversized)
	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 for a body past the 4096-byte cap (%s)", resp.Code, resp.Body.String())
	}
}

// TestUnsubscribePost_NoTokenAtAll_Returns200Generic: no ?token= at all and
// no body to fall back to. Consume treats an empty token as "not an error"
// (see services.TestConsume_EmptyTokenIsNotAnError), so this must answer
// with the exact same generic body as a valid token — not a distinct
// "missing token" response, which would itself be a distinguishing signal.
func TestUnsubscribePost_NoTokenAtAll_Returns200Generic(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	want := api.Post("/v1/notifications/unsubscribe?token=valid")
	got := api.PostBody("/v1/notifications/unsubscribe", "application/x-www-form-urlencoded", defaultProviderBody)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no token at all (%s)", got.Code, got.Body.String())
	}
	if got.Body.String() != want.Body.String() {
		t.Fatalf("a request with no token must answer exactly like a valid one:\nno-token: %q\nvalid:    %q", got.Body.String(), want.Body.String())
	}
}

// TestUnsubscribeGet_NoTokenAtAll_Returns200Generic is the GET counterpart.
func TestUnsubscribeGet_NoTokenAtAll_Returns200Generic(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	want := api.Get("/v1/notifications/unsubscribe?token=valid")
	got := api.Get("/v1/notifications/unsubscribe")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no token at all (%s)", got.Code, got.Body.String())
	}
	if got.Body.String() != want.Body.String() {
		t.Fatalf("a request with no token must answer exactly like a valid one:\nno-token: %q\nvalid:    %q", got.Body.String(), want.Body.String())
	}
}

// TestUnsubscribePost_EmptyTokenValue_Returns200Generic: ?token= present
// but empty — a link-scanner or a malformed template can produce this.
// getParamValue treats an empty query value the same as absent, so this
// must land on the same generic answer too.
func TestUnsubscribePost_EmptyTokenValue_Returns200Generic(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	want := api.Post("/v1/notifications/unsubscribe?token=valid")
	got := api.Post("/v1/notifications/unsubscribe?token=")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an empty token value (%s)", got.Code, got.Body.String())
	}
	if got.Body.String() != want.Body.String() {
		t.Fatalf("an empty token value must answer exactly like a valid token:\nempty: %q\nvalid: %q", got.Body.String(), want.Body.String())
	}
}

// TestUnsubscribeGet_EmptyTokenValue_Returns200Generic is the GET
// counterpart of TestUnsubscribePost_EmptyTokenValue_Returns200Generic: both
// routes bind the token query parameter through the same tag, so an empty
// value must be swallowed the same way on either verb.
func TestUnsubscribeGet_EmptyTokenValue_Returns200Generic(t *testing.T) {
	api, _ := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	want := api.Get("/v1/notifications/unsubscribe?token=valid")
	got := api.Get("/v1/notifications/unsubscribe?token=")
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an empty token value (%s)", got.Code, got.Body.String())
	}
	if got.Body.String() != want.Body.String() {
		t.Fatalf("an empty token value must answer exactly like a valid token:\nempty: %q\nvalid: %q", got.Body.String(), want.Body.String())
	}
}

// TestUnsubscribePost_DuplicateTokenQueryParam_UsesFirstValue: Huma's query
// parser (queryparam.Get) scans left to right and returns the first match,
// so ?token=a&token=b resolves to "a" — silently, not a validation error.
// Pin that the *first* value is the one actually consumed, by seeding only
// the first as valid and checking its opt-out was recorded.
func TestUnsubscribePost_DuplicateTokenQueryParam_UsesFirstValue(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.Post("/v1/notifications/unsubscribe?token=valid&token=garbage-second-value")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("the first token in a duplicated query parameter must be the one consumed")
	}
}

// TestUnsubscribeGet_DuplicateTokenQueryParam_UsesFirstValue is the GET
// counterpart: both routes bind token the same way, so the same
// first-value-wins resolution must hold here too.
func TestUnsubscribeGet_DuplicateTokenQueryParam_UsesFirstValue(t *testing.T) {
	api, optouts := newUnsubscribeTestAPI(t, map[string]string{"valid": "ada@example.test"})
	resp := api.Get("/v1/notifications/unsubscribe?token=valid&token=garbage-second-value")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", resp.Code, resp.Body.String())
	}
	if !optouts.has("ada@example.test") {
		t.Fatal("the first token in a duplicated query parameter must be the one consumed")
	}
}
