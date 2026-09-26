package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRegisterPDFRenderer_EmptyURL_NotRegistered(t *testing.T) {
	reg := module.NewServiceRegistry()
	if checks := registerPDFRenderer(reg, config.PDFRendererConfig{}, discardLogger()); checks != nil {
		t.Fatalf("checks = %v, want nil", checks)
	}
	if _, ok := module.GetTyped[iface.PDFRenderer](reg, module.ServicePDFRenderer); ok {
		t.Fatal("renderer registered with an empty URL")
	}
}

func TestRegisterPDFRenderer_URL_RegisteredEvenIfDown(t *testing.T) {
	reg := module.NewServiceRegistry()
	checks := registerPDFRenderer(reg, config.PDFRendererConfig{URL: "http://127.0.0.1:1"}, discardLogger())
	if _, ok := module.GetTyped[iface.PDFRenderer](reg, module.ServicePDFRenderer); !ok {
		t.Fatal("renderer must be registered even when unreachable at boot")
	}
	if len(checks) != 1 || checks[0].Name != "pdf_renderer" {
		t.Fatalf("checks = %+v", checks)
	}
}
