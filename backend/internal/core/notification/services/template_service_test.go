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
	upserts   []*models.TemplateDoc
	deletes   []string
}

func newFakeTemplateRepo() *fakeTemplateRepo {
	return &fakeTemplateRepo{
		docs:   map[string]*models.TemplateDoc{},
		exists: map[string]bool{},
	}
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
	f.docs[tplKey(doc.TemplateID, doc.Locale)] = &cp
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

func TestTemplateService_Render_RendersAllThreeBodies(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateRepo(), discardLogger())
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
	svc := NewTemplateService(newFakeTemplateRepo(), discardLogger())
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
	svc := NewTemplateService(newFakeTemplateRepo(), discardLogger())
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
	svc := NewTemplateService(newFakeTemplateRepo(), discardLogger())
	_, err := svc.Render(nil, nil)
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestTemplateService_Render_ParseError(t *testing.T) {
	svc := NewTemplateService(newFakeTemplateRepo(), discardLogger())
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
	svc := NewTemplateService(newFakeTemplateRepo(), discardLogger())
	_, err := svc.Get(context.Background(), "nope", "en")
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

func TestTemplateService_Get_OtherRepoErrorPassesThrough(t *testing.T) {
	repo := newFakeTemplateRepo()
	repo.getErr = errors.New("boom")
	svc := NewTemplateService(repo, discardLogger())
	_, err := svc.Get(context.Background(), "id", "en")
	if err == nil || errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected raw repo error, got %v", err)
	}
}

func TestTemplateService_Get_Found(t *testing.T) {
	repo := newFakeTemplateRepo()
	want := &models.TemplateDoc{TemplateID: "id", Locale: "en", Subject: "S"}
	repo.docs[tplKey("id", "en")] = want
	svc := NewTemplateService(repo, discardLogger())
	got, err := svc.Get(context.Background(), "id", "en")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != want {
		t.Fatalf("Get returned a different pointer")
	}
}

func TestTemplateService_Upsert_FillsDefaults(t *testing.T) {
	repo := newFakeTemplateRepo()
	svc := NewTemplateService(repo, discardLogger())
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
	repo := newFakeTemplateRepo()
	svc := NewTemplateService(repo, discardLogger())
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
	repo := newFakeTemplateRepo()
	repo.docs[tplKey("id", "en")] = &models.TemplateDoc{}
	svc := NewTemplateService(repo, discardLogger())
	if err := svc.Delete(context.Background(), "id", "en"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(repo.deletes) != 1 || repo.deletes[0] != tplKey("id", "en") {
		t.Fatalf("expected one delete for id|en, got %v", repo.deletes)
	}
}

func TestTemplateService_List_Passthrough(t *testing.T) {
	repo := newFakeTemplateRepo()
	repo.docs[tplKey("a", "en")] = &models.TemplateDoc{TemplateID: "a"}
	repo.docs[tplKey("b", "en")] = &models.TemplateDoc{TemplateID: "b"}
	svc := NewTemplateService(repo, discardLogger())
	got, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(got))
	}
}

func TestTemplateService_SeedDefaults_InsertsAllWhenMissing(t *testing.T) {
	repo := newFakeTemplateRepo()
	svc := NewTemplateService(repo, discardLogger())
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
	repo := newFakeTemplateRepo()
	// All defaults already in DB → no inserts expected.
	for _, def := range defaultTemplates {
		repo.exists[tplKey(def.TemplateID, def.Locale)] = true
	}
	svc := NewTemplateService(repo, discardLogger())
	if err := svc.SeedDefaults(context.Background()); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatalf("expected no upserts when all defaults exist, got %d", len(repo.upserts))
	}
}

func TestTemplateService_SeedDefaults_ExistsErrorPropagates(t *testing.T) {
	repo := newFakeTemplateRepo()
	repo.existsErr = errors.New("count failed")
	svc := NewTemplateService(repo, discardLogger())
	if err := svc.SeedDefaults(context.Background()); err == nil {
		t.Fatalf("expected error from ExistsSystemTemplate")
	}
}

func TestTemplateService_SeedDefaults_UpsertErrorPropagates(t *testing.T) {
	repo := newFakeTemplateRepo()
	repo.upsertErr = errors.New("write failed")
	svc := NewTemplateService(repo, discardLogger())
	if err := svc.SeedDefaults(context.Background()); err == nil {
		t.Fatalf("expected error from Upsert")
	}
}
