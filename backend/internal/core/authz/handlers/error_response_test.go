package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestAuthzInternalErrorDoesNotLeakPolicyEngineText(t *testing.T) {
	t.Parallel()
	err := authzInternalError(context.Background(), "create the role", errors.New("cedar evaluator: missing entity"))
	se, ok := err.(huma.StatusError)
	if !ok || se.GetStatus() != 500 {
		t.Fatalf("want 500 huma.StatusError, got %T (%v)", err, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "cedar") {
		t.Fatalf("policy-engine text reached the client: %q", err.Error())
	}
}
