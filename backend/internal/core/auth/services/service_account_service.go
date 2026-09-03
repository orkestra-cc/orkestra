package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var (
	ErrInvalidAccountName       = errors.New("service account name yields an empty slug")
	ErrAccountAlreadyExists     = errors.New("service account already exists")
	ErrServiceAccountNotFound   = errors.New("service account not found")
	ErrTooManyActiveCredentials = errors.New("service account already has two active credentials")
	// ErrServiceAccountLookupUnavailable signals that the service-account
	// DIRECTORY could not be read, so the gate could not tell whether the
	// target exists at all. It is ErrRefreshLookupUnavailable's sibling one
	// module over (spec §4.9's class, §8 #17): an infrastructure failure
	// reported as ErrServiceAccountNotFound tells an operator their account
	// was deleted, which is a verdict the platform never actually reached.
	// The one site that returns it wraps BOTH this sentinel and the cause
	// (`fmt.Errorf("...: %w: %w", ErrServiceAccountLookupUnavailable, err)`),
	// so errors.Is classifies while the cause survives into the log.
	// Translated to 503 service_account_lookup_unavailable at the handler.
	ErrServiceAccountLookupUnavailable = errors.New("service account lookup unavailable")
)

// Grant sentinels (Task 7). ErrInvalidClientCredentials is deliberately
// used for every rejection reason (unknown clientId, revoked credential,
// wrong secret, disabled account, non-service-account user) so a caller
// can never distinguish which one applied from the error alone.
//
// ErrClientRateLimited has no producer left (spec §4.1 D7): Grant's own
// lockout branch always has a live Verdict by the time it can answer —
// a counter-store error must fail OPEN, so that path falls through to
// the credential check instead of returning anything — and so it
// answers LockedAfter(v.RetryAfter) instead, which errors.Is-matches
// ErrAccountLocked, the same sentinel the password-login lockout uses.
// The sentinel and its handler arm (mapServiceTokenError,
// service_token_handler.go) are kept only for tolerance — the shared
// limiter's auth-facing surface is gone (H-1, Task 11) — not because
// anything still produces it. ErrClientRateLimited and ErrAccountLocked are
// deliberately NOT unified into a single errors.Is relationship even
// though both map to the same 429.
var (
	ErrUnsupportedGrantType     = errors.New("unsupported grant type")
	ErrInvalidClientCredentials = errors.New("invalid client credentials")
	ErrClientRateLimited        = errors.New("client credential attempts rate limited")
)

// ServiceTokenMinter is the token-minting seam Task 7's Grant flow
// consumes. Satisfied structurally by *jwtService — no import needed
// here, matching the auth package's existing seam pattern (see
// FirstAdminClaimer in password_auth_service.go).
type ServiceTokenMinter interface {
	GenerateAccessToken(user *iface.User) (string, error)
	AccessTokenTTL(ctx context.Context) time.Duration
}

// ServiceAccountService owns the account + credential lifecycle for
// machine principals (service accounts): create the backing user row
// and its first credential, list/get/update accounts, and issue/revoke
// credentials under a max-two-active rotation cap. Token issuance
// (Grant) is Task 7 and extends this same type.
type ServiceAccountService struct {
	creds  repository.ServiceAccountCredentialRepository
	users  iface.UserProvider
	lister iface.ServiceAccountLister
	hasher PasswordService
	minter ServiceTokenMinter
	// counter backs Grant's lockout check (spec §4.1 D7 — service
	// accounts share the same Redis attempt counters as the password
	// login and reset/verify flows). Not consulted by any lifecycle
	// (CRUD) method in this file.
	counter AttemptCounter
	// securityEventRepo persists the auth_security_events audit log for
	// account mutations and failed grants — the same collection
	// authService.recordAuthEvent (auth_service.go) writes to. Optional
	// (nil-safe); wired post-construction via SetSecurityEventRepo,
	// mirroring how module.go threads the shared instance into the
	// sibling AdminUserAuthHandler.
	securityEventRepo repository.SecurityEventRepository
	// auditSink mirrors the compliance audit-log lane. Optional
	// (nil-safe); wired post-construction via SetAuditSink, satisfying
	// iface.AuditSinkSetter the same way authService and
	// PasswordAuthService do — see recordEvent for the mechanism this
	// type mirrors exactly. Nothing wires this today, matching
	// authService's own current (unwired) state.
	auditSink iface.AuditSink
	// policy is the live admin-managed auth policy. Optional (nil-safe);
	// wired post-construction via SetPolicy so Grant's lockout check
	// reflects live threshold/duration edits, mirroring the login site
	// (password_auth_service.go Login, ~:477-482).
	policy *AuthPolicyService
}

// NewServiceAccountService wires the lifecycle service.
func NewServiceAccountService(
	creds repository.ServiceAccountCredentialRepository,
	users iface.UserProvider,
	lister iface.ServiceAccountLister,
	hasher PasswordService,
	minter ServiceTokenMinter,
	counter AttemptCounter,
) *ServiceAccountService {
	return &ServiceAccountService{
		creds:   creds,
		users:   users,
		lister:  lister,
		hasher:  hasher,
		minter:  minter,
		counter: counter,
	}
}

// SetSecurityEventRepo wires the auth_security_events persistence lane
// (optional; nil-safe). See the field doc for the mechanism this
// mirrors.
func (s *ServiceAccountService) SetSecurityEventRepo(repo repository.SecurityEventRepository) {
	s.securityEventRepo = repo
}

// SetAuditSink wires the compliance audit sink post-construction.
// Satisfies iface.AuditSinkSetter (optional; nil-safe).
func (s *ServiceAccountService) SetAuditSink(sink iface.AuditSink) {
	s.auditSink = sink
}

// SetPolicy wires the live AuthPolicyService (optional; nil-safe). See
// the field doc for what this unlocks in Grant.
func (s *ServiceAccountService) SetPolicy(policy *AuthPolicyService) {
	s.policy = policy
}

// CredentialView is the credential shape safe to return on read paths —
// it never carries the plaintext secret.
type CredentialView struct {
	ID         string     `json:"id"`
	ClientID   string     `json:"clientId"`
	Label      string     `json:"label"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

// CredentialWithSecret is returned exactly once, at issue/rotation time.
// The plaintext secret is never persisted, logged, or recoverable after
// this response leaves the service.
type CredentialWithSecret struct {
	CredentialView
	ClientSecret string `json:"clientSecret"`
}

// AccountView is the list/summary shape for a service account.
type AccountView struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Email             string    `json:"email"`
	IsActive          bool      `json:"isActive"`
	ActiveCredentials int64     `json:"activeCredentials"`
	CreatedAt         time.Time `json:"createdAt"`
}

// AccountDetail is an AccountView plus its full credential history
// (active and revoked).
type AccountDetail struct {
	AccountView
	Credentials []CredentialView `json:"credentials"`
}

// AccountWithSecret is returned exactly once, at creation time — the new
// account plus its first ("initial") credential's plaintext secret.
type AccountWithSecret struct {
	AccountView
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

// slugRe collapses any run of non a-z0-9 characters into a single
// hyphen; slugify then trims leading/trailing hyphens and caps length.
var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// newClientID mints a public credential identifier: "sa_" + 12 random
// bytes, hex-encoded. Not secret — safe to log, display, and use as a
// lookup key.
func newClientID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sa_" + hex.EncodeToString(b), nil
}

// newClientSecret mints the plaintext machine secret: "sas_" + 32
// random bytes, base64url-encoded (no padding). Callers must hash it
// before persisting and must never log the plaintext.
func newClientSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sas_" + base64.RawURLEncoding.EncodeToString(b), nil
}

// mintCredential generates a fresh clientId/secret pair, hashes the
// secret, and persists the credential row. The plaintext secret is
// returned to the immediate caller only — it is not retained anywhere
// on this service.
func (s *ServiceAccountService) mintCredential(ctx context.Context, userUUID, label string) (CredentialWithSecret, error) {
	clientID, err := newClientID()
	if err != nil {
		return CredentialWithSecret{}, err
	}
	secret, err := newClientSecret()
	if err != nil {
		return CredentialWithSecret{}, err
	}
	hash, err := s.hasher.Hash(secret)
	if err != nil {
		return CredentialWithSecret{}, err
	}

	cred := &models.ServiceAccountCredential{
		UUID:       iface.GenerateUUIDv7(),
		UserUUID:   userUUID,
		ClientID:   clientID,
		SecretHash: hash,
		Label:      label,
		CreatedAt:  time.Now(),
	}
	if err := s.creds.Create(ctx, cred); err != nil {
		return CredentialWithSecret{}, err
	}

	return CredentialWithSecret{
		CredentialView: CredentialView{
			ID:        cred.UUID,
			ClientID:  cred.ClientID,
			Label:     cred.Label,
			CreatedAt: cred.CreatedAt,
		},
		ClientSecret: secret,
	}, nil
}

// requireServiceAccount loads the user and confirms it is a machine
// principal. Every lifecycle method below gates on this first so a
// human user's UUID can never be targeted by these endpoints.
//
// It CLASSIFIES the lookup rather than collapsing it (spec §8 #17). Only a
// conforming UserProvider's not-found — iface.ErrUserNotFound, which
// user/services.ErrUserNotFound aliases — is a verdict; anything else is the
// directory failing, and answering that with a 404 tells an operator their
// service account was deleted. The user == nil test is written out rather
// than left to ||'s short-circuit, because splitting the error arm off moves
// the user.Kind dereference into a statement of its own.
func (s *ServiceAccountService) requireServiceAccount(ctx context.Context, userID string) (*iface.User, error) {
	user, err := s.users.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, iface.ErrUserNotFound) {
			return nil, ErrServiceAccountNotFound
		}
		return nil, fmt.Errorf("service account lookup failed: %w: %w",
			ErrServiceAccountLookupUnavailable, err)
	}
	if user == nil || user.Kind != iface.UserKindService {
		return nil, ErrServiceAccountNotFound
	}
	return user, nil
}

// recordEvent emits the module's three-lane audit signal for a
// service-account mutation, mirroring authService.recordAuthEvent
// (auth_service.go) EXACTLY: a structured slog line (always), a
// persisted auth_security_events row via securityEventRepo (when
// wired), and a mirrored row in the compliance audit log via auditSink
// (when wired). targetUUID is the affected account's user UUID;
// actorUUID is the operator who performed the action (read from ctx by
// the caller via ctxauth.GetUserUUID). Metadata must never carry secret
// material (client secrets, credential hashes) — enforced by
// convention at each call site, not here.
func (s *ServiceAccountService) recordEvent(ctx context.Context, eventType, actorUUID, targetUUID string, metadata map[string]interface{}) {
	if targetUUID == "" || eventType == "" {
		return
	}
	ip, _ := ctxauth.GetClientIP(ctx)

	args := []any{"event", eventType, "targetUUID", targetUUID}
	if actorUUID != "" {
		args = append(args, "actorUUID", actorUUID)
	}
	if ip != "" {
		args = append(args, "ip", ip)
	}
	for k, v := range metadata {
		args = append(args, k, v)
	}
	slog.InfoContext(ctx, "auth_security_event", args...)

	if s.securityEventRepo != nil {
		md := make(map[string]interface{}, len(metadata)+1)
		for k, v := range metadata {
			md[k] = v
		}
		if actorUUID != "" {
			md["actorUUID"] = actorUUID
		}
		event := &models.SecurityEvent{
			UserUUID:  targetUUID,
			EventType: eventType,
			IPAddress: ip,
			Success:   true,
			Metadata:  md,
			Timestamp: time.Now().UTC(),
		}
		if err := s.securityEventRepo.Insert(ctx, event); err != nil {
			slog.WarnContext(ctx, "auth_security_event persist failed",
				"eventType", eventType, "targetUUID", targetUUID, "error", err.Error())
		}
	}

	if s.auditSink != nil {
		actor := actorUUID
		if actor == "" {
			actor = targetUUID
		}
		s.auditSink.Emit(ctx, iface.AuditEvent{
			ActorUserID:  actor,
			ActorType:    "user",
			Action:       eventType,
			ResourceType: "service_account",
			ResourceID:   targetUUID,
			Outcome:      "success",
			IPAddress:    ip,
			Metadata:     metadata,
		})
	}
}

// emitGrantFailed logs a failed client-credentials grant attempt via
// the compliance audit sink only, mirroring
// PasswordAuthService.emitLoginFailed's shape exactly: no persisted
// auth_security_events row, no unconditional slog line — a rejected
// grant is a routine, high-frequency occurrence, not something that
// needs its own durable audit-log entry the way a successful account
// mutation does. reason carries the internal rejection detail (e.g.
// "unknown_client", "bad_secret", "disabled_account") for operators
// reading the compliance log; the HTTP response to the caller stays the
// single indistinguishable 401 regardless of reason. clientId is safe
// to log (see newClientID's doc comment) — the secret never is, and
// never appears here.
func (s *ServiceAccountService) emitGrantFailed(ctx context.Context, clientID, userUUID, ip, reason string) {
	if s.auditSink == nil {
		return
	}
	actorType := "anonymous"
	if userUUID != "" {
		actorType = "user"
	}
	s.auditSink.Emit(ctx, iface.AuditEvent{
		ActorUserID: userUUID,
		ActorType:   actorType,
		Action:      "service_account.grant_failed",
		Outcome:     "failure",
		IPAddress:   ip,
		Metadata:    map[string]any{"reason": reason, "clientId": clientID},
	})
}

// CreateAccount mints a new service-account user row plus its first
// ("initial") credential. The account's email is derived deterministically
// from the slugified name (sa-<slug>@service.invalid) — this is a
// synthetic, unreachable address; service accounts never receive mail.
// The plaintext secret is returned only in the response value — never
// logged, never persisted, and never recoverable afterward. The service
// never calls PasswordService.ValidatePolicy here: that enforces
// human-password rules and does not apply to a generated machine secret.
func (s *ServiceAccountService) CreateAccount(ctx context.Context, name string) (*AccountWithSecret, error) {
	slug := slugify(name)
	if slug == "" {
		return nil, ErrInvalidAccountName
	}
	email := fmt.Sprintf("sa-%s@service.invalid", slug)

	// Existence pre-check. The unique email index backs the actual race;
	// this just gives a friendlier error on the common path.
	if _, err := s.users.GetUserForAuth(ctx, email); err == nil {
		return nil, ErrAccountAlreadyExists
	}

	user, err := s.users.CreateUserWithPassword(ctx, &iface.CreateUserInput{
		Email:    email,
		FullName: name,
		Role:     "guest",
		Kind:     iface.UserKindService,
		// PasswordHash intentionally left empty — service accounts never
		// authenticate with a password. Login already refuses both
		// no_password and service_principal accounts.
	})
	if err != nil {
		// Create-race: a concurrent CreateAccount call (or a legitimate
		// signup that happens to collide with the synthetic email) may
		// have won between the existence pre-check above and this
		// insert. Re-check by identity — not by inspecting the error
		// type or a repository-level sentinel, which would couple this
		// service to the user module's internals — so a duplicate-key
		// failure surfaces the same 409 a caller would get from a
		// straightforward duplicate CreateAccount call.
		if _, findErr := s.users.GetUserForAuth(ctx, email); findErr == nil {
			return nil, ErrAccountAlreadyExists
		}
		return nil, err
	}

	// Orphan risk: if credential minting below fails after the user row
	// above has already committed, this call still returns an error, but
	// the service-account user row remains — a zero-credential orphan.
	// CreateAccount is not retryable at that point: the email is now
	// taken, so a retry hits the existence pre-check above and 409s via
	// ErrAccountAlreadyExists. Recovery is an explicit
	// IssueCredential(ctx, user.UUID, ...) call against the orphaned
	// account once the underlying mint failure (e.g. a transient Mongo
	// error) is resolved. Accepted Phase A tradeoff — mirrors the
	// best-effort rotation-cap comment on IssueCredential below.
	cred, err := s.mintCredential(ctx, user.UUID, "initial")
	if err != nil {
		return nil, err
	}

	actorUUID, _ := ctxauth.GetUserUUID(ctx)
	s.recordEvent(ctx, "service_account.created", actorUUID, user.UUID, map[string]interface{}{
		"name":  user.FullName,
		"email": user.Email,
	})

	return &AccountWithSecret{
		AccountView: AccountView{
			ID:                user.UUID,
			Name:              user.FullName,
			Email:             user.Email,
			IsActive:          user.IsActive,
			ActiveCredentials: 1,
			CreatedAt:         user.CreatedAt,
		},
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
	}, nil
}

// ListAccounts returns every service account, newest lister order
// preserved, with each row's live active-credential count.
func (s *ServiceAccountService) ListAccounts(ctx context.Context) ([]AccountView, error) {
	users, err := s.lister.ListUsersByKind(ctx, iface.UserKindService)
	if err != nil {
		return nil, err
	}

	out := make([]AccountView, 0, len(users))
	for _, u := range users {
		active, err := s.creds.CountActiveByUser(ctx, u.UUID)
		if err != nil {
			return nil, err
		}
		out = append(out, AccountView{
			ID:                u.UUID,
			Name:              u.FullName,
			Email:             u.Email,
			IsActive:          u.IsActive,
			ActiveCredentials: active,
			CreatedAt:         u.CreatedAt,
		})
	}
	return out, nil
}

// GetAccount returns one service account plus its full credential
// history (active and revoked).
func (s *ServiceAccountService) GetAccount(ctx context.Context, userID string) (*AccountDetail, error) {
	user, err := s.requireServiceAccount(ctx, userID)
	if err != nil {
		return nil, err
	}

	rows, err := s.creds.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	views := make([]CredentialView, 0, len(rows))
	var active int64
	for _, c := range rows {
		views = append(views, CredentialView{
			ID:         c.UUID,
			ClientID:   c.ClientID,
			Label:      c.Label,
			CreatedAt:  c.CreatedAt,
			LastUsedAt: c.LastUsedAt,
			RevokedAt:  c.RevokedAt,
		})
		if c.RevokedAt == nil {
			active++
		}
	}

	return &AccountDetail{
		AccountView: AccountView{
			ID:                user.UUID,
			Name:              user.FullName,
			Email:             user.Email,
			IsActive:          user.IsActive,
			ActiveCredentials: active,
			CreatedAt:         user.CreatedAt,
		},
		Credentials: views,
	}, nil
}

// UpdateAccount patches name and/or active state. Only non-nil fields
// are applied — a nil pointer leaves the corresponding value untouched.
//
// Rename never re-derives the synthetic email: sa-<slug>@service.invalid
// is fixed at CreateAccount time from the ORIGINAL name and is not
// recomputed here, so renaming "Reporting Bot" to "Nightly Reports" does
// not touch its sa-reporting-bot@service.invalid address. This is
// documented Phase A behaviour, not an oversight — a later CreateAccount
// call reusing the old name still collides on that email and 409s via
// ErrAccountAlreadyExists.
func (s *ServiceAccountService) UpdateAccount(ctx context.Context, userID string, name *string, active *bool) (*AccountView, error) {
	if _, err := s.requireServiceAccount(ctx, userID); err != nil {
		return nil, err
	}

	in := &iface.UpdateUserInput{}
	if name != nil {
		in.FullName = *name
	}
	if active != nil {
		in.IsActive = active
	}

	resp, err := s.users.UpdateUser(ctx, userID, in)
	if err != nil {
		return nil, err
	}

	activeCredentials, err := s.creds.CountActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Metadata records which fields the caller targeted and makes an
	// active-state transition explicit, rather than requiring a reader
	// to diff before/after account rows.
	changed := make([]string, 0, 2)
	metadata := map[string]interface{}{}
	if name != nil {
		changed = append(changed, "name")
	}
	if active != nil {
		changed = append(changed, "active")
		metadata["active"] = *active
	}
	metadata["changed"] = changed
	actorUUID, _ := ctxauth.GetUserUUID(ctx)
	s.recordEvent(ctx, "service_account.updated", actorUUID, userID, metadata)

	return &AccountView{
		ID:                resp.ID,
		Name:              resp.FullName,
		Email:             resp.Email,
		IsActive:          resp.IsActive,
		ActiveCredentials: activeCredentials,
		CreatedAt:         resp.CreatedAt,
	}, nil
}

// IssueCredential mints a new credential for an existing service
// account, enforcing the max-two-active rotation cap. An empty label
// defaults to "rotated" (the expected caller is a rotation flow;
// CreateAccount's first credential is separately labeled "initial").
// The cap is best-effort operational policy, not an atomic guarantee —
// see the comment on the count-then-insert check below.
func (s *ServiceAccountService) IssueCredential(ctx context.Context, userID, label string) (*CredentialWithSecret, error) {
	if _, err := s.requireServiceAccount(ctx, userID); err != nil {
		return nil, err
	}

	// Best-effort cap: this count-then-insert is not atomic, so two
	// concurrent IssueCredential calls against the same account can both
	// read active<2, both pass, and briefly leave 3+ active credentials.
	// That's an accepted tradeoff for Phase A, not an oversight — this is
	// admin-only operational policy to keep rotation windows tidy, not a
	// security invariant (credentials are individually revocable, and an
	// over-cap moment does not grant any access a correctly-capped account
	// wouldn't already have). Hard enforcement would need repository-level
	// support (e.g. an atomic conditional insert or a unique partial
	// index on active-credential count), which is out of scope here.
	active, err := s.creds.CountActiveByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if active >= 2 {
		return nil, ErrTooManyActiveCredentials
	}

	if label == "" {
		label = "rotated"
	}

	cred, err := s.mintCredential(ctx, userID, label)
	if err != nil {
		return nil, err
	}

	actorUUID, _ := ctxauth.GetUserUUID(ctx)
	s.recordEvent(ctx, "service_account.credential_issued", actorUUID, userID, map[string]interface{}{
		"credentialId": cred.ID,
		"clientId":     cred.ClientID,
		"label":        cred.Label,
	})

	return &cred, nil
}

// RevokeCredential revokes one of the account's credentials. Ownership
// is checked by scanning the account's own credential list for the
// target UUID — a credential belonging to a different account is
// indistinguishable from an unknown one, so a cross-account attempt
// returns the same not-found sentinel as a bogus ID (no cross-account
// revocation oracle).
//
// Design note: repository.Revoke is not idempotent — revoking an
// already-revoked credential returns repository.ErrServiceAccountCredentialNotFound
// (Task 5's ListByUser/GetByClientID surface revoked rows, but Revoke's
// underlying update only matches rows with no revokedAt yet). This
// method deliberately does not special-case that: the ownership scan
// above finds the (already-revoked) row and passes it through to
// Revoke, so a double-revoke surfaces the same not-found sentinel a
// caller would see from a genuinely unknown ID. That is acceptable —
// the end state ("this credential is not currently active under my
// account") is the same either way, and callers already have to handle
// the not-found case.
func (s *ServiceAccountService) RevokeCredential(ctx context.Context, userID, credentialID string) error {
	if _, err := s.requireServiceAccount(ctx, userID); err != nil {
		return err
	}

	rows, err := s.creds.ListByUser(ctx, userID)
	if err != nil {
		return err
	}

	owned := false
	for _, c := range rows {
		if c.UUID == credentialID {
			owned = true
			break
		}
	}
	if !owned {
		return repository.ErrServiceAccountCredentialNotFound
	}

	if err := s.creds.Revoke(ctx, credentialID, time.Now()); err != nil {
		return err
	}

	actorUUID, _ := ctxauth.GetUserUUID(ctx)
	s.recordEvent(ctx, "service_account.credential_revoked", actorUUID, userID, map[string]interface{}{
		"credentialId": credentialID,
	})
	return nil
}

// GrantInput is the client-credentials exchange request: a clientId +
// clientSecret pair, the declared grant type, and the caller's IP (used
// as one of the two rate-limit buckets).
type GrantInput struct {
	GrantType    string
	ClientID     string
	ClientSecret string
	IP           string
}

// GrantResult is the minted access token plus its lifetime in seconds.
// There is no refresh token — a service account re-authenticates with
// its clientId/clientSecret when the access token expires.
type GrantResult struct {
	AccessToken string
	ExpiresIn   int
}

// Grant exchanges a service account's clientId+clientSecret for a
// short-lived access token (OAuth2 client-credentials grant). Every
// rejection reason collapses to ErrInvalidClientCredentials — the
// caller cannot distinguish an unknown clientId from a wrong secret
// from a disabled account. An unknown or revoked clientId still burns a
// dummy Verify call against s.hasher.DummyHash() so that path costs the
// same wall-clock time as a wrong-secret rejection (timing parity).
// Every rejection branch also calls emitGrantFailed with the internal
// reason (unknown_client, revoked_credential, bad_secret,
// user_lookup_failed, not_service_account, disabled_account) — that
// granularity lands only in the compliance audit log, never in the HTTP
// response above. A successful grant does not emit a security event
// (routine, high-frequency — mirrors the login site's judgment on
// successful logins).
func (s *ServiceAccountService) Grant(ctx context.Context, in GrantInput) (*GrantResult, error) {
	if in.GrantType != "client_credentials" {
		return nil, ErrUnsupportedGrantType
	}
	// The lockout pre-check PEEKS. The limiter this replaces had to
	// expose a separate IsLockedOut for exactly this reason: its Check
	// consumed a token on every call, so gating every Grant on it let
	// back-to-back legitimate calls lock themselves out. Peeks are free
	// by construction now.
	if s.counter != nil {
		accountLim := Limit{Threshold: s.policy.LockoutThreshold(ctx), Window: s.policy.LockoutDuration(ctx)}
		addressLim := Limit{Threshold: s.policy.IPLockoutThreshold(ctx), Window: s.policy.IPLockoutDuration(ctx)}
		if v, err := s.counter.Locked(ctx, AttemptKeyIP(in.IP), addressLim); err == nil && v.Locked {
			return nil, LockedAfter(v.RetryAfter)
		}
		if v, err := s.counter.Locked(ctx, AttemptKeyClient(in.ClientID), accountLim); err == nil && v.Locked {
			return nil, LockedAfter(v.RetryAfter)
		}
	}
	cred, err := s.creds.GetByClientID(ctx, in.ClientID)
	if err != nil {
		// Constant-shape failure: burn a verify so unknown clientIds cost
		// the same as wrong secrets.
		_, _ = s.hasher.Verify(in.ClientSecret, s.hasher.DummyHash())
		s.recordFailed(ctx, in)
		s.emitGrantFailed(ctx, in.ClientID, "", in.IP, "unknown_client")
		return nil, ErrInvalidClientCredentials
	}
	if cred.RevokedAt != nil {
		// Same constant-shape treatment as an unknown clientId.
		_, _ = s.hasher.Verify(in.ClientSecret, s.hasher.DummyHash())
		s.recordFailed(ctx, in)
		s.emitGrantFailed(ctx, in.ClientID, cred.UserUUID, in.IP, "revoked_credential")
		return nil, ErrInvalidClientCredentials
	}
	ok, err := s.hasher.Verify(in.ClientSecret, cred.SecretHash)
	if err != nil || !ok {
		s.recordFailed(ctx, in)
		s.emitGrantFailed(ctx, in.ClientID, cred.UserUUID, in.IP, "bad_secret")
		return nil, ErrInvalidClientCredentials
	}
	user, err := s.users.GetUserByID(ctx, cred.UserUUID)
	if err != nil {
		s.recordFailed(ctx, in)
		s.emitGrantFailed(ctx, in.ClientID, cred.UserUUID, in.IP, "user_lookup_failed")
		return nil, ErrInvalidClientCredentials
	}
	if user.Kind != iface.UserKindService {
		s.recordFailed(ctx, in)
		s.emitGrantFailed(ctx, in.ClientID, user.UUID, in.IP, "not_service_account")
		return nil, ErrInvalidClientCredentials
	}
	if !user.IsActive {
		s.recordFailed(ctx, in)
		s.emitGrantFailed(ctx, in.ClientID, user.UUID, in.IP, "disabled_account")
		return nil, ErrInvalidClientCredentials
	}
	token, err := s.minter.GenerateAccessToken(user)
	if err != nil {
		return nil, err
	}
	// Best-effort observability: a failure to stamp last-used must not
	// fail an otherwise-successful token grant.
	_ = s.creds.StampLastUsed(ctx, cred.UUID, time.Now())
	return &GrantResult{AccessToken: token, ExpiresIn: int(s.minter.AccessTokenTTL(ctx).Seconds())}, nil
}

// recordFailed charges one failed attempt against the caller's address
// and the targeted clientId, so a distributed attacker (many IPs, one
// clientId) and a credential-stuffing attacker (one IP, many clientIds)
// are both throttled. A client ID IS an account and carries the account
// pair; the address carries the looser address pair.
func (s *ServiceAccountService) recordFailed(ctx context.Context, in GrantInput) {
	if s.counter == nil {
		return
	}
	_, _ = s.counter.RecordFailure(ctx, AttemptKeyIP(in.IP),
		Limit{Threshold: s.policy.IPLockoutThreshold(ctx), Window: s.policy.IPLockoutDuration(ctx)})
	_, _ = s.counter.RecordFailure(ctx, AttemptKeyClient(in.ClientID),
		Limit{Threshold: s.policy.LockoutThreshold(ctx), Window: s.policy.LockoutDuration(ctx)})
}
