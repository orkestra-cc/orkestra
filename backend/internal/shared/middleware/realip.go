// Package middleware — trusted-proxy-aware client IP resolution.
//
// `X-Forwarded-For`, `X-Real-IP`, `CF-Connecting-IP` and friends are just
// request headers: any client can send them, and a client that is not
// behind our proxies can send whatever it likes. Reading the leftmost
// XFF entry — the pattern chi's RealIP and the pre-fix
// utils.GetClientIP both used — means the source address of every
// request is attacker-controlled. That silently defeats four controls at
// once:
//
//   - the operator IP allowlist / blocklist (shared/middleware/ip_gate.go)
//   - the geo-block on login (auth policy `geoBlockCountries`)
//   - the per-IP login rate limiter / lockout bucket
//   - the IP recorded on every audit and security event
//
// The fix is to only believe the part of the forwarding chain that our
// own infrastructure appended. The direct peer (`r.RemoteAddr`) cannot
// be forged; each reverse proxy in front of us appends the address it
// received the connection from. So the trustworthy chain, read
// right-to-left, is: peer, then one entry per proxy hop. The client is
// the first entry we reach after skipping our own hops. Everything
// further left was supplied by the caller and is discarded.
//
// Configuration (see shared/config): TRUSTED_PROXY_COUNT names how many
// hops sit in front of the backend, or TRUSTED_PROXY_CIDRS names their
// networks (preferred — it survives a topology change without a
// recount). Neither set means "trust nothing": headers are ignored
// entirely and the peer address is used. That default is deliberately
// fail-closed — a deployment that forgets to configure this reports the
// proxy's address for everyone (visible, annoying, safe) rather than
// letting every caller pick their own (invisible, dangerous).
package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// maxForwardedHops caps how much of the X-Forwarded-For header we parse.
// The header is caller-extendable, so an unbounded split is a cheap way
// to make us allocate. No real deployment has more than a handful of
// hops.
const maxForwardedHops = 32

// TrustedProxyPolicy describes the reverse proxies sitting between the
// internet and this process. The zero value trusts nothing, which makes
// ResolveClientIP return the direct peer address and ignore every
// forwarding header.
type TrustedProxyPolicy struct {
	// Count is the number of trusted proxy hops in front of the backend.
	// Used when CIDRs is empty.
	Count int
	// CIDRs are the networks our own proxies live in. When non-empty it
	// takes precedence over Count: hops are skipped as long as they fall
	// inside one of these networks, so the policy stays correct if the
	// chain length changes.
	CIDRs []*net.IPNet
}

// NewTrustedProxyPolicy builds a policy from configuration. Malformed
// CIDRs are an error rather than a silent drop: a typo here would
// quietly widen what the process is willing to believe.
func NewTrustedProxyPolicy(count int, cidrs []string) (TrustedProxyPolicy, error) {
	p := TrustedProxyPolicy{Count: count}
	if count < 0 {
		return TrustedProxyPolicy{}, fmt.Errorf("trusted proxy count must not be negative, got %d", count)
	}
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return TrustedProxyPolicy{}, fmt.Errorf("invalid trusted proxy address %q", raw)
			}
			if ip.To4() != nil {
				raw += "/32"
			} else {
				raw += "/128"
			}
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			return TrustedProxyPolicy{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		p.CIDRs = append(p.CIDRs, n)
	}
	return p, nil
}

// Configured reports whether the policy trusts any proxy at all. Used at
// boot to warn when a production-like deployment left this unset.
func (p TrustedProxyPolicy) Configured() bool {
	return p.Count > 0 || len(p.CIDRs) > 0
}

// ResolveClientIP returns the address to treat as the caller's. It never
// returns a value that only appeared in a header supplied by an
// untrusted hop.
func (p TrustedProxyPolicy) ResolveClientIP(r *http.Request) string {
	peer := hostOnly(r.RemoteAddr)

	// Trust nothing → the peer is the client, full stop.
	if !p.Configured() {
		return peer
	}

	// Build the chain right-to-left: peer first, then the XFF entries in
	// reverse order (each proxy appends, so the rightmost XFF entry is
	// the address seen by the outermost trusted proxy).
	forwarded := reversedForwardedFor(r)
	chain := make([]string, 0, len(forwarded)+1)
	chain = append(chain, peer)
	chain = append(chain, forwarded...)

	if len(p.CIDRs) > 0 {
		return p.resolveByCIDR(r, chain, peer)
	}
	return resolveByCount(chain, p.Count, peer)
}

// resolveByCIDR walks the chain skipping hops that live in a trusted
// network. The first address outside those networks is the client.
func (p TrustedProxyPolicy) resolveByCIDR(r *http.Request, chain []string, peer string) string {
	for i, hop := range chain {
		if p.trusted(hop) {
			continue
		}
		// The first untrusted hop is the client — unless it is the peer
		// itself, in which case the connection did not come through our
		// proxies and any header it carried is self-reported.
		if i == 0 {
			return peer
		}
		return hop
	}
	// Every hop was one of ours: fall back to the last (leftmost) entry
	// we saw, or the peer when the chain held nothing else.
	if len(chain) > 1 {
		return chain[len(chain)-1]
	}
	return peer
}

// resolveByCount skips a fixed number of hops from the right.
func resolveByCount(chain []string, count int, peer string) string {
	if count >= len(chain) {
		// Chain shorter than the configured hop count — a
		// misconfiguration. Fall back to the unforgeable peer rather
		// than to a caller-supplied entry.
		return peer
	}
	return chain[count]
}

// trusted reports whether an address falls inside a trusted proxy
// network. Unparseable addresses are never trusted.
func (p TrustedProxyPolicy) trusted(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return false
	}
	for _, n := range p.CIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// reversedForwardedFor returns the parsed X-Forwarded-For entries in
// right-to-left order, falling back to X-Real-IP when XFF is absent.
// Malformed entries are dropped: a hop that cannot be parsed cannot be
// matched against a trusted network, so keeping it would only let a
// caller pad the chain.
func reversedForwardedFor(r *http.Request) []string {
	raw := r.Header.Get("X-Forwarded-For")
	if raw == "" {
		// X-Real-IP carries a single address rather than a chain. It is
		// still only believed when the peer turns out to be trusted,
		// which the caller of this function decides.
		if single := strings.TrimSpace(r.Header.Get("X-Real-IP")); single != "" && net.ParseIP(single) != nil {
			return []string{single}
		}
		return nil
	}

	parts := strings.Split(raw, ",")
	if len(parts) > maxForwardedHops {
		parts = parts[len(parts)-maxForwardedHops:]
	}
	out := make([]string, 0, len(parts))
	for i := len(parts) - 1; i >= 0; i-- {
		hop := hostOnly(strings.TrimSpace(parts[i]))
		if hop == "" || net.ParseIP(hop) == nil {
			continue
		}
		out = append(out, hop)
	}
	return out
}

// hostOnly strips a port from an address if present. Bare IPv6 literals
// (which contain colons but no port) are returned untouched.
func hostOnly(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return strings.Trim(addr, "[]")
}

// RealIP resolves the client address once, per policy, and rewrites
// r.RemoteAddr with the result — then deletes the forwarding headers so
// no downstream handler can re-derive a spoofed value by reading them
// itself. Mount it outermost on every mux, in place of
// chi/middleware.RealIP (which trusts the leftmost XFF entry
// unconditionally).
//
// After this middleware, r.RemoteAddr is the single source of truth for
// "who is calling", and utils.GetClientIP simply reads it.
func RealIP(policy TrustedProxyPolicy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip := policy.ResolveClientIP(r); ip != "" {
				r.RemoteAddr = ip
			}
			r.Header.Del("X-Forwarded-For")
			r.Header.Del("X-Real-IP")
			r.Header.Del("CF-Connecting-IP")
			r.Header.Del("X-Client-IP")
			next.ServeHTTP(w, r)
		})
	}
}
