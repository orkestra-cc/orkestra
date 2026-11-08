package notification

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// recordingMW records which system permission each route group demands and
// lets the request through only when the X-Perms header lists every one the
// request's path passed through.
type recordingMW struct {
	module.RoleMiddleware
	tier string
}

func (m recordingMW) RequireSystemPermission(p string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(","+r.Header.Get("X-Perms")+",", ","+p+",") {
				http.Error(w, "forbidden:"+p, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m recordingMW) RequireGlobal() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

func routedModule(t *testing.T, withClient bool) (op, cl chi.Router) {
	t.Helper()
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI("mongodb://127.0.0.1:1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Disconnect(context.Background()) })
	m := NewModule()
	deps := &module.Dependencies{
		DB: client.Database("notification_push_routes_test"), Services: module.NewServiceRegistry(),
		Platform: fakePlatform{}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := m.Init(deps); err != nil {
		t.Fatalf("Init: %v", err)
	}
	op = chi.NewRouter()
	ri := &module.RouteInfo{
		Operator:  &module.APISurface{PublicAPI: humachi.New(chi.NewRouter(), huma.DefaultConfig("p", "1")), ProtectedRouter: op, AuthMW: recordingMW{tier: "operator"}},
		APIConfig: huma.DefaultConfig("t", "1"),
	}
	if withClient {
		cl = chi.NewRouter()
		ri.Client = &module.APISurface{ProtectedRouter: cl, AuthMW: recordingMW{tier: "client"}}
	}
	m.RegisterRoutes(ri)
	return op, cl
}

func status(h http.Handler, method, path, perms string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"tier":"operator","userUuid":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Perms", perms)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestRegisterRoutes_TestPushNeedsNotificationTest(t *testing.T) {
	op, _ := routedModule(t, false)
	const p = "/v1/notifications/push/test"
	if c := status(op, http.MethodPost, p, ""); c != http.StatusForbidden {
		t.Fatalf("no permission: %d", c)
	}
	if c := status(op, http.MethodPost, p, "notification.log.read"); c != http.StatusForbidden {
		t.Fatalf("log.read alone must not send a push: %d", c)
	}
	if c := status(op, http.MethodPost, p, "notification.test"); c != http.StatusForbidden {
		t.Fatalf("notification.test alone must not skip the admin group gate: %d", c)
	}
	if c := status(op, http.MethodPost, p, "notification.log.read,notification.test"); c != http.StatusUnprocessableEntity {
		t.Fatalf("both permissions must reach body validation (422), got %d", c)
	}
	// Other admin routes stay behind log.read only (no notification.test
	// needed); a 403 here would mean the extra gate leaked onto the group.
	if c := status(op, http.MethodGet, "/v1/notifications", ""); c != http.StatusForbidden {
		t.Fatalf("the delivery log still needs log.read: %d", c)
	}
}

func TestRegisterRoutes_DeviceRoutesOnBothTiers(t *testing.T) {
	op, cl := routedModule(t, true)
	const p = "/v1/notifications/push/devices"
	for name, r := range map[string]chi.Router{"operator": op, "client": cl} {
		if c := status(r, http.MethodGet, p, ""); c != http.StatusUnauthorized {
			t.Errorf("%s: device list is mounted and self-service (401 without user), got %d", name, c)
		}
	}
	if c := status(cl, http.MethodPost, "/v1/notifications/push/test", "notification.test,notification.log.read"); c != http.StatusNotFound && c != http.StatusMethodNotAllowed {
		t.Errorf("admin test push must not exist on the client tier, got %d", c)
	}
}

func TestRegisterRoutes_NoClientSurface(t *testing.T) {
	routedModule(t, false) // must not panic when ri.Client is nil
}
