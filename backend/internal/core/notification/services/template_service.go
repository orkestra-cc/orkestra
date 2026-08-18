package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"strings"
	ttemplate "text/template"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var ErrTemplateNotFound = errors.New("notification: template not found")

// Rendered is the output of a template render.
type Rendered struct {
	Subject  string
	BodyText string
	BodyHTML string
}

// TemplateService resolves templates by ID + locale, seeds system defaults,
// and renders the template body with the provided data map.
type TemplateService interface {
	SeedDefaults(ctx context.Context) error
	SeedModuleTemplates(ctx context.Context, specs []module.NotificationTemplateSpec) error
	Get(ctx context.Context, templateID, locale string) (*models.TemplateDoc, error)
	List(ctx context.Context) ([]*models.TemplateDoc, error)
	Upsert(ctx context.Context, doc *models.TemplateDoc) error
	Delete(ctx context.Context, templateID, locale string) error
	Render(tmpl *models.TemplateDoc, data map[string]any) (*Rendered, error)
}

type templateService struct {
	repo   repository.TemplateRepository
	logger *slog.Logger
}

func NewTemplateService(repo repository.TemplateRepository, logger *slog.Logger) TemplateService {
	return &templateService{repo: repo, logger: logger}
}

func (s *templateService) SeedDefaults(ctx context.Context) error {
	for _, def := range defaultTemplates {
		if err := s.seedOne(ctx, def); err != nil {
			return err
		}
	}
	return nil
}

// SeedModuleTemplates seeds templates declared by modules through
// module.HasNotificationTemplates. Same insert-if-absent rule as
// SeedDefaults: an operator's edits survive a restart.
func (s *templateService) SeedModuleTemplates(ctx context.Context, specs []module.NotificationTemplateSpec) error {
	for _, spec := range specs {
		if err := s.seedOne(ctx, defaultTemplate{
			TemplateID:  spec.TemplateID,
			Locale:      spec.Locale,
			Subject:     spec.Subject,
			BodyText:    spec.BodyText,
			BodyHTML:    spec.BodyHTML,
			Description: spec.Description,
			Variables:   spec.Variables,
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedOne inserts a single template if it does not already exist as a
// system template. Never overwrites an existing document — an operator's
// edit to a previously-seeded template must survive re-seeding.
func (s *templateService) seedOne(ctx context.Context, def defaultTemplate) error {
	exists, err := s.repo.ExistsSystemTemplate(ctx, def.TemplateID, def.Locale)
	if err != nil {
		return fmt.Errorf("check template %s/%s: %w", def.TemplateID, def.Locale, err)
	}
	if exists {
		return nil
	}
	doc := &models.TemplateDoc{
		UUID:        uuid.Must(uuid.NewV7()).String(),
		TemplateID:  def.TemplateID,
		Locale:      def.Locale,
		Channel:     models.ChannelEmail,
		Subject:     def.Subject,
		BodyText:    def.BodyText,
		BodyHTML:    def.BodyHTML,
		Description: def.Description,
		Variables:   def.Variables,
		IsSystem:    true,
		Version:     1,
	}
	if err := s.repo.Upsert(ctx, doc); err != nil {
		return fmt.Errorf("seed template %s/%s: %w", def.TemplateID, def.Locale, err)
	}
	s.logger.Info("Seeded notification template",
		slog.String("templateId", def.TemplateID),
		slog.String("locale", def.Locale),
	)
	return nil
}

func (s *templateService) Get(ctx context.Context, templateID, locale string) (*models.TemplateDoc, error) {
	doc, err := s.repo.GetByID(ctx, templateID, locale)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrTemplateNotFound
	}
	return doc, err
}

func (s *templateService) List(ctx context.Context) ([]*models.TemplateDoc, error) {
	return s.repo.List(ctx)
}

func (s *templateService) Upsert(ctx context.Context, doc *models.TemplateDoc) error {
	if doc.UUID == "" {
		doc.UUID = uuid.Must(uuid.NewV7()).String()
	}
	if doc.Channel == "" {
		doc.Channel = models.ChannelEmail
	}
	if doc.Locale == "" {
		doc.Locale = "en"
	}
	return s.repo.Upsert(ctx, doc)
}

func (s *templateService) Delete(ctx context.Context, templateID, locale string) error {
	return s.repo.DeleteByID(ctx, templateID, locale)
}

// Render applies the template body against the data map. The subject and
// plain-text body are rendered with text/template, the HTML body with
// html/template for contextual escaping.
func (s *templateService) Render(tmpl *models.TemplateDoc, data map[string]any) (*Rendered, error) {
	if tmpl == nil {
		return nil, ErrTemplateNotFound
	}

	subject, err := renderText("subject", tmpl.Subject, data)
	if err != nil {
		return nil, fmt.Errorf("render subject: %w", err)
	}
	bodyText, err := renderText("body_text", tmpl.BodyText, data)
	if err != nil {
		return nil, fmt.Errorf("render body text: %w", err)
	}
	bodyHTML := ""
	if strings.TrimSpace(tmpl.BodyHTML) != "" {
		bodyHTML, err = renderHTML("body_html", tmpl.BodyHTML, data)
		if err != nil {
			return nil, fmt.Errorf("render body html: %w", err)
		}
	}

	return &Rendered{
		Subject:  subject,
		BodyText: bodyText,
		BodyHTML: bodyHTML,
	}, nil
}

func renderText(name, body string, data map[string]any) (string, error) {
	t, err := ttemplate.New(name).Parse(body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderHTML(name, body string, data map[string]any) (string, error) {
	t, err := template.New(name).Parse(body)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
