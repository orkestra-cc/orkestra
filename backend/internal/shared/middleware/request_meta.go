package middleware

import (
	"context"
	"net/http"
)

type requestMetaKey struct{}

// RequestMeta stores request provenance that a Huma handler cannot read
// from its context.Context — today the User-Agent — for the module-admin
// audit actor resolver (cmd/server/admin_wiring.go). Mounted on the admin
// mutation groups only.
func RequestMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), requestMetaKey{}, r.UserAgent())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserAgentFromContext returns the User-Agent RequestMeta stored, or "".
func UserAgentFromContext(ctx context.Context) string {
	ua, _ := ctx.Value(requestMetaKey{}).(string)
	return ua
}
