package services

import (
	"context"
	"log/slog"
	"sort"
)

type noopPushDriver struct{ logger *slog.Logger }

// NewNoopPushDriver logs each message (token prefix, title length, data keys —
// never the token, title or body) and reports it delivered.
func NewNoopPushDriver(logger *slog.Logger) PushDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &noopPushDriver{logger: logger}
}

func (d *noopPushDriver) Name() string { return "noop" }

func (d *noopPushDriver) Send(ctx context.Context, msgs []PushMessage) []PushOutcome {
	out := make([]PushOutcome, len(msgs))
	for i, m := range msgs {
		keys := make([]string, 0, len(m.Data))
		for k := range m.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		d.logger.Info("notification: push (noop driver)",
			"device_ref", tokenPrefix(m.Token), "title_len", len(m.Title), "data_keys", keys, "platform", m.Platform)
		out[i] = PushOutcome{Delivered: true}
	}
	return out
}
