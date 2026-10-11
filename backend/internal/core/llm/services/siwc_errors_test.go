package services

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/llm/providers"
)

// The services names are the providers identities, not look-alikes: an
// error built in providers matches the services sentinel and vice versa.
func TestSIWCErrorReExportsAreTheProviderSentinels(t *testing.T) {
	pairs := map[error]error{
		ErrSIWCRefreshTerminal: providers.ErrSIWCRefreshTerminal,
		ErrSIWCInvalidUser:     providers.ErrSIWCInvalidUser,
		ErrSIWCInvalidClient:   providers.ErrSIWCInvalidClient,
		ErrSIWCExchangeFailed:  providers.ErrSIWCExchangeFailed,
	}
	for svc, prov := range pairs {
		if svc != prov {
			t.Errorf("%v is not the providers sentinel", svc)
		}
	}
	if !errors.Is(providers.ClassifySIWCCode("invalid_grant"), ErrSIWCRefreshTerminal) {
		t.Error("terminal refresh code does not match the services sentinel")
	}
}
