package main

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/module"
	"github.com/orkestra/backend/pkg/sdk/pdf/gotenberg"
)

// registerPDFRenderer registers the Gotenberg client as the optional
// platform PDF renderer. With a URL it registers ALWAYS — the sidecar may
// start after the backend — and only logs the boot probe.
func registerPDFRenderer(reg *module.ServiceRegistry, cfg config.PDFRendererConfig, logger *slog.Logger) []module.PlatformCheck {
	if cfg.URL == "" {
		logger.Info("pdf renderer not configured (PDF_RENDERER_URL empty)")
		return nil
	}
	c := gotenberg.New(gotenberg.Config{URL: cfg.URL, Username: cfg.Username, Password: cfg.Password})
	reg.Register(module.ServicePDFRenderer, c)
	if err := c.Health(context.Background()); err != nil {
		logger.Warn("pdf renderer not reachable at boot; PDF features degrade until it is", slog.String("url", cfg.URL))
	} else {
		logger.Info("pdf renderer ready", slog.String("url", cfg.URL))
	}
	return []module.PlatformCheck{{Name: "pdf_renderer", Check: c.Health}}
}
