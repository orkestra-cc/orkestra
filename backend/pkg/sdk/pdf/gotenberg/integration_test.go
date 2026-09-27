package gotenberg

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Runs against a real Gotenberg: GOTENBERG_TEST_URL=http://… [GOTENBERG_TEST_USER/PASSWORD].
func TestIntegration_ConvertWithInlineAsset(t *testing.T) {
	url := os.Getenv("GOTENBERG_TEST_URL")
	if url == "" {
		t.Skip("set GOTENBERG_TEST_URL to run the live Gotenberg test")
	}
	c := New(Config{URL: url, Username: os.Getenv("GOTENBERG_TEST_USER"), Password: os.Getenv("GOTENBERG_TEST_PASSWORD")})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a} // header only: Chromium shows a broken image, the PDF is still valid
	out, err := c.RenderHTML(ctx, iface.HTMLDocument{
		HTML:   `<html><body><h1>Ricevuta — àèìòù €</h1><img src="asset-x.png"></body></html>`,
		Assets: map[string][]byte{"asset-x.png": png},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "%PDF-") {
		t.Fatalf("not a PDF: %q", out[:8])
	}
}
