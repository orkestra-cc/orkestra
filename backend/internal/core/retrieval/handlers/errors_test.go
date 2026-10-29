package handlers

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/orkestra/backend/internal/core/retrieval/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func TestMapError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{iface.ErrRetrievalNotConfigured, 503, errcode.RetrievalNotConfigured},
		{fmt.Errorf("wrap: %w", iface.ErrRetrievalUnavailable), 503, errcode.RetrievalUnavailable},
		{iface.ErrRetrievalUnknownCorpus, 404, errcode.RetrievalCorpusNotFound},
		{services.ErrCorpusNotFound, 404, errcode.RetrievalCorpusNotFound},
		{services.ErrCorpusRegistered, 409, errcode.RetrievalCorpusRegistered},
		{iface.ErrRetrievalCorpusUnavailable, 503, errcode.RetrievalCorpusUnavailable},
		{iface.ErrRetrievalForbidden, 403, errcode.RetrievalForbidden},
		{iface.ErrRetrievalInvalidFilter, 422, errcode.RetrievalInvalidFilter},
		{iface.ErrRetrievalInvalidRequest, 422, errcode.RetrievalInvalidRequest},
		{iface.ErrRetrievalLocality, 409, errcode.RetrievalLocalityViolation},
		{iface.ErrRetrievalVisibilityCheck, 503, errcode.RetrievalVisibilityCheckFailed},
		{errors.New("mongo: dial tcp 10.0.0.3:27017"), 500, errcode.RetrievalInternal},
	}
	for _, c := range cases {
		e, ok := mapError(c.err).(*errcode.Error)
		if !ok {
			t.Fatalf("mapError(%v) is %T, want *errcode.Error", c.err, mapError(c.err))
		}
		if e.Status != c.status {
			t.Errorf("mapError(%v).Status = %d, want %d", c.err, e.Status, c.status)
		}
		if e.Code != c.code {
			t.Errorf("mapError(%v).Code = %q, want %q", c.err, e.Code, c.code)
		}
		if strings.Contains(e.Detail, c.err.Error()) {
			t.Errorf("mapError(%v).Detail leaks the cause: %q", c.err, e.Detail)
		}
	}
}

func TestMapError_NotConfiguredIsExpected(t *testing.T) {
	t.Parallel()
	e := mapError(iface.ErrRetrievalNotConfigured).(*errcode.Error)
	if !e.ExpectedUnavailable() {
		t.Error("retrieval.not_configured must be an expected unavailable (FeatureNotConfigured)")
	}
}
