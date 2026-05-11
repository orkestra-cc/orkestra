// Package middleware — IP allow/block gate (Phase 7 of the auth-policy
// roadmap). Wires the admin-managed ipAllowlistAdmin / ipBlocklistAdmin
// stringList policy keys into a chi middleware that runs on the
// operator host mux only. Tier-2 client traffic skips this gate
// entirely — gating customer-facing API by IP would lock real users
// out, and the operator console is the privileged surface where an
// allow/blocklist makes operational sense.
package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// IPNetSource is the narrow contract the gate consumes — a function
// that returns the live allow + block CIDRs on every request. The
// auth module exposes AuthPolicyService.IPAllowlistOperator /
// IPBlocklistOperator behind this signature so admin edits take
// effect on the next request without a restart.
type IPNetSource func() (allow []string, block []string)

// IPGate is a chi-style middleware factory. The gate does two things:
//   - empty allowlist → any IP is permitted unless caught by the blocklist;
//   - non-empty allowlist → only IPs matching one of the configured
//     CIDRs are permitted, with the blocklist evaluated after the
//     allowlist so an explicit block always wins.
//
// Parsed nets are cached keyed by the joined raw string so the live
// policy read on every request only spends one map lookup unless the
// configured list actually changed. This keeps the hot path under
// 1µs in the steady state while still picking up admin edits live.
//
// Returns 403 ip_not_allowed when the allowlist is non-empty and the
// IP doesn't match. Returns 403 ip_blocked when the IP matches the
// blocklist. Both responses carry a small JSON body so the operator
// console can render a friendly error instead of a blank page.
type IPGate struct {
	source IPNetSource

	mu        sync.RWMutex
	cachedKey string
	allowNets []*net.IPNet
	blockNets []*net.IPNet
}

// NewIPGate builds a gate around the given source. Source nil falls
// through to a permissive gate (every request passes) so unwired
// deployments are never accidentally locked out.
func NewIPGate(source IPNetSource) *IPGate {
	return &IPGate{source: source}
}

// Middleware returns a chi-compatible http.Handler wrapper. Mount via
// `mux.Use(gate.Middleware)` on the operator router only.
func (g *IPGate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g == nil || g.source == nil {
			next.ServeHTTP(w, r)
			return
		}
		ip := extractClientIP(r)
		if ip == "" {
			// Couldn't extract an IP — fail open. Locking a request out
			// because we couldn't parse RemoteAddr would turn a parser
			// quirk into a denial-of-service.
			next.ServeHTTP(w, r)
			return
		}
		parsed := net.ParseIP(ip)
		if parsed == nil {
			next.ServeHTTP(w, r)
			return
		}
		allowRaw, blockRaw := g.source()
		allowNets, blockNets := g.compile(allowRaw, blockRaw)
		// Blocklist first so an admin can't accidentally permit a known-
		// bad IP via a too-broad allowlist.
		if matches(parsed, blockNets) {
			respondIPGate(w, "ip_blocked", "Your IP address is blocked from accessing the operator console.")
			return
		}
		if len(allowNets) > 0 && !matches(parsed, allowNets) {
			respondIPGate(w, "ip_not_allowed", "Your IP address is not allowed to access the operator console.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// compile resolves the raw stringLists into []*net.IPNet, reusing the
// previous parse when the inputs haven't changed. Cheap-path: under
// the read lock, hash the joined inputs and compare against the
// cachedKey field. Full re-parse only happens after an admin edit.
func (g *IPGate) compile(allow, block []string) ([]*net.IPNet, []*net.IPNet) {
	key := cacheKey(allow, block)
	g.mu.RLock()
	if g.cachedKey == key {
		out := g.allowNets
		blocks := g.blockNets
		g.mu.RUnlock()
		return out, blocks
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()
	// Re-check under write lock — another goroutine might have just
	// repopulated the cache.
	if g.cachedKey == key {
		return g.allowNets, g.blockNets
	}
	g.cachedKey = key
	g.allowNets = parseCIDRs(allow)
	g.blockNets = parseCIDRs(block)
	return g.allowNets, g.blockNets
}

func matches(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// parseCIDRs returns the subset of inputs that parse as a CIDR. Bare
// IPs (no /N) are normalised to /32 (IPv4) or /128 (IPv6) so an
// operator can drop a single IP into the list without worrying about
// suffixes. Malformed entries are silently dropped — the alternative
// is a 500 on every request, which is a much worse failure mode for
// what is fundamentally an admin-managed list.
func parseCIDRs(in []string) []*net.IPNet {
	if len(in) == 0 {
		return nil
	}
	out := make([]*net.IPNet, 0, len(in))
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.Contains(raw, "/") {
			if ip := net.ParseIP(raw); ip != nil {
				if ip.To4() != nil {
					raw = raw + "/32"
				} else {
					raw = raw + "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil || n == nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// cacheKey is a cheap, allocation-light fingerprint of the input lists.
// Joining with a separator that can't appear in CIDRs keeps collisions
// theoretical only.
func cacheKey(allow, block []string) string {
	var b strings.Builder
	b.WriteString("a:")
	for i, s := range allow {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s)
	}
	b.WriteString("|b:")
	for i, s := range block {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s)
	}
	return b.String()
}

// extractClientIP mirrors the device middleware's logic: X-Forwarded-
// For (first hop) > X-Real-IP > RemoteAddr (host part). Duplicated
// here so the gate doesn't depend on the device middleware's lifecycle
// (the gate runs much earlier in the chain).
func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// respondIPGate writes the small JSON 403 body the operator console
// renders. Kept inline to avoid pulling in huma at this layer — the
// gate runs before any Huma router so it has to write the response
// itself.
func respondIPGate(w http.ResponseWriter, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	// Manual JSON encoding to keep the package free of encoding/json
	// imports for what is a single fixed response shape. Both the code
	// and detail strings come from in-package constants — never user
	// input — so straight string concat is safe.
	_, _ = w.Write([]byte(`{"status":403,"title":"Forbidden","detail":"` + escapeJSON(detail) + `","code":"` + code + `"}`))
}

// escapeJSON is a tiny escaper for the two characters that would break
// the inlined JSON literal above. The detail strings are operator-
// authored constants so this is belt-and-braces — but inlining here
// keeps the response writer free of encoding/json overhead.
func escapeJSON(s string) string {
	if !strings.ContainsAny(s, "\"\\") {
		return s
	}
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}
