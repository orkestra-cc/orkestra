package services

import (
	"net"
	"net/url"
	"strings"
)

// ValidateEndpoint normalizes an operator-supplied base URL and, in
// production-like environments, refuses anything that is not a public
// HTTPS host: loopback, private, link-local, multicast, ULA, the metadata
// host and the .local/.internal suffixes. The string check is the first
// line; PR 2's transport re-validates every resolved IP at dial time.
func ValidateEndpoint(raw string, productionLike bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrEndpointNotAllowed
	}
	if productionLike {
		if u.Scheme != "https" {
			return "", ErrEndpointNotAllowed
		}
		host := strings.ToLower(u.Hostname())
		if host == "localhost" || host == "metadata.google.internal" ||
			strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
			return "", ErrEndpointNotAllowed
		}
		if ip := net.ParseIP(host); ip != nil && !IsPublicIP(ip) {
			return "", ErrEndpointNotAllowed
		}
	}
	u.Fragment = ""
	u.RawQuery = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// IsPublicIP is shared with PR 2's dial-time check.
func IsPublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast())
}
