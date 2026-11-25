package idempotency

import (
	"encoding/json"
	"net/http"
)

// Error codes returned in the problem+json body.
const (
	CodeKeyRequired       = "idempotency.key_required"
	CodeInvalidKey        = "idempotency.invalid_key"
	CodeUnauthenticated   = "idempotency.unauthenticated"
	CodeBodyTooLarge      = "idempotency.body_too_large"
	CodeInProgress        = "idempotency.in_progress"
	CodeKeyReused         = "idempotency.key_reused"
	CodeReplayUnavailable = "idempotency.replay_unavailable"
	CodeUnavailable       = "idempotency.unavailable"
)

type problem struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Code   string `json:"code"`
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Status: status, Title: http.StatusText(status), Detail: detail, Code: code})
}
