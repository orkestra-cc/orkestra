package handlers

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestServiceAccountRoleAllowed(t *testing.T) {
	cases := []struct {
		kind, role string
		want       bool
	}{
		{"", "super_admin", true},                       // humans: existing rules apply
		{iface.UserKindService, "guest", true},
		{iface.UserKindService, "operator", true},
		{iface.UserKindService, "administrator", false}, // machines never privileged
		{iface.UserKindService, "super_admin", false},
	}
	for _, c := range cases {
		if got := serviceAccountRoleAllowed(c.kind, c.role); got != c.want {
			t.Errorf("serviceAccountRoleAllowed(%q,%q) = %v, want %v", c.kind, c.role, got, c.want)
		}
	}
}
