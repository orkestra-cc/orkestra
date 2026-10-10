package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/core/llm/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/internal/testkit"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
)

type harness struct {
	admin *AdminHandler
	self  *SelfHandler
	ctx   context.Context
	logs  *bytes.Buffer
}

// newHarness wires the real CatalogService and Gateway over in-memory repos.
// The acting user u-admin is an org owner of the internal tenant t1 and, like
// u1/u2, a member of it. keyHex "" builds a vault with no secret key.
func newHarness(t *testing.T, keyHex string) *harness {
	t.Helper()
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	vault, err := services.NewVault(keyHex, logger)
	if err != nil {
		t.Fatal(err)
	}
	creds := &fakeCreds{rows: map[string]models.Credential{}}
	mods := &fakeModels{rows: map[string]models.Model{}}
	grants := &fakeGrants{rows: map[string]map[string][]string{}}
	dir := &fakeDir{members: map[string][]string{"t1": {"u-admin", "u1", "u2"}}}
	cfg := func() services.CatalogConfig { return services.CatalogConfig{AllowHosted: true} }
	catalog := services.NewCatalogService(creds, mods, grants, vault, dir, cfg, logger)
	gw := services.NewGateway(services.NewAccessResolver(mods, grants), catalog, providers.NewRegistry(), vault, logger)
	id := testkit.NewIdentity("u-admin", "admin@example.test", "administrator").WithTenant("t1", []string{"org_owner"}, true)
	ctx := ctxauth.WithTenantKind(id.ContextFor(context.Background(), "t1"), "internal")
	return &harness{admin: NewAdminHandler(catalog, logger), self: NewSelfHandler(gw, logger), ctx: ctx, logs: logs}
}

var testKeyHex = strings.Repeat("ab", 32)

func codeOf(t *testing.T, err error) (int, string, string) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("not an errcode.Error: %v", err)
	}
	return e.Status, e.Code, e.Detail
}

func (h *harness) credential(t *testing.T, name string) *LLMCredentialResponse {
	t.Helper()
	resp, err := h.admin.CreateCredential(h.ctx, &LLMCredentialCreateRequest{Body: models.LLMCredentialCreateBody{
		Name: name, Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef",
	}})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func modelBody(name, credUUID string) models.LLMModelBody {
	return models.LLMModelBody{
		Name: name, Provider: models.ProviderOpenAI, ModelID: "gpt-5.6-terra",
		Capabilities:  models.LLMModelCapabilities{Chat: true},
		CredentialRef: models.LLMCredentialRef{Kind: models.CredentialKindOrg, CredentialUUID: credUUID},
		Purposes:      []models.LLMModelPurpose{{Purpose: "default", Priority: 1}},
		Access:        models.AccessGranted,
	}
}

func (h *harness) model(t *testing.T, name, credUUID string) *LLMModelResponse {
	t.Helper()
	resp, err := h.admin.CreateModel(h.ctx, &LLMModelCreateRequest{Body: modelBody(name, credUUID)})
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestAdmin_CredentialNeverEchoesSecret(t *testing.T) {
	h := newHarness(t, testKeyHex)
	resp := h.credential(t, "OpenAI")
	if !resp.Body.HasSecret || resp.Body.SecretLast4 != "cdef" {
		t.Fatalf("view = %+v", resp.Body)
	}
	list, err := h.admin.ListCredentials(h.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []any{resp.Body, list.Body} {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("sk-live")) || bytes.Contains(raw, []byte("ciphertext")) || bytes.Contains(raw, []byte("tenantId")) {
			t.Fatalf("response leaks the secret envelope or tenant: %s", raw)
		}
	}
	dup, err := h.admin.CreateCredential(h.ctx, &LLMCredentialCreateRequest{Body: models.LLMCredentialCreateBody{
		Name: "OpenAI", Provider: models.ProviderOpenAI, Secret: "sk-live-xyzxyz",
	}})
	if dup != nil || err == nil {
		t.Fatal("duplicate must fail")
	}
	if status, code, _ := codeOf(t, err); status != http.StatusConflict || code != errcode.LLMNameInUse {
		t.Fatalf("dup = %d %s", status, code)
	}
}

func TestAdmin_CredentialLifecycle(t *testing.T) {
	h := newHarness(t, testKeyHex)
	c := h.credential(t, "OpenAI")
	got, err := h.admin.GetCredential(h.ctx, &LLMCredentialPath{UUID: c.Body.UUID})
	if err != nil || got.Body.Name != "OpenAI" {
		t.Fatalf("get = %+v, %v", got, err)
	}
	renamed := "Primary"
	patched, err := h.admin.PatchCredential(h.ctx, &LLMCredentialPatchRequest{UUID: c.Body.UUID, Body: models.LLMCredentialPatchBody{Name: &renamed}})
	if err != nil || patched.Body.Name != "Primary" || patched.Body.SecretLast4 != "cdef" {
		t.Fatalf("patch = %+v, %v", patched, err)
	}
	rotated, err := h.admin.RotateCredential(h.ctx, &LLMCredentialRotateRequest{UUID: c.Body.UUID, Body: models.LLMCredentialRotateBody{Secret: "sk-live-987654"}})
	if err != nil || rotated.Body.SecretLast4 != "7654" {
		t.Fatalf("rotate = %+v, %v", rotated, err)
	}
	// An active model pins the credential.
	m := h.model(t, "Fast", c.Body.UUID)
	_, err = h.admin.DeleteCredential(h.ctx, &LLMCredentialPath{UUID: c.Body.UUID})
	if status, code, _ := codeOf(t, err); status != http.StatusConflict || code != errcode.LLMCredentialInUse {
		t.Fatalf("delete pinned = %d %s", status, code)
	}
	if _, err := h.admin.DeleteModel(h.ctx, &LLMModelPath{UUID: m.Body.UUID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.admin.DeleteCredential(h.ctx, &LLMCredentialPath{UUID: c.Body.UUID}); err != nil {
		t.Fatal(err)
	}
	_, err = h.admin.GetCredential(h.ctx, &LLMCredentialPath{UUID: c.Body.UUID})
	if status, code, _ := codeOf(t, err); status != http.StatusNotFound || code != errcode.LLMNotFound {
		t.Fatalf("get deleted = %d %s", status, code)
	}
}

func TestAdmin_SecretWritesNeedAKey(t *testing.T) {
	h := newHarness(t, "")
	_, err := h.admin.CreateCredential(h.ctx, &LLMCredentialCreateRequest{Body: models.LLMCredentialCreateBody{
		Name: "OpenAI", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef",
	}})
	if status, code, _ := codeOf(t, err); status != http.StatusServiceUnavailable || code != errcode.LLMSecretKeyMissing {
		t.Fatalf("create without key = %d %s", status, code)
	}
	if strings.Contains(h.logs.String(), "sk-live") {
		t.Fatalf("the secret reached the log: %s", h.logs.String())
	}
}

func TestAdmin_ValidationDetailIsWrittenNotEchoed(t *testing.T) {
	h := newHarness(t, testKeyHex)
	_, err := h.admin.CreateCredential(h.ctx, &LLMCredentialCreateRequest{Body: models.LLMCredentialCreateBody{
		Name: "!bad", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef",
	}})
	status, code, detail := codeOf(t, err)
	if status != http.StatusUnprocessableEntity || code != errcode.LLMInvalidRequest {
		t.Fatalf("bad name = %d %s", status, code)
	}
	if detail == "" || strings.Contains(detail, "llm:") || detail == models.ErrInvalidName.Error() {
		t.Fatalf("detail %q must be a written sentence, not the error text", detail)
	}
}

func TestAdmin_PatchModelChangesOnlyProvidedFields(t *testing.T) {
	h := newHarness(t, testKeyHex)
	c := h.credential(t, "OpenAI")
	m := h.model(t, "Fast", c.Body.UUID)
	renamed := "Faster"
	got, err := h.admin.PatchModel(h.ctx, &LLMModelPatchRequest{UUID: m.Body.UUID, Body: models.LLMModelPatchBody{Name: &renamed}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Body.Name != "Faster" {
		t.Fatalf("name = %q", got.Body.Name)
	}
	if got.Body.ModelID != "gpt-5.6-terra" || got.Body.Access != models.AccessGranted || got.Body.Status != models.ModelStatusActive ||
		!got.Body.Capabilities.Chat || len(got.Body.Purposes) != 1 || got.Body.CredentialRef.CredentialUUID != c.Body.UUID {
		t.Fatalf("a partial patch changed other fields: %+v", got.Body)
	}
	// The merged model is validated as a whole: dropping every capability
	// leaves a model that can do nothing.
	none := models.LLMModelCapabilities{}
	_, err = h.admin.PatchModel(h.ctx, &LLMModelPatchRequest{UUID: m.Body.UUID, Body: models.LLMModelPatchBody{Capabilities: &none}})
	if status, code, _ := codeOf(t, err); status != http.StatusUnprocessableEntity || code != errcode.LLMInvalidRequest {
		t.Fatalf("invalid merged model = %d %s", status, code)
	}
	after, err := h.admin.GetModel(h.ctx, &LLMModelPath{UUID: m.Body.UUID})
	if err != nil || !after.Body.Capabilities.Chat {
		t.Fatalf("a rejected patch was persisted: %+v, %v", after, err)
	}
}

func TestAdmin_GrantNonMemberIs422(t *testing.T) {
	h := newHarness(t, testKeyHex)
	c := h.credential(t, "OpenAI")
	m := h.model(t, "Fast", c.Body.UUID)
	_, err := h.admin.PutGrants(h.ctx, &LLMGrantsPutRequest{UUID: m.Body.UUID, Body: models.LLMGrantsPutBody{UserUUIDs: []string{"u1", "ghost"}}})
	status, code, detail := codeOf(t, err)
	if status != http.StatusUnprocessableEntity || code != errcode.LLMGrantNotMember {
		t.Fatalf("grant = %d %s", status, code)
	}
	if strings.Contains(detail, "ghost") {
		t.Fatalf("detail echoes the rejected id: %q", detail)
	}
	// One non-member rejects the whole list: u1 was not granted either.
	got, err := h.admin.GetModel(h.ctx, &LLMModelPath{UUID: m.Body.UUID})
	if err != nil || len(got.Body.Grants) != 0 {
		t.Fatalf("grants after a rejected put = %+v, %v", got, err)
	}
}

func TestSelf_MyModelsHonoursGrants(t *testing.T) {
	h := newHarness(t, testKeyHex)
	c := h.credential(t, "OpenAI")
	m := h.model(t, "Fast", c.Body.UUID)
	mine, err := h.self.MyModels(h.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine.Body.Items) != 0 {
		t.Fatalf("managing a model must not grant its use, got %+v", mine.Body.Items)
	}
	put, err := h.admin.PutGrants(h.ctx, &LLMGrantsPutRequest{UUID: m.Body.UUID, Body: models.LLMGrantsPutBody{UserUUIDs: []string{"u-admin"}}})
	if err != nil || len(put.Body.Items) != 1 || put.Body.Items[0].UserUUID != "u-admin" {
		t.Fatalf("put grants = %+v, %v", put, err)
	}
	mine, err = h.self.MyModels(h.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine.Body.Items) != 1 || mine.Body.Items[0].UUID != m.Body.UUID || mine.Body.Items[0].CredentialKind != models.CredentialKindOrg {
		t.Fatalf("after grant = %+v", mine.Body.Items)
	}
	// Revoking every grant removes it again.
	if _, err := h.admin.PutGrants(h.ctx, &LLMGrantsPutRequest{UUID: m.Body.UUID, Body: models.LLMGrantsPutBody{UserUUIDs: []string{}}}); err != nil {
		t.Fatal(err)
	}
	mine, _ = h.self.MyModels(h.ctx, nil)
	if len(mine.Body.Items) != 0 {
		t.Fatalf("after revoke = %+v", mine.Body.Items)
	}
}

func TestMapError(t *testing.T) {
	wrapInvalid := func(e error) error { return fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, e) }
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid name", wrapInvalid(models.ErrInvalidName), 422, errcode.LLMInvalidRequest},
		{"invalid request without detail", iface.ErrLLMInvalidRequest, 422, errcode.LLMInvalidRequest},
		{"not found", services.ErrNotFound, 404, errcode.LLMNotFound},
		{"name in use", services.ErrNameInUse, 409, errcode.LLMNameInUse},
		{"credential in use", services.ErrCredentialInUse, 409, errcode.LLMCredentialInUse},
		{"secret key missing", services.ErrSecretKeyMissing, 503, errcode.LLMSecretKeyMissing},
		{"endpoint", services.ErrEndpointNotAllowed, 422, errcode.LLMEndpointNotAllowed},
		{"hosted", services.ErrHostedDisabled, 422, errcode.LLMHostedDisabled},
		{"mock", services.ErrMockNotAllowed, 422, errcode.LLMMockNotAllowed},
		{"grant non member", fmt.Errorf("%w: u9", services.ErrGrantNotMember), 422, errcode.LLMGrantNotMember},
		{"not configured", iface.ErrLLMNotConfigured, 503, errcode.LLMNotConfigured},
		{"no eligible model", iface.ErrLLMNoEligibleModel, 403, errcode.LLMNoEligibleModel},
		{"access denied", iface.ErrLLMModelAccessDenied, 403, errcode.LLMModelAccessDenied},
		{"capability", iface.ErrLLMCapabilityMismatch, 422, errcode.LLMCapabilityMismatch},
		{"provider", fmt.Errorf("%w: routing not wired", iface.ErrLLMProviderUnavailable), 503, errcode.LLMProviderUnavailable},
		{"tenant scope missing", tenantrepo.ErrTenantScopeMissing, 500, ""},
		{"tenant kind mismatch", fmt.Errorf("%w: want internal, got \"external\"", tenantrepo.ErrTenantKindMismatch), 403, ""},
		{"unknown", errors.New("mongo: connection reset by peer 10.0.0.7"), 500, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code, detail := codeOf(t, MapError(tc.err))
			if status != tc.status || code != tc.code {
				t.Fatalf("= %d %q, want %d %q", status, code, tc.status, tc.code)
			}
			if detail == "" || detail == tc.err.Error() || strings.Contains(detail, "llm:") || strings.Contains(detail, "10.0.0.7") || strings.Contains(detail, "u9") {
				t.Fatalf("detail %q is empty or echoes the error text", detail)
			}
		})
	}
	var e *errcode.Error
	if !errors.As(MapError(iface.ErrLLMNotConfigured), &e) || !e.ExpectedUnavailable() {
		t.Fatal("not_configured must be an expected 503 (FeatureNotConfigured)")
	}
}

func TestMapError_EveryModelValidationErrorHasItsOwnSentence(t *testing.T) {
	generic := MapError(iface.ErrLLMInvalidRequest).(*errcode.Error).Detail
	seen := map[string]error{}
	sentinels := []error{
		models.ErrInvalidName, models.ErrInvalidProvider, models.ErrBaseURLFixed, models.ErrBaseURLRequired,
		models.ErrBaseURLNotAllowed, models.ErrSecretRequired, models.ErrInvalidModelID, models.ErrInvalidCredentialRef,
		models.ErrUserAccountProvider, models.ErrUserAccountCapabilities, models.ErrUserAccountDefaults,
		models.ErrInvalidPurposes, models.ErrInvalidAccess, models.ErrInvalidCapabilities, models.ErrInvalidEffort,
		models.ErrInvalidReserve, services.ErrInvalidStatus, services.ErrProviderMismatch,
	}
	for _, sentinel := range sentinels {
		detail := MapError(fmt.Errorf("%w: %w", iface.ErrLLMInvalidRequest, sentinel)).(*errcode.Error).Detail
		if detail == generic {
			t.Errorf("%v falls back to the generic detail", sentinel)
		}
		if prev, dup := seen[detail]; dup {
			t.Errorf("%v and %v share the detail %q", prev, sentinel, detail)
		}
		seen[detail] = sentinel
	}
}

// Under TENANT_KIND_ENFORCEMENT=warn an external tenant reaches the
// handlers: it is refused with 403 at WARN and nothing is written.
func TestAdmin_ExternalTenantIs403AndWritesNothing(t *testing.T) {
	h := newHarness(t, testKeyHex)
	id := testkit.NewIdentity("u-ext", "ext@example.test", "guest").WithTenant("ext-1", []string{"org_owner"}, true)
	ext := ctxauth.WithTenantKind(id.ContextFor(context.Background(), "ext-1"), "external")
	_, err := h.admin.CreateCredential(ext, &LLMCredentialCreateRequest{Body: models.LLMCredentialCreateBody{
		Name: "OpenAI", Provider: models.ProviderOpenAI, Secret: "sk-live-abcdef",
	}})
	if status, code, _ := codeOf(t, err); status != http.StatusForbidden || code != "" {
		t.Fatalf("external create = %d %q", status, code)
	}
	if _, err := h.self.MyModels(ext, nil); err == nil {
		t.Fatal("an external tenant must not list models")
	} else if status, _, _ := codeOf(t, err); status != http.StatusForbidden {
		t.Fatalf("external self list = %d", status)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "level=WARN") || strings.Contains(logs, "level=ERROR") {
		t.Fatalf("a tier mismatch must log at WARN, never ERROR: %s", logs)
	}
	if list, err := h.admin.ListCredentials(h.ctx, nil); err != nil || len(list.Body.Items) != 0 {
		t.Fatalf("an external-tenant create wrote a row: %+v, %v", list, err)
	}
}

// A request with no tenant kind is a wiring fault: 500, logged at ERROR.
func TestAdmin_MissingTenantScopeIs500(t *testing.T) {
	t.Setenv("ENV", "production") // dev panics on an unset tenant kind
	h := newHarness(t, testKeyHex)
	_, err := h.admin.ListCredentials(context.Background(), nil)
	if status, code, _ := codeOf(t, err); status != http.StatusInternalServerError || code != "" {
		t.Fatalf("no tenant = %d %q", status, code)
	}
	if !strings.Contains(h.logs.String(), "level=ERROR") {
		t.Fatalf("a missing scope must log at ERROR: %s", h.logs.String())
	}
}

func TestFailure_LogsTheCauseNotTheClient(t *testing.T) {
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	cause := errors.New("mongo: socket closed")
	_, _, detail := codeOf(t, failure(context.Background(), logger, "list credentials", cause))
	if strings.Contains(detail, "socket") {
		t.Fatalf("detail leaks the cause: %q", detail)
	}
	if !strings.Contains(logs.String(), "socket closed") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("cause not logged at ERROR: %s", logs.String())
	}
	logs.Reset()
	_ = failure(context.Background(), logger, "get credential", services.ErrNotFound)
	_ = failure(context.Background(), logger, "list my models", iface.ErrLLMNotConfigured)
	if logs.Len() != 0 {
		t.Fatalf("expected outcomes must not be logged: %s", logs.String())
	}
}
