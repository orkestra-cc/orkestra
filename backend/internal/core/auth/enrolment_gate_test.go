package auth

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sharederrors "github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// stubRoleMiddleware satisfies module.RoleMiddleware and DELIBERATELY does not
// implement module.EnrolmentProofGate — it stands in for a fork's own
// middleware written before the sub-interface existed. Every gate is a
// pass-through, so if enrolmentGate resolved the enrolment gate off this type
// the request would sail past ungated, which is precisely the failure the
// fail-closed fallback exists to prevent.
type stubRoleMiddleware struct{}

func passThrough() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

func (stubRoleMiddleware) RequirePermission(string) func(http.Handler) http.Handler {
	return passThrough()
}
func (stubRoleMiddleware) RequireSystemPermission(string) func(http.Handler) http.Handler {
	return passThrough()
}
func (stubRoleMiddleware) RequireCapability(string) func(http.Handler) http.Handler {
	return passThrough()
}
func (stubRoleMiddleware) RequireGlobal() func(http.Handler) http.Handler { return passThrough() }
func (stubRoleMiddleware) RequireMFA() func(http.Handler) http.Handler    { return passThrough() }
func (stubRoleMiddleware) RequireStepUp(time.Duration) func(http.Handler) http.Handler {
	return passThrough()
}
func (stubRoleMiddleware) RequireLowRisk(float64) func(http.Handler) http.Handler {
	return passThrough()
}
func (stubRoleMiddleware) RequireInternalTenant() func(http.Handler) http.Handler {
	return passThrough()
}
func (stubRoleMiddleware) RequireExternalTenant() func(http.Handler) http.Handler {
	return passThrough()
}

// driveGate serves one claim-less request through the middleware the helper
// returned. A claim-less request is the discriminator between the two arms:
// the real gate answers with the errorManager's generic 401, which carries NO
// top-level `code`, while the fail-closed stand-in answers step_up_required
// unconditionally. Nothing here has to reach into the middleware package's
// unexported context key to tell them apart.
func driveGate(t *testing.T, gate func(http.Handler) http.Handler) (int, map[string]any, bool) {
	t.Helper()
	rec := httptest.NewRecorder()
	downstreamRan := false
	gate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downstreamRan = true
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/auth/operator/mfa/enroll/begin", nil))

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec.Code, body, downstreamRan
}

func TestEnrolmentGate_RealMiddlewareResolvesTheRealGate(t *testing.T) {
	mw := authMiddleware.NewAuthMiddleware(nil, sharederrors.NewManager(slog.Default(), false))

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	status, body, ran := driveGate(t, enrolmentGate(logger, "operator", mw, 5*time.Minute))
	if ran {
		t.Fatal("a claim-less request must never reach the handler")
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	// The real gate's no-claims branch goes through sendErrorResponse, which
	// emits no top-level code. Getting step_up_required here would mean the
	// assertion failed and the stand-in was substituted for a middleware that
	// does implement the interface.
	if code, ok := body["code"]; ok {
		t.Fatalf("body carries code %v — the fail-closed stand-in was substituted for the real gate", code)
	}
	if logs.Len() != 0 {
		t.Fatalf("a satisfied assertion must log nothing, got %q", logs.String())
	}
}

func TestEnrolmentGate_MissingSubInterfaceRefusesAndWarns(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))

	status, body, ran := driveGate(t, enrolmentGate(logger, "client", stubRoleMiddleware{}, 5*time.Minute))
	if ran {
		t.Fatal("a middleware without the sub-interface must refuse, never pass through — " +
			"that would leave MFA enrolment ungated, which is H-2/H-3 itself")
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
	if code, _ := body["code"].(string); code != "step_up_required" {
		t.Fatalf("body.code = %q, want step_up_required", code)
	}
	if body["maxAgeSeconds"] != float64(300) {
		t.Errorf("maxAgeSeconds = %v, want 300", body["maxAgeSeconds"])
	}

	// The WARN is what makes a fork's missing implementation visible at boot
	// rather than at a user's 401, so it is part of the contract.
	out := logs.String()
	if !strings.Contains(out, "module.EnrolmentProofGate") {
		t.Errorf("warning must name the interface; got %q", out)
	}
	if !strings.Contains(out, "surface=client") {
		t.Errorf("warning must name the surface; got %q", out)
	}
	if !strings.Contains(out, "refuse every caller") {
		t.Errorf("warning must name the consequence; got %q", out)
	}
}

// A nil logger must not panic the wiring path — a module built without one
// still has to mount its routes.
func TestEnrolmentGate_NilLoggerStillRefuses(t *testing.T) {
	status, body, ran := driveGate(t, enrolmentGate(nil, "operator", stubRoleMiddleware{}, 5*time.Minute))
	if ran || status != http.StatusUnauthorized {
		t.Fatalf("want a 401 refusal, got status %d ran=%v", status, ran)
	}
	if code, _ := body["code"].(string); code != "step_up_required" {
		t.Fatalf("body.code = %q, want step_up_required", code)
	}
}

// The real AuthMiddleware must satisfy the sub-interface at compile time —
// the assertion above is a runtime check, and a rename would otherwise turn
// the whole enrolment surface into a silent refusal.
var _ module.EnrolmentProofGate = (*authMiddleware.AuthMiddleware)(nil)
