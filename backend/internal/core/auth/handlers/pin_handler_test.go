package handlers

import (
	"context"
	"fmt"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Every PIN sentinel has exactly one wire identity.
func TestMapPINError_StatusAndCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{services.ErrPINLoginDisabled, 403, "auth.pin_login_disabled"},
		{services.ErrPINInvalid, 422, "auth.pin_invalid"},
		{services.ErrPINTooWeak, 422, "auth.pin_too_weak"},
		{services.ErrPINUsernameRequired, 409, "auth.pin_username_required"},
		{services.ErrPINUsernameNotUnique, 409, "auth.pin_username_not_unique"},
		{services.ErrPINNotAllowed, 409, "auth.pin_not_allowed"},
		{services.ErrPINNotSet, 409, "auth.pin_not_set"},
		{services.ErrPINDeviceNotFound, 404, "auth.pin_device_not_found"},
		{services.ErrPINSessionForbidden, 403, "auth.pin_session_forbidden"},
		{services.ErrPINTargetAboveCaller, 403, "user.target_tier_forbidden"},
		{services.ErrInvalidCredentials, 401, "auth.invalid_credentials"},
		{services.ErrLoginDisabled, 403, "auth.login_disabled"},
		{services.LockedAfter(0), 429, "auth.too_many_attempts"},
		{fmt.Errorf("%w: x", services.ErrAuthPolicyUnavailable), 503, "auth.policy_unavailable"},
		{iface.ErrUserNotFound, 404, ""},
	}
	for _, c := range cases {
		assertStatusAndCode(t, mapPINError(c.err), c.status, c.code)
	}
}

// An unwired PIN service (a fork's user provider without
// iface.PINCredentialStore) answers the disabled code, never a 500.
func TestPINLogin_UnwiredServiceIsDisabled(t *testing.T) {
	h := NewPINHandler(services.NewPINAuthService(services.PINAuthConfig{
		Policy: services.NewAuthPolicyServiceForTest(map[string]string{"pinLoginEnabledAdmin": "true"}),
	}), "rt", "", true)
	req := &PINLoginRequest{}
	req.Body.Username, req.Body.PIN = "mario", "48213"
	_, err := h.Login(context.Background(), req)
	assertStatusAndCode(t, err, 403, "auth.pin_login_disabled")
}

func TestGetAuthPolicy_PINFields(t *testing.T) {
	ctx := context.Background()
	resp, err := policyHandlerFor(t, services.NewAuthPolicyServiceForTest(map[string]string{}), services.AudienceOperator).GetAuthPolicy(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body.PINLoginEnabled || resp.Body.PINLength != services.DefaultPINLength || !resp.Body.PINRequiresTrustedDevice {
		t.Fatalf("defaults: %+v", resp.Body)
	}
	resp, err = policyHandlerFor(t, services.NewAuthPolicyServiceForTest(map[string]string{
		"pinLoginEnabledClient": "true", "pinLength": "4", "pinRequiresTrustedDevice": "false",
	}), services.AudienceClient).GetAuthPolicy(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Body.PINLoginEnabled || resp.Body.PINLength != 4 || resp.Body.PINRequiresTrustedDevice {
		t.Fatalf("configured: %+v", resp.Body)
	}
	// A malformed PIN toggle hides the keypad; it does not fail the read
	// the password form depends on.
	resp, err = policyHandlerFor(t, services.NewAuthPolicyServiceForTest(map[string]string{"pinLoginEnabledAdmin": "maybe"}), services.AudienceOperator).GetAuthPolicy(ctx, nil)
	if err != nil || resp.Body.PINLoginEnabled {
		t.Fatalf("malformed toggle: resp=%+v err=%v", resp, err)
	}
}
