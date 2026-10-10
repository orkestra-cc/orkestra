package ctxauth

import (
	"context"
	"sync"
)

// RequestAnnotations carries values resolved downstream — the principal that
// authentication resolves, and the machine-readable code of an error response
// plus whether that error is an expected unavailability —
// back up to middleware that ran before them. RequireAuth hands the next
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
	// ErrorCode is the code of the error response the handler wrote, "" when
	// the response carried none (see SetErrorCode).
	ErrorCode string
	// ExpectedUnavailable is true when the error response declared itself an
	// expected unavailability of an optional, unconfigured feature (see
	// MarkExpectedUnavailable).
	ExpectedUnavailable bool
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

// SetErrorCode records the machine-readable code of the error response being
// written. The first non-empty code wins.
func (a *RequestAnnotations) SetErrorCode(code string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.snap.ErrorCode == "" {
		a.snap.ErrorCode = code
	}
}

// MarkExpectedUnavailable records that the error response being written
// declared itself an expected unavailability of an optional feature this
// installation has not configured, so the request logger can grade it apart
// from a real server fault. The code alone never implies it: the error must
// opt in (errcode.FeatureNotConfigured).
func (a *RequestAnnotations) MarkExpectedUnavailable() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.snap.ExpectedUnavailable = true
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
