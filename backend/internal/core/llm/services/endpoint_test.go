package services

import (
	"errors"
	"testing"
)

func TestValidateEndpoint(t *testing.T) {
	ok := []struct {
		raw  string
		prod bool
		want string
	}{
		{"https://api.example.com/v1/", true, "https://api.example.com/v1"},
		{"http://ollama:11434", false, "http://ollama:11434"},
		{"http://127.0.0.1:11434/", false, "http://127.0.0.1:11434"},
		{"https://api.example.com/v1?x=1#frag", true, "https://api.example.com/v1"},
	}
	for _, c := range ok {
		got, err := ValidateEndpoint(c.raw, c.prod)
		if err != nil || got != c.want {
			t.Errorf("ValidateEndpoint(%q,%v) = %q, %v", c.raw, c.prod, got, err)
		}
	}
	bad := []struct {
		raw  string
		prod bool
	}{
		{"", false}, {"ftp://x", false}, {"https://user:pw@host", false}, {"not a url", false},
		{"http://api.example.com", true},           // plain http in prod
		{"https://localhost:11434", true},          // loopback in prod
		{"https://10.0.0.5", true},                 // private in prod
		{"https://[fd00::1]", true},                // ULA v6 in prod
		{"https://169.254.169.254", true},          // link-local / metadata in prod
		{"https://metadata.google.internal", true}, // metadata host in prod
		{"https://svc.internal", true}, {"https://box.local", true}, {"https://224.0.0.1", true},
	}
	for _, c := range bad {
		if _, err := ValidateEndpoint(c.raw, c.prod); !errors.Is(err, ErrEndpointNotAllowed) {
			t.Errorf("ValidateEndpoint(%q,%v) err = %v, want ErrEndpointNotAllowed", c.raw, c.prod, err)
		}
	}
}
