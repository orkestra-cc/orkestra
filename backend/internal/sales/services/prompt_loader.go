package services

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/sales/models"
	"github.com/orkestra/backend/internal/sales/prompts"
	"github.com/orkestra/backend/internal/sales/repository"
)

// Prompt metadata for seeding
var promptMeta = []struct {
	Category    string
	Name        string
	DisplayName string
	Description string
}{
	{"agents", "company-research", "Company Research", "Firmographic analysis and business profiling"},
	{"agents", "contact-finder", "Contact Finder", "Identify decision makers and stakeholders"},
	{"agents", "opportunity-scoring", "Opportunity Scoring", "BANT + MEDDIC lead qualification scoring"},
	{"agents", "competitive-analysis", "Competitive Analysis", "Competitive landscape and market positioning"},
	{"agents", "outreach-strategy", "Outreach Strategy", "Multi-touch outreach email sequence generation"},
	{"skills", "research", "Company Research", "Firmographic analysis and business profiling"},
	{"skills", "qualify", "Lead Qualification", "BANT + MEDDIC lead qualification scoring"},
	{"skills", "contacts", "Contact Finder", "Identify decision makers and stakeholders"},
	{"skills", "outreach", "Outreach", "Generate cold outreach email sequences"},
	{"skills", "followup", "Follow-up", "Generate follow-up sequences"},
	{"skills", "prep", "Meeting Prep", "Pre-meeting intelligence brief"},
	{"skills", "proposal", "Proposal", "Client proposal generation"},
	{"skills", "objections", "Objections", "Objection handling playbook"},
	{"skills", "icp", "ICP Builder", "Ideal Customer Profile generation"},
	{"skills", "competitors", "Competitors", "Competitive landscape analysis"},
}

// PromptLoader loads prompt templates from MongoDB with embedded fallback.
type PromptLoader struct {
	repo   repository.PromptRepository
	fs     fs.FS
	cache  map[string]*template.Template
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewPromptLoader creates a PromptLoader backed by MongoDB with embedded fallback.
func NewPromptLoader(repo repository.PromptRepository, logger *slog.Logger) *PromptLoader {
	return &PromptLoader{
		repo:   repo,
		fs:     prompts.FS,
		cache:  make(map[string]*template.Template),
		logger: logger.With(slog.String("component", "prompt-loader")),
	}
}

// SeedDefaults ensures all embedded prompts exist in the DB.
// Existing prompts (especially customized ones) are never overwritten.
func (l *PromptLoader) SeedDefaults(ctx context.Context) error {
	seeded := 0
	for _, meta := range promptMeta {
		// Skip if already in DB
		if _, err := l.repo.GetByCategoryAndName(ctx, meta.Category, meta.Name); err == nil {
			continue
		}

		path := meta.Category + "/" + meta.Name + ".md"
		data, err := fs.ReadFile(l.fs, path)
		if err != nil {
			l.logger.Warn("embedded prompt not found", slog.String("path", path))
			continue
		}

		prompt := &models.SalesPrompt{
			UUID:        uuid.New().String(),
			Category:    meta.Category,
			Name:        meta.Name,
			DisplayName: meta.DisplayName,
			Description: meta.Description,
			Content:     string(data),
			IsCustom:    false,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := l.repo.Upsert(ctx, prompt); err != nil {
			l.logger.Error("failed to seed prompt", slog.String("name", meta.Name), slog.String("error", err.Error()))
			continue
		}
		seeded++
	}

	if seeded > 0 {
		l.logger.Info("seeded missing prompts", slog.Int("count", seeded))
	}
	return nil
}

// GetDefaultContent returns the embedded default content for a prompt
func (l *PromptLoader) GetDefaultContent(category, name string) (string, error) {
	path := category + "/" + name + ".md"
	data, err := fs.ReadFile(l.fs, path)
	if err != nil {
		return "", fmt.Errorf("embedded prompt not found: %s", path)
	}
	return string(data), nil
}

// Load returns a rendered prompt for the given name and locale.
// Priority: DB (with locale match) → DB (base) → embedded file.
func (l *PromptLoader) Load(category, name, locale string, vars map[string]any) (string, error) {
	content, err := l.loadContent(category, name, locale)
	if err != nil {
		return "", fmt.Errorf("prompt not found: %s/%s (locale=%s): %w", category, name, locale, err)
	}

	tmpl, err := l.getOrParse(category+"/"+name, content)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("prompt template execution failed for %s: %w", name, err)
	}
	return buf.String(), nil
}

// LoadSkill is a convenience wrapper for loading skill prompts
func (l *PromptLoader) LoadSkill(name, locale string, vars map[string]any) (string, error) {
	return l.Load("skills", name, locale, vars)
}

// LoadAgent is a convenience wrapper for loading agent prompts
func (l *PromptLoader) LoadAgent(name, locale string, vars map[string]any) (string, error) {
	return l.Load("agents", name, locale, vars)
}

func (l *PromptLoader) loadContent(category, name, locale string) (string, error) {
	ctx := context.Background()

	// Try locale-specific from DB first
	if locale != "" {
		localeName := name + "-" + locale
		if p, err := l.repo.GetByCategoryAndName(ctx, category, localeName); err == nil {
			return p.Content, nil
		}
	}

	// Try base name from DB
	if p, err := l.repo.GetByCategoryAndName(ctx, category, name); err == nil {
		return p.Content, nil
	}

	// Fall back to embedded
	paths := []string{}
	if locale != "" {
		paths = append(paths, "locale/"+locale+"/"+name+".md")
	}
	paths = append(paths, category+"/"+name+".md")

	for _, path := range paths {
		data, err := fs.ReadFile(l.fs, path)
		if err == nil {
			return string(data), nil
		}
	}

	return "", fmt.Errorf("no prompt found for %s/%s", category, name)
}

func (l *PromptLoader) getOrParse(key, content string) (*template.Template, error) {
	// Use content hash as cache key to invalidate on edit
	cacheKey := key + ":" + fmt.Sprintf("%d", len(content))

	l.mu.RLock()
	if tmpl, ok := l.cache[cacheKey]; ok {
		l.mu.RUnlock()
		return tmpl, nil
	}
	l.mu.RUnlock()

	// Use missingkey=zero to avoid errors on optional template vars
	tmpl, err := template.New(key).Option("missingkey=zero").Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", key, err)
	}

	l.mu.Lock()
	l.cache[cacheKey] = tmpl
	l.mu.Unlock()

	return tmpl, nil
}

// InvalidateCache clears the template cache (call after prompt edit)
func (l *PromptLoader) InvalidateCache() {
	l.mu.Lock()
	l.cache = make(map[string]*template.Template)
	l.mu.Unlock()
}

// FormatAgentName converts "company-research" to "Company Research"
func FormatAgentName(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
