package iface

import (
	"context"
	"testing"
)

type coarseSender struct{ configured bool }

func (c coarseSender) IsConfigured(context.Context) bool { return c.configured }
func (c coarseSender) Send(context.Context, NotificationRequest) (*NotificationResult, error) {
	return nil, nil
}
func (c coarseSender) SendTemplated(context.Context, TemplatedNotificationRequest) (*NotificationResult, error) {
	return nil, nil
}

type exactSender struct {
	coarseSender
	perCategory map[string]bool
	asked       []string
}

func (e *exactSender) IsConfiguredFor(_ context.Context, category string) bool {
	e.asked = append(e.asked, category)
	return e.perCategory[category]
}

// A sender implementing the companion gets the exact answer; one that does
// not falls back to IsConfigured — the fork-compatibility guarantee (D7).
func TestIsConfiguredForCategory(t *testing.T) {
	ctx := context.Background()
	if IsConfiguredForCategory(ctx, nil, "auth.verify_email") {
		t.Fatal("nil sender must be not configured")
	}
	if !IsConfiguredForCategory(ctx, coarseSender{configured: true}, "auth.verify_email") {
		t.Fatal("a sender without the companion falls back to IsConfigured")
	}
	if IsConfiguredForCategory(ctx, coarseSender{configured: false}, "auth.verify_email") {
		t.Fatal("fallback must honour IsConfigured=false")
	}
	e := &exactSender{coarseSender: coarseSender{configured: true}, perCategory: map[string]bool{"auth.verify_email": false, "crm.campaign": true}}
	if IsConfiguredForCategory(ctx, e, "auth.verify_email") {
		t.Fatal("exact answer must win over the coarse true")
	}
	if !IsConfiguredForCategory(ctx, e, "crm.campaign") {
		t.Fatal("exact answer must win")
	}
	if len(e.asked) != 2 || e.asked[0] != "auth.verify_email" {
		t.Fatalf("companion must be asked for the category: %v", e.asked)
	}
}
