package services

import (
	"errors"
	"net"
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
		{"https://8.8.8.8", true, "https://8.8.8.8"},
		{"https://[2606:4700:4700::1111]", true, "https://[2606:4700:4700::1111]"},
		{"https://[64:ff9b::808:808]", true, "https://[64:ff9b::808:808]"}, // NAT64 of a public v4
		// Outside production the brief's behaviour is unchanged: local and
		// private endpoints are the point of ollama.
		{"http://localhost:11434", false, "http://localhost:11434"},
		{"http://10.0.0.5:11434", false, "http://10.0.0.5:11434"},
		{"http://box.local", false, "http://box.local"},
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
		// Trailing-dot FQDNs and mixed case must not beat the name checks.
		{"https://localhost.", true}, {"https://metadata.google.internal.", true}, {"https://box.local.", true},
		{"https://LOCALHOST", true}, {"https://svc.internal..", true}, {"https://a.localhost.", true},
		// Zoned IPv6 literals.
		{"https://[fe80::1%25en0]", true}, {"https://[::1%25lo]", true},
		// CGNAT (Alibaba metadata 100.100.100.200) and other special-use v4.
		{"https://100.100.100.200", true}, {"https://100.64.0.1", true}, {"https://100.127.255.255", true},
		{"https://192.0.0.192", true}, {"https://198.18.0.1", true}, {"https://198.19.255.255", true},
		{"https://0.0.0.1", true}, {"https://240.0.0.1", true}, {"https://255.255.255.255", true},
		// IPv6 forms that embed or alias a v4 address.
		{"https://[64:ff9b::a9fe:a9fe]", true}, // NAT64 of 169.254.169.254
		{"https://[64:ff9b::7f00:1]", true},    // NAT64 of 127.0.0.1
		{"https://[64:ff9b:1::1]", true},       // local-use NAT64
		{"https://[::ffff:169.254.169.254]", true}, {"https://[::ffff:10.0.0.1]", true},
		{"https://[::1]", true}, {"https://[::]", true}, {"https://[fe80::1]", true},
		{"https://[2002:a9fe:a9fe::1]", true}, // 6to4 of 169.254.169.254
		// Non-canonical IPv4 spellings a resolver may still turn into an address.
		{"https://2130706433", true}, {"https://0x7f.1", true}, {"https://127.1", true}, {"https://0177.0.0.1", true},
	}
	for _, c := range bad {
		if _, err := ValidateEndpoint(c.raw, c.prod); !errors.Is(err, ErrEndpointNotAllowed) {
			t.Errorf("ValidateEndpoint(%q,%v) err = %v, want ErrEndpointNotAllowed", c.raw, c.prod, err)
		}
	}
}

func TestIsPublicIP(t *testing.T) {
	public := []string{"8.8.8.8", "1.1.1.1", "100.63.255.255", "100.128.0.1", "198.17.255.255", "198.20.0.1", "2606:4700:4700::1111", "64:ff9b::808:808"}
	for _, s := range public {
		if !IsPublicIP(net.ParseIP(s)) {
			t.Errorf("IsPublicIP(%s) = false, want true", s)
		}
	}
	nonPublic := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "224.0.0.1", "0.0.0.0", "0.1.2.3",
		"100.64.0.0", "100.100.100.200", "100.127.255.255", "192.0.0.1", "198.18.0.0", "198.19.255.255", "240.0.0.1", "255.255.255.255",
		"::1", "::", "fe80::1", "fd00::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:100.100.100.200",
		"64:ff9b::a9fe:a9fe", "64:ff9b::6464:64c8", "64:ff9b:1::1", "2002:a9fe:a9fe::1",
	}
	for _, s := range nonPublic {
		if IsPublicIP(net.ParseIP(s)) {
			t.Errorf("IsPublicIP(%s) = true, want false", s)
		}
	}
	if IsPublicIP(nil) {
		t.Error("IsPublicIP(nil) must be false")
	}
}
