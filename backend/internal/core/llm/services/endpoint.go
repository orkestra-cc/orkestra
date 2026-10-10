package services

import (
	"net/url"
	"strings"

	"github.com/orkestra/backend/internal/core/llm/netguard"
)

// ValidateEndpoint normalizes an operator-supplied base URL and, in
// production-like environments, refuses anything that is not a public
// HTTPS host: loopback, private, link-local, multicast, ULA, CGNAT and the
// other special-use ranges (see IsPublicIP), the metadata host and the
// .local/.internal suffixes. Host names are compared lower-cased and
// without trailing dots; IPv6 zones are ignored. The string check is the
// first line; the providers transport re-validates every resolved IP at dial time.
func ValidateEndpoint(raw string, productionLike bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrEndpointNotAllowed
	}
	if productionLike {
		if u.Scheme != "https" || !netguard.PublicHost(u.Hostname()) {
			return "", ErrEndpointNotAllowed
		}
	}
	u.Fragment = ""
	u.RawQuery = ""
	return strings.TrimRight(u.String(), "/"), nil
}
