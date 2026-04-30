package main

import (
	"net/http"
	"strings"
)

// hostMux dispatches incoming HTTP requests to one of several backend
// handlers based on the request's Host header. Each entry in routes maps
// a fully qualified hostname (e.g. "console.orkestra.com",
// "console.localhost:3000") to the chi.Mux that serves that audience.
//
// Lookup matches the Host header in two passes:
//
//  1. exact match including port (lets dev distinguish
//     console.localhost:3000 from console.localhost:8080)
//  2. exact match with port stripped (lets prod ingress hit
//     console.orkestra.com when the configured key is the bare host)
//
// On no match:
//
//   - When devFallthrough is non-nil, requests fall through to it. This
//     preserves "curl http://localhost:3000" ergonomics in development
//     where DNS for `*.localhost` may not resolve to 127.0.0.1 on every
//     platform.
//   - Otherwise the response is 421 Misdirected Request (RFC 7540 §9.1.2),
//     the canonical signal that an HTTP/1.1 request reached a host that
//     does not serve it. Closes the door on host-header smuggling against
//     the Tier-1 console.
type hostMux struct {
	routes         map[string]http.Handler
	devFallthrough http.Handler
}

func newHostMux(routes map[string]http.Handler, devFallthrough http.Handler) *hostMux {
	normalized := make(map[string]http.Handler, len(routes)*2)
	for host, h := range routes {
		key := strings.ToLower(strings.TrimSpace(host))
		if key == "" || h == nil {
			continue
		}
		normalized[key] = h
		// Index the bare-host variant too so "console.orkestra.com"
		// matches a request whose Host header includes a non-default
		// port (rare in prod, common in tests).
		if bare, _, ok := splitHostPort(key); ok {
			normalized[bare] = h
		}
	}
	return &hostMux{routes: normalized, devFallthrough: devFallthrough}
}

func (h *hostMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(strings.TrimSpace(r.Host))

	if handler, ok := h.routes[host]; ok {
		handler.ServeHTTP(w, r)
		return
	}
	if bare, _, ok := splitHostPort(host); ok {
		if handler, ok := h.routes[bare]; ok {
			handler.ServeHTTP(w, r)
			return
		}
	}

	if h.devFallthrough != nil {
		h.devFallthrough.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.Error(w, "421 Misdirected Request", http.StatusMisdirectedRequest)
}

// splitHostPort splits "host:port" into ("host", "port", true). Returns
// false when no port is present so callers can distinguish "no port" from
// "ambiguous". Mirrors net.SplitHostPort but tolerates IPv6 brackets and
// returns the boolean ok flag instead of an error to keep the call sites
// terse.
func splitHostPort(s string) (host, port string, ok bool) {
	if s == "" {
		return "", "", false
	}
	// IPv6 literal e.g. "[::1]:3000"
	if strings.HasPrefix(s, "[") {
		end := strings.LastIndex(s, "]")
		if end < 0 {
			return "", "", false
		}
		host = s[1:end]
		rest := s[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return host, "", false
		}
		return host, rest[1:], true
	}
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return s, "", false
	}
	return s[:idx], s[idx+1:], true
}
