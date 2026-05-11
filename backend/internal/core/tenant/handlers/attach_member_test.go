package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

// TestAttachMemberAdmin_RejectsEmptyRole pins the handler-level validation
// path: a missing role string must 400 before any service or registry call,
// so an admin who forgets to fill the dropdown gets a clean error rather
// than a 500 from a downstream nil-deref.
func TestAttachMemberAdmin_RejectsEmptyRole(t *testing.T) {
	h := New(nil, nil)
	in := &attachMemberAdminInput{TenantID: "tenant-1"}
	in.Body.UserUUID = "user-1"
	// Role left blank.
	_, err := h.attachMemberAdmin(context.Background(), in)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	se, ok := err.(huma.StatusError)
	if !ok {
		t.Fatalf("expected huma.StatusError, got %T (%v)", err, err)
	}
	if got := se.GetStatus(); got != 400 {
		t.Fatalf("status = %d, want 400", got)
	}
	if !strings.Contains(strings.ToLower(se.Error()), "role") {
		t.Fatalf("error message should mention role: %s", se.Error())
	}
}

// TestAttachMemberAdmin_RejectsMissingUserSelector covers the second pre-svc
// guard: the admin must supply either userUuid or userEmail.
func TestAttachMemberAdmin_RejectsMissingUserSelector(t *testing.T) {
	h := New(nil, nil)
	in := &attachMemberAdminInput{TenantID: "tenant-1"}
	in.Body.Role = "org_member"
	// Both UserUUID and UserEmail left blank.
	_, err := h.attachMemberAdmin(context.Background(), in)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	se, ok := err.(huma.StatusError)
	if !ok {
		t.Fatalf("expected huma.StatusError, got %T (%v)", err, err)
	}
	if got := se.GetStatus(); got != 400 {
		t.Fatalf("status = %d, want 400", got)
	}
	msg := strings.ToLower(se.Error())
	if !strings.Contains(msg, "useruuid") && !strings.Contains(msg, "useremail") {
		t.Fatalf("error message should reference the missing selector: %s", se.Error())
	}
}
