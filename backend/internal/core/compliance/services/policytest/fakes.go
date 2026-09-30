package policytest

import (
	"context"
	"fmt"
	"sync"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Tenants is a tenant lookup keyed by UUID; a missing tenant wraps
// iface.ErrTenantNotFound like the tenant module does.
type Tenants map[string]*iface.Tenant

func (t Tenants) GetTenant(_ context.Context, id string) (*iface.Tenant, error) {
	if tn, ok := t[id]; ok {
		return tn, nil
	}
	return nil, fmt.Errorf("tenant %s: %w", id, iface.ErrTenantNotFound)
}

// Sink records audit events.
type Sink struct {
	mu     sync.Mutex
	events []iface.AuditEvent
}

func (s *Sink) Emit(_ context.Context, e iface.AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *Sink) Events() []iface.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]iface.AuditEvent(nil), s.events...)
}

func (s *Sink) Actions() []string {
	events := s.Events()
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Action)
	}
	return out
}
