package openapiauth

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMint_ErrorPathsOmitPayload is regression coverage for the mint()
// failure paths that used to embed the upstream response body (truncated to
// 200 bytes) into both the log line and the returned error. oauth.openapi.it
// is shared by the billing and company products; a failure body here is more
// likely to be an auth rejection about the operator's own account than a
// third party's fiscal data, but it is still a raw third-party response body
// and must not reach the log sink or the error chain that bubbles up through
// callers like billing's resolveBearerToken.
func TestMint_ErrorPathsOmitPayload(t *testing.T) {
	const piiBody = `{"success":false,"message":"rejected","data":{"codice_fiscale":"RSSMRA85D15F205X",` +
		`"denominazione":"Rossi Costruzioni Edili SRL","indirizzo":"Via dei Tigli, 42"}}`

	forbidden := []string{
		"RSSMRA85D15F205X",
		"Rossi Costruzioni Edili SRL",
		"Via dei Tigli",
	}

	cases := []struct {
		name   string
		status int
	}{
		{name: "401 credentials rejected (also logs)", status: http.StatusUnauthorized},
		{name: "403 credentials rejected (also logs)", status: http.StatusForbidden},
		{name: "500 generic upstream failure", status: http.StatusInternalServerError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(piiBody))
			}))
			defer srv.Close()

			var logBuf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			m := NewMinter(Config{
				AccountEmail: "a@b.it",
				APIKey:       "k",
				OAuthBaseURL: srv.URL,
				Scopes:       []string{"GET:x"},
				Tag:          "billing",
			}, nil, nil, logger)

			_, err := m.Token(context.Background())
			if err == nil {
				t.Fatalf("expected an error from a %d response, got nil", tc.status)
			}

			errMsg := err.Error()
			logOut := logBuf.String()
			for _, f := range forbidden {
				if strings.Contains(errMsg, f) {
					t.Errorf("error string leaked %q:\n%s", f, errMsg)
				}
				if strings.Contains(logOut, f) {
					t.Errorf("log output leaked %q:\n%s", f, logOut)
				}
			}
		})
	}
}

// TestMint_EmptyTokenErrorOmitsPayload covers the third body-embedding site:
// a 2xx response that parses but carries no usable token field.
func TestMint_EmptyTokenErrorOmitsPayload(t *testing.T) {
	const piiBody = `{"success":true,"token":"","data":{"codice_fiscale":"RSSMRA85D15F205X",` +
		`"denominazione":"Rossi Costruzioni Edili SRL","indirizzo":"Via dei Tigli, 42"}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(piiBody))
	}))
	defer srv.Close()

	m := NewMinter(Config{
		AccountEmail: "a@b.it",
		APIKey:       "k",
		OAuthBaseURL: srv.URL,
		Scopes:       []string{"GET:x"},
		Tag:          "company",
	}, nil, nil, nil)

	_, err := m.Token(context.Background())
	if err == nil {
		t.Fatal("expected an error for an empty-token response, got nil")
	}

	msg := err.Error()
	for _, f := range []string{"RSSMRA85D15F205X", "Rossi Costruzioni Edili SRL", "Via dei Tigli"} {
		if strings.Contains(msg, f) {
			t.Errorf("error string leaked %q:\n%s", f, msg)
		}
	}
}
