package utils

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// LogPolicyResolver hands the PolicyHandler the log-content policy for a
// tenant; tenantID == "" asks for the strictest policy in force (spec D6).
// The returned pointer is immutable and must stay the same while the policy
// is unchanged: the handler caches derived handlers by pointer.
type LogPolicyResolver interface {
	LogContentFor(tenantID string) *iface.LogContentPolicy
}

// StaticLogPolicyResolver serves one policy to everyone. SetupLogger boots
// with the platform defaults until the compliance module swaps in the live
// resolver.
type StaticLogPolicyResolver struct{ policy *iface.LogContentPolicy }

func NewStaticLogPolicyResolver(p iface.LogContentPolicy) StaticLogPolicyResolver {
	return StaticLogPolicyResolver{policy: &p}
}

func (r StaticLogPolicyResolver) LogContentFor(string) *iface.LogContentPolicy { return r.policy }

type logPolicyBox struct {
	p atomic.Pointer[LogPolicyResolver]
}

// globalPolicyBox is the resolver box of the handler built by the most
// recent SetupLogger; SwapLogPolicyResolver targets it (mirrors
// globalPerModule / SwapLevelResolver).
var globalPolicyBox atomic.Pointer[logPolicyBox]

// SwapLogPolicyResolver replaces the resolver behind every logger derived
// from the most recent SetupLogger. Called from main.go once the compliance
// module is up. No-op before SetupLogger or with a nil resolver.
func SwapLogPolicyResolver(r LogPolicyResolver) {
	if b := globalPolicyBox.Load(); b != nil && r != nil {
		b.p.Store(&r)
	}
}

// fallbackPolicy is used when no resolver answers. Package-level so its
// address is stable for the derived-handler cache.
var fallbackPolicy = iface.DefaultLogContentPolicy()

const maxDerivedHandlers = 32

// withStep is one WithAttrs or WithGroup call, kept raw: attributes are
// masked at write time because the policy depends on the record's tenant
// and can change at runtime (spec §2.3).
type withStep struct {
	group string
	attrs []slog.Attr
}

// derivedEntry is the base handler with the With steps replayed under one
// policy. When a WithGroup name is claimed by a group key rule (secret, IP,
// user agent, subject or PII key) the group is sealed: steps after it are
// discarded and everything nested under it collapses into the single outcome
// attribute, exactly as an inline slog.Group with that name would.
type derivedEntry struct {
	h slog.Handler
	// sealed: only outcome (when keep) is emitted, and only when something
	// was logged under the group (nested: later With attrs exist).
	sealed  bool
	keep    bool
	outcome slog.Attr
	nested  bool
}

type derivedHandlers struct {
	mu sync.RWMutex
	m  map[*iface.LogContentPolicy]*derivedEntry
}

// PolicyHandler masks every record according to the compliance policy of
// the record's tenant before it reaches the fan-out (stdout and OTLP).
type PolicyHandler struct {
	base    slog.Handler
	box     *logPolicyBox
	hashKey []byte
	steps   []withStep
	root    *derivedEntry // base itself: the answer when there are no steps
	cache   *derivedHandlers
}

func NewPolicyHandler(base slog.Handler, r LogPolicyResolver, hashKey []byte) *PolicyHandler {
	box := &logPolicyBox{}
	if r != nil {
		box.p.Store(&r)
	}
	return &PolicyHandler{base: base, box: box, hashKey: hashKey, root: &derivedEntry{h: base}, cache: newDerivedHandlers()}
}

func newDerivedHandlers() *derivedHandlers {
	return &derivedHandlers{m: map[*iface.LogContentPolicy]*derivedEntry{}}
}

// SetResolver swaps the resolver for this handler and all its clones.
func (h *PolicyHandler) SetResolver(r LogPolicyResolver) {
	if r != nil {
		h.box.p.Store(&r)
	}
}

func (h *PolicyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *PolicyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.with(withStep{attrs: slices.Clone(attrs)})
}

func (h *PolicyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.with(withStep{group: name})
}

func (h *PolicyHandler) with(s withStep) *PolicyHandler {
	return &PolicyHandler{
		base:    h.base,
		box:     h.box,
		hashKey: h.hashKey,
		steps:   append(slices.Clip(h.steps), s),
		root:    h.root,
		cache:   newDerivedHandlers(),
	}
}

func (h *PolicyHandler) Handle(ctx context.Context, r slog.Record) error {
	p := h.policyFor(ctx)
	m := logMasker{p: p, key: h.hashKey}
	out := slog.NewRecord(r.Time, r.Level, m.safeText(r.Message), r.PC)
	d := h.derived(p)
	if d.sealed {
		// The record's own attributes live under a sealed group: they are
		// replaced by the group's single masked outcome, or dropped.
		if d.keep && (d.nested || r.NumAttrs() > 0) {
			out.AddAttrs(d.outcome)
		}
		return d.h.Handle(ctx, out)
	}
	// Collected and added in one call: adding attribute by attribute grows the
	// record's overflow slice repeatedly.
	var buf [16]slog.Attr
	masked := buf[:0]
	r.Attrs(func(a slog.Attr) bool {
		if ma, keep := m.maskAttr(a); keep {
			masked = append(masked, ma)
		}
		return true
	})
	out.AddAttrs(masked...)
	return d.h.Handle(ctx, out)
}

func (h *PolicyHandler) policyFor(ctx context.Context) *iface.LogContentPolicy {
	tenant := ""
	if ctx != nil {
		if t, ok := ctxauth.GetTenantID(ctx); ok {
			tenant = t
		} else {
			tenant = ctxauth.RequestAnnotationsFrom(ctx).Snapshot().TenantID
		}
	}
	if rp := h.box.p.Load(); rp != nil {
		if p := (*rp).LogContentFor(tenant); p != nil {
			return p
		}
	}
	return &fallbackPolicy
}

// derived returns base with this handler's With steps replayed, masked
// under p. Cached per policy pointer; the cache is reset past a small bound
// so replaced policies cannot accumulate.
func (h *PolicyHandler) derived(p *iface.LogContentPolicy) *derivedEntry {
	if len(h.steps) == 0 {
		return h.root
	}
	h.cache.mu.RLock()
	d, ok := h.cache.m[p]
	h.cache.mu.RUnlock()
	if ok {
		return d
	}
	m := logMasker{p: p, key: h.hashKey}
	d = &derivedEntry{h: h.base}
replay:
	for i, s := range h.steps {
		if s.group != "" {
			if attr, keep, handled := m.groupKeyRule(s.group); handled {
				d.sealed, d.keep, d.outcome = true, keep, attr
				for _, later := range h.steps[i+1:] {
					if len(later.attrs) > 0 {
						d.nested = true
					}
				}
				break replay
			}
			d.h = d.h.WithGroup(s.group)
			continue
		}
		masked := make([]slog.Attr, 0, len(s.attrs))
		for _, a := range s.attrs {
			if ma, keep := m.maskAttr(a); keep {
				masked = append(masked, ma)
			}
		}
		d.h = d.h.WithAttrs(masked)
	}
	h.cache.mu.Lock()
	if len(h.cache.m) >= maxDerivedHandlers {
		clear(h.cache.m)
	}
	h.cache.m[p] = d
	h.cache.mu.Unlock()
	return d
}

// LogHashKeyFromEnv derives the HMAC key for "hashed" log values from
// OAUTH_TOKEN_ENCRYPTION_KEY (32-byte hex) with HKDF-SHA256. Nothing is
// stored and every replica derives the same key. Returns nil when the
// variable is missing or malformed: hashed modes then drop the value.
func LogHashKeyFromEnv() []byte {
	secret, err := hex.DecodeString(os.Getenv("OAUTH_TOKEN_ENCRYPTION_KEY"))
	if err != nil || len(secret) != 32 {
		return nil
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "orkestra/log-hash/v1", 32)
	if err != nil {
		return nil
	}
	return key
}
