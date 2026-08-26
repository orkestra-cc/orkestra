package services

import (
	"context"
	"log/slog"
)

// noopDriver logs the rendered message instead of sending it — the dev /
// bootstrap transport every fresh install boots with.
type noopDriver struct{ logger *slog.Logger }

func NewNoopDriver(logger *slog.Logger) EmailDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &noopDriver{logger: logger}
}

func (d *noopDriver) Name() string                   { return "noop" }
func (d *noopDriver) Requires() []ProfileRequirement { return nil }

func (d *noopDriver) Send(_ context.Context, _ SenderProfile, msg EmailMessage) error {
	d.logger.Info("notification.email noop send",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
	)
	d.logger.Debug("notification.email body",
		slog.String("text", truncate(msg.BodyText, 500)),
	)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
