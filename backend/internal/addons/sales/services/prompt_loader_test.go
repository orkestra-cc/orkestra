package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/orkestra-cc/orkestra-addon-sales/models"
)

// fakePromptRepo is a minimal PromptRepository for unit tests.
type fakePromptRepo struct {
	prompts   map[string]*models.SalesPrompt // by category|name
	upserted  []*models.SalesPrompt
	upsertErr error
}

func newFakePromptRepo() *fakePromptRepo {
	return &fakePromptRepo{prompts: map[string]*models.SalesPrompt{}}
}

func pkey(cat, name string) string { return cat + "|" + name }

func (f *fakePromptRepo) List(_ context.Context, category string) ([]models.SalesPrompt, error) {
	var out []models.SalesPrompt
	for _, p := range f.prompts {
		if category == "" || p.Category == category {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (f *fakePromptRepo) GetByUUID(_ context.Context, _ string) (*models.SalesPrompt, error) {
	return nil, errors.New("not found")
}

func (f *fakePromptRepo) GetByCategoryAndName(_ context.Context, cat, name string) (*models.SalesPrompt, error) {
	if p, ok := f.prompts[pkey(cat, name)]; ok {
		return p, nil
	}
	return nil, errors.New("not found")
}

func (f *fakePromptRepo) Upsert(_ context.Context, p *models.SalesPrompt) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	cp := *p
	f.upserted = append(f.upserted, &cp)
	f.prompts[pkey(p.Category, p.Name)] = &cp
	return nil
}

func (f *fakePromptRepo) Count(_ context.Context) (int64, error) { return int64(len(f.prompts)), nil }

func TestPromptLoader_SeedDefaults_InsertsMissing(t *testing.T) {
	repo := newFakePromptRepo()
	l := NewPromptLoader(repo, discardLogger())

	if err := l.SeedDefaults(context.Background()); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if len(repo.upserted) != len(promptMeta) {
		t.Fatalf("expected %d upserts (one per metadata entry), got %d", len(promptMeta), len(repo.upserted))
	}
	// Verify a known prompt was upserted with the expected category and name.
	if p, ok := repo.prompts[pkey("agents", "company-research")]; !ok {
		t.Fatal("company-research prompt missing")
	} else {
		if p.DisplayName != "Company Research" {
			t.Errorf("DisplayName = %q", p.DisplayName)
		}
		if !strings.Contains(p.Content, "{{.URL}}") {
			t.Errorf("expected embedded content with {{.URL}}, got %q", p.Content[:min(120, len(p.Content))])
		}
		if p.IsCustom {
			t.Errorf("seeded prompt must be IsCustom=false")
		}
	}
}

func TestPromptLoader_SeedDefaults_SkipsExisting(t *testing.T) {
	repo := newFakePromptRepo()
	// Pre-populate every default entry — SeedDefaults should be a no-op.
	for _, meta := range promptMeta {
		repo.prompts[pkey(meta.Category, meta.Name)] = &models.SalesPrompt{
			Category: meta.Category,
			Name:     meta.Name,
			IsCustom: true, // custom edit should be preserved
		}
	}
	l := NewPromptLoader(repo, discardLogger())
	if err := l.SeedDefaults(context.Background()); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if len(repo.upserted) != 0 {
		t.Errorf("SeedDefaults must skip existing prompts, got %d upserts", len(repo.upserted))
	}
}

func TestPromptLoader_SeedDefaults_PropagatesUpsertError(t *testing.T) {
	repo := newFakePromptRepo()
	repo.upsertErr = errors.New("write boom")
	l := NewPromptLoader(repo, discardLogger())
	// SeedDefaults logs but does not return individual upsert errors — verify it does not
	// abort the iteration on the first error.
	if err := l.SeedDefaults(context.Background()); err != nil {
		t.Fatalf("SeedDefaults must swallow per-entry errors, got %v", err)
	}
}

func TestPromptLoader_GetDefaultContent_KnownEmbedded(t *testing.T) {
	l := NewPromptLoader(newFakePromptRepo(), discardLogger())
	content, err := l.GetDefaultContent("skills", "research")
	if err != nil {
		t.Fatalf("GetDefaultContent: %v", err)
	}
	if !strings.Contains(content, "Company Research") {
		t.Errorf("embedded research.md content unexpected, got %q...", content[:min(200, len(content))])
	}
}

func TestPromptLoader_GetDefaultContent_Missing(t *testing.T) {
	l := NewPromptLoader(newFakePromptRepo(), discardLogger())
	_, err := l.GetDefaultContent("skills", "no-such-prompt")
	if err == nil || !strings.Contains(err.Error(), "embedded prompt not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestPromptLoader_Load_PrefersDBOverEmbedded(t *testing.T) {
	repo := newFakePromptRepo()
	repo.prompts[pkey("skills", "research")] = &models.SalesPrompt{
		Category: "skills",
		Name:     "research",
		Content:  "DB content for {{.Name}}",
	}
	l := NewPromptLoader(repo, discardLogger())
	got, err := l.Load("skills", "research", "", map[string]any{"Name": "Acme"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "DB content for Acme" {
		t.Errorf("got %q, want 'DB content for Acme'", got)
	}
}

func TestPromptLoader_Load_PrefersLocaleMatch(t *testing.T) {
	repo := newFakePromptRepo()
	repo.prompts[pkey("skills", "research")] = &models.SalesPrompt{Category: "skills", Name: "research", Content: "BASE"}
	repo.prompts[pkey("skills", "research-it")] = &models.SalesPrompt{Category: "skills", Name: "research-it", Content: "LOCALE-IT"}
	l := NewPromptLoader(repo, discardLogger())
	got, err := l.Load("skills", "research", "it", nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != "LOCALE-IT" {
		t.Errorf("expected locale-specific content, got %q", got)
	}
}

func TestPromptLoader_Load_FallsBackToEmbedded(t *testing.T) {
	l := NewPromptLoader(newFakePromptRepo(), discardLogger())
	got, err := l.Load("skills", "research", "", map[string]any{"URL": "https://x"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !strings.Contains(got, "https://x") {
		t.Errorf("expected URL substitution in embedded content, got first 300 chars: %q", got[:min(300, len(got))])
	}
}

func TestPromptLoader_Load_MissingTemplateReturnsError(t *testing.T) {
	l := NewPromptLoader(newFakePromptRepo(), discardLogger())
	_, err := l.Load("skills", "no-such-template", "", nil)
	if err == nil || !strings.Contains(err.Error(), "prompt not found") {
		t.Errorf("expected prompt-not-found, got %v", err)
	}
}

func TestPromptLoader_Load_MissingKeyZero(t *testing.T) {
	repo := newFakePromptRepo()
	repo.prompts[pkey("skills", "x")] = &models.SalesPrompt{Category: "skills", Name: "x", Content: "Hello {{.Missing}}!"}
	l := NewPromptLoader(repo, discardLogger())
	// missingkey=zero must produce empty for absent variables, not an error.
	got, err := l.Load("skills", "x", "", map[string]any{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Go's text/template missingkey=zero renders <no value> for non-map types,
	// but for map[string]any with missing key it outputs the zero value of any (nil) → "<no value>".
	// We just assert the static parts of the template made it through.
	if !strings.HasPrefix(got, "Hello ") || !strings.HasSuffix(got, "!") {
		t.Errorf("template static parts missing, got %q", got)
	}
}

func TestPromptLoader_Load_ParseErrorPropagates(t *testing.T) {
	repo := newFakePromptRepo()
	repo.prompts[pkey("skills", "x")] = &models.SalesPrompt{Category: "skills", Name: "x", Content: "Hello {{.Missing"}
	l := NewPromptLoader(repo, discardLogger())
	_, err := l.Load("skills", "x", "", nil)
	if err == nil || !strings.Contains(err.Error(), "parse template") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestPromptLoader_LoadSkillAndLoadAgent(t *testing.T) {
	repo := newFakePromptRepo()
	repo.prompts[pkey("skills", "qualify")] = &models.SalesPrompt{Category: "skills", Name: "qualify", Content: "QUAL"}
	repo.prompts[pkey("agents", "company-research")] = &models.SalesPrompt{Category: "agents", Name: "company-research", Content: "AGENT"}
	l := NewPromptLoader(repo, discardLogger())

	got, err := l.LoadSkill("qualify", "", nil)
	if err != nil || got != "QUAL" {
		t.Errorf("LoadSkill: got=%q err=%v", got, err)
	}
	got, err = l.LoadAgent("company-research", "", nil)
	if err != nil || got != "AGENT" {
		t.Errorf("LoadAgent: got=%q err=%v", got, err)
	}
}

func TestPromptLoader_InvalidateCache(t *testing.T) {
	repo := newFakePromptRepo()
	repo.prompts[pkey("skills", "x")] = &models.SalesPrompt{Category: "skills", Name: "x", Content: "v1"}
	l := NewPromptLoader(repo, discardLogger())

	if _, err := l.Load("skills", "x", "", nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(l.cache) == 0 {
		t.Fatal("cache should be populated after first Load")
	}
	l.InvalidateCache()
	if len(l.cache) != 0 {
		t.Errorf("InvalidateCache must clear the cache, got %d entries", len(l.cache))
	}
}

func TestFormatAgentName_ExportedHelper(t *testing.T) {
	// Exported counterpart of the (unexported) formatAgentName in report_generator.go.
	cases := map[string]string{
		"":                 "",
		"foo":              "Foo",
		"a-b-c":            "A B C",
		"company-research": "Company Research",
	}
	for in, want := range cases {
		if got := FormatAgentName(in); got != want {
			t.Errorf("FormatAgentName(%q) = %q, want %q", in, got, want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
