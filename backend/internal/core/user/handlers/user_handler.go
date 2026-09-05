package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/user/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/metrics"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// auditEmitter is the narrow capability the handler probes on the
// userService — when wired by the compliance addon's post-Init loop,
// AuditSink returns a live sink and lifecycle events fire. When the
// compliance addon is disabled the assertion still succeeds (the
// concrete service exposes the method) but AuditSink returns nil, so
// emit is a quiet no-op. Defining the interface here instead of on
// services.UserService keeps the broader service interface unchanged
// — test fakes don't have to grow this method.
type auditEmitter interface {
	AuditSink() iface.AuditSink
}

// UserHandler handles user HTTP requests
type UserHandler struct {
	userService services.UserService
	// services resolves cross-module collaborators lazily at request
	// time. It cannot be a constructor argument resolved eagerly: the
	// registry sorts user before auth, so auth's services do not exist
	// yet when this module initialises. Optional — a nil registry means
	// the optional collaborators simply aren't available.
	services *module.ServiceRegistry
	// platform classifies the deployment environment. Read only by the
	// caller-role guard's synthetic dev-token exception, which must be
	// inert anywhere production-like. Optional, and a nil platform is
	// treated AS production-like — so an un-wired handler (every unit
	// test) never opens the exception.
	platform module.PlatformInfo
}

// NewUserHandler creates a new user handler
func NewUserHandler(userService services.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// SetServiceRegistry wires the module service registry so the handler can
// resolve later-initialised collaborators (currently the auth module's
// iface.SessionTerminator) at request time. Called from the user module's
// Init; safe to leave unset in tests.
func (h *UserHandler) SetServiceRegistry(reg *module.ServiceRegistry) {
	h.services = reg
}

// SetPlatform wires the deployment's environment classification, which the
// caller-role guard needs before it can honour the synthetic dev-token
// exception. Called from the user module's Init; leaving it unset disables
// the exception, which is the fail-closed default.
func (h *UserHandler) SetPlatform(p module.PlatformInfo) {
	h.platform = p
}

// terminateSessions best-effort evicts every session of a user whose
// right to be signed in was just revoked (deactivate / delete).
//
// Deliberately silent on failure. The state change is already persisted
// and the auth refresh paths refuse a deactivated user regardless, so the
// worst case is that in-flight access tokens live out their remaining TTL
// — the behaviour before this call existed. Failing the operator's
// request instead would report that the deactivation did not happen,
// which is both wrong and more dangerous.
func (h *UserHandler) terminateSessions(ctx context.Context, userUUID string) {
	_ = h.terminateSessionsReporting(ctx, userUUID)
}

// terminateSessionsReporting is terminateSessions with the outcome
// returned instead of dropped, for the caller that records it in an
// audit row (the role-change branch, spec §4.6 D27). Identical
// best-effort semantics — it never fails the request; false means only
// "the sessions were not torn down here", which covers a terminator that
// errored, one that is not wired, and a handler with no registry at all.
func (h *UserHandler) terminateSessionsReporting(ctx context.Context, userUUID string) bool {
	if h.services == nil || userUUID == "" {
		return false
	}
	terminator, ok := module.GetTyped[iface.SessionTerminator](h.services, module.ServiceAuthService)
	if !ok || terminator == nil {
		return false
	}
	if err := terminator.TerminateAllSessionsByUUID(ctx, userUUID); err != nil {
		slog.WarnContext(ctx, "user: could not terminate sessions after access revocation",
			slog.String("user_uuid", userUUID),
			slog.String("error", err.Error()))
		return false
	}
	return true
}

// invalidateAuthz retires every authorization verdict cached for one
// user, so the next decision recomputes from the role that was just
// written instead of answering from the permission cache (M-13).
//
// Best-effort BY DESIGN, and the caller must not turn a failure into a
// refusal (spec §4.6 D27 as amended — the rule splits by direction, and
// a system-role change lands on the write-then-report side in both
// directions):
//
//   - refusing a DEMOTION is M-13 itself, made permanent. The finding is
//     "a demoted administrator keeps administrator verdicts"; answering
//     503 leaves them an administrator indefinitely rather than for at
//     most one cache TTL. And with the cache store down, reads bypass the
//     cache entirely, so a written demotion takes effect immediately.
//   - refusing a PROMOTION only delays a stale DENY, which is harmless.
//
// A missing invalidator is reported exactly like a failing one. That is
// deliberately stricter than the "degrade quietly" note on
// iface.AuthzCacheInvalidator, which addresses consumers that only READ
// verdicts: here the audit row is what tells the operator whether the
// change is live now or within the cache TTL, so "nothing was retired"
// must not be recorded as success.
func (h *UserHandler) invalidateAuthz(ctx context.Context, userUUID string) error {
	if userUUID == "" {
		return errors.New("user uuid required")
	}
	// Nil-checked before GetTyped: ServiceRegistry.Get locks the
	// receiver, so a nil registry is a panic, not a miss.
	if h.services == nil {
		return fmt.Errorf("%w: no service registry", errAuthzInvalidatorUnwired)
	}
	inv, ok := module.GetTyped[iface.AuthzCacheInvalidator](h.services, module.ServiceAuthzProvider)
	if !ok || inv == nil {
		return fmt.Errorf("%w: %s is absent or does not implement it", errAuthzInvalidatorUnwired, module.ServiceAuthzProvider)
	}
	return inv.InvalidateUserPermissions(ctx, userUUID)
}

// errAuthzInvalidatorUnwired separates "there is no permission cache to
// retire" from "there is one and it could not be retired". Both leave
// cache_invalidated false in the audit row — nothing was retired either
// way — but only the second leaves stale verdicts behind, so only the
// second may say so in the log or move the failure counter. (A third
// state, a wired authz module with no Redis, is not an error at all:
// InvalidateUserPermissions returns nil there, truthfully.)
var errAuthzInvalidatorUnwired = errors.New("no authz cache invalidator is wired")

// emitAudit forwards an event to the compliance audit sink if one was
// wired onto the underlying user service. Best-effort: a nil sink, a
// userService that doesn't satisfy auditEmitter (custom test fakes), or
// any internal sink error are all silent no-ops — auditing must never
// break the hot path. Resource type/id and actor identity are
// populated by callers from ctxauth + request data.
func (h *UserHandler) emitAudit(ctx context.Context, event iface.AuditEvent) {
	emitter, ok := h.userService.(auditEmitter)
	if !ok {
		return
	}
	sink := emitter.AuditSink()
	if sink == nil {
		return
	}
	sink.Emit(ctx, event)
}

// actorFromCtx pulls the admin's UUID + email off the request context
// for stamping into the AuditEvent.ActorUser fields. Defensive: when
// the gate stripped them (which shouldn't happen on these admin
// routes), the returned values are empty and the sink infers actorType
// from the remaining fields.
func actorFromCtx(ctx context.Context) (string, string) {
	uuid, _ := ctxauth.GetUserUUID(ctx)
	email, _ := ctxauth.GetUserEmail(ctx)
	return uuid, email
}

// Create User Request
type CreateUserRequest struct {
	Body iface.CreateUserInput `json:"user" doc:"User data to create"`
}

// Create User Response
type CreateUserResponse struct {
	Body iface.UserManagementResponse `json:"user" doc:"Created user data"`
}

// CreateUser handles POST /api/users. The role-escalation guard from
// UpdateUser applies symmetrically here — an administrator can't seed
// a fresh super_admin via the create path either, and the caller's own
// role for that comparison is read from the database (see callerRole).
func (h *UserHandler) CreateUser(ctx context.Context, req *CreateUserRequest) (*CreateUserResponse, error) {
	actorUUID, actorEmail := actorFromCtx(ctx)
	if req.Body.Role != "" {
		callerRole, err := h.callerRole(ctx)
		if err != nil {
			// Every other refusal on this path lands an audit row, and
			// SOC2 reads that trail to tell one denial from another. A
			// 500 is still a denied privileged mutation: record it with
			// its own code so "we refused you" and "we could not tell"
			// are distinguishable after the fact.
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.create.refused",
				ResourceType: "user",
				Outcome:      "denied",
				Metadata: map[string]any{
					"code":      errcode.UserRoleLookupUnavailable,
					"attempted": "role_assignment",
					"to":        req.Body.Role,
					"email":     req.Body.Email,
				},
			})
			return nil, err
		}
		if !canAssignRole(callerRole, req.Body.Role) {
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.create.refused",
				ResourceType: "user",
				Outcome:      "denied",
				Metadata: map[string]any{
					"code":      errcode.UserRoleEscalationForbidden,
					"attempted": "role_escalation",
					"to":        req.Body.Role,
					"email":     req.Body.Email,
				},
			})
			return nil, errcode.Forbidden(errcode.UserRoleEscalationForbidden,
				"You cannot create a user with a role higher than your own")
		}
	}

	user, err := h.userService.CreateUser(ctx, &req.Body)
	if err != nil {
		switch err {
		case services.ErrEmailNotUnique:
			return nil, errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid input data", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to create user", err)
		}
	}

	return &CreateUserResponse{Body: *user}, nil
}

// Get User Request
type GetUserRequest struct {
	ID string `path:"id" doc:"User ID (UUID)"`
}

// Get User Response
type GetUserResponse struct {
	Body iface.UserManagementResponse `json:"user" doc:"User data"`
}

// GetUser handles GET /api/users/{id}
func (h *UserHandler) GetUser(ctx context.Context, req *GetUserRequest) (*GetUserResponse, error) {
	user, err := h.userService.GetUser(ctx, req.ID)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid user ID", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to get user", err)
		}
	}

	return &GetUserResponse{Body: *user}, nil
}

// Update User Request
type UpdateUserRequest struct {
	ID   string                `path:"id" doc:"User ID (UUID)"`
	Body iface.UpdateUserInput `json:"user" doc:"User data to update"`
}

// Update User Response
type UpdateUserResponse struct {
	Body iface.UserManagementResponse `json:"user" doc:"Updated user data"`
}

// UpdateUser handles PUT /api/users/{id}. Three independent guards
// protect privileged state from being mutated by an under-privileged
// caller: (1) **role escalation** — the caller's own system role, read
// from the database rather than the `srole` claim (D28), must
// be at least as high in the tier ladder as any role they assign
// (super_admin > administrator > developer > manager > operator >
// guest); the cascade rule on authz.CreateBinding does not cover the
// User.Role field directly, so this is the user module's own guard
// (403 user.role_escalation_forbidden). (2) **last-administrator** —
// a deactivation or demotion that would leave zero active platform
// administrators is refused (403 user.last_admin_forbidden). (3)
// Self-target is allowed for role/active patches *except* role
// escalation against oneself, which gets the role-escalation gate.
// Successful patches emit user.activated / user.deactivated /
// user.role.changed; refused patches emit user.update.refused so the
// SOC2 trail sees both successes and denials.
func (h *UserHandler) UpdateUser(ctx context.Context, req *UpdateUserRequest) (*UpdateUserResponse, error) {
	actorUUID, actorEmail := actorFromCtx(ctx)

	// Snapshot the pre-change state so we can compute lifecycle deltas
	// after a successful update AND so the role-escalation guard can
	// compare the target's existing role against the caller's. On a
	// patch that carries no Role a read failure here stays non-fatal —
	// downstream UpdateUser will surface a clean 404 / 500, and nothing
	// security-relevant hangs off the snapshot. On a Role patch it is
	// fatal: see the guard below.
	previous, previousErr := h.userService.GetUser(ctx, req.ID)

	// Role-escalation guard. Order matters: this fires *before* the
	// last-admin check so a denied promotion to super_admin doesn't
	// also report a misleading quorum failure.
	if req.Body.Role != "" {
		// Fail closed when the target's current role is unreadable.
		// `previous` is what decides whether the role actually CHANGED,
		// and only a change retires the authz cache and ends the
		// sessions minted under the old role (see
		// emitUpdateLifecycleEvents). A transient read failure that
		// still let the write through would therefore apply the new
		// role while silently skipping both — leaving the old role's
		// cached verdicts and access tokens live, which is the hole
		// this PR closes (M-13). Refuse rather than write blind.
		//
		// "No such user" is deliberately NOT that case: nothing will be
		// written, so no effect can be lost, and the request falls
		// through to UpdateUser's clean 404.
		if previousErr != nil && !errors.Is(previousErr, services.ErrUserNotFound) {
			slog.ErrorContext(ctx, "user: refusing a role change, the target's current role could not be read",
				slog.String("user_uuid", req.ID),
				slog.String("error", previousErr.Error()))
			return nil, errcode.Internal(errcode.UserRoleLookupUnavailable,
				"Could not read the user's current role. Retry shortly.")
		}
		// Target's current role for the denied-event metadata. Empty
		// when previous is nil (the user does not exist); the
		// downstream UpdateUser will then surface 404 cleanly.
		previousRole := ""
		if previous != nil {
			previousRole = previous.Role
		}
		callerRole, err := h.callerRole(ctx)
		if err != nil {
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.update.refused",
				ResourceType: "user",
				ResourceID:   req.ID,
				Outcome:      "denied",
				Metadata: map[string]any{
					"code":      errcode.UserRoleLookupUnavailable,
					"attempted": "role_assignment",
					"from":      previousRole,
					"to":        req.Body.Role,
				},
			})
			return nil, err
		}
		if !canAssignRole(callerRole, req.Body.Role) {
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.update.refused",
				ResourceType: "user",
				ResourceID:   req.ID,
				Outcome:      "denied",
				Metadata: map[string]any{
					"code":      errcode.UserRoleEscalationForbidden,
					"attempted": "role_escalation",
					"from":      previousRole,
					"to":        req.Body.Role,
				},
			})
			return nil, errcode.Forbidden(errcode.UserRoleEscalationForbidden,
				"You cannot assign a role higher than your own")
		}
		// Privileged-role guard for service accounts. Fails closed when the
		// target's identity cannot be read: a transient GetUser error must not
		// let a privileged role land on a service account. Non-privileged
		// assignments with a nil pre-read fall through — downstream UpdateUser
		// surfaces the 404/500 cleanly.
		if (previous == nil && isPrivilegedSystemRole(req.Body.Role)) || (previous != nil && !serviceAccountRoleAllowed(previous.Kind, req.Body.Role)) {
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.update.refused",
				ResourceType: "user",
				ResourceID:   req.ID,
				Outcome:      "denied",
				Metadata: map[string]any{
					"code":      errcode.UserRoleEscalationForbidden,
					"attempted": "service_account_privileged_role",
					"to":        req.Body.Role,
				},
			})
			return nil, errcode.Forbidden(errcode.UserRoleEscalationForbidden,
				"Service accounts cannot hold privileged system roles")
		}
	}

	if removesAdminPrivilege(&req.Body) {
		if err := h.checkLastAdminRemoval(ctx, req.ID); err != nil {
			if isLastAdminError(err) {
				h.emitAudit(ctx, iface.AuditEvent{
					ActorUserID:  actorUUID,
					ActorEmail:   actorEmail,
					ActorType:    "user",
					Action:       "user.update.refused",
					ResourceType: "user",
					ResourceID:   req.ID,
					Outcome:      "denied",
					Metadata:     updateRefusalMetadata(&req.Body),
				})
			}
			return nil, err
		}
	}
	user, err := h.userService.UpdateUser(ctx, req.ID, &req.Body)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrEmailNotUnique:
			return nil, errcode.Conflict(errcode.AuthEmailInUse, "Email already in use")
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid input data", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to update user", err)
		}
	}

	h.emitUpdateLifecycleEvents(ctx, actorUUID, actorEmail, previous, user, &req.Body)

	return &UpdateUserResponse{Body: *user}, nil
}

// emitUpdateLifecycleEvents compares the pre-update snapshot to the
// post-update result and, for each distinct lifecycle delta, carries out
// whatever that delta obliges beyond the write and emits one audit
// event. isActive flip → user.activated / user.deactivated, and a
// deactivation ends the user's sessions. Role change to a value other
// than the prior one → the authz cache is retired, the sessions minted
// under the old role are ended, and user.role.changed records
// before/after plus whether each of those two actually succeeded.
// Profile-only patches (name, phone, etc.) don't get a dedicated event
// today — they roll up under "no audit event" by design; revisit when the
// operator UI grows a way to view generic profile-edit history.
//
// Everything here runs AFTER the write has landed and none of it can
// fail the request: see invalidateAuthz for why a role change is never
// refused in either direction (spec §4.6 D27 as amended, closing M-13).
//
// Only "after the write" is invariant. Within that, the branches run in
// patch order, so a role-ONLY patch goes write → invalidate → terminate
// → audit, while a patch that ALSO deactivates ends the sessions in the
// isActive branch first (write → terminate → invalidate → audit). Both
// are correct — the two effects are independent, and endSessions is
// memoised so the teardown happens once either way.
func (h *UserHandler) emitUpdateLifecycleEvents(
	ctx context.Context,
	actorUUID, actorEmail string,
	previous *iface.UserManagementResponse,
	current *iface.UserManagementResponse,
	patch *iface.UpdateUserInput,
) {
	if current == nil {
		return
	}
	// endSessions is memoised so a patch that both deactivates and
	// changes the role tears the sessions down once, and the audit row
	// reports the outcome of that one attempt rather than a second.
	var terminated *bool
	endSessions := func() bool {
		if terminated == nil {
			ok := h.terminateSessionsReporting(ctx, current.ID)
			terminated = &ok
		}
		return *terminated
	}

	if patch.IsActive != nil && (previous == nil || previous.IsActive != *patch.IsActive) {
		action := "user.activated"
		if !*patch.IsActive {
			action = "user.deactivated"
			// Revoking the right to be signed in has to end the
			// sessions that right produced — otherwise the account is
			// "disabled" while its live bearers keep working.
			endSessions()
		}
		h.emitAudit(ctx, iface.AuditEvent{
			ActorUserID:  actorUUID,
			ActorEmail:   actorEmail,
			ActorType:    "user",
			Action:       action,
			ResourceType: "user",
			ResourceID:   current.ID,
			Outcome:      "success",
		})
	}
	if patch.Role != "" && previous != nil && previous.Role != patch.Role {
		// M-13. The new role is in the database, but two caches still
		// answer with the old one: the authz permission cache (up to its
		// own TTL) and the `srole` claim in every live access token (up
		// to a whole token lifetime). Retire the first, end the sessions
		// that carry the second.
		cacheInvalidated := true
		if err := h.invalidateAuthz(ctx, current.ID); err != nil {
			cacheInvalidated = false
			if errors.Is(err, errAuthzInvalidatorUnwired) {
				// Nothing was retired because there is nothing to
				// retire — no permission cache exists to hold a verdict
				// from the old role. Not a degraded request, so it does
				// not move the failure counter and must not claim stale
				// verdicts survive.
				slog.WarnContext(ctx, "user: role changed with no authz cache invalidator wired; there is no cached verdict to retire",
					slog.String("user_uuid", current.ID),
					slog.String("error", err.Error()))
			} else {
				metrics.Default().RecordAuthzCacheInvalidationFailure()
				slog.ErrorContext(ctx, "user: role changed but the authz cache was not retired; verdicts from the old role may survive up to the cache TTL",
					slog.String("user_uuid", current.ID),
					slog.String("from", previous.Role),
					slog.String("to", patch.Role),
					slog.String("error", err.Error()))
			}
		}
		// Terminating on every change rather than on demotions alone
		// keeps one invariant instead of two code paths, and makes a
		// promotion visible immediately instead of at the next refresh.
		sessionsTerminated := endSessions()
		h.emitAudit(ctx, iface.AuditEvent{
			ActorUserID:  actorUUID,
			ActorEmail:   actorEmail,
			ActorType:    "user",
			Action:       "user.role.changed",
			ResourceType: "user",
			ResourceID:   current.ID,
			Outcome:      "success",
			Metadata: map[string]any{
				"from": previous.Role,
				"to":   patch.Role,
				// The two flags are what makes "the change is live now"
				// distinguishable from "the change is live within the
				// cache TTL / the access-token lifetime" after the fact.
				"cache_invalidated":   cacheInvalidated,
				"sessions_terminated": sessionsTerminated,
			},
		})
	}
}

// updateRefusalMetadata captures which protected field the rejected
// update was trying to change, so the SOC2 view can tell a deactivate
// attempt from a role-demote attempt. Both are denied with the same
// last_admin_forbidden code but the operator intent differed.
func updateRefusalMetadata(input *iface.UpdateUserInput) map[string]any {
	meta := map[string]any{"code": errcode.UserLastAdminForbidden}
	if input == nil {
		return meta
	}
	if input.IsActive != nil && !*input.IsActive {
		meta["attempted"] = "deactivate"
	} else if input.Role != "" {
		meta["attempted"] = "role_change"
		meta["to"] = input.Role
	}
	return meta
}

// Delete User Request
type DeleteUserRequest struct {
	ID string `path:"id" doc:"User ID (UUID)"`
}

// Delete User Response
type DeleteUserResponse struct {
	Body struct {
		Message string `json:"message" doc:"Success message"`
	}
}

// DeleteUser handles DELETE /api/users/{id}. Soft-deletes via the email-
// aliasing path so the unique index releases the original address — see
// services.UserService.SoftDeleteAndAliasEmail. Guards: callers can never
// delete themselves (403 user.self_delete_forbidden); the request is also
// refused when it would leave zero live, active platform administrators
// (403 user.last_admin_forbidden).
func (h *UserHandler) DeleteUser(ctx context.Context, req *DeleteUserRequest) (*DeleteUserResponse, error) {
	actorUUID, actorEmail := actorFromCtx(ctx)
	if actorUUID != "" && actorUUID == req.ID {
		// Self-delete refused — emit the denied event so SOC2 sees the
		// attempt. Metadata carries the wire code so dashboards can
		// distinguish self-delete from last-admin refusals.
		h.emitAudit(ctx, iface.AuditEvent{
			ActorUserID:  actorUUID,
			ActorEmail:   actorEmail,
			ActorType:    "user",
			Action:       "user.delete.refused",
			ResourceType: "user",
			ResourceID:   req.ID,
			Outcome:      "denied",
			Metadata:     map[string]any{"code": errcode.UserSelfDeleteForbidden},
		})
		return nil, errcode.Forbidden(errcode.UserSelfDeleteForbidden, "You cannot delete your own account")
	}
	if err := h.checkLastAdminRemoval(ctx, req.ID); err != nil {
		if isLastAdminError(err) {
			h.emitAudit(ctx, iface.AuditEvent{
				ActorUserID:  actorUUID,
				ActorEmail:   actorEmail,
				ActorType:    "user",
				Action:       "user.delete.refused",
				ResourceType: "user",
				ResourceID:   req.ID,
				Outcome:      "denied",
				Metadata:     map[string]any{"code": errcode.UserLastAdminForbidden},
			})
		}
		return nil, err
	}
	if err := h.userService.SoftDeleteAndAliasEmail(ctx, req.ID); err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid user ID", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to delete user", err)
		}
	}

	// A soft-deleted user is gone from every lookup the auth flows use,
	// so their refresh attempts already fail — but their in-flight access
	// tokens would otherwise run out their TTL. End the sessions now.
	h.terminateSessions(ctx, req.ID)

	h.emitAudit(ctx, iface.AuditEvent{
		ActorUserID:  actorUUID,
		ActorEmail:   actorEmail,
		ActorType:    "user",
		Action:       "user.deleted",
		ResourceType: "user",
		ResourceID:   req.ID,
		Outcome:      "success",
	})

	return &DeleteUserResponse{
		Body: struct {
			Message string `json:"message" doc:"Success message"`
		}{
			Message: "User deleted successfully",
		},
	}, nil
}

// isLastAdminError is true when err is the last-administrator guard's
// 403. The guard returns either nil, a generic 500, or this specific
// Forbidden envelope — we discriminate by the wire code so a transient
// quorum-count failure (500) doesn't masquerade as a denied event.
func isLastAdminError(err error) bool {
	if err == nil {
		return false
	}
	if ec, ok := err.(*errcode.Error); ok {
		return ec.Code == errcode.UserLastAdminForbidden
	}
	return false
}

// checkLastAdminRemoval refuses the operation when removing the target
// user from the platform-administrator pool would leave zero active
// administrators. The check is best-effort under concurrent edits — a
// follow-up could promote it to a Mongo transaction. Returns nil when the
// target isn't currently an active administrator (nothing to protect).
func (h *UserHandler) checkLastAdminRemoval(ctx context.Context, targetID string) error {
	target, err := h.userService.GetUser(ctx, targetID)
	if err != nil {
		// If the lookup fails, defer the error to the calling mutation —
		// it will surface a clean 404 / 400 / 500 via its own switch.
		return nil
	}
	if !target.IsActive {
		return nil
	}
	if target.Role != "super_admin" && target.Role != "administrator" {
		return nil
	}
	remaining, err := h.userService.CountActiveAdministrators(ctx, targetID)
	if err != nil {
		return huma.Error500InternalServerError("Failed to verify administrator quorum", err)
	}
	if remaining > 0 {
		return nil
	}
	return errcode.Forbidden(errcode.UserLastAdminForbidden, "Refusing to remove the last active administrator")
}

// devTokenUUIDPrefix is the `sub` prefix POST /dev/token stamps on the
// synthetic principals it mints. Pinned by value to devtoken's own format
// string; a real user UUID never carries it (they come from
// uuid.NewString()).
const devTokenUUIDPrefix = "dev-"

// devTokenSystemRoles is the set of roles a synthetic dev-token principal
// may claim. Pinned by value to the set the authz evaluator's own
// dev-token fallback accepts (internal/core/authz/module.go validDevRoles)
// and to the six names systemRoleTier ranks.
var devTokenSystemRoles = map[string]struct{}{
	"super_admin": {}, "administrator": {}, "developer": {},
	"manager": {}, "operator": {}, "guest": {},
}

// devTokenSystemRole answers the caller's role for a SYNTHETIC dev-token
// principal — the one identity that legitimately has no database row.
//
// POST /dev/token mints `sub = dev-<role>-<unix>` without writing a user,
// and that token is the documented local flow (the root CLAUDE.md Quick
// Start, scripts/devtoken.sh). Resolving its role from the store misses by
// construction, so D28's "a lookup miss is a 500" would take every
// dev-token role assignment down.
//
// This is the carve-out the authz evaluator already makes for the same
// identities (internal/core/authz/module.go, the UserSystemRoleLookup
// closure), guarded the same way — THREE conditions, all of which must
// hold:
//
//  1. the deployment is not production-like. Deliberately STRICTER than
//     authz's IsProduction(): staging is internet-reachable, which is why
//     POST /dev/token is itself gated on IsProductionLike(). A nil platform
//     counts as production-like, so the exception is opt-in wiring, never a
//     default;
//  2. the UUID carries the `dev-` prefix the dev-token endpoint stamps;
//  3. the `srole` claim names one of the six real system roles.
//
// Guard 1 is what makes trusting the claim in 3 safe: on staging and in
// production this cannot return true whatever the token says, so no
// reachable deployment lets a claim decide a role assignment.
func devTokenSystemRole(ctx context.Context, platform module.PlatformInfo, actorUUID string) (string, bool) {
	if platform == nil || platform.IsProductionLike() {
		return "", false
	}
	if !strings.HasPrefix(actorUUID, devTokenUUIDPrefix) {
		return "", false
	}
	role, ok := ctxauth.GetSystemRole(ctx)
	if !ok {
		return "", false
	}
	if _, valid := devTokenSystemRoles[role]; !valid {
		return "", false
	}
	return role, true
}

// callerRole resolves the calling operator's system role from the
// DATABASE — never from the `srole` JWT claim.
//
// The claim can be up to one access-token lifetime stale. That window is
// exactly what the role-change propagation closes (emitUpdateLifecycleEvents
// retires the authz cache and ends the sessions minted under the old
// role), so reading `srole` in the guard that decides whether a caller may
// assign a role would put the same hole straight back: a demoted
// administrator would keep minting administrators until their last access
// token expired. Spec §4.6 D28.
//
// Outcomes, all fail-closed:
//
//   - No authenticated principal on the context (a degraded gate — these
//     routes all sit behind RequireSystemPermission). There is no identity
//     to resolve a role for, so the role is empty and canAssignRole's
//     unknown tier (-1) refuses every assignment. That is a refusal of the
//     caller's request, not a report of a broken database.
//   - A synthetic dev-token principal in a non-production-like deployment
//     resolves from the claim — see devTokenSystemRole for the three guards
//     that make that safe and why nothing on staging or production can take
//     the branch.
//   - The row is absent, or the read failed. Both are a 500
//     (user.role_lookup_unavailable), NEVER a fallback to the claim:
//     falling back would make the claim authoritative again exactly when
//     the database cannot contradict it. The two are DISTINGUISHED in the
//     log — services.ErrUserNotFound is a true alias of
//     iface.ErrUserNotFound and arrives unwrapped, so the handler can tell
//     them apart — because they need different operator responses: a
//     missing row is an identity defect, a failed read is an outage. They
//     get the same wire answer because neither is evidence of any role.
//
// Only called on the guarded path (an assignment that actually names a
// role), so an ordinary profile patch costs no extra read. GetUserByID
// rather than GetUser: the guard needs one field, and GetUser additionally
// runs the OAuth-link enrichment — a second collection read — to build a
// response DTO this path throws away.
func (h *UserHandler) callerRole(ctx context.Context) (string, error) {
	actorUUID, _ := ctxauth.GetUserUUID(ctx)
	if actorUUID == "" {
		return "", nil
	}
	if role, ok := devTokenSystemRole(ctx, h.platform, actorUUID); ok {
		return role, nil
	}
	actor, err := h.userService.GetUserByID(ctx, actorUUID)
	switch {
	case errors.Is(err, services.ErrUserNotFound) || (err == nil && actor == nil):
		slog.ErrorContext(ctx, "user: the calling user has no row; refusing the role assignment",
			slog.String("actor_uuid", actorUUID))
		return "", roleLookupUnavailable()
	case err != nil:
		slog.ErrorContext(ctx, "user: could not read the calling user's system role; refusing the role assignment",
			slog.String("actor_uuid", actorUUID),
			slog.String("error", err.Error()))
		return "", roleLookupUnavailable()
	}
	return actor.Role, nil
}

// systemRoleTier ranks the six platform system roles from highest
// (super_admin = 5) to lowest (guest = 0). canAssignRole compares the
// caller's tier to the requested role's tier so an administrator
// cannot promote anyone (including themselves) to super_admin, a
// developer cannot promote to administrator, and so on. Unknown role
// names (custom roles, typos) map to -1, which higher tier zero
// rejects — caller must be at least operator/0 to assign any
// recognised role, which already requires `system.users.admin`.
//
// This is the User.Role-field counterpart of the authz cascade rule
// on CreateBinding (services.go:1137). The cascade rule applies only
// to bindings; this guard plugs the matching invariant on direct
// User.Role mutation.
func systemRoleTier(role string) int {
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

// canAssignRole reports whether a caller holding callerRole may assign
// targetRole. Equal-tier assignments are allowed (an administrator can
// assign another user to administrator) — the prohibition is only on
// strict elevation. An unknown caller tier (-1) refuses every
// assignment, which is what makes the empty role callerRole returns for
// an unidentifiable principal fail closed.
//
// callerRole is the value the DATABASE holds for the caller — see
// (*UserHandler).callerRole. Never pass the `srole` claim here.
func canAssignRole(callerRole, targetRole string) bool {
	caller := systemRoleTier(callerRole)
	target := systemRoleTier(targetRole)
	if caller < 0 || target < 0 {
		return false
	}
	return caller >= target
}

// isPrivilegedSystemRole reports whether a role is a privileged system role
// that machine principals must never hold.
func isPrivilegedSystemRole(role string) bool {
	return role == "super_admin" || role == "administrator"
}

// serviceAccountRoleAllowed refuses privileged system roles for machine
// principals: a service account must never hold super_admin or
// administrator, regardless of the granter's own tier.
func serviceAccountRoleAllowed(kind, role string) bool {
	if kind != iface.UserKindService {
		return true
	}
	return !isPrivilegedSystemRole(role)
}

// removesAdminPrivilege reports whether the given update would, if
// applied, take a user out of the platform-administrator pool. Either
// flipping isActive to false or assigning a non-privileged role
// qualifies; the check is intentionally over-eager — checkLastAdminRemoval
// re-reads the row and short-circuits when the target wasn't an active
// administrator to begin with.
func removesAdminPrivilege(input *iface.UpdateUserInput) bool {
	if input == nil {
		return false
	}
	if input.IsActive != nil && !*input.IsActive {
		return true
	}
	if input.Role != "" && input.Role != "super_admin" && input.Role != "administrator" {
		return true
	}
	return false
}

// List Users Request
type ListUsersRequest struct {
	// Query parameters for filtering
	Role          string `query:"role" doc:"Filter by user role"`
	IsActive      bool   `query:"isActive" doc:"Filter by active status"`
	EmailVerified bool   `query:"emailVerified" doc:"Filter by email verification status"`
	Search        string `query:"search" doc:"Search in name, email, username"`

	// Pagination parameters
	Page     int `query:"page" default:"1" minimum:"1" doc:"Page number"`
	PageSize int `query:"pageSize" default:"10" minimum:"1" maximum:"100" doc:"Number of items per page"`
}

// List Users Response
type ListUsersResponse struct {
	Body iface.UserManagementListResponse `json:"users" doc:"Paginated list of users"`
}

// ListUsers handles GET /api/users
func (h *UserHandler) ListUsers(ctx context.Context, req *ListUsersRequest) (*ListUsersResponse, error) {
	filters := &iface.UserFilters{
		Role:   req.Role,
		Search: req.Search,
	}

	// Handle optional boolean flags - only set if provided
	if req.IsActive {
		filters.IsActive = &req.IsActive
	}
	if req.EmailVerified {
		filters.EmailVerified = &req.EmailVerified
	}

	pagination := &iface.PaginationParams{
		Page:     req.Page,
		PageSize: req.PageSize,
	}

	users, err := h.userService.ListUsers(ctx, filters, pagination)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list users", err)
	}

	return &ListUsersResponse{Body: *users}, nil
}

// Get Users by Role Request
type GetUsersByRoleRequest struct {
	Role string `path:"role" doc:"User role to filter by"`
}

// Get Users by Role Response
type GetUsersByRoleResponse struct {
	Body struct {
		Users []iface.UserManagementResponse `json:"users" doc:"List of users with the specified role"`
		Total int                            `json:"total" doc:"Total number of users"`
	}
}

// GetUsersByRole handles GET /api/users/role/{role}
func (h *UserHandler) GetUsersByRole(ctx context.Context, req *GetUsersByRoleRequest) (*GetUsersByRoleResponse, error) {
	users, err := h.userService.GetUsersByRole(ctx, req.Role)
	if err != nil {
		switch err {
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid role", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to get users by role", err)
		}
	}

	// Convert to response format
	userResponses := make([]iface.UserManagementResponse, len(users))
	for i, user := range users {
		userResponses[i] = *user
	}

	return &GetUsersByRoleResponse{
		Body: struct {
			Users []iface.UserManagementResponse `json:"users" doc:"List of users with the specified role"`
			Total int                            `json:"total" doc:"Total number of users"`
		}{
			Users: userResponses,
			Total: len(userResponses),
		},
	}, nil
}

// Get User by Email Request
type GetUserByEmailRequest struct {
	Email string `query:"email" doc:"User email address"`
}

// GetUserByEmail handles GET /api/users/by-email
func (h *UserHandler) GetUserByEmail(ctx context.Context, req *GetUserByEmailRequest) (*GetUserResponse, error) {
	if req.Email == "" {
		return nil, huma.Error400BadRequest("Email parameter is required", nil)
	}

	user, err := h.userService.GetUserByEmail(ctx, req.Email)
	if err != nil {
		switch err {
		case services.ErrUserNotFound:
			return nil, huma.Error404NotFound("User not found", err)
		case services.ErrInvalidInput:
			return nil, huma.Error400BadRequest("Invalid email", err)
		default:
			return nil, huma.Error500InternalServerError("Failed to get user", err)
		}
	}

	return &GetUserResponse{Body: *user}, nil
}

// Get User Count Request
type GetUserCountRequest struct {
	// Query parameters for filtering (same as ListUsersRequest)
	Role          string `query:"role" doc:"Filter by user role"`
	IsActive      bool   `query:"isActive" doc:"Filter by active status"`
	EmailVerified bool   `query:"emailVerified" doc:"Filter by email verification status"`
	Search        string `query:"search" doc:"Search in name, email, username"`
}

// Get User Count Response
type GetUserCountResponse struct {
	Body struct {
		Count int64 `json:"count" doc:"Total number of users matching the filters"`
	}
}

// GetUserCount handles GET /api/users/count
func (h *UserHandler) GetUserCount(ctx context.Context, req *GetUserCountRequest) (*GetUserCountResponse, error) {
	filters := &iface.UserFilters{
		Role:   req.Role,
		Search: req.Search,
	}

	// Handle optional boolean flags - only set if provided
	if req.IsActive {
		filters.IsActive = &req.IsActive
	}
	if req.EmailVerified {
		filters.EmailVerified = &req.EmailVerified
	}

	count, err := h.userService.GetUserCount(ctx, filters)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to count users", err)
	}

	return &GetUserCountResponse{
		Body: struct {
			Count int64 `json:"count" doc:"Total number of users matching the filters"`
		}{
			Count: count,
		},
	}, nil
}
