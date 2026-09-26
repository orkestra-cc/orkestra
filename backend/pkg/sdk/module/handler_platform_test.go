package module

import (
	"context"
	"errors"
	"testing"
)

func TestHealthCheck_PlatformChecks(t *testing.T) {
	h := NewModuleAdminHandler(nil, NewModuleRegistry(nil))
	h.SetPlatformChecks([]PlatformCheck{
		{Name: "pdf_renderer", Check: func(context.Context) error { return nil }},
		{Name: "other", Check: func(context.Context) error { return errors.New("dial tcp: refused") }},
	})
	out, err := h.HealthCheck(context.Background(), &struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Body.Platform) != 2 || out.Body.Platform[0].Status != "up" || out.Body.Platform[1].Status != "down" {
		t.Fatalf("platform = %+v", out.Body.Platform)
	}
	if out.Body.Platform[1].Error != "unreachable" {
		t.Fatalf("error must be a fixed word, never the raw cause: %q", out.Body.Platform[1].Error)
	}
}

func TestHealthCheck_NoPlatformChecks_OmitsField(t *testing.T) {
	h := NewModuleAdminHandler(nil, NewModuleRegistry(nil))
	out, _ := h.HealthCheck(context.Background(), &struct{}{})
	if out.Body.Platform != nil {
		t.Fatalf("platform = %+v, want nil", out.Body.Platform)
	}
}
