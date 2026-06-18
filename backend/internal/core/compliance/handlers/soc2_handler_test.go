package handlers

import (
	"context"
	"testing"
)

// When compliance.soc2_enabled is off, the always-mounted evidence route must
// 404 — the gate runs before the service is touched, so a nil service is safe
// here. (assertStatus lives in me_handler_test.go.)
func TestSOC2EvidenceDisabledIs404(t *testing.T) {
	t.Parallel()
	h := NewSOC2Handler(nil, func(context.Context) bool { return false })
	_, err := h.Evidence(context.Background(), &struct{}{})
	assertStatus(t, err, 404)
}
