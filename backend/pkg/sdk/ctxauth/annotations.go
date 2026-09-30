package ctxauth

import (
	"context"
	"sync"
)

// RequestAnnotations carries the principal that authentication resolves
// back up to middleware that ran before it. RequireAuth hands the next
// handler a derived context, so an outer middleware such as the request
// logger never sees the values it stamps; a pointer installed in the outer
// context is shared with every derived one and closes that gap.
type RequestAnnotations struct {
	mu   sync.RWMutex
	snap AnnotationSnapshot
}

// AnnotationSnapshot is a copy of the annotations at one point in time.
type AnnotationSnapshot struct {
	TenantID   string
	TenantKind string
	UserID     string
	UserRole   string
	Audience   string
}

type annotationsKey struct{}

// WithRequestAnnotations installs an empty holder in ctx.
func WithRequestAnnotations(ctx context.Context) (context.Context, *RequestAnnotations) {
	a := &RequestAnnotations{}
	return context.WithValue(ctx, annotationsKey{}, a), a
}

// RequestAnnotationsFrom returns the holder installed in ctx, or nil.
func RequestAnnotationsFrom(ctx context.Context) *RequestAnnotations {
	if ctx == nil {
		return nil
	}
	a, _ := ctx.Value(annotationsKey{}).(*RequestAnnotations)
	return a
}

// SetPrincipal records the resolved principal. Empty values are ignored so a
// later, less informed stamp cannot erase an earlier one.
func (a *RequestAnnotations) SetPrincipal(tenantID, tenantKind, userID, userRole string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	setIfNotEmpty(&a.snap.TenantID, tenantID)
	setIfNotEmpty(&a.snap.TenantKind, tenantKind)
	setIfNotEmpty(&a.snap.UserID, userID)
	setIfNotEmpty(&a.snap.UserRole, userRole)
}

// SetAudience records the audience RequireAudience matched.
func (a *RequestAnnotations) SetAudience(aud string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	setIfNotEmpty(&a.snap.Audience, aud)
}

// Snapshot returns a copy of the recorded values.
func (a *RequestAnnotations) Snapshot() AnnotationSnapshot {
	if a == nil {
		return AnnotationSnapshot{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.snap
}

func setIfNotEmpty(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}
