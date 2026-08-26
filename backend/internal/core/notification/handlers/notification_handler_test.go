package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/notification/services"
)

const hostileSecret = "s3cr=t hunter2"

// hostileDriver is what a careless fork driver looks like: it returns the
// vendor's response verbatim.
type hostileDriver struct{}

func (hostileDriver) Name() string                            { return "hostile" }
func (hostileDriver) Requires() []services.ProfileRequirement { return nil }
func (hostileDriver) Send(context.Context, services.SenderProfile, services.EmailMessage) error {
	return fmt.Errorf("vendor response: 401 user=s12345_67 secret=%s <html>", hostileSecret)
}

func hostileService() *services.NotificationService {
	loader := func(context.Context) services.SenderConfig {
		return services.SenderConfig{Legacy: services.LegacyProfile(services.SenderProfile{Provider: "hostile"})}
	}
	return services.NewNotificationService(nil, nil, nil, nil,
		services.NewSenderResolver(loader), services.NewDriverRegistry(hostileDriver{}), nil, services.Options{})
}

// TestSendTestEmail_HostileDriverTextNeverReachesTheResponse: the HTTP
// detail, and every message huma attaches from the error chain, carry only
// the bounded reason.
func TestSendTestEmail_HostileDriverTextNeverReachesTheResponse(t *testing.T) {
	h := NewNotificationHandler(hostileService())
	req := &testEmailRequest{}
	req.Body.To = "a@example.com"
	_, err := h.SendTestEmail(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error")
	}
	texts := []string{err.Error()}
	var em *huma.ErrorModel
	if errors.As(err, &em) {
		texts = append(texts, em.Detail)
		for _, d := range em.Errors {
			texts = append(texts, d.Message)
		}
	}
	for _, text := range texts {
		if strings.Contains(text, hostileSecret) || strings.Contains(text, "<html>") || strings.Contains(text, "vendor response") {
			t.Fatalf("driver text reached the response: %q", text)
		}
	}
}
