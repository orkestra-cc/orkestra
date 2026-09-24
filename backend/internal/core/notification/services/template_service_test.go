package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakeTemplateRepo struct {
	docs      map[string]*models.TemplateDoc // by templateId|locale
	exists    map[string]bool                // by templateId|locale
	existsErr error
	upsertErr error
	deleteErr error
	getErr    error
	listErr   error
	// getOwnedErr, when non-nil, fails GetOwned — the transient-store branch
	// Resolve must NOT swallow by falling through to the system template.
	getOwnedErr error
	upserts     []*models.TemplateDoc
	deletes     []string
	owned       map[string]*models.TemplateDoc // by owner/templateId/locale
}

func newFakeTemplateRepo() *fakeTemplateRepo {
	return &fakeTemplateRepo{
		docs:   map[string]*models.TemplateDoc{},
		exists: map[string]bool{},
	}
}

// newTestTemplateService builds a templateService wired to a fresh
// fakeTemplateRepo, for tests that need to assert on the repo's captured
// state (upserts, docs) after exercising the service.
func newTestTemplateService(t *testing.T) (*templateService, *fakeTemplateRepo) {
	t.Helper()
	repo := newFakeTemplateRepo()
	svc, ok := NewTemplateService(repo, discardLogger()).(*templateService)
	if !ok {
		t.Fatalf("NewTemplateService did not return *templateService")
	}
	return svc, repo
}

func tplKey(id, locale string) string { return id + "|" + locale }

func (f *fakeTemplateRepo) GetByID(_ context.Context, id, locale string) (*models.TemplateDoc, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if d, ok := f.docs[tplKey(id, locale)]; ok {
		return d, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeTemplateRepo) List(_ context.Context) ([]*models.TemplateDoc, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]*models.TemplateDoc, 0, len(f.docs))
	for _, d := range f.docs {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeTemplateRepo) Upsert(_ context.Context, doc *models.TemplateDoc) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	cp := *doc
	f.upserts = append(f.upserts, &cp)
	key := tplKey(doc.TemplateID, doc.Locale)
	f.docs[key] = &cp
	// Mirrors the real Mongo-backed repo: ExistsSystemTemplate counts
	// documents by templateId+locale, so a successful Upsert must be
	// reflected there too — otherwise a fake-only reseed would never see
	// its own prior insert and would overwrite operator edits.
	f.exists[key] = true
	return nil
}

func (f *fakeTemplateRepo) DeleteByID(_ context.Context, id, locale string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletes = append(f.deletes, tplKey(id, locale))
	delete(f.docs, tplKey(id, locale))
	return nil
}

func (f *fakeTemplateRepo) ExistsSystemTemplate(_ context.Context, id, locale string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.exists[tplKey(id, locale)], nil
}

// --- tenant-owned surface ---------------------------------------------------
//
// The fake's `docs` map is the SYSTEM store (ownerTenantId ""); owned
// documents live in `owned`, keyed owner/templateId/locale, so the system
// methods above can never see them — the isolation the Mongo-backed repo gets
// from its filter. Only the owner guards are reproduced here; the prefix
// grammar of DeleteOwnedByPrefix is the real repository's own invariant and is
// covered by its Mongo integration test.

func ownedKey(owner, id, locale string) string { return owner + "/" + id + "/" + locale }

func (f *fakeTemplateRepo) ownedStore() map[string]*models.TemplateDoc {
	if f.owned == nil {
		f.owned = map[string]*models.TemplateDoc{}
	}
	return f.owned
}

func (f *fakeTemplateRepo) GetOwned(_ context.Context, owner, id, locale string) (*models.TemplateDoc, error) {
	if f.getOwnedErr != nil {
		return nil, f.getOwnedErr
	}
	if owner == "" {
		return nil, repository.ErrNotFound
	}
	if d, ok := f.ownedStore()[ownedKey(owner, id, locale)]; ok {
		return d, nil
	}
	return nil, repository.ErrNotFound
}

func (f *fakeTemplateRepo) Create(_ context.Context, doc *models.TemplateDoc) error {
	if doc.OwnerTenantID == "" {
		return errors.New("notification: Create requires an owner")
	}
	key := ownedKey(doc.OwnerTenantID, doc.TemplateID, doc.Locale)
	if _, ok := f.ownedStore()[key]; ok {
		return repository.ErrExists
	}
	cp := *doc
	f.ownedStore()[key] = &cp
	return nil
}

func (f *fakeTemplateRepo) UpsertOwned(_ context.Context, doc *models.TemplateDoc) error {
	if doc.OwnerTenantID == "" {
		return errors.New("notification: UpsertOwned requires an owner")
	}
	cp := *doc
	f.ownedStore()[ownedKey(doc.OwnerTenantID, doc.TemplateID, doc.Locale)] = &cp
	return nil
}

func (f *fakeTemplateRepo) DeleteOwnedByPrefix(_ context.Context, owner, prefix string) (int64, error) {
	if owner == "" {
		return 0, errors.New("notification: DeleteOwnedByPrefix requires an owner")
	}
	var n int64
	for key, d := range f.ownedStore() {
		if d.OwnerTenantID != owner {
			continue
		}
		if d.TemplateID == prefix || strings.HasPrefix(d.TemplateID, prefix+":") {
			delete(f.ownedStore(), key)
			n++
		}
	}
	return n, nil
}

func TestTemplateService_Render_RendersAllThreeBodies(t *testing.T) {
	svc, _ := newTestTemplateService(t)
	doc := &models.TemplateDoc{
		Subject:  "Welcome {{.Name}}",
		BodyText: "Hi {{.Name}}, your code is {{.Code}}",
		BodyHTML: "<p>Hi {{.Name}}, code <code>{{.Code}}</code></p>",
	}
	out, err := svc.Render(doc, map[string]any{"Name": "Alice", "Code": "ABC"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out.Subject != "Welcome Alice" {
		t.Fatalf("subject = %q", out.Subject)
	}
	if out.BodyText != "Hi Alice, your code is ABC" {
		t.Fatalf("bodyText = %q", out.BodyText)
	}
	if !strings.Contains(out.BodyHTML, "<p>Hi Alice, code <code>ABC</code></p>") {
		t.Fatalf("bodyHTML = %q", out.BodyHTML)
	}
}

func TestTemplateService_Render_HTMLContextualEscaping(t *testing.T) {
	svc, _ := newTestTemplateService(t)
	doc := &models.TemplateDoc{
		Subject:  "S",
		BodyText: "{{.Name}}",
		BodyHTML: "<p>{{.Name}}</p>",
	}
	out, err := svc.Render(doc, map[string]any{"Name": "<script>alert(1)</script>"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out.BodyHTML, "<script>") {
		t.Fatalf("html/template should escape <script>, got %q", out.BodyHTML)
	}
	// text/template does not escape — proves the two pipelines are different.
	if !strings.Contains(out.BodyText, "<script>") {
		t.Fatalf("text body should be left verbatim, got %q", out.BodyText)
	}
}

func TestTemplateService_Render_SkipsHTMLWhenBlank(t *testing.T) {
	svc, _ := newTestTemplateService(t)
	doc := &models.TemplateDoc{
		Subject:  "S",
		BodyText: "plain",
		BodyHTML: "   ", // whitespace only — TrimSpace should treat this as empty
	}
	out, err := svc.Render(doc, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if out.BodyHTML != "" {
		t.Fatalf("blank HTML body should render to empty string, got %q", out.BodyHTML)
	}
}

func TestTemplateService_Render_NilTemplate(t *testing.T) {
	svc, _ := newTestTemplateService(t)
	_, err := svc.Render(nil, nil)
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestTemplateService_Render_ParseError(t *testing.T) {
	svc, _ := newTestTemplateService(t)
	// {{.Name with no closing braces is a parse error in text/template.
	doc := &models.TemplateDoc{Subject: "{{.Name", BodyText: "x"}
	_, err := svc.Render(doc, nil)
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if !strings.Contains(err.Error(), "render subject") {
		t.Fatalf("error should mention subject: %v", err)
	}
}

func TestTemplateService_Get_NotFoundMapped(t *testing.T) {
	svc, _ := newTestTemplateService(t)
	_, err := svc.Get(context.Background(), "nope", "en")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestTemplateService_Get_OtherRepoErrorPassesThrough(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	repo.getErr = errors.New("boom")
	_, err := svc.Get(context.Background(), "id", "en")
	if err == nil || errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected raw repo error, got %v", err)
	}
}

// ---- Resolve: the send-time owner→system cascade --------------------------
//
// These run against the REAL templateService (NewTemplateService + the repo
// fake), not the orchestrator's fakeTemplateService double, so the ORDER of
// the two lookups is pinned here rather than reimplemented by a test double.
// Swapping the two lookups in Resolve turns the first two red.

// resolveKit stages the same (id, locale) in BOTH scopes so a test can say
// which one Resolve picked.
func resolveKit(t *testing.T) (*templateService, *fakeTemplateRepo, *models.TemplateDoc, *models.TemplateDoc) {
	t.Helper()
	svc, repo := newTestTemplateService(t)
	sys := &models.TemplateDoc{TemplateID: "auth.verify_email", Locale: "en", Subject: "sys", IsSystem: true}
	own := &models.TemplateDoc{OwnerTenantID: "A", TemplateID: "auth.verify_email", Locale: "en", Subject: "owned"}
	repo.docs[tplKey("auth.verify_email", "en")] = sys
	return svc, repo, sys, own
}

func TestTemplateService_Resolve_OwnedWinsOverSystem(t *testing.T) {
	svc, repo, _, own := resolveKit(t)
	repo.ownedStore()[ownedKey("A", "auth.verify_email", "en")] = own

	got, err := svc.Resolve(context.Background(), "A", "auth.verify_email", "en")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != own {
		t.Fatalf("Resolve must prefer the owner's document, got %+v", got)
	}
}

func TestTemplateService_Resolve_OwnedMissFallsBackToSystem(t *testing.T) {
	svc, _, sys, _ := resolveKit(t) // nothing staged in the owned store

	got, err := svc.Resolve(context.Background(), "A", "auth.verify_email", "en")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != sys {
		t.Fatalf("an owned miss must fall back to the system template, got %+v", got)
	}
}

func TestTemplateService_Resolve_EmptyOwnerGoesStraightToSystem(t *testing.T) {
	svc, repo, sys, own := resolveKit(t)
	// Stage an owned document too: with no owner it must be unreachable.
	repo.ownedStore()[ownedKey("A", "auth.verify_email", "en")] = own

	got, err := svc.Resolve(context.Background(), "", "auth.verify_email", "en")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != sys {
		t.Fatalf("an empty owner must resolve the system template, got %+v", got)
	}
}

// A repository failure on the owned lookup must NOT degrade into "no owned
// document, use the system one": that would deliver a different body than the
// tenant configured, silently, on a transient Mongo error.
func TestTemplateService_Resolve_OwnedRepoErrorDoesNotFallBackToSystem(t *testing.T) {
	svc, repo, _, _ := resolveKit(t) // the system template IS staged
	boom := errors.New("boom")
	repo.getOwnedErr = boom

	got, err := svc.Resolve(context.Background(), "A", "auth.verify_email", "en")
	if got != nil {
		t.Fatalf("a store failure must not yield a template, got %+v", got)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("the repository error must pass through, got %v", err)
	}
}

func TestTemplateService_Get_Found(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	want := &models.TemplateDoc{TemplateID: "id", Locale: "en", Subject: "S"}
	repo.docs[tplKey("id", "en")] = want
	got, err := svc.Get(context.Background(), "id", "en")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != want {
		t.Fatalf("Get returned a different pointer")
	}
}

func TestTemplateService_Upsert_FillsDefaults(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	doc := &models.TemplateDoc{TemplateID: "id", Subject: "S"}
	if err := svc.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if doc.UUID == "" {
		t.Fatalf("expected UUID to be filled")
	}
	if doc.Channel != models.ChannelEmail {
		t.Fatalf("Channel default = %q, want %q", doc.Channel, models.ChannelEmail)
	}
	if doc.Locale != "en" {
		t.Fatalf("Locale default = %q, want en", doc.Locale)
	}
	if len(repo.upserts) != 1 {
		t.Fatalf("expected one upsert, got %d", len(repo.upserts))
	}
}

func TestTemplateService_Upsert_PreservesProvidedFields(t *testing.T) {
	svc, _ := newTestTemplateService(t)
	doc := &models.TemplateDoc{
		UUID:       "manual-uuid",
		TemplateID: "id",
		Channel:    "email",
		Locale:     "it",
		Subject:    "S",
	}
	if err := svc.Upsert(context.Background(), doc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if doc.UUID != "manual-uuid" || doc.Locale != "it" || doc.Channel != "email" {
		t.Fatalf("Upsert overwrote provided fields: %+v", doc)
	}
}

func TestTemplateService_Delete_Passthrough(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	repo.docs[tplKey("id", "en")] = &models.TemplateDoc{}
	if err := svc.Delete(context.Background(), "id", "en"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(repo.deletes) != 1 || repo.deletes[0] != tplKey("id", "en") {
		t.Fatalf("expected one delete for id|en, got %v", repo.deletes)
	}
}

func TestTemplateService_List_Passthrough(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	repo.docs[tplKey("a", "en")] = &models.TemplateDoc{TemplateID: "a"}
	repo.docs[tplKey("b", "en")] = &models.TemplateDoc{TemplateID: "b"}
	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(got))
	}
}

func TestTemplateService_SeedDefaults_InsertsAllWhenMissing(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	if err := svc.SeedDefaults(context.Background()); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if len(repo.upserts) != len(defaultTemplates) {
		t.Fatalf("expected %d upserts, got %d", len(defaultTemplates), len(repo.upserts))
	}
	for _, d := range repo.upserts {
		if !d.IsSystem {
			t.Fatalf("seeded template %s should have IsSystem=true", d.TemplateID)
		}
		if d.Channel != models.ChannelEmail {
			t.Fatalf("seeded template %s should be channel=email, got %q", d.TemplateID, d.Channel)
		}
		if d.UUID == "" {
			t.Fatalf("seeded template %s missing UUID", d.TemplateID)
		}
		if d.Version != 1 {
			t.Fatalf("seeded template %s should have Version=1, got %d", d.TemplateID, d.Version)
		}
	}
}

func TestTemplateService_SeedDefaults_SkipsExisting(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	// All defaults already in DB → no inserts expected.
	for _, def := range defaultTemplates {
		repo.exists[tplKey(def.TemplateID, def.Locale)] = true
	}
	if err := svc.SeedDefaults(context.Background()); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatalf("expected no upserts when all defaults exist, got %d", len(repo.upserts))
	}
}

func TestTemplateService_SeedDefaults_ExistsErrorPropagates(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	repo.existsErr = errors.New("count failed")
	if err := svc.SeedDefaults(context.Background()); err == nil {
		t.Fatalf("expected error from ExistsSystemTemplate")
	}
}

func TestTemplateService_SeedDefaults_UpsertErrorPropagates(t *testing.T) {
	svc, repo := newTestTemplateService(t)
	repo.upsertErr = errors.New("write failed")
	if err := svc.SeedDefaults(context.Background()); err == nil {
		t.Fatalf("expected error from Upsert")
	}
}

func TestRenderKeepsOutlookConditionals(t *testing.T) {
	svc := NewTemplateService(nil, discardLogger())
	doc := &models.TemplateDoc{
		Subject:  "Hi {{.Name}}",
		BodyText: "Hi {{.Name}}",
		BodyHTML: `<body><!--[if mso]><v:roundrect href="{{.URL}}"><center>Go</center></v:roundrect><![endif]--><a href="{{.URL}}">Go</a></body>`,
	}
	r, err := svc.Render(doc, map[string]any{"Name": "Mario", "URL": "https://x.example/a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.BodyHTML, `<!--[if mso]><v:roundrect href="https://x.example/a">`) {
		t.Fatalf("conditional dropped or unrendered: %s", r.BodyHTML)
	}
	if !strings.HasSuffix(r.BodyHTML, `<a href="https://x.example/a">Go</a></body>`) {
		t.Fatalf("body altered: %s", r.BodyHTML)
	}
	if r.Subject != "Hi Mario" {
		t.Fatalf("subject: %q", r.Subject)
	}
}
