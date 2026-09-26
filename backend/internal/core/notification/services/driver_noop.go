package services

import (
	"context"
	"log/slog"
	"regexp"
	"sort"
)

// tokenQueryParam matches a "token=<value>" query parameter — the shape a
// one-click unsubscribe URL takes both in the RFC 8058 List-Unsubscribe
// header and in the mandatory footer link every rendered template carries
// ("Unsubscribe from marketing: {{.UnsubscribeURL}}"). Since the header is
// built from the SAME raw token as the footer link, redacting only the
// header (see Send below) was not enough: the body debug line carried the
// identical secret straight back in. Chosen over dropping the body line
// entirely so dev/noop logging stays useful for everything else in the
// rendered mail.
var tokenQueryParam = regexp.MustCompile(`token=[^\s&]+`)

func redactTokens(s string) string {
	return tokenQueryParam.ReplaceAllString(s, "token=[redacted]")
}

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
		slog.String("text", truncate(redactTokens(msg.BodyText), 500)),
	)
	// Names only, never values: a header's value can carry a secret (the
	// RFC 8058 List-Unsubscribe header embeds a raw, single-use unsubscribe
	// token), and "never reaches a log — not in warn, not in error, not in
	// debug" has no exception for a dev-only driver. Sorted for a
	// deterministic line, same reasoning as the smtp driver's MIME writer.
	if len(msg.Headers) > 0 {
		keys := make([]string, 0, len(msg.Headers))
		for k := range msg.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		d.logger.Debug("notification.email headers", slog.Any("headerNames", keys))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
