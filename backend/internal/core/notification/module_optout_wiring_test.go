package notification

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// fakePlatform is the minimal module.PlatformInfo Init needs to build the
// notification URL builder. Values are arbitrary — nothing in this test
// exercises them.
type fakePlatform struct{}

func (fakePlatform) IsProduction() bool     { return false }
func (fakePlatform) IsStaging() bool        { return false }
func (fakePlatform) IsDevelopment() bool    { return true }
func (fakePlatform) IsProductionLike() bool { return false }
func (fakePlatform) GetEnvironment() string { return "development" }
func (fakePlatform) FrontendURL() string    { return "https://example.test" }

// TestInit_WiresTheOptoutSeam pins the fact that module.go's Init leaves
// NotificationService.optouts non-nil. This is cheap on purpose: it needs no
// live MongoDB, because building repositories only stores the *mongo.Database
// reference (mongo.Connect never dials for it) — no query runs here.
//
// Why this test exists: dispatchEmail treats a nil opt-out seam as
// fail-closed, not as "no check" (see notification_service.go), so a silent
// unwiring wouldn't show up as delivered marketing to opted-out addresses —
// it would show up as EVERY marketing send failing. Either way, it must be
// caught here rather than discovered when this module is rebased onto a
// branch that rewrites Init.
func TestInit_WiresTheOptoutSeam(t *testing.T) {
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("mongo.Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })

	m := NewModule()
	deps := &module.Dependencies{
		DB:       client.Database("notification_optout_wiring_test"),
		Services: module.NewServiceRegistry(),
		Platform: fakePlatform{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := m.Init(deps); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.svc.Optouts() == nil {
		t.Fatal("Init must leave the opt-out seam wired (non-nil); a nil seam fails every marketing send closed")
	}
}
