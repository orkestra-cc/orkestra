package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/internal/shared/errors"
	authMiddleware "github.com/orkestra/backend/internal/shared/middleware"
)

// setupMiddleware configures all global HTTP middleware on the router.
//
// audience and audCfg are populated by ADR-0003 PR-C: each audience-scoped
// mux gets its own CORS allowlist (falling back to the legacy
// cfg.Server.CORSOrigins when the per-audience list is empty) and its own
// RequireAudience gate so a token issued for the other tier fails closed
// at this mux's edge. Pass audience="" / audCfg.Host="" to disable the
// audience gate (used by the AI sidecar's single-surface mode and any
// future caller that legitimately serves a non-audience surface).
func setupMiddleware(
	router *chi.Mux,
	cfg *config.Config,
	errorManager *errors.Manager,
	deviceMW *authMiddleware.DeviceMiddleware,
	audience string,
	audCfg config.AudienceConfig,
	logger *slog.Logger,
) {
	// ADR-0005 §2 — structured request logger sits as outer as possible
	// so CORS rejects, body-size rejects, and audience-mismatch rejects
	// all produce a log line. RequestID + RealIP must run first so the
	// logger can attach request_id and the real client IP; otherwise the
	// logger has no way to learn them from r.
	router.Use(chiMiddleware.RequestID)
	router.Use(chiMiddleware.RealIP)
	router.Use(authMiddleware.RequestLogger(logger, authMiddleware.NewRequestLoggerOptions()))

	// Security headers
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			if cfg.IsProductionLike() {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	})

	// Request body size limit
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > cfg.Server.MaxBodySize {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, cfg.Server.MaxBodySize)
			next.ServeHTTP(w, r)
		})
	})

	// CORS — per-audience allowlist with legacy fallback.
	corsOrigins := audCfg.CORSOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = cfg.Server.CORSOrigins
	}
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID", "X-Tenant-ID", "X-Org-ID"},
		ExposedHeaders:   []string{"Link", "X-Total-Count", "X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-New-Access-Token", "X-Token-Refreshed"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// ADR-0003 PR-C: RequireAudience runs immediately after CORS so a
	// cross-audience token is rejected before any handler logic runs.
	// Mounted at the mux level (not per-route) is what makes the gate
	// non-skippable: a route mistakenly registered on the wrong mux still
	// fails closed.
	if audience != "" {
		router.Use(authMiddleware.RequireAudience(audience))
	}

	router.Use(deviceMW.ExtractDeviceInfo)

	// Inject HTTP request into context for Huma handlers
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), "http_request", r)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})

	// Error handling
	router.Use(errorManager.GetErrorHandler().Middleware())
	router.Use(errorManager.GetValidator().Middleware())
	router.Use(errorManager.GetRateLimiter().Middleware("api:general"))

	router.Use(chiMiddleware.Recoverer)

	// Timeout with SSE bypass. Must exceed the longest sync endpoint budget
	// (currently SALES_QUICK_TIMEOUT=5m); http.Server WriteTimeout is the hard ceiling.
	router.Use(func(next http.Handler) http.Handler {
		timeoutHandler := chiMiddleware.Timeout(6 * time.Minute)(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/stream") {
				next.ServeHTTP(w, r)
				return
			}
			timeoutHandler.ServeHTTP(w, r)
		})
	})
}
