package services

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNoopPushDriver_DeliversAllAndNeverLogsFullToken(t *testing.T) {
	var buf bytes.Buffer
	d := NewNoopPushDriver(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if d.Name() != "noop" {
		t.Fatal(d.Name())
	}
	tok := "abcdefgh-SECRET-TAIL-1234567890"
	out := d.Send(context.Background(), []PushMessage{
		{Token: tok, Title: "Hello", Body: "secret body", Data: map[string]string{"k": "v"}},
		{Token: "short"},
	})
	if len(out) != 2 || !out[0].Delivered || !out[1].Delivered {
		t.Fatalf("outcomes = %+v", out)
	}
	logs := buf.String()
	if strings.Contains(logs, tok) || strings.Contains(logs, "SECRET-TAIL") || strings.Contains(logs, "secret body") {
		t.Fatalf("log leaks content: %s", logs)
	}
	if !strings.Contains(logs, "abcdefgh") {
		t.Fatalf("token prefix missing: %s", logs)
	}
}
