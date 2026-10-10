// Package netguard decides whether a destination may be dialled by the llm
// module in a production-like environment. It is a leaf package (stdlib
// only) so both services (endpoint string validation) and providers
// (dial-time re-validation) share one rule without an import cycle.
package netguard

import (
	"net"
	"net/netip"
	"strings"
)

// PublicHost reports whether a URL host (name or IP literal) may be
// dialled in a production-like environment.
func PublicHost(hostname string) bool {
	host := strings.TrimRight(strings.ToLower(hostname), ".")
	if host == "" {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return IsPublicAddr(addr)
	}
	// Not a canonical IP literal. A last label that is numeric or 0x-hex is
	// one of the legacy spellings (2130706433, 127.1, 0x7f.1, 0177.0.0.1)
	// that some resolvers still turn into an address; no real TLD looks
	// like that.
	last := host[strings.LastIndex(host, ".")+1:]
	if last == "" || isDigits(last) || strings.HasPrefix(last, "0x") {
		return false
	}
	return host != "localhost" && host != "metadata.google.internal" &&
		!strings.HasSuffix(host, ".localhost") && !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".internal")
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

var nonPublicPrefixes = func() []netip.Prefix {
	raw := []string{
		"0.0.0.0/8",       // "this network"
		"100.64.0.0/10",   // CGNAT (incl. Alibaba metadata 100.100.100.200)
		"192.0.0.0/24",    // IETF protocol assignments
		"198.18.0.0/15",   // benchmarking
		"240.0.0.0/4",     // reserved + broadcast
		"::/96",           // IPv4-compatible (deprecated) incl. :: and ::1
		"64:ff9b:1::/48",  // local-use NAT64
		"2001::/32",       // Teredo
		"fec0::/10",       // deprecated site-local
		"100::/64",        // discard-only
		"2001:db8::/32",   // documentation
		"192.0.2.0/24",    // documentation
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
	}
	out := make([]netip.Prefix, 0, len(raw))
	for _, p := range raw {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

var (
	nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")
)

// IsPublicIP reports whether ip is a globally routable unicast address.
// It is shared with the providers dial-time check. IPv4-mapped addresses are
// unmapped, and NAT64 (64:ff9b::/96) and 6to4 (2002::/16) addresses are
// judged by the IPv4 address they embed.
func IsPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	return IsPublicAddr(addr)
}

// IsPublicAddr is IsPublicIP for a netip.Addr.
func IsPublicAddr(addr netip.Addr) bool {
	addr = addr.WithZone("").Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	b := addr.As16()
	switch {
	case nat64Prefix.Contains(addr):
		return IsPublicAddr(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
	case sixToFour.Contains(addr):
		return IsPublicAddr(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
	}
	return true
}
