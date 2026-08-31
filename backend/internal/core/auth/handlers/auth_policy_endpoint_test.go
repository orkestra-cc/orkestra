package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

// policyHandlerFor builds the minimal AuthHandler surface GetAuthPolicy
// touches: the policy reader plus the tier string. Every other field
// stays zero — the endpooint reads nothing else.
func policyHandlerFor(t *testing.T, p *services.AuthPolicyService, tier string) *AuthHandler {
	t.Helper()
	h := &AuthHandler{}
	h.SetPolicy(p)
	h.SetTier(tier)
	return h
}

func TestGetAuthPolicy_PasswordLoginFields(t *testing.T) {
	ctx := context.Background()

	t.Run("persisted true is non-null, flag false even with the env set", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"})
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("GetAuthPolicy: %v", err)
		}
		if resp.Body.PasswordLoginEnabled == nil || !*resp.Body.PasswordLoginEnabled {
			t.Fatalf("want non-null true, got %v", resp.Body.PasswordLoginEnabled)
		}
		if resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("a stored true needs no override; flag must be false")
		}
	})
	t.Run("persisted false + operator override → non-null false, flag true", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("GetAuthPolicy: %v", err)
		}
		if resp.Body.PasswordLoginEnabled == nil || *resp.Body.PasswordLoginEnabled {
			t.Fatalf("want non-null false, got %v", resp.Body.PasswordLoginEnabled)
		}
		if !resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("override over a stored false must flag the emergency form")
		}
	})
	t.Run("read error without override → 503", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		_, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
	})
	t.Run("read error + operator override → 200 null + flag", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceOperator).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("emergency read must answer 200, got %v", err)
		}
		if resp.Body.PasswordLoginEnabled != nil {
			t.Fatalf("unknown persisted state must be null, got %v", *resp.Body.PasswordLoginEnabled)
		}
		if !resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("the emergency form must be reachable")
		}
	})
	t.Run("client endpoint never exposes the override", func(t *testing.T) {
		p := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledClient": "false"})
		p.SetOperatorBreakGlass(true)
		resp, err := policyHandlerFor(t, p, services.AudienceClient).GetAuthPolicy(ctx, nil)
		if err != nil {
			t.Fatalf("GetAuthPolicy: %v", err)
		}
		if resp.Body.PasswordLoginEnabled == nil || *resp.Body.PasswordLoginEnabled {
			t.Fatalf("want non-null false, got %v", resp.Body.PasswordLoginEnabled)
		}
		if resp.Body.PasswordLoginBreakGlassEffective {
			t.Fatal("client endpoint must never flag the override")
		}
		pErr := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		pErr.SetOperatorBreakGlass(true)
		_, err = policyHandlerFor(t, pErr, services.AudienceClient).GetAuthPolicy(ctx, nil)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
	})
}
