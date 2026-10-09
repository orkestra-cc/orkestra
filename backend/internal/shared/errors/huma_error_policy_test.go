package errors

// Huma serialises every error a handler wraps into the response body
// (`errors[].message` = err.Error()). Twenty-five auth handler sites pass an
// infrastructure error straight into huma.Error5xx(msg, err), so a Redis or
// Mongo outage answered anonymous callers with dial errors naming hosts and
// ports. The transformer under test withholds those details on 5xx outside
// development and logs them instead; 4xx bodies and Huma's own validation
// errors are untouched.

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

const infraError = "dial tcp 10.0.0.5:6379: connect: connection refused"

type policyValidatedInput struct {
	Body struct {
		Name string `json:"name" required:"true" minLength:"1"`
	}
}

func newPolicyAPI(t *testing.T, productionLike bool) (humatest.TestAPI, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	cfg := huma.DefaultConfig("test", "1.0")
	cfg.Transformers = append(cfg.Transformers, HumaErrorDetailPolicy(productionLike, logger))
	_, api := humatest.New(t, cfg)

	huma.Register(api, huma.Operation{OperationID: "boom", Method: http.MethodGet, Path: "/boom"},
		func(context.Context, *struct{}) (*struct{}, error) {
			return nil, huma.Error500InternalServerError("Failed to create OAuth state", stderrors.New(infraError))
		})
	huma.Register(api, huma.Operation{OperationID: "unauth", Method: http.MethodGet, Path: "/unauth"},
		func(context.Context, *struct{}) (*struct{}, error) {
			return nil, huma.Error401Unauthorized("Invalid ID token", stderrors.New("token is malformed"))
		})
	huma.Register(api, huma.Operation{OperationID: "plain", Method: http.MethodGet, Path: "/plain"},
		func(context.Context, *struct{}) (*struct{}, error) {
			return nil, stderrors.New(infraError) // a bare error: Huma wraps it as a 500 with the text
		})
	huma.Register(api, huma.Operation{OperationID: "validated", Method: http.MethodPost, Path: "/validated"},
		func(context.Context, *policyValidatedInput) (*struct{}, error) { return nil, nil })
	return api, &logs
}

func errorsField(t *testing.T, body map[string]any) []any {
	t.Helper()
	raw, present := body["errors"]
	if !present {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("errors is %T, want array", raw)
	}
	return items
}

func TestHumaErrorPolicy_ProductionLike5xxWithholdsWrappedErrors(t *testing.T) {
	api, logs := newPolicyAPI(t, true)

	for _, path := range []string{"/boom", "/plain"} {
		resp := api.Get(path)
		if resp.Code != http.StatusInternalServerError {
			t.Fatalf("%s: status = %d, want 500", path, resp.Code)
		}
		if strings.Contains(resp.Body.String(), "10.0.0.5") {
			t.Fatalf("%s: infrastructure error reached the client: %s", path, resp.Body.String())
		}
		body := decodeJSONBody(t, resp.Body.Bytes())
		if items := errorsField(t, body); len(items) != 0 {
			t.Fatalf("%s: errors[] present on a production-like 5xx: %v", path, items)
		}
		if body["detail"] == "" {
			t.Fatalf("%s: the handler's own detail must survive", path)
		}
	}
	if !strings.Contains(logs.String(), "10.0.0.5") {
		t.Fatalf("withheld detail must be logged for operators, log = %s", logs.String())
	}
}

func TestHumaErrorPolicy_4xxKeepsItsDetails(t *testing.T) {
	api, _ := newPolicyAPI(t, true)
	resp := api.Get("/unauth")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "token is malformed") {
		t.Fatalf("4xx detail was withheld: %s", resp.Body.String())
	}
}

func TestHumaErrorPolicy_ValidationErrorsAreUntouched(t *testing.T) {
	api, _ := newPolicyAPI(t, true)
	resp := api.Post("/validated", map[string]any{})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", resp.Code, resp.Body.String())
	}
	body := decodeJSONBody(t, resp.Body.Bytes())
	if items := errorsField(t, body); len(items) == 0 {
		t.Fatalf("validation errors[] must survive: %s", resp.Body.String())
	}
}

func TestHumaErrorPolicy_DevelopmentKeeps5xxDetails(t *testing.T) {
	api, _ := newPolicyAPI(t, false)
	resp := api.Get("/boom")
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "10.0.0.5") {
		t.Fatalf("development must keep the wrapped error for debugging: %s", resp.Body.String())
	}
}

func decodeJSONBody(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, raw)
	}
	return body
}
