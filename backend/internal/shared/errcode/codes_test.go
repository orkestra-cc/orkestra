package errcode

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// goldenCodes is the wire-contract snapshot for every code declared in
// codes.go. Each entry maps the Go const name to the exact string the
// HTTP response carries. To add a code: declare it in codes.go AND add
// a row here in the same PR. To rename or remove a code: same, but the
// diff IS the wire-contract break — the SPA and any external clients
// must be coordinated.
//
// TestCodesMatchGoldenSnapshot fails if a const's value drifts;
// TestEveryConstSnapshotted fails if a new const is added without a
// matching row here (so a forgotten snapshot blocks the PR).
var goldenCodes = map[string]string{
	"NotificationSenderPatternInvalid":   "notification.sender_pattern_invalid",
	"NotificationSenderNoDefault":        "notification.sender_no_default",
	"NotificationSenderDuplicateDefault": "notification.sender_duplicate_default",
	"NotificationSenderPatternConflict":  "notification.sender_pattern_conflict",
	"NotificationSenderUnknownDriver":    "notification.sender_unknown_driver",
	"NotificationSenderIncomplete":       "notification.sender_incomplete",
	"NotificationSenderNotFound":         "notification.sender_not_found",
	"NotificationSendFailed":             "notification.send_failed",
	"AuthEmailInUse":                     "auth.email_in_use",
	"AuthJWTNotConfigured":               "auth.jwt_not_configured",
	"AuthUnavailable":                    "auth.unavailable",
	"AuthEmailNotVerified":               "auth.email_not_verified",
	"AuthRegistrationDisabled":           "auth.registration_disabled",
	"AuthEmailDomainNotAllowed":          "auth.email_domain_not_allowed",
	"AuthLoginDisabled":                  "auth.login_disabled",
	"AuthCountryBlocked":                 "auth.country_blocked",
	"AuthPasswordConfirmUnavailable":     "auth.password_confirm_unavailable",
	"AuthOAuthProviderDisabled":          "auth.oauth_provider_disabled",
	"AuthPolicyUnavailable":              "auth.policy_unavailable",
	"AuthOAuthEmailUnverified":           "auth.oauth_email_unverified",
	"AuthPasswordLoginDisabled":          "auth.password_login_disabled",
	"AuthLoginMethodLockout":             "auth.login_method_lockout",
	"AuthTooManyAttempts":                "auth.too_many_attempts",
	"AuthIPThresholdBelowAccount":        "auth.ip_threshold_below_account",
	"AuthzPermissionUnknown":             "authz.permission_unknown",
	"AuthzSystemPermissionForbidden":     "authz.system_permission_forbidden",
	"TenantSlugAlreadyInUse":             "tenant.slug_already_in_use",
	"TenantProvisioningLocked":           "tenant.provisioning_locked",
	"TenantInternalModeInvalid":          "tenant.internal_mode_invalid",
	"TenantSingleModeConflict":           "tenant.single_mode_conflict",
	"TenantDefaultReassignmentRequired":  "tenant.default_reassignment_required",
	"MarketingCardCodeCollision":         "marketing.card_code_collision",
	"MarketingCardInvalidTransition":     "marketing.card_invalid_transition",
	"NavigationOverrideUnknownParent":    "navigation.override_unknown_parent",
	"NavigationOverrideChildNotFound":    "navigation.override_child_not_found",
	"NavigationOverrideDuplicateChild":   "navigation.override_duplicate_child",
	"UserSelfDeleteForbidden":            "user.self_delete_forbidden",
	"UserLastAdminForbidden":             "user.last_admin_forbidden",
	"UserRoleEscalationForbidden":        "user.role_escalation_forbidden",
	"LoggingLogProviderUnavailable":      "logging.log_provider_unavailable",
	"LoggingLogPreviewInvalid":           "logging.log_preview_invalid",
	"LoggingLogProviderTimeout":          "logging.log_provider_timeout",
	"LoggingLogProviderFailed":           "logging.log_provider_failed",
	"LoggingMutationInvalid":             "logging.mutation_invalid",
	"LoggingConfigConflict":              "logging.config_conflict",
	"LoggingPersistenceFailed":           "logging.persistence_failed",
	"SetupStatusUnavailable":             "setup.status_unavailable",
	"SetupFinalizerStateUnavailable":     "setup.finalizer_state_unavailable",
	"SetupFinalizerBoundToAnotherAdmin":  "setup.finalizer_bound_to_another_admin",
	"SetupRecoveryRequiresSuperAdmin":    "setup.recovery_requires_super_admin",
	"SetupFinalizationAlreadyStarted":    "setup.finalization_already_started",
	"SetupAlreadyCompleted":              "setup.already_completed",
	"SetupTenantNameRequired":            "setup.tenant_name_required",
	"SetupTenantSlugRequired":            "setup.tenant_slug_required",
}

// TestCodesMatchGoldenSnapshot asserts every snapshotted code resolves
// to its expected wire value. Uses go/ast to read codes.go so renames
// or value drift are caught even if the constant is not directly
// referenced from this test file.
func TestCodesMatchGoldenSnapshot(t *testing.T) {
	t.Parallel()

	got := parseCodeConsts(t)

	for name, want := range goldenCodes {
		actual, ok := got[name]
		if !ok {
			t.Errorf("snapshot lists %q but it is not declared in codes.go — remove from goldenCodes or restore the const", name)
			continue
		}
		if actual != want {
			t.Errorf("%s = %q, want %q (golden snapshot)", name, actual, want)
		}
	}
}

// TestEveryConstSnapshotted asserts every const declared in codes.go
// has a snapshot row. Forces a developer adding a code to also commit
// the wire value into the lock — otherwise the next renamer ships
// undetected.
func TestEveryConstSnapshotted(t *testing.T) {
	t.Parallel()

	got := parseCodeConsts(t)

	for name := range got {
		if _, ok := goldenCodes[name]; !ok {
			t.Errorf("const %s is declared in codes.go but not in goldenCodes — add %q: %q to the snapshot", name, name, got[name])
		}
	}
}

// parseCodeConsts walks codes.go and returns every top-level
// `const X = "..."` declaration as name → value. Source-locating is
// done via runtime.Caller so the test works regardless of the test
// runner's working directory.
func parseCodeConsts(t *testing.T) map[string]string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate codes.go")
	}
	codesPath := filepath.Join(filepath.Dir(thisFile), "codes.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, codesPath, nil, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse %s: %v", codesPath, err)
	}

	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Values) == 0 {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			val, err := strconv.Unquote(lit.Value)
			if err != nil {
				continue
			}
			for _, name := range vs.Names {
				if name.IsExported() {
					out[name.Name] = val
				}
			}
		}
	}
	return out
}

// TestNoCollisionWithSDKOwnedCodes: pkg/sdk/module owns
// module.config_revision_stale (it cannot import this package to share a
// constant), so the whole "module." namespace is reserved to the SDK. A
// constant here that reused it would give two different failures one wire
// identity.
func TestNoCollisionWithSDKOwnedCodes(t *testing.T) {
	for name, value := range goldenCodes {
		if value == module.CodeConfigRevisionStale || strings.HasPrefix(value, "module.") {
			t.Errorf("%s = %q collides with the SDK-owned module.* namespace", name, value)
		}
	}
}
