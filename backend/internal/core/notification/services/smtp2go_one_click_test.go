package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
)

// smtp2go declares the RFC 8058 one-click capability (driver_smtp2go.go,
// Capabilities). These tests pin what that declaration buys end to end, with
// the requirement ON (the default): a marketing profile on smtp2go saves, its
// marketing sends preflight clean, and the message the driver hands the API
// carries both headers in custom_headers.

func TestValidateSenderConfig_SMTP2GOMarketingProfileIsAdmissible(t *testing.T) {
	if err := ValidateSenderConfig(oneClickProfile("smtp2go", "marketing"), validationDrivers(), requiredWith("https://api.example")); err != nil {
		t.Fatalf("a marketing profile on smtp2go must save with the requirement on: %v", err)
	}
	// The base URL is still required: the driver carries the headers, the
	// module must still have an address to point them at.
	if err := ValidateSenderConfig(oneClickProfile("smtp2go", "marketing"), validationDrivers(), requiredWith("")); err == nil {
		t.Fatal("without a public API base URL the profile must still be refused")
	}
}

func TestDispatch_SMTP2GOMarketingCarriesOneClickHeaders(t *testing.T) {
	var raw []byte
	driver, _ := smtp2goServer(t, okHandler(&raw))
	k := newOneClickKit(t, driver, "https://api.example", false)
	p := smtp2goProfile()
	p.Slug, p.Categories, p.AllowedTypes = "camp", []string{"*"}, []string{models.TypeMarketing}
	k.resolver.profile = p

	ctx := context.Background()
	for _, sender := range []string{"camp", ""} {
		if err := k.svc.PreflightDelivery(ctx, sender, "crm.campaign", models.TypeMarketing); err != nil {
			t.Fatalf("sender=%q: smtp2go marketing must preflight clean with the requirement on: %v", sender, err)
		}
	}
	res, err := k.svc.Send(ctx, marketingTo("ada@example.test"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if res.Status != models.StatusSent {
		t.Fatalf("status = %q, want sent", res.Status)
	}
	var got smtp2goRequest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("payload: %v", err)
	}
	headers := map[string]string{}
	for _, h := range got.CustomHeaders {
		if _, dup := headers[h.Header]; dup {
			t.Fatalf("header %q sent twice: %+v", h.Header, got.CustomHeaders)
		}
		headers[h.Header] = h.Value
	}
	if v := headers["List-Unsubscribe"]; !strings.HasPrefix(v, "<https://api.example/v1/notifications/unsubscribe?token=") || !strings.HasSuffix(v, ">") {
		t.Fatalf("List-Unsubscribe = %q, want this API's one-click endpoint", v)
	}
	if v := headers["List-Unsubscribe-Post"]; v != "List-Unsubscribe=One-Click" {
		t.Fatalf("List-Unsubscribe-Post = %q", v)
	}
}
