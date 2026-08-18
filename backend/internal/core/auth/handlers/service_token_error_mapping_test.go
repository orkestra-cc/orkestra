package handlers

// Task 8: pure-function coverage for mapServiceTokenError, mirroring the
// style of error_mapping_test.go's TestMapPasswordError_KnownCodes /
// TestMapMFAError_KnownCodes tables.

import (
	"errors"
	"net/http"
	"testing"

	"github.com/orkestra/backend/internal/core/auth/services"
)

func TestMapServiceTokenError(t *testing.T) {
	cases := []struct {
		in         error
		wantStatus int
	}{
		{services.ErrUnsupportedGrantType, http.StatusBadRequest},
		{services.ErrInvalidClientCredentials, http.StatusUnauthorized},
		{services.ErrClientRateLimited, http.StatusTooManyRequests},
	}
	for _, c := range cases {
		err := mapServiceTokenError(c.in)
		var se interface{ GetStatus() int }
		if !errors.As(err, &se) || se.GetStatus() != c.wantStatus {
			t.Errorf("map(%v) status = %v, want %d", c.in, err, c.wantStatus)
		}
	}
}
