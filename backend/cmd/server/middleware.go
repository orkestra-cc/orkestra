package main

import (
	"context"
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
func setupMiddleware(
	router *chi.Mux,
	cfg *config.Config,
	errorManager *errors.Manager,
	deviceMW *authMiddleware.DeviceMiddleware,
) {
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

	// CORS
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.Server.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "X-Request-ID", "X-Tenant-ID", "X-Org-ID"},
		ExposedHeaders:   []string{"Link", "X-Total-Count", "X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-New-Access-Token", "X-Token-Refreshed"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	router.Use(chiMiddleware.RequestID)
	router.Use(chiMiddleware.RealIP)

	// Logger that excludes /health
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/health" {
				chiMiddleware.Logger(next).ServeHTTP(w, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	})

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
