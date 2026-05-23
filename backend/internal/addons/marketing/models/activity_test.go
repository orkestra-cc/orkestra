package models

import (
	"errors"
	"testing"
	"time"
)

// TestComputeDedupKeyDeterministic pins the canonical form of the
// dedup_key: re-running the hash on the same inputs must always
// produce the same digest. This is the contract the repository's
// unique index relies on for idempotent re-imports.
func TestComputeDedupKeyDeterministic(t *testing.T) {
	when := time.Date(2026, 5, 21, 10, 30, 0, 0, time.UTC)
	a := ComputeDedupKey("person-uuid-1", KindEmailOpened, when, "esp:msg:42")
	b := ComputeDedupKey("person-uuid-1", KindEmailOpened, when, "esp:msg:42")
	if a != b {
		t.Errorf("ComputeDedupKey not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("ComputeDedupKey length = %d, want 64 (sha256 hex)", len(a))
	}
}

// TestComputeDedupKeyDistinguishesInputs makes sure that changing any
// component of the tuple yields a different key — otherwise the
// dedup_key invariant breaks.
func TestComputeDedupKeyDistinguishesInputs(t *testing.T) {
	when := time.Date(2026, 5, 21, 10, 30, 0, 0, time.UTC)
	base := ComputeDedupKey("person-1", KindEmailOpened, when, "ext-1")
	cases := []struct {
		name string
		got  string
	}{
		{"different person", ComputeDedupKey("person-2", KindEmailOpened, when, "ext-1")},
		{"different kind", ComputeDedupKey("person-1", KindEmailClicked, when, "ext-1")},
		{"different time", ComputeDedupKey("person-1", KindEmailOpened, when.Add(time.Second), "ext-1")},
		{"different externalId", ComputeDedupKey("person-1", KindEmailOpened, when, "ext-2")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got == base {
				t.Errorf("ComputeDedupKey should differ from base for %s", c.name)
			}
		})
	}
}

// TestComputeDedupKeyUTCNormalisation pins the timezone behaviour:
// two occurredAt timestamps that represent the same instant in
// different zones must hash to the same key. Otherwise an ESP that
// reports UTC and an importer that injects Europe/Rome would
// fingerprint the same event twice.
func TestComputeDedupKeyUTCNormalisation(t *testing.T) {
	utc := time.Date(2026, 5, 21, 10, 30, 0, 0, time.UTC)
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skip("Europe/Rome timezone unavailable on this host")
	}
	// Same instant, different zone (CEST = UTC+2 on this date)
	romeTime := utc.In(rome)

	a := ComputeDedupKey("p", KindEmailOpened, utc, "ext")
	b := ComputeDedupKey("p", KindEmailOpened, romeTime, "ext")
	if a != b {
		t.Errorf("dedup_key not stable across timezones: utc=%q rome=%q", a, b)
	}
}

// TestComputeDedupKeyEmptyExternalID accepts an empty externalId.
// Manual activities mint a UUID for externalId in the service layer
// before reaching the hasher; this test pins the pure-function
// behaviour when callers pass "".
func TestComputeDedupKeyEmptyExternalID(t *testing.T) {
	when := time.Date(2026, 5, 21, 10, 30, 0, 0, time.UTC)
	got := ComputeDedupKey("p", KindCallMade, when, "")
	if got == "" {
		t.Fatalf("ComputeDedupKey returned empty for empty externalId")
	}
}

// TestActivityValidateHappyPath confirms a fully-populated activity
// passes Validate.
func TestActivityValidateHappyPath(t *testing.T) {
	a := &Activity{
		UUID:       "act-1",
		PersonUUID: "person-1",
		Kind:       KindEmailOpened,
		OccurredAt: time.Now().UTC(),
		Source:     ActivitySourceImporter,
	}
	if err := a.Validate(); err != nil {
		t.Errorf("Validate happy path: unexpected error %v", err)
	}
}

// TestActivityValidateRejections walks every required-field path so a
// regression that drops a check fails loudly here.
func TestActivityValidateRejections(t *testing.T) {
	good := Activity{
		PersonUUID: "person-1",
		Kind:       KindEmailOpened,
		OccurredAt: time.Now().UTC(),
		Source:     ActivitySourceImporter,
	}

	cases := []struct {
		name   string
		mutate func(*Activity)
	}{
		{"nil-personUuid", func(a *Activity) { a.PersonUUID = "" }},
		{"empty-kind", func(a *Activity) { a.Kind = "" }},
		{"unknown-kind", func(a *Activity) { a.Kind = ActivityKind("invented") }},
		{"zero-occurredAt", func(a *Activity) { a.OccurredAt = time.Time{} }},
		{"empty-source", func(a *Activity) { a.Source = "" }},
		{"unknown-source", func(a *Activity) { a.Source = ActivitySource("ghost") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := good
			c.mutate(&a)
			err := a.Validate()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidActivity) {
				t.Errorf("expected ErrInvalidActivity in chain, got %v", err)
			}
		})
	}
}

// TestActivityValidateNilReceiver checks the defensive guard against
// nil — services never pass nil, but a missed nil-check upstream
// would otherwise panic.
func TestActivityValidateNilReceiver(t *testing.T) {
	var a *Activity
	if err := a.Validate(); !errors.Is(err, ErrInvalidActivity) {
		t.Errorf("expected ErrInvalidActivity for nil receiver, got %v", err)
	}
}

// TestActivityRefsIsZero pins the predicate the service layer uses
// to decide whether to emit refs at all on insert.
func TestActivityRefsIsZero(t *testing.T) {
	var zero ActivityRefs
	if !zero.IsZero() {
		t.Errorf("zero ActivityRefs.IsZero() = false, want true")
	}
	nonZero := ActivityRefs{CampaignUUID: "c-1"}
	if nonZero.IsZero() {
		t.Errorf("populated ActivityRefs.IsZero() = true, want false")
	}
}

// TestIsKnownKindIsManualKind pins the surface used by the POST
// manual-activity handler. Operators must not be able to forge an
// email_opened by hand, and unknown kinds must reject at validation.
func TestIsKnownKindIsManualKind(t *testing.T) {
	if !IsKnownKind(KindEmailOpened) {
		t.Errorf("KindEmailOpened expected to be known")
	}
	if IsKnownKind(ActivityKind("nope")) {
		t.Errorf("'nope' should not be a known kind")
	}
	if !IsManualKind(KindCallMade) {
		t.Errorf("KindCallMade expected to be manual")
	}
	if IsManualKind(KindEmailOpened) {
		t.Errorf("KindEmailOpened must not be manual — webhook-only")
	}
}

// TestIsKnownSource mirrors the kind check for the Source enum.
func TestIsKnownSource(t *testing.T) {
	if !IsKnownSource(ActivitySourceImporter) {
		t.Errorf("ActivitySourceImporter expected known")
	}
	if IsKnownSource(ActivitySource("rumor")) {
		t.Errorf("'rumor' must not be a known source")
	}
}
