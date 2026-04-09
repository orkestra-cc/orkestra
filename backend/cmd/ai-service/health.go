package main

import (
	"context"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
)

func registerAIHealthEndpoints(api huma.API, db *mongo.Database, redisClient *redis.Client) {
	huma.Register(api, huma.Operation{
		OperationID: "ai-health-check",
		Method:      "GET",
		Path:        "/health",
		Summary:     "Health check",
		Description: "Returns the health status of the AI service",
		Tags:        []string{"Health"},
	}, func(ctx context.Context, _ *struct{}) (*struct {
		Body struct {
			Status  string            `json:"status"`
			Service string            `json:"service"`
			Time    string            `json:"time"`
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
				Service string            `json:"service"`
				Time    string            `json:"time"`
				Checks  map[string]string `json:"checks"`
			}
		}{
			Body: struct {
				Status  string            `json:"status"`
				Service string            `json:"service"`
				Time    string            `json:"time"`
				Checks  map[string]string `json:"checks"`
			}{
				Status:  status,
				Service: "ai-service",
				Time:    time.Now().UTC().Format(time.RFC3339),
				Checks:  checks,
			},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "ai-readiness-check",
		Method:      "GET",
		Path:        "/ready",
		Summary:     "Readiness check",
		Description: "Returns whether the AI service is ready to accept requests",
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
