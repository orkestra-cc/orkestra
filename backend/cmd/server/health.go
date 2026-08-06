package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

// lanOpsHandler serves /health, /ready, and (when provided) /metrics as
// plain HTTP for LAN probes (HAProxy, k8s liveness, ALB target health,
// Prometheus scrape) that hit the pod by IP / service DNS and therefore
// can't satisfy the audience-scoped hostMux. Same Mongo+Redis
// reachability checks as the Huma /health, but emitted without the
// audience-aware middleware stack so the response is identical on every
// host that reaches the listener. Mounted as the hostMux opsHandler — only
// the paths in opsPaths fall through here when no audience host matches.
//
// metricsHandler is the Prometheus collector handler (or nil when
// METRICS_ENABLED=false). When non-nil it is mounted at /metrics so
// Prometheus can scrape the pod without spoofing an operator Host header.
func lanOpsHandler(db *mongo.Database, redisClient *redis.Client, metricsHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	if metricsHandler != nil {
		mux.Handle("/metrics", metricsHandler)
	}

	writeJSON := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		checks := map[string]string{}
		if err := db.Client().Ping(ctx, nil); err != nil {
			checks["mongodb"] = "down"
		} else {
			checks["mongodb"] = "up"
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			checks["redis"] = "down"
		} else {
			checks["redis"] = "up"
		}

		status := "healthy"
		code := http.StatusOK
		for _, c := range checks {
			if c == "down" {
				status = "unhealthy"
				code = http.StatusServiceUnavailable
				break
			}
		}

		writeJSON(w, code, map[string]any{
			"status":  status,
			"time":    time.Now().UTC().Format(time.RFC3339),
			"version": "1.0.0",
			"checks":  checks,
		})
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		ready := true
		if err := db.Client().Ping(ctx, nil); err != nil {
			ready = false
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			ready = false
		}

		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{"ready": ready})
	})

	return mux
}

// registerHealthEndpoints registers /health and /ready endpoints.
func registerHealthEndpoints(api huma.API, db *mongo.Database, redisClient *redis.Client) {
	huma.Register(api, huma.Operation{
		OperationID: "health-check",
		Method:      "GET",
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the application",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Status  string            `json:"status"`
			Time    string            `json:"time"`
			Version string            `json:"version"`
			Checks  map[string]string `json:"checks"`
		}
	}, error) {
		checks := map[string]string{}

		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := db.Client().Ping(ctx, nil); err != nil {
			checks["mongodb"] = "down"
		} else {
			checks["mongodb"] = "up"
		}

		if err := redisClient.Ping(ctx).Err(); err != nil {
			checks["redis"] = "down"
		} else {
			checks["redis"] = "up"
		}

		status := "healthy"
		for _, check := range checks {
			if check == "down" {
				status = "unhealthy"
				break
			}
		}

		return &struct {
			Body struct {
				Status  string            `json:"status"`
				Time    string            `json:"time"`
				Version string            `json:"version"`
				Checks  map[string]string `json:"checks"`
			}
		}{
			Body: struct {
				Status  string            `json:"status"`
				Time    string            `json:"time"`
				Version string            `json:"version"`
				Checks  map[string]string `json:"checks"`
			}{
				Status:  status,
				Time:    time.Now().UTC().Format(time.RFC3339),
				Version: Version,
				Checks:  checks,
			},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "readiness-check",
		Method:      "GET",
		Path:        "/ready",
		Summary:     "Readiness check",
		Description: "Returns whether the application is ready to accept requests",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Ready bool `json:"ready"`
		}
	}, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		ready := true
		if err := db.Client().Ping(ctx, nil); err != nil {
			ready = false
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			ready = false
		}

		return &struct {
			Body struct {
				Ready bool `json:"ready"`
			}
		}{
			Body: struct {
				Ready bool `json:"ready"`
			}{
				Ready: ready,
			},
		}, nil
	})
}

// registerHealthProbes mounts /health and /ready as plain chi routes on the
// given mux, mirroring the response bodies of the Huma health/readiness
// operations (always HTTP 200; the payload carries the status).
//
// It exists because operatorAPI and clientAPI share a single OpenAPI document
// (both built from the same huma.Config), and huma v2.39+ panics on a
// duplicate operation ID. The Huma operations are therefore registered once,
// on the operator API (which owns the shared document); the client host serves
// the same probes through these raw routes so orchestrator probes can still
// hit either host without re-registering the operations. The OpenAPI document
// already documents /health and /ready via the operator registration.
func registerHealthProbes(mux *chi.Mux, db *mongo.Database, redisClient *redis.Client) {
	writeJSON := func(w http.ResponseWriter, status int, body any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	mux.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		checks := map[string]string{}
		if err := db.Client().Ping(ctx, nil); err != nil {
			checks["mongodb"] = "down"
		} else {
			checks["mongodb"] = "up"
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			checks["redis"] = "down"
		} else {
			checks["redis"] = "up"
		}

		status := "healthy"
		for _, c := range checks {
			if c == "down" {
				status = "unhealthy"
				break
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"status":  status,
			"time":    time.Now().UTC().Format(time.RFC3339),
			"version": Version,
			"checks":  checks,
		})
	})

	mux.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		ready := true
		if err := db.Client().Ping(ctx, nil); err != nil {
			ready = false
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			ready = false
		}

		writeJSON(w, http.StatusOK, map[string]any{"ready": ready})
	})
}
