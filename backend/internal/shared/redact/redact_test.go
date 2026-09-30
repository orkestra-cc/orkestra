package redact

import "testing"

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"user_id":        "userid",
		"Client-IP":      "clientip",
		"codice.fiscale": "codicefiscale",
		"E-Mail":         "email",
		"":               "",
	}
	for in, want := range cases {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSecretKey(t *testing.T) {
	for _, k := range []string{"password", "new_passwd", "client_secret", "refreshToken", "Authorization", "cookie", "api_key", "private-key", "credentials"} {
		if !IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"module", "trace_id", "email", "ip", "filename"} {
		if IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = true, want false", k)
		}
	}
}
