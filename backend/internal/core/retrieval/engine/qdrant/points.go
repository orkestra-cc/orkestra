package qdrant

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/orkestra/backend/internal/core/retrieval/engine"
)

const (
	rtTenantID       = "_rt.tenantId"
	rtSourceID       = "_rt.sourceId"
	rtSourceRevision = "_rt.sourceRevision"
	rtSubjects       = "_rt.subjects"
	rtVisibleTo      = "_rt.visibleTo"
)

func pointsPath(collection string) string { return collectionPath(collection) + "/points" }

// runtimePayload is payload namespace _rt: the runtime fields plus the
// indexed text.
type runtimePayload struct {
	engine.Runtime
	Text string `json:"text"`
}

type pointPayload struct {
	RT runtimePayload `json:"_rt"`
	F  map[string]any `json:"f"`
}

type pointBody struct {
	ID      string               `json:"id"`
	Vector  map[string][]float32 `json:"vector"`
	Payload pointPayload         `json:"payload"`
}

func matchValue(key string, v any) map[string]any {
	return map[string]any{"key": key, "match": map[string]any{"value": v}}
}

func matchAny(key string, vs any) map[string]any {
	return map[string]any{"key": key, "match": map[string]any{"any": vs}}
}

// Upsert writes the points conditionally on their revision: Qdrant applies
// a write only when the stored _rt.sourceRevision is <= the incoming one, so
// one request carries points of a single revision. A missing point is
// inserted whatever its revision (ADR D11 handles that on reads). A failure
// in a later revision group leaves the earlier groups written; the ADR D11
// read path and the re-queued intent handle the partial write.
func (d *Driver) Upsert(ctx context.Context, collection string, points []engine.Point) error {
	if len(points) == 0 {
		return nil
	}
	for i, p := range points {
		switch {
		case p.ID == "":
			return fmt.Errorf("qdrant: upsert: point %d has no id", i)
		case len(p.Dense) == 0:
			return fmt.Errorf("qdrant: upsert: point %d has no dense vector", i)
		case p.Runtime.TenantID == "" || p.Runtime.SourceID == "":
			return fmt.Errorf("qdrant: upsert: point %d has no tenant or source id", i)
		}
	}

	var revisions []int64
	groups := map[int64][]engine.Point{}
	for _, p := range points {
		rev := p.Runtime.SourceRevision
		if _, seen := groups[rev]; !seen {
			revisions = append(revisions, rev)
		}
		groups[rev] = append(groups[rev], p)
	}

	path := pointsPath(collection) + "?wait=true"
	for _, rev := range revisions {
		group := groups[rev]
		for start := 0; start < len(group); start += maxPointsPerRequest {
			batch := group[start:min(start+maxPointsPerRequest, len(group))]
			bodies := make([]pointBody, len(batch))
			for i, p := range batch {
				fields := p.Fields
				if fields == nil {
					fields = map[string]any{}
				}
				bodies[i] = pointBody{
					ID:     p.ID,
					Vector: map[string][]float32{denseVector: p.Dense},
					Payload: pointPayload{
						RT: runtimePayload{Runtime: p.Runtime, Text: p.Text},
						F:  fields,
					},
				}
			}
			body := map[string]any{
				"points": bodies,
				"update_filter": map[string]any{"must": []any{
					map[string]any{"key": rtSourceRevision, "range": map[string]any{"lte": rev}},
				}},
			}
			if err := d.do(ctx, "upsert points", http.MethodPut, path, body, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteOtherRevisions removes the item's points whose revision differs from
// keep, older and newer alike.
func (d *Driver) DeleteOtherRevisions(ctx context.Context, collection, tenantID, sourceID string, keep int64) error {
	if tenantID == "" || sourceID == "" {
		return errors.New("qdrant: delete other revisions: tenant and source id are required")
	}
	filter := map[string]any{
		"must":     []any{matchValue(rtTenantID, tenantID), matchValue(rtSourceID, sourceID)},
		"must_not": []any{matchValue(rtSourceRevision, keep)},
	}
	return d.deletePoints(ctx, collection, filter)
}

// DeleteByFilter removes every point the selector matches.
func (d *Driver) DeleteByFilter(ctx context.Context, collection string, sel engine.Selector) error {
	filter, err := selectorFilter(sel)
	if err != nil {
		return fmt.Errorf("qdrant: delete by filter: %w", err)
	}
	return d.deletePoints(ctx, collection, filter)
}

// Count returns the exact number of points the selector matches.
func (d *Driver) Count(ctx context.Context, collection string, sel engine.Selector) (int64, error) {
	filter, err := selectorFilter(sel)
	if err != nil {
		return 0, fmt.Errorf("qdrant: count: %w", err)
	}
	var out struct {
		Count int64 `json:"count"`
	}
	body := map[string]any{"filter": filter, "exact": true}
	if err := d.do(ctx, "count points", http.MethodPost, pointsPath(collection)+"/count", body, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

func (d *Driver) deletePoints(ctx context.Context, collection string, filter map[string]any) error {
	return d.do(ctx, "delete points", http.MethodPost, pointsPath(collection)+"/delete?wait=true",
		map[string]any{"filter": filter}, nil)
}

var (
	errEmptySelector          = errors.New("empty selector")
	errEmptySourceIDs         = errors.New("selector source ids are empty or hold an empty id")
	errSourceIDsWithoutTenant = errors.New("selector source ids need a tenant id")
)

// selectorFilter ANDs the selector's set fields. It fails closed: a zero
// selector, SourceIDs non-nil but empty or holding an empty id, or SourceIDs
// without a TenantID (source ids are tenant-local, so the same id would match
// in every tenant) would otherwise select more than the caller named.
func selectorFilter(sel engine.Selector) (map[string]any, error) {
	if sel.IsZero() {
		return nil, errEmptySelector
	}
	if sel.SourceIDs != nil && sel.TenantID == "" {
		return nil, errSourceIDsWithoutTenant
	}
	if sel.SourceIDs != nil && len(sel.SourceIDs) == 0 {
		return nil, errEmptySourceIDs
	}
	for _, id := range sel.SourceIDs {
		if id == "" {
			return nil, errEmptySourceIDs
		}
	}
	var must []any
	if sel.TenantID != "" {
		must = append(must, matchValue(rtTenantID, sel.TenantID))
	}
	if len(sel.SourceIDs) > 0 {
		must = append(must, matchAny(rtSourceID, sel.SourceIDs))
	}
	if sel.Subject != "" {
		must = append(must, matchValue(rtSubjects, sel.Subject))
	}
	return map[string]any{"must": must}, nil
}
