package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

// lanOpsHandler serves /health and /ready as plain HTTP for LAN probes
// (HAProxy, k8s liveness, ALB target health) that hit the pod by IP and
// therefore can't satisfy the audience-scoped hostMux. Same Mongo+Redis
// reachability checks as the Huma /health, but emitted without the
// audience-aware middleware stack so the response is identical on every
// host that reaches the listener. Mounted as the hostMux opsHandler — only
// the paths in opsPaths fall through here when no audience host matches.
func lanOpsHandler(db *mongo.Database, redisClient *redis.Client) http.Handler {
	mux := http.NewServeMux()

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
				Version: "1.0.0",
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
