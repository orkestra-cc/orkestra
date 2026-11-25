package services

import (
	"context"
	"errors"
	"slices"

	"github.com/orkestra/backend/internal/core/retrieval/models"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// ErasureReader finds the tombstones that may cover the given keys.
type ErasureReader interface {
	Covering(ctx context.Context, corpus string, sourceIDs, subjects []string) ([]models.Erasure, error)
}

// Probe is one item to check: its source ID and the subjects it is about.
type Probe struct {
	SourceID string
	Subjects []string
}

// Tombstones answers whether erasures cover items.
type Tombstones struct{ reader ErasureReader }

func NewTombstones(r ErasureReader) *Tombstones { return &Tombstones{reader: r} }

// Covered reports, per SourceID, whether any tombstone covers it: global user
// tombstone on any subject, tenant tombstone for the tenant, item tombstone
// for (tenant, corpus, sourceID), subject tombstone for (tenant, subject).
// Callers treat an error as "covered" for writes (the worker reschedules; the
// read path fails closed).
func (t *Tombstones) Covered(ctx context.Context, tenantID, corpus string, probes []Probe) (map[string]bool, error) {
	if tenantID == "" {
		return nil, errors.New("retrieval tombstones: tenant ID is required")
	}
	if ctxTenant, _ := ctxauth.GetTenantID(ctx); ctxTenant != tenantID {
		return nil, errors.New("retrieval tombstones: context tenant does not match the requested tenant")
	}
	out := make(map[string]bool, len(probes))
	if len(probes) == 0 {
		return out, nil
	}
	var ids, subjects []string
	for _, p := range probes {
		out[p.SourceID] = false
		ids = append(ids, p.SourceID)
		for _, s := range p.Subjects {
			if !slices.Contains(subjects, s) {
				subjects = append(subjects, s)
			}
		}
	}
	docs, err := t.reader.Covering(ctx, corpus, ids, subjects)
	if err != nil {
		return nil, err
	}
	for _, p := range probes {
		for _, e := range docs {
			if covers(e, tenantID, corpus, p) {
				out[p.SourceID] = true
				break
			}
		}
	}
	return out, nil
}

// covers re-matches a stored tombstone against one probe; the reader is not
// trusted to have filtered exactly.
func covers(e models.Erasure, tenantID, corpus string, p Probe) bool {
	switch e.Scope {
	case models.ErasureScopeGlobal:
		return e.Kind == models.ErasureKindUser && e.Subject != "" && slices.Contains(p.Subjects, e.Subject)
	case models.ErasureScopeTenant:
		if e.TenantID != tenantID {
			return false
		}
		switch e.Kind {
		case models.ErasureKindTenant:
			return true
		case models.ErasureKindItem:
			return e.Corpus == corpus && e.SourceID == p.SourceID
		case models.ErasureKindSubject:
			return e.Subject != "" && slices.Contains(p.Subjects, e.Subject)
		}
	}
	return false
}
