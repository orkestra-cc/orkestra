package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/services"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

type fakeDecider struct {
	d   services.PasswordAuthDecision
	err error
}

func (f *fakeDecider) PasswordLoginDecision(context.Context, services.PolicyAudience) (services.PasswordAuthDecision, error) {
	return f.d, f.err
}

// newLoginChallenge mints a password-sourced login challenge for the given
// audience through the real in-memory challenge service, so the tests
// assert consumption against real challenge semantics rather than a stub.
func newLoginChallenge(t *testing.T, svc services.MFAChallengeService, audience string) *services.MFAChallenge {
	t.Helper()
	ch, err := svc.BeginLogin(context.Background(), services.LoginChallengeInput{
		UserUUID:    "u-1",
		SessionID:   "sid-1",
		SourceAMR:   []string{"pwd"},
		LoginMethod: "password",
		Audience:    audience,
	})
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	return ch
}

func newChallengeService(t *testing.T) services.MFAChallengeService {
	t.Helper()
	return services.NewMFAChallengeService(services.NewMemoryOAuthStateStore())
}

func TestRecheckPasswordChallenge(t *testing.T) {
	ctx := context.Background()

	t.Run("oauth-sourced challenge is untouched, decider never called", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"oauth"}, LoginMethod: "oauth", Audience: "operator",
		})
		bg, err := recheckPasswordChallenge(ctx, nil, svc, ch) // nil decider must not matter
		if err != nil || bg {
			t.Fatalf("oauth challenge must pass untouched, got (%v, %v)", bg, err)
		}
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("challenge must be retained")
		}
	})
	t.Run("allowed proceeds and reports the decision's break-glass", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		bg, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{Allowed: true, BreakGlassUsed: true}}, svc, ch)
		if err != nil || !bg {
			t.Fatalf("want rescued allow, got (%v, %v)", bg, err)
		}
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("allowed path must not consume — the one-winner Consume happens later")
		}
	})
	t.Run("disabled consumes atomically and maps 403", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "client")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{}}, svc, ch)
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("refused challenge must be consumed")
		}
	})
	t.Run("policy outage is 503 and RETAINS the challenge", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{err: services.ErrAuthPolicyUnavailable}, svc, ch)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("transient outage must retain the challenge for retry within its TTL")
		}
	})
	t.Run("nil decider on a password challenge is an outage, challenge retained", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		_, err := recheckPasswordChallenge(ctx, nil, svc, ch)
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("missing wiring must not consume")
		}
	})
	t.Run("empty audience is invalid and consumed", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{Allowed: true}}, svc, ch)
		assertStatusAndCode(t, err, 401, "")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("pre-v3 challenge must be consumed")
		}
	})
	t.Run("unknown audience is invalid and consumed", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "service")
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{Allowed: true}}, svc, ch)
		assertStatusAndCode(t, err, 401, "")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("unknown-audience challenge must be consumed")
		}
	})
	t.Run("pwd in SourceAMR alone marks the challenge password-sourced", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"pwd"}, LoginMethod: "", Audience: "operator",
		})
		_, err := recheckPasswordChallenge(ctx, &fakeDecider{d: services.PasswordAuthDecision{}}, svc, ch)
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
	})
}

type completionJWT struct{ services.JWTService }

func (completionJWT) RefreshTokenTTL() time.Duration { return time.Hour }

type completionMFA struct{ services.MFAService }

func (completionMFA) Verify(context.Context, string, string) error { return nil }

type completionWebAuthn struct{ services.WebAuthnService }

func (completionWebAuthn) FinishAssertion(context.Context, *iface.User, string, services.MFAChallengePurpose, []byte) error {
	return nil
}

type completionUsers struct{ iface.UserProvider }

func (completionUsers) GetUserByID(_ context.Context, id string) (*iface.User, error) {
	return &iface.User{UUID: id, Email: "u@example.com", IsActive: true, EmailVerified: true, Role: "operator"}, nil
}

type completionIssuer struct {
	mu         sync.Mutex
	issued     int
	breakGlass []string // audiences EmitBreakGlassUsed was called with
}

func (c *completionIssuer) IssueLoginTokensForSession(_ context.Context, user *iface.User, in services.LoginTokenContext, _ []string, _ int64) (*authModels.TokenResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.issued++
	return &authModels.TokenResponse{AccessToken: "at", TokenType: "Bearer", SessionID: in.SessionID, RefreshToken: "rt"}, nil
}

func (c *completionIssuer) EmitBreakGlassUsed(_ context.Context, audience, _, _, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.breakGlass = append(c.breakGlass, audience)
}

func newCompletionMFAHandler(t *testing.T, policy *services.AuthPolicyService, challenges services.MFAChallengeService, issuer *completionIssuer) *MFAHandler {
	t.Helper()
	h := NewMFAHandler(completionMFA{}, challenges, completionJWT{}, completionUsers{}, issuer, "cookie", "", false)
	h.SetPolicy(policy)
	return h
}

func TestLoginVerify_PasswordPolicyRecheck(t *testing.T) {
	ctx := context.Background()
	verifyReq := func(id string) *MFALoginVerifyRequest {
		r := &MFALoginVerifyRequest{}
		r.Body.ChallengeID = id
		r.Body.Code = "123456"
		return r
	}

	t.Run("post-flip refusal consumes and answers 403", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		issuer := &completionIssuer{}
		_, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("refused challenge must be consumed")
		}
		if issuer.issued != 0 || len(issuer.breakGlass) != 0 {
			t.Fatal("nothing may be minted or audited on a refusal")
		}
	})
	t.Run("missing policy wiring answers 503 and retains the challenge", func(t *testing.T) {
		// Built exactly like newCompletionMFAHandler but WITHOUT SetPolicy, so
		// h.policy is a nil *AuthPolicyService. decider() must hand the helper
		// a nil INTERFACE rather than a typed-nil one — a typed nil would sail
		// past the helper's nil check and call methods on it.
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		issuer := &completionIssuer{}
		h := NewMFAHandler(completionMFA{}, svc, completionJWT{}, completionUsers{}, issuer, "cookie", "", false)
		_, err := h.LoginVerify(ctx, verifyReq(ch.ID))
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("missing wiring must not consume the challenge")
		}
		if issuer.issued != 0 || len(issuer.breakGlass) != 0 {
			t.Fatal("nothing may be minted or audited without a policy")
		}
	})
	t.Run("transient policy error answers 503 and retains the challenge", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTestErr(errors.New("mongo down"))
		issuer := &completionIssuer{}
		_, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		assertStatusAndCode(t, err, 503, "auth.policy_unavailable")
		if _, perr := svc.Peek(ctx, ch.ID); perr != nil {
			t.Fatal("challenge must survive a transient outage for retry within its TTL")
		}
	})
	t.Run("oauth-sourced challenge completes under a password-off policy", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"oauth"}, LoginMethod: "oauth", Audience: "operator",
		})
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		issuer := &completionIssuer{}
		resp, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("oauth challenge must be unaffected, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 0 {
			t.Fatal("no rescue happened; nothing to audit")
		}
	})
	t.Run("break-glass permits the completion and audits exactly once", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		policy.SetOperatorBreakGlass(true)
		issuer := &completionIssuer{}
		resp, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("rescued completion must succeed, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 1 || issuer.breakGlass[0] != "operator" {
			t.Fatalf("want exactly one operator rescue event, got %v", issuer.breakGlass)
		}
	})
	t.Run("challenge minted WITH BreakGlassUsed audits even when completion reads true", func(t *testing.T) {
		svc := newChallengeService(t)
		ch, _ := svc.BeginLogin(ctx, services.LoginChallengeInput{
			UserUUID: "u-1", SessionID: "sid-1", SourceAMR: []string{"pwd"}, LoginMethod: "password",
			Audience: "operator", BreakGlassUsed: true,
		})
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "true"})
		issuer := &completionIssuer{}
		resp, err := newCompletionMFAHandler(t, policy, svc, issuer).LoginVerify(ctx, verifyReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("completion must succeed, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 1 {
			t.Fatalf("the initial check's rescue must still be audited once, got %v", issuer.breakGlass)
		}
	})
}

func TestWebAuthnLoginFinish_PasswordPolicyRecheck(t *testing.T) {
	ctx := context.Background()
	finishReq := func(id string) *webAuthnLoginFinishRequest {
		r := &webAuthnLoginFinishRequest{}
		r.Body.LoginChallengeID = id
		r.Body.WebAuthnChallengeID = "wa-1"
		r.Body.AssertionResponse = json.RawMessage(`{}`)
		return r
	}
	newHandler := func(policy *services.AuthPolicyService, challenges services.MFAChallengeService, issuer *completionIssuer) *WebAuthnHandler {
		h := NewWebAuthnHandler(completionWebAuthn{}, challenges, completionJWT{}, completionUsers{}, issuer, "cookie", "", false)
		h.SetPolicy(policy)
		return h
	}

	t.Run("post-flip refusal consumes and answers 403", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "client")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledClient": "false"})
		issuer := &completionIssuer{}
		_, err := newHandler(policy, svc, issuer).LoginFinish(ctx, finishReq(ch.ID))
		assertStatusAndCode(t, err, 403, "auth.password_login_disabled")
		if _, perr := svc.Peek(ctx, ch.ID); perr == nil {
			t.Fatal("refused challenge must be consumed")
		}
	})
	t.Run("break-glass rescue audits exactly once", func(t *testing.T) {
		svc := newChallengeService(t)
		ch := newLoginChallenge(t, svc, "operator")
		policy := services.NewAuthPolicyServiceForTest(map[string]string{"passwordLoginEnabledAdmin": "false"})
		policy.SetOperatorBreakGlass(true)
		issuer := &completionIssuer{}
		resp, err := newHandler(policy, svc, issuer).LoginFinish(ctx, finishReq(ch.ID))
		if err != nil || !resp.Body.Success {
			t.Fatalf("rescued completion must succeed, got (%v, %v)", resp, err)
		}
		if len(issuer.breakGlass) != 1 {
			t.Fatalf("want exactly one rescue event, got %v", issuer.breakGlass)
		}
	})
}
