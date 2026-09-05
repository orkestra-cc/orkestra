// Package metrics owns the Orkestra backend's Prometheus metric surface.
//
// Phase 5.3 of the tenancy plan lands three metric families that make the
// preceding phases' invariants measurable:
//
//   - orkestra_cedar_shadow_divergence_total — every time the Cedar engine
//     disagrees with the legacy role-table decision in shadow mode. The
//     drift signal operators watch before flipping Cedar to enforce.
//   - orkestra_capability_denied_total — every 402 Payment Required
//     returned by RequireCapability. Shows which capabilities generate
//     the most tenant friction ("who bought the wrong tier?").
//   - orkestra_entitlement_projection_lag_seconds — time since the last
//     successful entitlement grant/revoke landed, grouped by tenant tier.
//     The Phase 2 plan calls for <2s propagation; this exposes it.
//
// ADR-0005 Phase B adds a fourth family:
//
//   - orkestra_http_request_duration_seconds — request latency histogram
//     labelled by audience / method / route-template / status_class with
//     trace_id attached as a Prometheus exemplar so Grafana can jump from
//     a slow bucket straight to the matching Tempo span. Populated by the
//     structured request logger middleware.
//
// Session revocation telemetry adds:
//
//   - orkestra_auth_session_revocation_store_failures_total — bounded
//     lookup/write Redis failures while evaluating or recording revoked JWT
//     session identifiers. Lookup failures still fail open.
//
// ADR-0017 (session lifetime and token retention) adds three more:
//
//   - orkestra_auth_session_cap_expiries_total — count of sessions
//     terminated for reaching the configured absolute maximum age.
//     Unlabelled: distinguishes a cap that works from one signing out
//     too many people.
//   - orkestra_auth_session_cap_event_failures_total — count of cap
//     expiries whose security-event write failed. Unlabelled; the
//     credentials are already terminated, so this is observational
//     only.
//   - orkestra_auth_session_anchor_anomalies_total — count of refreshes
//     permitted because the cap could not read a session-cap anchor,
//     labelled by kind ("missing" or "zero_timestamp", anything else
//     collapses to "unknown"). Zero for 30 consecutive production days
//     is the gate on tightening the ADR-0017 compatibility window to
//     fail-closed.
//
// The refresh-token retention sweep (ADR-0017) adds three more, all
// labelled by tier ("operator" or "client"; anything else collapses to
// "unknown"):
//
//   - orkestra_auth_token_sweep_deleted_total — rows deleted per
//     completed sweep batch.
//   - orkestra_auth_token_sweep_backlog_estimate — the current backlog
//     estimate. Seeded by a single indexed count on entry to drain
//     mode, then decremented locally as batches delete rows, and reset
//     when a tier reports no more work. Never recomputed exactly every
//     cycle — at the five-minute drain cadence that would scan the
//     whole eligible range 288 times a day for a number that stays an
//     approximation either way. Operators watch it reach zero to know
//     the first cleanup of an upgraded installation has finished.
//   - orkestra_auth_token_sweep_duration_seconds — wall time of one
//     sweep batch.
//
// The auth attempt counters (spec 2026-09-03 §4.1 D10) add three more:
//
//   - orkestra_auth_attempt_store_failures_total — Redis attempt-counter
//     operations that could not be answered, labelled by operation
//     (peek / record / reset). Every one fails OPEN to the durable
//     per-account lock, so this measures degraded throttling, not
//     failed logins.
//   - orkestra_auth_lockouts_total — increments that reached their
//     threshold, labelled by scope (ip / email / client / reset-* /
//     verify-* / mfa-verify). scope="ip" is the stuffing alert.
//   - orkestra_auth_mail_dropped_total — transactional auth emails
//     dropped by a full dispatcher queue, labelled by template id.
//
// The authz invalidation contract (spec 2026-09-03 §4.6 D27, as amended
// by ruling P22) adds two, both unlabelled — the only dimensions
// available are the user UUID and the operation, and neither is bounded:
//
//   - orkestra_authz_cache_invalidation_failures_total — permission-cache
//     generation bumps that failed AFTER the write had landed, so a
//     verdict cached around the write can be served until its 60s TTL
//     expires. Every revocation and every platform-issued grant reports
//     here; the change itself stood.
//   - orkestra_authz_cache_invalidation_refusals_total — grants REFUSED
//     because the cache could not be retired before the write. Nothing
//     was written. A moving rate means operators cannot change
//     permissions at all, which is a louder condition than the one
//     above and deserves its own alert.
//
// ADR-0002 (docs/adr/0002-metrics-label-schema.md) freezes the label
// schema. Adding labels requires a new ADR — Prometheus cardinality
// explodes silently, and history breaks when labels change. The raw
// tenant.id is deliberately NOT a label on any metric here; it lives on
// span attributes in Tempo, not on Prometheus time series.
package metrics

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector bundles the metric families alongside the registry they
// are registered on. One Collector is created at boot (Register) and
// reused by every call site.
//
// Keeping the metrics behind a struct (rather than package-level globals)
// lets tests spin up an isolated registry per case — important because
// client_golang panics when the same metric is registered on the default
// registry twice during `go test -count=N`.
type Collector struct {
	registry *prometheus.Registry

	cedarDivergence                *prometheus.CounterVec
	cedarEnforced                  *prometheus.CounterVec
	capabilityDenied               *prometheus.CounterVec
	sessionRevocationStoreFailures *prometheus.CounterVec
	sessionCapExpiries             prometheus.Counter
	sessionCapEventFailures        prometheus.Counter
	sessionAnchorAnomalies         *prometheus.CounterVec
	attemptStoreFailures           *prometheus.CounterVec
	authLockouts                   *prometheus.CounterVec
	authMailDropped                *prometheus.CounterVec
	tokenSweepDeleted              *prometheus.CounterVec
	authzCacheInvalidationFailures prometheus.Counter
	authzCacheInvalidationRefusals prometheus.Counter
	tokenSweepBacklog              *prometheus.GaugeVec
	tokenSweepDuration             *prometheus.HistogramVec

	// entitlementLag is a GaugeFunc that reads lastApply on every scrape;
	// the map is keyed by tenant kind ("internal" | "external"). Stored
	// under a RWMutex because the scrape and the mutation path race.
	entitlementLag *prometheus.GaugeVec
	lastApplyMu    sync.RWMutex
	lastApply      map[string]time.Time

	// httpDuration is the ADR-0005 Phase B latency histogram. Cast to the
	// ObserverVec interface for exemplar support — ObserveWithExemplar is
	// only available on the *prometheus.HistogramVec receiver, but the
	// type assertion isolates that detail so callers stay vanilla.
	httpDuration *prometheus.HistogramVec

	// registered tracks whether the collector has already been bound to
	// the registry, so double-registration in tests is a no-op rather
	// than a panic.
	registered uint32
}

// NewCollector builds a Collector on a fresh registry. Use Default()
// unless you are writing a test.
func NewCollector() *Collector {
	c := &Collector{
		registry:  prometheus.NewRegistry(),
		lastApply: map[string]time.Time{},
	}
	c.buildMetrics()
	return c
}

func (c *Collector) buildMetrics() {
	c.cedarDivergence = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "cedar",
			Name:      "shadow_divergence_total",
			Help:      "Count of Cedar shadow evaluations whose decision disagreed with the role-table decision. See ADR-0002.",
		},
		// Labels: action_suffix is the tail of the dotted permission key
		// (read / create / update / …) — low cardinality by design.
		// outcome captures which side said yes ("role_only", "cedar_only",
		// "both", "neither"). matched_policy is the Cedar policy id that
		// fired; may be empty for "no match" outcomes.
		[]string{"action_suffix", "matched_policy", "outcome"},
	)

	c.cedarEnforced = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "cedar",
			Name:      "enforced_total",
			Help:      "Count of authorization checks for actions where Cedar is the authoritative verdict (Section B item #1 enforce mode). outcome distinguishes whether Cedar agreed with the role table, overrode it, or fell back due to a Cedar-side failure.",
		},
		// Labels: action_suffix is the dotted-key tail (low cardinality).
		// outcome is one of agree_allow, agree_deny, cedar_override_allow,
		// cedar_override_deny, fallback_role.
		[]string{"action_suffix", "outcome"},
	)

	c.capabilityDenied = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "capability",
			Name:      "denied_total",
			Help:      "Count of requests that failed with 402 Payment Required because the acting tenant lacked an entitlement to the required capability.",
		},
		// capability_id comes from the Capability catalog (finite set
		// declared by modules at boot — cardinality bounded by
		// len(Capabilities)).
		[]string{"capability_id"},
	)

	c.sessionRevocationStoreFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "session_revocation_store_failures_total",
			Help:      "Count of Redis failures while reading or writing revoked session identifiers.",
		},
		[]string{"operation"},
	)

	c.sessionCapExpiries = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "session_cap_expiries_total",
			Help:      "Count of sessions terminated because they reached the configured absolute maximum age. Unlabelled by design (ADR-0017 D8): the value distinguishes a cap that works from one signing out too many people, and no dimension of it is bounded.",
		},
	)

	c.sessionCapEventFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "session_cap_event_failures_total",
			Help:      "Count of cap expiries whose security-event write failed. Credentials are already terminated when this increments — the failure is observational, never restorative.",
		},
	)

	c.sessionAnchorAnomalies = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "session_anchor_anomalies_total",
			Help:      "Count of refreshes that could not read a session-cap anchor and were permitted under the ADR-0017 compatibility window. Zero for 30 consecutive production days is the gate on tightening this to fail-closed.",
		},
		// kind is a closed allowlist: "missing" (clean not-found) or
		// "zero_timestamp" (row present, no usable StartedAt/CreatedAt).
		// Repository errors are NOT anomalies — they fail closed and use
		// ordinary error telemetry. ADR-0017 D8.
		[]string{"kind"},
	)

	c.attemptStoreFailures = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "attempt_store_failures_total",
			Help:      "Count of Redis attempt-counter operations that could not be answered. Every one of these fails OPEN — the durable per-account lock is the second line — so a sustained non-zero rate means brute-force throttling is degraded, not that logins are failing.",
		},
		// operation is one of peek, record, reset; anything else
		// collapses to unknown (ADR-0002 cardinality bound).
		[]string{"operation"},
	)

	c.authLockouts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "lockouts_total",
			Help:      "Count of attempt-counter increments that reached their threshold. scope=\"ip\" moving is the credential-stuffing signal worth alerting on; scope=\"email\" is ordinary user error at low rates.",
		},
		// scope is the closed set declared in attempt_counter.go;
		// anything else collapses to unknown. NEVER the identifier
		// itself — that would make every attacked address a time series.
		[]string{"scope"},
	)

	c.authMailDropped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "mail_dropped_total",
			Help:      "Count of transactional auth emails dropped because the bounded dispatcher's queue was full. A drop is a lost mail the user can re-request inside the per-address caps; a non-zero rate means the queue or the worker count is undersized.",
		},
		// template is the notification template id — a finite set
		// declared in-tree; anything else collapses to unknown.
		[]string{"template"},
	)

	c.tokenSweepDeleted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "token_sweep_deleted_total",
			Help:      "Refresh-token rows deleted by the retention sweep, per audience tier.",
		},
		// Closed label set: tier ∈ {operator, client}. ADR-0017 D8.
		[]string{"tier"},
	)

	c.tokenSweepBacklog = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "token_sweep_backlog_estimate",
			Help:      "Estimated refresh-token rows still eligible for deletion, per tier. An ESTIMATE: seeded by one indexed count on entry to drain mode, then decremented locally, because rows become eligible during a drain. Operators watch it reach zero to see the drain finish.",
		},
		[]string{"tier"},
	)

	c.tokenSweepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "orkestra",
			Subsystem: "auth",
			Name:      "token_sweep_duration_seconds",
			Help:      "Wall time of one refresh-token sweep batch, per tier. Measured before promotion so the first cleanup of an upgraded installation is an observed event rather than a discovered one.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"tier"},
	)

	c.authzCacheInvalidationFailures = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "authz",
			Name:      "cache_invalidation_failures_total",
			Help:      "Count of permission-cache generation bumps that failed AFTER their write had landed, leaving a verdict cached during the write readable until its 60s TTL expires. Unlabelled by design (ADR-0002): the available dimensions are the user UUID and the operation, neither of which is bounded. A bump that fails BEFORE the write is not counted here — it refuses the mutation and answers 503.",
		},
	)

	c.authzCacheInvalidationRefusals = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "authz",
			Name:      "cache_invalidation_refusals_total",
			Help:      "Count of permission GRANTS refused because the effective-permission cache could not be retired before the write. Nothing was written; the caller got 503 authz.cache_unavailable and must retry. Unlabelled by design (ADR-0002). Distinct from cache_invalidation_failures_total, where the change did land.",
		},
	)

	c.entitlementLag = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "orkestra",
			Subsystem: "entitlement",
			Name:      "projection_lag_seconds",
			Help:      "Seconds since the last successful entitlement apply (grant or revoke), per tenant kind. Permanently high means the Stripe webhook → projection path has stalled.",
		},
		[]string{"tenant_kind"},
	)

	// httpDuration: HTTP request latency histogram. Labels are bounded:
	//   audience      — "operator" | "client" | "service" (3 values).
	//   method        — HTTP verb (~7 values).
	//   route         — Chi RoutePattern() — the template (e.g.
	//                   "/v1/users/{id}"), NEVER the raw path. ADR-0002
	//                   forbids raw path labels; the template is bounded
	//                   by the OpenAPI surface. Unmatched routes (404)
	//                   collapse to "unknown" so a probe of random URLs
	//                   can't blow out cardinality.
	//   status_class  — "2xx" | "3xx" | "4xx" | "5xx" (4 values).
	// Upper bound ≈ 3 × 7 × 200 × 4 = 16800 series. Real-world will
	// stay much lower because most routes only see a handful of methods.
	c.httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "orkestra",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds, labelled by audience / method / route-template / status_class. trace_id is attached as an exemplar so Grafana's Prometheus → Tempo jump works. See ADR-0005.",
			Buckets:   prometheus.DefBuckets,
			// NativeHistogramBucketFactor enables sparse native histograms
			// when the scrape target advertises support; the classic
			// buckets remain populated for backwards compatibility.
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		},
		[]string{"audience", "method", "route", "status_class"},
	)
}

// Register adds the collector's metrics to its internal registry. Safe to
// call multiple times; subsequent calls are no-ops so call sites do not
// need to guard a boot-time sync.Once.
func (c *Collector) Register() error {
	if !atomic.CompareAndSwapUint32(&c.registered, 0, 1) {
		return nil
	}
	for _, m := range []prometheus.Collector{c.cedarDivergence, c.cedarEnforced, c.capabilityDenied, c.sessionRevocationStoreFailures, c.sessionCapExpiries, c.sessionCapEventFailures, c.sessionAnchorAnomalies, c.tokenSweepDeleted, c.tokenSweepBacklog, c.tokenSweepDuration, c.entitlementLag, c.httpDuration, c.attemptStoreFailures, c.authLockouts, c.authMailDropped, c.authzCacheInvalidationFailures, c.authzCacheInvalidationRefusals} {
		if err := c.registry.Register(m); err != nil {
			// rollback so the caller can retry with a fresh collector
			atomic.StoreUint32(&c.registered, 0)
			return err
		}
	}
	return nil
}

// Handler returns the http.Handler that serves the Prometheus exposition
// format. Mount it at /metrics on the public router; no authentication
// (deployments that need per-network ACLs should front it with an IP
// allowlist or a sidecar).
//
// EnableOpenMetrics negotiates the OpenMetrics format with clients that
// advertise support via the Accept header (modern Prometheus scrapers
// since 2.5). This is what carries exemplars on histograms — without
// it the ADR-0005 Phase B trace_id exemplars never leave the process,
// even though they are recorded. Scrapers that don't ask for
// OpenMetrics keep getting the classic text format, so the upgrade is
// backwards compatible.
func (c *Collector) Handler() http.Handler {
	return promhttp.HandlerFor(c.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// RecordCedarDivergence increments the divergence counter. outcome is one
// of "role_only" (role-table allowed, Cedar denied), "cedar_only" (Cedar
// allowed, role-table denied), "both" (both allowed — reported only when
// matchedPolicy disagrees), or "neither". actionSuffix is the last
// dotted segment of the permission key.
//
// All inputs are assumed to come from a bounded set; passing a free-form
// string here is the fastest way to blow out Prometheus cardinality.
func (c *Collector) RecordCedarDivergence(actionSuffix, matchedPolicy, outcome string) {
	if c == nil || c.cedarDivergence == nil {
		return
	}
	c.cedarDivergence.WithLabelValues(actionSuffix, matchedPolicy, outcome).Inc()
}

// RecordCedarEnforced increments the enforce counter. outcome is one of
// "agree_allow", "agree_deny", "cedar_override_allow", "cedar_override_deny"
// (Cedar's verdict differed from the role-table and won), or "fallback_role"
// (Cedar errored or panicked; the call resolved to the role-table verdict).
//
// Same cardinality discipline as RecordCedarDivergence: pass an
// action_suffix from a bounded set (the tail of a permission key).
func (c *Collector) RecordCedarEnforced(actionSuffix, outcome string) {
	if c == nil || c.cedarEnforced == nil {
		return
	}
	c.cedarEnforced.WithLabelValues(actionSuffix, outcome).Inc()
}

// RecordCapabilityDenied increments the 402 counter. capabilityID must be
// one of the IDs declared in a module's Capabilities() — values outside
// that set indicate a wiring bug caught by the Phase 5.1 policy-coverage
// gate.
func (c *Collector) RecordCapabilityDenied(capabilityID string) {
	if c == nil || c.capabilityDenied == nil {
		return
	}
	c.capabilityDenied.WithLabelValues(capabilityID).Inc()
}

// RecordSessionRevocationStoreFailure increments the Redis-revocation
// failure counter. The operation label is deliberately limited to lookup,
// write, and unknown to keep metric cardinality bounded.
func (c *Collector) RecordSessionRevocationStoreFailure(operation string) {
	if c == nil || c.sessionRevocationStoreFailures == nil {
		return
	}
	if operation != "lookup" && operation != "write" {
		operation = "unknown"
	}
	c.sessionRevocationStoreFailures.WithLabelValues(operation).Inc()
}

// RecordSessionCapExpiry counts one session terminated for reaching its
// configured maximum age. Emitted only by the caller that won the
// isActive transition, so concurrent refreshes on the same session count
// once. ADR-0017 D4.
func (c *Collector) RecordSessionCapExpiry() {
	if c == nil || c.sessionCapExpiries == nil {
		return
	}
	c.sessionCapExpiries.Inc()
}

// RecordSessionCapEventFailure counts a cap expiry whose security-event
// write failed. Durable state is already terminated at that point.
func (c *Collector) RecordSessionCapEventFailure() {
	if c == nil || c.sessionCapEventFailures == nil {
		return
	}
	c.sessionCapEventFailures.Inc()
}

// RecordAuthzCacheInvalidationFailure counts one post-write
// permission-cache invalidation that failed. The write has already
// landed when this increments, so the failure is observational: a
// verdict cached during the write survives until its own 60s TTL. A
// non-zero rate means permission changes can be up to a minute late,
// not that they were lost. Spec 2026-09-03 §4.6 D27.
func (c *Collector) RecordAuthzCacheInvalidationFailure() {
	if c == nil || c.authzCacheInvalidationFailures == nil {
		return
	}
	c.authzCacheInvalidationFailures.Inc()
}

// RecordAuthzCacheInvalidationRefusal counts one permission grant turned
// away because the cache could not be retired before the write. Nothing
// was written when this increments — the caller was told to retry.
// Spec 2026-09-03 §4.6 D27 / ruling P22.
func (c *Collector) RecordAuthzCacheInvalidationRefusal() {
	if c == nil || c.authzCacheInvalidationRefusals == nil {
		return
	}
	c.authzCacheInvalidationRefusals.Inc()
}

// RecordSessionAnchorAnomaly counts a refresh permitted because the cap
// could not read an anchor. kind is limited to "missing" and
// "zero_timestamp"; anything else collapses to "unknown" so a caller bug
// cannot turn a session UUID into a Prometheus label. ADR-0017 D8.
func (c *Collector) RecordSessionAnchorAnomaly(kind string) {
	if c == nil || c.sessionAnchorAnomalies == nil {
		return
	}
	if kind != "missing" && kind != "zero_timestamp" {
		kind = "unknown"
	}
	c.sessionAnchorAnomalies.WithLabelValues(kind).Inc()
}

// attemptStoreOperations / lockoutScopes / droppedTemplates are the
// closed label sets. Anything outside them collapses to "unknown" so a
// caller bug cannot turn an email address, an IP or a user UUID into a
// Prometheus time series (ADR-0002).
var (
	attemptStoreOperations = map[string]struct{}{"peek": {}, "record": {}, "reset": {}}
	lockoutScopes          = map[string]struct{}{
		"ip": {}, "email": {}, "client": {},
		"reset-email": {}, "reset-ip": {},
		"verify-email": {}, "verify-ip": {},
		"mfa-verify": {},
	}
	droppedTemplates = map[string]struct{}{
		"auth.reset_password":   {},
		"auth.verify_email":     {},
		"auth.mfa_factor_added": {},
	}
)

func collapse(v string, allowed map[string]struct{}) string {
	if _, ok := allowed[v]; ok {
		return v
	}
	return "unknown"
}

// RecordAuthAttemptStoreFailure counts one Redis attempt-counter
// operation that could not be answered. The caller has already failed
// open; this is the signal that throttling is degraded.
func (c *Collector) RecordAuthAttemptStoreFailure(operation string) {
	if c == nil || c.attemptStoreFailures == nil {
		return
	}
	c.attemptStoreFailures.WithLabelValues(collapse(operation, attemptStoreOperations)).Inc()
}

// RecordAuthLockout counts one increment that reached its threshold.
func (c *Collector) RecordAuthLockout(scope string) {
	if c == nil || c.authLockouts == nil {
		return
	}
	c.authLockouts.WithLabelValues(collapse(scope, lockoutScopes)).Inc()
}

// RecordAuthMailDropped counts one transactional auth email dropped by a
// full dispatcher queue.
func (c *Collector) RecordAuthMailDropped(template string) {
	if c == nil || c.authMailDropped == nil {
		return
	}
	c.authMailDropped.WithLabelValues(collapse(template, droppedTemplates)).Inc()
}

// normaliseTier keeps the sweep label set closed at {operator, client}.
// Anything else — a collection name, an empty string, a caller bug —
// collapses to "unknown" rather than minting a new time series.
func normaliseTier(tier string) string {
	if tier != "operator" && tier != "client" {
		return "unknown"
	}
	return tier
}

// RecordTokenSweep records one completed refresh-token sweep batch for a
// tier: rows deleted are added to the running total (a non-positive
// count leaves the counter untouched — the batch still happened, so
// the duration is always observed), and the batch's wall time is
// always observed. ADR-0017 D8.
func (c *Collector) RecordTokenSweep(tier string, deleted int64, duration time.Duration) {
	if c == nil || c.tokenSweepDeleted == nil {
		return
	}
	t := normaliseTier(tier)
	if deleted > 0 {
		c.tokenSweepDeleted.WithLabelValues(t).Add(float64(deleted))
	}
	c.tokenSweepDuration.WithLabelValues(t).Observe(duration.Seconds())
}

// SetTokenSweepBacklog publishes the current backlog estimate for a
// tier. The value is deliberately an estimate — seeded by one indexed
// count on entry to drain mode, then decremented locally as batches
// delete rows, and reset when the tier reports no more work — never
// recomputed exactly every cycle. See the package doc block.
func (c *Collector) SetTokenSweepBacklog(tier string, remaining int64) {
	if c == nil || c.tokenSweepBacklog == nil {
		return
	}
	if remaining < 0 {
		remaining = 0
	}
	c.tokenSweepBacklog.WithLabelValues(normaliseTier(tier)).Set(float64(remaining))
}

// RecordEntitlementApply marks an entitlement change (grant or revoke) as
// having successfully landed for the given tenant tier. The projection
// lag gauge reads time-since-last-apply on every scrape; an empty tier
// is ignored so background workers that don't know the tenant tier do
// not pollute the metric.
func (c *Collector) RecordEntitlementApply(tenantKind string) {
	if c == nil || c.entitlementLag == nil || tenantKind == "" {
		return
	}
	c.lastApplyMu.Lock()
	c.lastApply[tenantKind] = time.Now()
	c.lastApplyMu.Unlock()
	// Refresh the gauge immediately so a scrape right after apply shows
	// ~0 seconds rather than the stale previous value.
	c.refreshLag()
}

// refreshLag recomputes the gauge values for every tenant kind seen so
// far. Called from RecordEntitlementApply and from a ticker in Start.
func (c *Collector) refreshLag() {
	c.lastApplyMu.RLock()
	defer c.lastApplyMu.RUnlock()
	now := time.Now()
	for kind, when := range c.lastApply {
		c.entitlementLag.WithLabelValues(kind).Set(now.Sub(when).Seconds())
	}
}

// RecordHTTPRequest observes a single HTTP request latency. Called from
// the structured request logger middleware (ADR-0005 Phase B) after the
// downstream handler has returned, so the route pattern is populated and
// the final response status is known.
//
// audience is "operator" | "client" | "service" (or empty for surfaces
// without an audience gate — the AI sidecar's internal mode). method is
// the HTTP verb. route is the Chi RoutePattern() — the route template,
// NEVER the raw path; an empty value is rewritten to "unknown" so 404s
// on probe traffic don't blow out cardinality. status is the final HTTP
// status code; values <100 or ≥600 are clamped to "unknown" status_class
// to keep the label domain bounded.
//
// traceID, when non-empty, is attached as a Prometheus exemplar on the
// observation so Grafana's Prometheus → Tempo "View trace" jump works
// without external join tables. Pass the empty string when no span is
// active; the observation still records but without an exemplar.
//
// Safe to call with a nil receiver — the no-op path lets call sites
// pass metrics.Default() unconditionally without a nil guard.
func (c *Collector) RecordHTTPRequest(audience, method, route string, status int, duration time.Duration, traceID string) {
	if c == nil || c.httpDuration == nil {
		return
	}
	if route == "" {
		route = "unknown"
	}
	if audience == "" {
		audience = "unknown"
	}
	observer := c.httpDuration.WithLabelValues(audience, method, route, statusClassForCode(status))
	if traceID == "" {
		observer.Observe(duration.Seconds())
		return
	}
	// ObserveWithExemplar is only available on the *prometheus.Histogram
	// receiver returned by GetMetricWith[LabelValues]. The type assertion
	// is required because HistogramVec.WithLabelValues returns the
	// generic prometheus.Observer interface.
	if ex, ok := observer.(prometheus.ExemplarObserver); ok {
		ex.ObserveWithExemplar(duration.Seconds(), prometheus.Labels{"trace_id": traceID})
		return
	}
	observer.Observe(duration.Seconds())
}

// statusClassForCode collapses an integer HTTP status to a bounded label
// per ADR-0002 ("prefer status_class over http_status"). Values outside
// the standard 100–599 range fall into "unknown" rather than creating a
// new series.
func statusClassForCode(status int) string {
	switch {
	case status >= 100 && status < 200:
		return "1xx"
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500 && status < 600:
		return "5xx"
	default:
		// Defensive: keep "unknown" rather than encoding the raw int
		// so an off-by-one (e.g. status==0 from a hijacked connection)
		// can't create one series per pathological value.
		return "unknown"
	}
}

// Start launches a ticker that refreshes the entitlement-lag gauge every
// 15 seconds so a long-idle backend still reports a growing lag value.
// Returns a stop function for graceful shutdown.
func (c *Collector) Start(interval time.Duration) (stop func()) {
	if c == nil || c.entitlementLag == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				c.refreshLag()
			case <-done:
				ticker.Stop()
				return
			}
		}
	}()
	return func() { close(done) }
}

// --- package-level singleton -----------------------------------------------
//
// Call sites in the broader codebase (middleware, service layer) access
// the collector via Default() so they do not need to plumb it through
// every function signature. The singleton is lazily initialized and
// safe for concurrent use.

var (
	defaultCollector *Collector
	defaultOnce      sync.Once
)

// Default returns the process-wide collector, lazily constructing it on
// first call. main.go should call Default().Register() at boot before
// any other code writes to it.
func Default() *Collector {
	defaultOnce.Do(func() {
		defaultCollector = NewCollector()
	})
	return defaultCollector
}
