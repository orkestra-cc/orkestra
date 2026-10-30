package services

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	authModels "github.com/orkestra/backend/internal/core/auth/models"
	"github.com/orkestra/backend/internal/core/auth/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// PINLoginPermission is the permission a user must hold to have, and to
// sign in with, a PIN. Declared by the auth module and granted to the
// operator platform role by default (revocable per user through roles).
const PINLoginPermission = "auth.pin_login"

// PIN method errors. Each maps to one stable errcode in the handlers.
var (
	ErrPINLoginDisabled     = stderrors.New("pin sign-in is disabled for this surface")
	ErrPINInvalid           = stderrors.New("pin must be exactly the configured number of digits")
	ErrPINTooWeak           = stderrors.New("pin is a repeated digit or a sequence")
	ErrPINUsernameRequired  = stderrors.New("user has no username")
	ErrPINUsernameNotUnique = stderrors.New("username is shared by another user of the tier")
	ErrPINNotAllowed        = stderrors.New("user may not hold or use a pin")
	ErrPINNotSet            = stderrors.New("user has no pin")
	ErrPINDeviceNotFound    = repository.ErrPINDeviceNotFound
	ErrPINSessionForbidden  = stderrors.New("a pin session cannot manage credentials")
	// ErrPINTargetAboveCaller mirrors the user module's target-tier guard:
	// an administrator acting on another user's PIN may not reach a user
	// whose system role is above their own.
	ErrPINTargetAboveCaller = stderrors.New("target user's role is above the caller's")
)

// PINPermissionChecker is the slice of iface.AuthzProvider the PIN service
// needs: a system-level permission check for a user with no token yet.
type PINPermissionChecker interface {
	HasPermission(ctx context.Context, userUUID, orgUUID, permission string) (bool, error)
}

// PINSessionRevoker ends the PIN sessions of a user (authService).
type PINSessionRevoker interface {
	RevokePINSessions(ctx context.Context, userUUID, deviceID, reason string) (int, error)
}

// PINSessionChecker re-validates a PIN session when it refreshes
// (PINAuthService). A refresh does not re-enter the sign-in, so without it
// a session would outlive the PIN, the device or the eligibility it was
// opened under: ErrRefreshSessionInactive when the session may no longer
// continue, ErrSessionEnforcementUnavailable when that cannot be decided.
type PINSessionChecker interface {
	CheckPINSession(ctx context.Context, userUUID, deviceID string) error
}

// PINEventRecorder persists auth_security_events rows (authService).
type PINEventRecorder interface {
	RecordAdminAuthEvent(ctx context.Context, eventType, actorUUID, targetUUID string, fields map[string]interface{})
	RecordSelfAuthEvent(ctx context.Context, eventType, userUUID string, fields map[string]interface{})
}

// PINAuthConfig wires a PINAuthService for one audience tier.
type PINAuthConfig struct {
	// Login is the tier's password service: the PIN path reuses its token
	// issuance, lockout pairs, audit emitter and user-response builder so
	// a PIN session is minted exactly like every other session.
	Login *PasswordAuthService
	Store iface.PINCredentialStore
	Users iface.UserProvider
	// Actors resolves the administrator behind an admin action for the
	// target-tier guard. Administrators are operator users whatever the
	// target's tier, so the client tier's service needs the operator
	// provider here; nil falls back to Users.
	Actors    iface.UserProvider
	Devices   repository.PINDeviceRepository
	Codes     PINEnrollmentCodeStore
	Passwords PasswordService
	Policy    *AuthPolicyService
	Audience  PolicyAudience
	Attempts  AttemptCounter
	Authz     PINPermissionChecker
	Sessions  PINSessionRevoker
	Events    PINEventRecorder
	Logger    *slog.Logger
}

// PINAuthService owns the PIN sign-in method for one tier:
// the credential (admin and self-service), the
// device registry and its one-time enrollment codes, and the sign-in.
//
// Invariants, each pinned by a test:
//   - the PIN is stored as an argon2id hash and never leaves the service;
//   - a PIN session (amr ["pin"]) never manages a credential;
//   - privileged roles and service principals never hold a PIN;
//   - with device binding on, a PIN is accepted only alongside the secret
//     of a device enrolled for that user, and a wrong device secret costs
//     the same as a wrong PIN without revealing whether the PIN was right;
//   - every set, change, removal and device revocation ends the PIN
//     sessions the old state opened.
type PINAuthService struct {
	login     *PasswordAuthService
	store     iface.PINCredentialStore
	users     iface.UserProvider
	actors    iface.UserProvider
	devices   repository.PINDeviceRepository
	codes     PINEnrollmentCodeStore
	passwords PasswordService
	policy    *AuthPolicyService
	audience  PolicyAudience
	attempts  AttemptCounter
	authz     PINPermissionChecker
	sessions  PINSessionRevoker
	events    PINEventRecorder
	logger    *slog.Logger
}

// NewPINAuthService builds the service. A nil Store or Devices leaves the
// method unavailable: every route answers ErrPINLoginDisabled.
func NewPINAuthService(cfg PINAuthConfig) *PINAuthService {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Actors == nil {
		cfg.Actors = cfg.Users
	}
	return &PINAuthService{
		login: cfg.Login, store: cfg.Store, users: cfg.Users, actors: cfg.Actors, devices: cfg.Devices,
		codes: cfg.Codes, passwords: cfg.Passwords, policy: cfg.Policy, audience: cfg.Audience,
		attempts: cfg.Attempts, authz: cfg.Authz, sessions: cfg.Sessions, events: cfg.Events,
		logger: logger,
	}
}

func (s *PINAuthService) wired() bool {
	return s != nil && s.login != nil && s.store != nil && s.devices != nil && s.users != nil && s.passwords != nil && s.policy != nil
}

// Audience reports the tier the service is bound to.
func (s *PINAuthService) Audience() PolicyAudience { return s.audience }

// --- read ---

// View returns the PIN slice of the auth-methods aggregators.
func (s *PINAuthService) View(ctx context.Context, user *iface.User) *authModels.PINMethodView {
	view := &authModels.PINMethodView{Devices: []authModels.PINDeviceDoc{}, Length: s.policy.PINLength(ctx)}
	if !s.wired() || user == nil {
		return view
	}
	enabled, err := s.policy.PINLoginEnabled(ctx, s.audience)
	view.Available = err == nil && enabled
	requires, err := s.policy.PINRequiresTrustedDevice(ctx)
	view.RequiresTrustedDevice = err != nil || requires
	view.Set = user.PINHash != ""
	if view.Set {
		view.UpdatedAt = user.PINUpdatedAt
		if user.PINLockedUntil != nil && time.Now().Before(*user.PINLockedUntil) {
			view.LockedUntil = user.PINLockedUntil
		}
	}
	if devices, err := s.devices.ListByUser(ctx, user.UUID); err == nil {
		view.Devices = devices
	} else {
		s.logger.Warn("pin: device list unavailable for auth-methods view",
			slog.String("user_uuid", user.UUID), slog.String("error", err.Error()))
	}
	return view
}

// ViewByUUID loads the user and returns View, or nil when the user
// cannot be read (the aggregators then omit the slice).
func (s *PINAuthService) ViewByUUID(ctx context.Context, userUUID string) *authModels.PINMethodView {
	if s == nil || s.users == nil {
		return nil
	}
	user, err := s.users.GetUserByID(ctx, userUUID)
	if err != nil || user == nil {
		return nil
	}
	return s.View(ctx, user)
}

// --- credential management ---

// AdminSetPIN sets or replaces the target's PIN. The surface toggle is not
// consulted: an administrator may prepare PINs before switching the
// method on.
func (s *PINAuthService) AdminSetPIN(ctx context.Context, actorUUID, targetUUID, pin string) error {
	user, err := s.loadAdminTarget(ctx, actorUUID, targetUUID)
	if err != nil {
		return err
	}
	if err := s.checkEligible(ctx, user); err != nil {
		return err
	}
	if err := s.writePIN(ctx, user, pin); err != nil {
		return err
	}
	s.revokePINSessions(ctx, user.UUID, "", "pin_set_by_admin")
	s.recordAdmin(ctx, "admin_pin_set", actorUUID, user.UUID, map[string]interface{}{
		"replaced": user.PINHash != "",
	})
	return nil
}

// AdminRemovePIN removes the target's PIN and every enrolled device.
func (s *PINAuthService) AdminRemovePIN(ctx context.Context, actorUUID, targetUUID string) error {
	user, err := s.loadAdminTarget(ctx, actorUUID, targetUUID)
	if err != nil {
		return err
	}
	if user.PINHash == "" {
		return ErrPINNotSet
	}
	devices, err := s.clearPIN(ctx, user.UUID, "", "pin_removed_by_admin")
	if err != nil {
		return err
	}
	s.recordAdmin(ctx, "admin_pin_removed", actorUUID, user.UUID, map[string]interface{}{
		"devicesRemoved": devices,
	})
	return nil
}

// ChangeOwnPIN sets or replaces the caller's own PIN. amr is the
// caller's resolved token amr: a PIN session is refused.
func (s *PINAuthService) ChangeOwnPIN(ctx context.Context, userUUID string, amr []string, pin string) error {
	if authModels.HasPINAMR(amr) {
		return ErrPINSessionForbidden
	}
	user, err := s.loadManaged(ctx, userUUID)
	if err != nil {
		return err
	}
	if err := s.checkEligible(ctx, user); err != nil {
		return err
	}
	if err := s.writePIN(ctx, user, pin); err != nil {
		return err
	}
	s.revokePINSessions(ctx, user.UUID, "", "pin_changed_by_user")
	s.recordSelf(ctx, "self_pin_changed", user.UUID, map[string]interface{}{
		"replaced": user.PINHash != "",
	})
	return nil
}

// RemoveOwnPIN removes the caller's own PIN and devices.
func (s *PINAuthService) RemoveOwnPIN(ctx context.Context, userUUID string, amr []string) error {
	if authModels.HasPINAMR(amr) {
		return ErrPINSessionForbidden
	}
	user, err := s.loadManaged(ctx, userUUID)
	if err != nil {
		return err
	}
	if user.PINHash == "" {
		return ErrPINNotSet
	}
	devices, err := s.clearPIN(ctx, user.UUID, "", "pin_removed_by_user")
	if err != nil {
		return err
	}
	s.recordSelf(ctx, "self_pin_removed", user.UUID, map[string]interface{}{
		"devicesRemoved": devices,
	})
	return nil
}

// IssueEnrollmentCode mints the one-time code a device redeems to become
// trusted for the target's PIN. self=true means the caller is the target,
// with amr their token's; an administrator passes self=false.
func (s *PINAuthService) IssueEnrollmentCode(ctx context.Context, actorUUID, targetUUID string, self bool, amr []string) (string, time.Time, error) {
	if self && authModels.HasPINAMR(amr) {
		return "", time.Time{}, ErrPINSessionForbidden
	}
	if s.codes == nil {
		return "", time.Time{}, ErrPINLoginDisabled
	}
	load := s.loadManaged
	if !self {
		load = func(ctx context.Context, id string) (*iface.User, error) {
			return s.loadAdminTarget(ctx, actorUUID, id)
		}
	}
	user, err := load(ctx, targetUUID)
	if err != nil {
		return "", time.Time{}, err
	}
	if user.PINHash == "" {
		return "", time.Time{}, ErrPINNotSet
	}
	if err := s.checkEligible(ctx, user); err != nil {
		return "", time.Time{}, err
	}
	code, err := newPINEnrollmentCode()
	if err != nil {
		return "", time.Time{}, err
	}
	via := authModels.PINDeviceViaAdmin
	event := "admin_pin_enrollment_code"
	if self {
		via = authModels.PINDeviceViaSelf
		event = "self_pin_enrollment_code"
	}
	expires := time.Now().Add(PINEnrollmentCodeTTL)
	if err := s.codes.Put(ctx, s.audience, user.UUID, PINEnrollmentCode{
		Hash: hashPINSecret(normalisePINEnrollmentCode(code)), Via: via, By: actorUUID,
		PIN: pinFingerprint(user.PINHash),
	}, PINEnrollmentCodeTTL); err != nil {
		return "", time.Time{}, fmt.Errorf("store enrollment code: %w", err)
	}
	fields := map[string]interface{}{"expiresAt": expires.UTC().Format(time.RFC3339)}
	if self {
		s.recordSelf(ctx, event, user.UUID, fields)
	} else {
		s.recordAdmin(ctx, event, actorUUID, user.UUID, fields)
	}
	return code, expires, nil
}

// RevokeDevice removes one enrolled device and ends the PIN sessions it
// opened.
func (s *PINAuthService) RevokeDevice(ctx context.Context, actorUUID, targetUUID, deviceID string, self bool) error {
	if !s.wired() {
		return ErrPINLoginDisabled
	}
	if !self {
		if _, err := s.loadAdminTarget(ctx, actorUUID, targetUUID); err != nil {
			return err
		}
	}
	if err := s.devices.Delete(ctx, targetUUID, deviceID); err != nil {
		return err
	}
	s.revokePINSessions(ctx, targetUUID, deviceID, "pin_device_revoked")
	fields := map[string]interface{}{"deviceId": deviceID}
	if self {
		s.recordSelf(ctx, "self_pin_device_revoked", targetUUID, fields)
	} else {
		s.recordAdmin(ctx, "admin_pin_device_revoked", actorUUID, targetUUID, fields)
	}
	return nil
}

// --- device enrollment (public) ---

// PINDeviceEnrollInput is the public device-enrollment request.
type PINDeviceEnrollInput struct {
	Username   string
	Code       string
	DeviceID   string
	DeviceName string
	Platform   string
	IP         string
}

// EnrollDevice redeems an enrollment code and returns the device secret,
// shown exactly once. Failures are one generic ErrInvalidCredentials on
// their own attempt budget (ScopePINEnroll + the shared address scope).
func (s *PINAuthService) EnrollDevice(ctx context.Context, in PINDeviceEnrollInput) (string, error) {
	username := strings.TrimSpace(in.Username)
	code := normalisePINEnrollmentCode(in.Code)
	deviceID := strings.TrimSpace(in.DeviceID)
	if username == "" || code == "" || deviceID == "" || len(deviceID) > 128 {
		return "", ErrInvalidCredentials
	}
	if err := s.surfaceOpen(ctx); err != nil {
		return "", err
	}
	if s.codes == nil {
		return "", ErrPINLoginDisabled
	}
	enrollKey := AttemptKeyPINEnroll(s.audience, username)
	if v, locked := s.peek(ctx, in.IP, enrollKey); locked {
		return "", LockedAfter(v.RetryAfter)
	}
	fail := func() (string, error) {
		s.charge(ctx, in.IP, enrollKey)
		return "", ErrInvalidCredentials
	}
	user, err := s.store.GetUserForPINAuth(ctx, username)
	if err != nil {
		return fail()
	}
	stored, err := s.codes.Peek(ctx, s.audience, user.UUID)
	hash := hashPINSecret(code)
	if err != nil || subtle.ConstantTimeCompare([]byte(stored.Hash), []byte(hash)) != 1 {
		return fail()
	}
	if ok, err := s.codes.TakeIf(ctx, s.audience, user.UUID, hash); err != nil || !ok {
		return fail()
	}
	if !user.IsActive || user.PINHash == "" || stored.PIN != pinFingerprint(user.PINHash) || s.checkEligible(ctx, user) != nil {
		// The code was genuine, so this is not a guess: no charge. The
		// user changed after the code was minted (deactivated, PIN
		// removed or replaced, no longer eligible); the answer stays
		// generic.
		return "", ErrInvalidCredentials
	}
	secret, err := newPINDeviceSecret()
	if err != nil {
		return "", err
	}
	if err := s.devices.Upsert(ctx, &authModels.PINDeviceDoc{
		UUID:       authModels.GenerateUUIDv7(),
		UserUUID:   user.UUID,
		DeviceID:   deviceID,
		SecretHash: hashPINSecret(secret),
		DeviceName: truncate(strings.TrimSpace(in.DeviceName), 100),
		Platform:   in.Platform,
		EnrolledBy: stored.By,
		Via:        stored.Via,
		CreatedAt:  time.Now(),
	}); err != nil {
		return "", fmt.Errorf("store pin device: %w", err)
	}
	s.reset(ctx, enrollKey)
	// A re-enrollment replaces the device's secret: whatever the old secret
	// opened ends with it.
	s.revokePINSessions(ctx, user.UUID, deviceID, "pin_device_reenrolled")
	s.recordAdmin(ctx, "pin_device_enrolled", stored.By, user.UUID, map[string]interface{}{
		"deviceId": deviceID, "deviceName": in.DeviceName, "platform": in.Platform, "via": stored.Via,
	})
	return secret, nil
}

// --- sign-in ---

// PINLoginInput is the public PIN sign-in request.
type PINLoginInput struct {
	Username     string
	PIN          string
	DeviceID     string
	DeviceSecret string
	Platform     string
	IP           string
	UserAgent    string
}

// Login authenticates username + PIN (+ device secret) and mints a PIN
// session: amr ["pin"], the PIN access lifetime and idle timeout. The
// order mirrors PasswordAuthService.Login — cheap gates that must leave
// counters and audit untouched first, then the lockout peek, then the
// lookup — and every failure after the peek costs one argon2 verify.
func (s *PINAuthService) Login(ctx context.Context, in PINLoginInput) (*authModels.TokenResponse, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" || in.PIN == "" {
		return nil, ErrInvalidCredentials
	}
	if s.policy != nil && !s.policy.LoginAllowed(ctx, s.audience) {
		return nil, ErrLoginDisabled
	}
	if err := s.surfaceOpen(ctx); err != nil {
		return nil, err
	}
	requireDevice, err := s.policy.PINRequiresTrustedDevice(ctx)
	if err != nil {
		return nil, err
	}

	// Geo block, as on the password route: before the peek, so a blocked
	// country never moves a counter.
	if s.login.countryBlocked(ctx, in.IP) {
		s.login.emitLoginFailed(ctx, "", "", in.IP, "country_blocked")
		return nil, ErrCountryBlocked
	}

	pinKey := AttemptKeyPIN(s.audience, username)
	if v, locked := s.peek(ctx, in.IP, pinKey); locked {
		s.dummyVerify(in.PIN)
		s.login.emitLoginFailed(ctx, "", "", in.IP, "pin_rate_limited")
		return nil, LockedAfter(v.RetryAfter)
	}
	refuse := func(user *iface.User, reason string) (*authModels.TokenResponse, error) {
		s.dummyVerify(in.PIN)
		s.charge(ctx, in.IP, pinKey)
		uid := ""
		if user != nil {
			uid = user.UUID
		}
		s.login.emitLoginFailed(ctx, "", uid, in.IP, reason)
		return nil, ErrInvalidCredentials
	}

	user, err := s.store.GetUserForPINAuth(ctx, username)
	if err != nil {
		if stderrors.Is(err, iface.ErrUsernameAmbiguous) {
			s.logger.Warn("pin: sign-in refused, username shared by several users of the tier",
				slog.String("audience", string(s.audience)))
		}
		return refuse(nil, "pin_unknown_user")
	}
	// Inactive-account auto-disable, as on the password route: without it a
	// dormant account would sign in with a PIN, and the lastLogin stamp of
	// that sign-in would clear the condition for every later method.
	s.login.autoDisableIfInactive(ctx, user, in.IP)
	if !user.IsActive {
		return refuse(user, "pin_user_inactive")
	}
	if user.Kind == iface.UserKindService {
		return refuse(user, "pin_service_principal")
	}
	if user.PINHash == "" {
		return refuse(user, "pin_not_set")
	}
	if user.PINLockedUntil != nil && time.Now().Before(*user.PINLockedUntil) {
		// Not recorded: a lock must not extend itself.
		s.dummyVerify(in.PIN)
		s.login.emitLoginFailed(ctx, "", user.UUID, in.IP, "pin_locked")
		return nil, LockedAfter(time.Until(*user.PINLockedUntil))
	}

	var device *authModels.PINDeviceDoc
	if requireDevice {
		//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
		device, err = s.devices.Find(ctx, user.UUID, strings.TrimSpace(in.DeviceID))
		if err != nil || in.DeviceSecret == "" ||
			subtle.ConstantTimeCompare([]byte(device.SecretHash), []byte(hashPINSecret(in.DeviceSecret))) != 1 {
			// The PIN is NOT verified here (dummy cost instead), so a
			// caller without the device secret learns nothing about the
			// PIN. Nor is it a PIN guess, so it is charged to the attempt
			// counters (rate limiting) and NOT to the durable PIN count:
			// otherwise anyone knowing a username could walk the count to
			// the cap and wipe the worker's PIN and devices.
			s.dummyVerify(in.PIN)
			s.charge(ctx, in.IP, pinKey)
			s.login.emitLoginFailed(ctx, "", user.UUID, in.IP, "pin_untrusted_device")
			return nil, ErrInvalidCredentials
		}
	}

	// Reserve the attempt BEFORE verifying: the atomic increment gives every
	// concurrent request a distinct count, so a parallel burst gets at most
	// PINMaxConsecutiveFailures verifications, not one per request.
	attempt, err := s.store.RecordFailedPIN(ctx, user.UUID)
	if err != nil {
		s.dummyVerify(in.PIN)
		return nil, fmt.Errorf("%w: pin attempt not recorded: %w", ErrAuthPolicyUnavailable, err)
	}
	if attempt > PINMaxConsecutiveFailures {
		s.dummyVerify(in.PIN)
		s.charge(ctx, in.IP, pinKey)
		s.disablePIN(ctx, user.UUID, user.PINHash, attempt-1)
		s.login.emitLoginFailed(ctx, "", user.UUID, in.IP, "pin_disabled_after_failures")
		return nil, ErrInvalidCredentials
	}
	ok, err := s.passwords.Verify(in.PIN, user.PINHash)
	if err != nil || !ok {
		s.recordPINFailure(ctx, in.IP, pinKey, user.UUID, user.PINHash, attempt)
		s.login.emitLoginFailed(ctx, "", user.UUID, in.IP, "pin_bad_pin")
		return nil, ErrInvalidCredentials
	}
	// The PIN was right: from here an answer may be specific.
	if err := s.checkEligible(ctx, user); err != nil {
		s.login.emitLoginFailed(ctx, "", user.UUID, in.IP, "pin_not_allowed")
		if stderrors.Is(err, ErrAuthPolicyUnavailable) {
			return nil, err
		}
		return nil, ErrPINNotAllowed
	}

	if err := s.credentialStillCurrent(ctx, user, device); err != nil {
		s.login.emitLoginFailed(ctx, "", user.UUID, in.IP, "pin_changed_during_sign_in")
		return nil, err
	}

	// The reserved attempt succeeded: the consecutive-failure run ends.
	_ = s.store.ClearFailedPINs(ctx, user.UUID)
	s.reset(ctx, pinKey)
	now := time.Now()
	if device != nil {
		_ = s.devices.TouchLastUsed(ctx, user.UUID, device.DeviceID, now)
	}
	s.login.emitAudit(ctx, iface.AuditEvent{
		ActorUserID: user.UUID,
		ActorEmail:  user.Email,
		ActorType:   "user",
		Action:      "auth.login.succeeded",
		Outcome:     "success",
		IPAddress:   in.IP,
		Metadata:    map[string]any{"method": authModels.LoginMethodPIN, "deviceId": in.DeviceID, "platform": in.Platform},
	})
	platform, deviceType := pinPlatform(in.Platform)
	return s.login.issueTokensForSession(ctx, user, LoginTokenContext{
		SessionID:   uuid.NewString(),
		DeviceID:    strings.TrimSpace(in.DeviceID),
		DeviceType:  deviceType,
		Platform:    platform,
		IPAddress:   in.IP,
		UserAgent:   in.UserAgent,
		LoginMethod: authModels.LoginMethodPIN,
		AccessTTL:   s.policy.PINAccessTokenTTL(ctx),
		RefreshTTL:  s.policy.PINRefreshTokenTTL(ctx),
	}, []string{authModels.AMRPIN}, 0)
}

// CheckPINSession implements PINSessionChecker: the surface still offers
// PIN sign-in, the user is active and still holds a PIN, the device the
// session signed in from is still enrolled (when the surface binds PINs to
// devices), and the user is still eligible — a user promoted, or given a
// privileged membership, since signing in must not keep refreshing a PIN
// session that a new sign-in would refuse.
func (s *PINAuthService) CheckPINSession(ctx context.Context, userUUID, deviceID string) error {
	if !s.wired() {
		return ErrRefreshSessionInactive
	}
	if err := s.surfaceOpen(ctx); err != nil {
		if stderrors.Is(err, ErrPINLoginDisabled) {
			return ErrRefreshSessionInactive
		}
		return ErrSessionEnforcementUnavailable
	}
	user, err := s.users.GetUserByID(ctx, userUUID)
	if err != nil {
		if stderrors.Is(err, iface.ErrUserNotFound) {
			return ErrRefreshSessionInactive
		}
		return ErrSessionEnforcementUnavailable
	}
	if user == nil || !user.IsActive || user.PINHash == "" {
		return ErrRefreshSessionInactive
	}
	requireDevice, err := s.policy.PINRequiresTrustedDevice(ctx)
	if err != nil {
		return ErrSessionEnforcementUnavailable
	}
	if requireDevice {
		//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
		if _, err := s.devices.Find(ctx, user.UUID, deviceID); err != nil {
			if stderrors.Is(err, repository.ErrPINDeviceNotFound) {
				return ErrRefreshSessionInactive
			}
			return ErrSessionEnforcementUnavailable
		}
	}
	if err := s.checkEligible(ctx, user); err != nil {
		if stderrors.Is(err, ErrAuthPolicyUnavailable) {
			return ErrSessionEnforcementUnavailable
		}
		return ErrRefreshSessionInactive
	}
	return nil
}

// credentialStillCurrent re-reads, just before tokens are issued, the PIN
// and device a sign-in verified: a removal or replacement that landed
// during the verification must not be followed by a new session.
func (s *PINAuthService) credentialStillCurrent(ctx context.Context, user *iface.User, device *authModels.PINDeviceDoc) error {
	fresh, err := s.users.GetUserByID(ctx, user.UUID)
	if err != nil || fresh == nil {
		return fmt.Errorf("%w: user re-read: %v", ErrAuthPolicyUnavailable, err)
	}
	if fresh.PINHash != user.PINHash || !fresh.IsActive {
		return ErrInvalidCredentials
	}
	if device != nil {
		//tenantscope:allow PIN devices are keyed on the user, like the user rows they belong to
		if _, err := s.devices.Find(ctx, user.UUID, device.DeviceID); err != nil {
			if stderrors.Is(err, repository.ErrPINDeviceNotFound) {
				return ErrInvalidCredentials
			}
			return fmt.Errorf("%w: device re-read: %w", ErrAuthPolicyUnavailable, err)
		}
	}
	return nil
}

// --- helpers ---

func (s *PINAuthService) surfaceOpen(ctx context.Context) error {
	if !s.wired() {
		return ErrPINLoginDisabled
	}
	enabled, err := s.policy.PINLoginEnabled(ctx, s.audience)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrPINLoginDisabled
	}
	return nil
}

func (s *PINAuthService) loadManaged(ctx context.Context, userUUID string) (*iface.User, error) {
	if !s.wired() {
		return nil, ErrPINLoginDisabled
	}
	user, err := s.users.GetUserByID(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, iface.ErrUserNotFound
	}
	return user, nil
}

// loadAdminTarget loads the target of an administrator's PIN action and
// applies the target-tier guard against the actor's DATABASE role (never
// a claim), as the user module does for every user update.
func (s *PINAuthService) loadAdminTarget(ctx context.Context, actorUUID, targetUUID string) (*iface.User, error) {
	user, err := s.loadManaged(ctx, targetUUID)
	if err != nil {
		return nil, err
	}
	if actorUUID == targetUUID {
		return user, nil
	}
	actor, err := s.actors.GetUserByID(ctx, actorUUID)
	if err != nil || actor == nil {
		return nil, fmt.Errorf("%w: caller role unavailable", ErrAuthPolicyUnavailable)
	}
	if pinRoleTier(user.Role) > pinRoleTier(actor.Role) {
		return nil, ErrPINTargetAboveCaller
	}
	return user, nil
}

// pinRoleTier is the system-role ladder of the user module's target-tier
// guard (user/handlers systemRoleTier). Kept in step by
// TestPIN_RoleLadderMatchesUserModule's fixed expectations; an unknown
// role ranks below everything, so an unknown caller reaches nobody with a
// known role.
func pinRoleTier(role string) int {
	switch role {
	case "super_admin":
		return 5
	case "administrator":
		return 4
	case "developer":
		return 3
	case "manager":
		return 2
	case "operator":
		return 1
	case "guest":
		return 0
	}
	return -1
}

// checkEligible is the one rule for who may hold and use a PIN.
func (s *PINAuthService) checkEligible(ctx context.Context, user *iface.User) error {
	if user.Kind == iface.UserKindService {
		return ErrPINNotAllowed
	}
	username := strings.TrimSpace(user.Username)
	if username == "" {
		return ErrPINUsernameRequired
	}
	n, err := s.store.CountUsersByUsername(ctx, user.Username)
	if err != nil {
		return fmt.Errorf("%w: count username: %w", ErrAuthPolicyUnavailable, err)
	}
	if n != 1 {
		return ErrPINUsernameNotUnique
	}
	// Field roles only: a PIN is never stronger than what the operator
	// role may do (read + self-service), so a manager, developer or
	// administrator never holds one — whatever their permission set says.
	if pinRoleTier(user.Role) < 0 || pinRoleTier(user.Role) > pinRoleTier("operator") {
		return ErrPINNotAllowed
	}
	// Privileged memberships never hold a weak credential either. The
	// built-in list is checked even when MFA enforcement is off
	// (MFARequired short-circuits to false then), and the configured list
	// on top of it.
	// A failed lookup refuses: reading it as "no memberships" would let an
	// org_owner through during a tenant-store outage.
	memberships, err := s.login.loadMembershipsStrict(ctx, user.UUID)
	if err != nil {
		return fmt.Errorf("%w: memberships: %w", ErrAuthPolicyUnavailable, err)
	}
	if RoleRequiresMFA(user, memberships) || s.policy.MFARequired(user, memberships) {
		return ErrPINNotAllowed
	}
	// auth.pin_login is evaluated on the operator tier only: the authz
	// evaluator resolves system roles from operator users, so a client
	// user's permission is not answerable there. On the client tier the
	// grant is the administrator setting the PIN (and removing it revokes
	// it), inside the role ceiling above.
	if s.audience != PolicyAudienceOperator {
		return nil
	}
	if s.authz == nil {
		// Fail closed: without an evaluator nobody provably holds the
		// permission.
		return ErrPINNotAllowed
	}
	ok, err := s.authz.HasPermission(ctx, user.UUID, "", PINLoginPermission)
	if err != nil {
		return fmt.Errorf("%w: permission check: %w", ErrAuthPolicyUnavailable, err)
	}
	if !ok {
		return ErrPINNotAllowed
	}
	return nil
}

func (s *PINAuthService) writePIN(ctx context.Context, user *iface.User, pin string) error {
	if err := validateNewPIN(pin, s.policy.PINLength(ctx)); err != nil {
		return err
	}
	hash, err := s.passwords.Hash(pin)
	if err != nil {
		return fmt.Errorf("hash pin: %w", err)
	}
	return s.store.SetPINHash(ctx, user.UUID, hash)
}

// clearPIN removes the PIN, its devices and its sessions. A non-empty
// expectedHash binds the removal to that hash (iface.ErrPINChanged when the
// user has another PIN by now): the failure cap passes the hash it judged,
// so it never removes a PIN set after the attempt.
func (s *PINAuthService) clearPIN(ctx context.Context, userUUID, expectedHash, reason string) (int64, error) {
	if err := s.store.ClearPIN(ctx, userUUID, expectedHash); err != nil {
		return 0, err
	}
	n, err := s.devices.DeleteAllByUser(ctx, userUUID)
	if err != nil {
		// The PIN itself is gone, so no device can sign in any more; the
		// rows are orphans, not a credential.
		s.logger.Warn("pin: devices not removed with the pin",
			slog.String("user_uuid", userUUID), slog.String("error", err.Error()))
	}
	s.revokePINSessions(ctx, userUUID, "", reason)
	return n, nil
}

func (s *PINAuthService) revokePINSessions(ctx context.Context, userUUID, deviceID, reason string) {
	if s.sessions == nil {
		return
	}
	if _, err := s.sessions.RevokePINSessions(ctx, userUUID, deviceID, reason); err != nil {
		// The credential change already happened; a failed revocation is
		// logged, as for every other credential change.
		s.logger.Warn("pin: session revocation incomplete",
			slog.String("user_uuid", userUUID), slog.String("reason", reason), slog.String("error", err.Error()))
	}
}

// recordPINFailure completes a wrong PIN whose attempt was already
// reserved (attempt = the durable count including it): both counter
// scopes, and the durable lock on the counter lock or on every
// threshold-th failure. The cap itself is enforced at the next
// reservation, before any verify.
func (s *PINAuthService) recordPINFailure(ctx context.Context, ip, pinKey, userUUID, hash string, attempt int) {
	v := s.charge(ctx, ip, pinKey)
	threshold := s.policy.LockoutThreshold(ctx)
	if v.Locked || (threshold > 0 && attempt%threshold == 0) {
		// Bound to the hash the attempt was verified against: an attempt
		// that started before an administrator replaced the PIN never
		// locks the new one.
		err := s.store.LockPIN(ctx, userUUID, hash, time.Now().Add(s.policy.LockoutDuration(ctx)))
		if err != nil && !stderrors.Is(err, iface.ErrPINChanged) {
			s.logger.Warn("pin: lock not recorded", slog.String("user_uuid", userUUID), slog.String("error", err.Error()))
		}
	}
}

// disablePIN removes a PIN whose consecutive-failure run passed the cap,
// only while it is still the PIN (hash) the run was counted against.
func (s *PINAuthService) disablePIN(ctx context.Context, userUUID, hash string, failures int) {
	if _, err := s.clearPIN(ctx, userUUID, hash, "pin_disabled_after_failures"); err != nil {
		if stderrors.Is(err, iface.ErrPINChanged) {
			return
		}
		s.logger.Error("pin: could not remove a pin after the consecutive-failure cap",
			slog.String("user_uuid", userUUID), slog.String("error", err.Error()))
		return
	}
	s.recordAdmin(ctx, "pin_disabled_after_failures", "", userUUID, map[string]interface{}{
		"failures": failures,
	})
}

func (s *PINAuthService) peek(ctx context.Context, ip, accountKey string) (Verdict, bool) {
	if s.attempts == nil {
		return Verdict{}, false
	}
	if v, err := s.attempts.Locked(ctx, AttemptKeyIP(ip), s.login.addressLimit(ctx)); err == nil && v.Locked {
		return v, true
	}
	if v, err := s.attempts.Locked(ctx, accountKey, s.login.accountLimit(ctx)); err == nil && v.Locked {
		return v, true
	}
	return Verdict{}, false
}

func (s *PINAuthService) charge(ctx context.Context, ip, accountKey string) Verdict {
	if s.attempts == nil {
		return Verdict{}
	}
	_, _ = s.attempts.RecordFailure(ctx, AttemptKeyIP(ip), s.login.addressLimit(ctx))
	v, err := s.attempts.RecordFailure(ctx, accountKey, s.login.accountLimit(ctx))
	if err != nil {
		return Verdict{}
	}
	return v
}

func (s *PINAuthService) reset(ctx context.Context, accountKey string) {
	if s.attempts != nil {
		_ = s.attempts.Reset(ctx, accountKey)
	}
}

func (s *PINAuthService) recordAdmin(ctx context.Context, eventType, actorUUID, targetUUID string, fields map[string]interface{}) {
	if s.events != nil {
		s.events.RecordAdminAuthEvent(ctx, eventType, actorUUID, targetUUID, fields)
	}
}

func (s *PINAuthService) recordSelf(ctx context.Context, eventType, userUUID string, fields map[string]interface{}) {
	if s.events != nil {
		s.events.RecordSelfAuthEvent(ctx, eventType, userUUID, fields)
	}
}

func (s *PINAuthService) dummyVerify(pin string) {
	_, _ = s.passwords.Verify(pin, s.passwords.DummyHash())
}

// validateNewPIN enforces the configured length and refuses the
// guesses every attacker tries first.
func validateNewPIN(pin string, length int) error {
	if len(pin) != length {
		return ErrPINInvalid
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return ErrPINInvalid
		}
	}
	same, up, down := true, true, true
	for i := 1; i < len(pin); i++ {
		d := int(pin[i]) - int(pin[i-1])
		same = same && d == 0
		up = up && d == 1
		down = down && d == -1
	}
	if same || up || down {
		return ErrPINTooWeak
	}
	return nil
}

func newPINDeviceSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("device secret entropy: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// pinPlatform normalises the client-reported platform onto the refresh
// row's vocabulary.
func pinPlatform(p string) (platform, deviceType string) {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "ios":
		return "ios", "mobile"
	case "android":
		return "android", "mobile"
	}
	return "web", "desktop"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// pinFingerprint names a stored PIN hash without carrying it: what an
// enrollment code records to stay bound to the PIN it was issued for.
func pinFingerprint(pinHash string) string {
	if pinHash == "" {
		return ""
	}
	return hashPINSecret("pin-code-binding:" + pinHash)
}
