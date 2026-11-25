package services

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/providers"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// A failed probe records a closed llm.* code, never provider text.
func TestProbeCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("%w: %w", providers.ErrUnauthorized, iface.ErrLLMProviderUnavailable), errcode.LLMCredentialRejected},
		{fmt.Errorf("%w: daily", iface.ErrLLMQuotaExhausted), errcode.LLMPlanQuotaExhausted},
		{fmt.Errorf("dial: %w", providers.ErrDestinationNotPublic), errcode.LLMEndpointNotAllowed},
		{ErrSecretKeyMissing, errcode.LLMSecretKeyMissing},
		{context.DeadlineExceeded, errcode.LLMProviderUnavailable},
		{errors.New("upstream said: invalid key sk-123"), errcode.LLMProviderUnavailable},
	}
	for _, tc := range cases {
		if got := probeCode(tc.err); got != tc.want {
			t.Errorf("probeCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}
