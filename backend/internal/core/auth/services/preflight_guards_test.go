package services

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestPreflightGuards_AskForTheCategoryTheySend is the ADR-0019 D7
// checklist: every pre-flight guard asks for the category of the send that
// follows it, and no coarse IsConfigured(ctx) guard remains. Counted from
// source because the eight call sites live inside methods whose fixtures
// are large; the runtime behaviour of the accessor is tested in pkg/sdk/iface.
func TestPreflightGuards_AskForTheCategoryTheySend(t *testing.T) {
	want := map[string]map[string]int{
		"password_auth_service.go": {
			"CategoryAuthVerifyEmail":    2, // signup admission + resend verification
			"CategoryAuthResetPassword":  2, // forgot password + admin-initiated reset
			"CategoryAuthNewDeviceLogin": 1,
			"CategoryAuthAdminInvite":    1,
		},
		"suspicious_login_notifier.go": {
			"CategoryAuthSuspiciousLogin":      1,
			"CategoryAuthAdminSuspiciousLogin": 1,
		},
	}
	total := 0
	for file, cats := range want {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(src), ".IsConfigured(ctx)"); n != 0 {
			t.Errorf("%s: %d coarse IsConfigured(ctx) guard(s) remain", file, n)
		}
		for c, n := range cats {
			re := regexp.MustCompile(`IsConfiguredForCategory\(ctx, \w+\.notifier, notifModels\.` + c + `\)`)
			if got := len(re.FindAllIndex(src, -1)); got != n {
				t.Errorf("%s: %d guard(s) for %s, want %d", file, got, c, n)
			}
			total += n
		}
	}
	if total != 8 {
		t.Fatalf("checklist covers %d guards, want 8", total)
	}

	// The categories the sends carry must be the auth.* constants, or an
	// operator's auth.* pattern silently misses verification and reset mail.
	src, _ := os.ReadFile("password_auth_service.go")
	for _, bare := range []string{"Category:   authModels.EmailTokenPurposeResetPassword", "Category:   authModels.EmailTokenPurposeVerifyEmail"} {
		if strings.Contains(string(src), bare) {
			t.Errorf("a send still carries a bare category: %s", bare)
		}
	}
}
