package services

import (
	"context"
	"errors"
	"strings"

	"github.com/orkestra/backend/internal/core/authz/models"
)

var (
	// ErrRolePermissionsRequired — a supplied permission list that is
	// empty, or empty after trimming. Distinct from "not supplied": an
	// UpdateRoleInput with a nil Permissions field is a patch that does
	// not touch permissions at all, and is not validated.
	ErrRolePermissionsRequired = errors.New("authz: permissions cannot be empty")

	// ErrUnknownPermission — a key that is not in the registered
	// catalog. The catalog is the union of every module's Permissions()
	// registered once at boot; a key outside it can never be checked by
	// anything, so writing it into a role is at best dead weight and at
	// worst a typo hiding a real grant. Wrapped errors carry the
	// offending key — read it with OffendingPermissionKey.
	ErrUnknownPermission = errors.New("authz: unknown permission")

	// ErrSystemPermissionInCustomRole — a platform-reserved key, or the
	// wildcard, in a custom (tenant) role. Platform permissions are
	// granted through platform system roles and global bindings; a
	// tenant role that carried one would be an escalation path out of
	// its own tenant. Wrapped errors carry the offending key.
	ErrSystemPermissionInCustomRole = errors.New("authz: system permissions cannot be granted through a custom role")

	// ErrRoleNameRequired — an empty or whitespace-only role name.
	// Replaces the bare errors.New path that surfaced as a 500.
	ErrRoleNameRequired = errors.New("authz: name cannot be empty")
)

// ErrGranterRequired (declared with the binding sentinels in service.go)
// is reused here for a role write with no actor: without a known actor
// the cascade cannot be evaluated, and defaulting to "allow" is exactly
// the hole D21 closes.

// maxEchoedPermissionKey bounds the offending key an error carries. A
// permission key arrives in the request body and the role schema puts no
// length limit on one, so the key travels back out through the handler's
// response detail — cap it rather than mirroring an arbitrary payload.
const maxEchoedPermissionKey = 100

// permissionKeyError names the permission key that caused one of the
// sentinels above. Handlers read the key through OffendingPermissionKey
// instead of parsing an error string, so the message stays free to
// change.
type permissionKeyError struct {
	sentinel error
	key      string
}

func (e *permissionKeyError) Error() string { return e.sentinel.Error() + ": " + e.key }
func (e *permissionKeyError) Unwrap() error { return e.sentinel }

// permissionKeyErrorf wraps sentinel with the offending key, truncated to
// maxEchoedPermissionKey.
func permissionKeyErrorf(sentinel error, key string) error {
	if len(key) > maxEchoedPermissionKey {
		key = key[:maxEchoedPermissionKey] + "…"
	}
	return &permissionKeyError{sentinel: sentinel, key: key}
}

// OffendingPermissionKey returns the permission key that caused err, when
// err came from custom-role permission validation (ErrUnknownPermission
// or ErrSystemPermissionInCustomRole). The key is caller-supplied input,
// already truncated to a bounded length, so it is safe to put in a
// response detail. Returns ok=false for every other error, including nil.
func OffendingPermissionKey(err error) (string, bool) {
	var pk *permissionKeyError
	if errors.As(err, &pk) {
		return pk.key, true
	}
	return "", false
}

// IsReservedActor reports whether actor is the platform sentinel that
// waives the D21 cascade (the literal "system"). Handlers use it to
// refuse a request whose authenticated subject spells the sentinel, so
// the waiver can only ever be chosen by in-process code — never by a
// token. Not reachable today (subjects are uuid.NewString()), but the
// sentinel must not be spellable from outside.
func IsReservedActor(actor string) bool { return actor == granterSystem }

// classifyPermissionKeys reports the first key outside the registered
// catalog and the first platform-reserved key (or the wildcard), under a
// single read lock so the caller never holds s.mu across a repo call.
// The wildcard is skipped by the catalog pass because it is never a
// registered key; the platform pass refuses it.
//
// Both passes run because the checks are ordered, not exclusive: an
// unknown key must be reported as unknown rather than as forbidden.
func (s *Service) classifyPermissionKeys(keys []string) (unknown, reserved string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, k := range keys {
		if k == "*" {
			continue
		}
		if _, ok := s.allPermissionSet[k]; !ok {
			unknown = k
			break
		}
	}
	for _, k := range keys {
		if k == "*" {
			reserved = k
			break
		}
		if _, ok := s.systemPermissionSet[k]; ok {
			reserved = k
			break
		}
	}
	return unknown, reserved
}

// validateCustomRolePermissions is the single gate every supplied
// permission list passes, on create and on update alike.
//
// H-4: UpdateRole replaced `permissions` verbatim, so a caller who could
// edit a custom role could write any string into it — a platform key, a
// permission nobody holds, the wildcard — and then bind themselves to
// it. Bindings have had a cascade check since the org-role split; roles
// did not, which made the role editor the way around it.
//
// Four checks, in this order, because each explains a different refusal:
//
//  1. non-empty after trim and de-duplication;
//  2. every key exists in the registered catalog;
//  3. no key is "*" or a platform-reserved key — this one binds even a
//     super_admin (edge case 14): the wildcard is about what the ACTOR
//     may grant, not about what a TENANT role may carry;
//  4. the cascade: the actor must already hold every key, unless the
//     actor is the literal "system" (platform-issued writes), which
//     bypasses ONLY this step.
//
// Check 2 leans on the in-memory catalog RegisterPermissions populates
// once at boot from the union of every module's Permissions() (and that
// ensureSeeded replays after a live DB wipe). An empty catalog would make
// every key look unknown, so it is asserted directly by
// TestAllPermissionSet_IsPopulatedAfterRegisterPermissions — the gate
// must fail closed there, never open.
//
// Returns the cleaned list to persist.
func (s *Service) validateCustomRolePermissions(ctx context.Context, tenantID, actor string, keys []string) ([]string, error) {
	cleaned := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		cleaned = append(cleaned, k)
	}
	if len(cleaned) == 0 {
		return nil, ErrRolePermissionsRequired
	}

	unknown, reserved := s.classifyPermissionKeys(cleaned)
	if unknown != "" {
		return nil, permissionKeyErrorf(ErrUnknownPermission, unknown)
	}
	if reserved != "" {
		return nil, permissionKeyErrorf(ErrSystemPermissionInCustomRole, reserved)
	}

	if actor == "" {
		return nil, ErrGranterRequired
	}
	if actor == granterSystem {
		return cleaned, nil
	}

	granterPerms, err := s.GetEffectivePermissions(ctx, actor, tenantID)
	if err != nil {
		return nil, err
	}
	// The same pure helper CreateBinding uses, so a role and a binding
	// cannot drift on what "the caller already holds" means.
	if err := validateBindingCascade(&models.Role{Permissions: cleaned}, granterPerms); err != nil {
		return nil, err
	}
	return cleaned, nil
}
