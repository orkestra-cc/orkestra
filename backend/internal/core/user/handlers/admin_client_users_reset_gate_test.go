package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// The client-user reset twin maps the auth sentinels across the module
// boundary by IDENTITY (errors.Is on the iface vars), never by message —
// wrapped errors must keep their status.
func TestMapInviteErr_PasswordPolicySentinels(t *testing.T) {
	cases := []struct {
		name       string
		in         error
		wantStatus int
	}{
		{"disabled maps to 409", iface.ErrPasswordLoginDisabled, http.StatusConflict},
		{"disabled survives wrapping", fmt.Errorf("outer: %w", iface.ErrPasswordLoginDisabled), http.StatusConflict},
		{"policy unavailable maps to 503", iface.ErrAuthPolicyUnavailable, http.StatusServiceUnavailable},
		{"policy unavailable survives wrapping", fmt.Errorf("read passwordLoginEnabledClient: %w", iface.ErrAuthPolicyUnavailable), http.StatusServiceUnavailable},
		{"unknown stays 500", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := mapInviteErr(tc.in, "generic")
			var se huma.StatusError
			if !errors.As(out, &se) {
				t.Fatalf("want huma.StatusError, got %T", out)
			}
			if se.GetStatus() != tc.wantStatus {
				t.Fatalf("status = %d, want %d", se.GetStatus(), tc.wantStatus)
			}
		})
	}
}
