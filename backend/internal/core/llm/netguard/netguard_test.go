package netguard

import (
	"net"
	"testing"
)

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
