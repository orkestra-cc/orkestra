package services

// The user module's not-found sentinel must be classifiable from OUTSIDE the
// module. internal/core/auth/services cannot import this package (root
// CLAUDE.md forbids cross-module service imports), but it must distinguish
// "this account is gone" (a terminal 401 on the refresh path) from "the store
// is unreachable" (a 503). Aliasing the module sentinel to the SDK one is what
// makes errors.Is work across that boundary without an import.

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestErrUserNotFound_IsTheSDKSentinel(t *testing.T) {
	if !errors.Is(ErrUserNotFound, iface.ErrUserNotFound) {
		t.Fatalf("errors.Is(ErrUserNotFound, iface.ErrUserNotFound) = false — a consumer outside this module cannot classify a deleted account, and §4.9 would answer 503 forever for one")
	}
}

// The message must not change: it is asserted verbatim by existing callers and
// appears in operator-facing logs.
func TestErrUserNotFound_MessageUnchanged(t *testing.T) {
	if got := ErrUserNotFound.Error(); got != "user not found" {
		t.Fatalf("ErrUserNotFound.Error() = %q, want %q", got, "user not found")
	}
}
