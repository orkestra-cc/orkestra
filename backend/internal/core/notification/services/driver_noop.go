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

// Capabilities: noop delivers nothing, so it trivially guarantees whatever
// it is asked to guarantee about what reaches a recipient — there is no
// recipient, and so no way to drop a header on the way to one.
func (d *noopDriver) Capabilities() DriverCapabilities {
	return DriverCapabilities{ListUnsubscribeHeaders: true}
}

func (d *noopDriver) Send(_ context.Context, _ SenderProfile, msg EmailMessage) error {
	d.logger.Info("notification.email noop send",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
	)
	d.logger.Debug("notification.email body",
		slog.String("text", truncate(msg.BodyText, 500)),
	)
	if len(msg.Headers) > 0 {
		d.logger.Debug("notification.email headers", slog.Any("headers", msg.Headers))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
