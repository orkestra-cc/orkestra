package services

import (
	"context"
	"errors"
	"testing"
)

// TestAttachMember_ValidationErrors locks in the input-validation contract:
// empty tenantUUID, userUUID, or roleName each produce ErrAttachInput before
// any repository call. The repo on the test Service is nil — if a code path
// drifts past the validation guard the test panics on the nil-deref, which
// is exactly the loud failure we want.
func TestAttachMember_ValidationErrors(t *testing.T) {
	s := New(nil)

	cases := []struct {
		name       string
		tenantUUID string
		userUUID   string
		role       string
	}{
		{"empty tenant", "", "user-1", "org_member"},
		{"empty user", "tenant-1", "", "org_member"},
		{"empty role", "tenant-1", "user-1", ""},
		{"whitespace tenant", "   ", "user-1", "org_member"},
		{"whitespace user", "tenant-1", "  ", "org_member"},
		{"whitespace role", "tenant-1", "user-1", "  "},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := s.AttachMember(context.Background(), c.tenantUUID, c.userUUID, c.role, false)
			if !errors.Is(err, ErrAttachInput) {
				t.Fatalf("got %v, want ErrAttachInput", err)
			}
		})
	}
}
