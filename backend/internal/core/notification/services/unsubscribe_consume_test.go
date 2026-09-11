package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
)

// ---------------------------------------------------------------------------
// Fakes for the consume sequence. Style follows notification_optout_test.go
// and notification_service_test.go: small structs, no mocks, and assertions
// on the state they persisted rather than on call bookkeeping.
// ---------------------------------------------------------------------------

// consumeDeps describes the world one consume runs in.
type consumeDeps struct {
	userUUID string // the token carries a user (so the preference step runs)
	category string // the token's category ("" exercises the marketing default)
	noToken  bool   // no token row matches the hash at all

	optoutErr error
	prefErr   error
	sinkErr   error
	claimMiss bool // the claim finds nothing: already used, or expired

	noOptoutSeam bool // the opt-out repository was never wired
	noPrefSeam   bool // the preference service was never wired
	noSink       bool // no sink firer was wired

	logTo io.Writer // capture the log lines instead of discarding them

	onOptout func()
	onClaim  func()
}

// consumeOptouts is the durable opt-out seam.
type consumeOptouts struct {
	err     error
	on      func()
	last    models.MarketingOptoutDoc
	n       int
	ordered *[]string
}

func (f *consumeOptouts) Upsert(_ context.Context, doc models.MarketingOptoutDoc) error {
	f.n++
	*f.ordered = append(*f.ordered, "optout")
	if f.on != nil {
		f.on()
	}
	if f.err != nil {
		return f.err
	}
	f.last = doc
	return nil
}

func (f *consumeOptouts) IsOptedOut(context.Context, string, string) (bool, error) {
	return false, nil
}

// consumeTokenStore is an in-memory UnsubscribeRepository that keeps the
// document's pending flags, so the tests can assert on what was persisted.
type consumeTokenStore struct {
	doc       *models.UnsubscribeTokenDoc
	claimMiss bool
	on        func()
	ordered   *[]string

	lastSinkPending bool
	lastPrefPending bool
	claims          int
}

func (f *consumeTokenStore) Create(context.Context, *models.UnsubscribeTokenDoc) error { return nil }

func (f *consumeTokenStore) GetByHash(_ context.Context, _ string) (*models.UnsubscribeTokenDoc, error) {
	if f.doc == nil {
		return nil, repository.ErrNotFound
	}
	cp := *f.doc
	return &cp, nil
}

// ClaimToken mirrors the repository: the winner is stamped, and the flags for
// the work that still has to happen go up in the same write.
func (f *consumeTokenStore) ClaimToken(_ context.Context, _ string, now time.Time, hasUser bool) (*models.UnsubscribeTokenDoc, error) {
	f.claims++
	*f.ordered = append(*f.ordered, "claim")
	if f.on != nil {
		f.on()
	}
	if f.claimMiss || f.doc == nil {
		return nil, nil
	}
	f.doc.UsedAt = &now
	f.doc.SinkPending = true
	f.lastSinkPending = true
	if hasUser {
		f.doc.PrefPending = true
		f.lastPrefPending = true
	}
	cp := *f.doc
	return &cp, nil
}

func (f *consumeTokenStore) ClearSinkPending(context.Context, string) error {
	if f.doc != nil {
		f.doc.SinkPending = false
	}
	f.lastSinkPending = false
	return nil
}

func (f *consumeTokenStore) ClearPrefPending(context.Context, string) error {
	if f.doc != nil {
		f.doc.PrefPending = false
	}
	f.lastPrefPending = false
	return nil
}

// The reconciler's three methods. Consume never calls them; the reconciler's
// own fixture models them.
func (f *consumeTokenStore) ListPending(context.Context, time.Time, int) ([]models.UnsubscribeTokenDoc, error) {
	return nil, nil
}

func (f *consumeTokenStore) RecordFailedAttempt(context.Context, string, time.Time) error { return nil }

func (f *consumeTokenStore) MarkDeadLettered(context.Context, string, time.Time) error { return nil }

// consumePrefs is the preference seam.
type consumePrefs struct {
	err     error
	ordered *[]string

	n                           int
	lastUser, lastCat, lastChan string
	lastOptedIn                 bool
}

func (f *consumePrefs) CanDeliver(context.Context, string, string, string, string) (bool, error) {
	return true, nil
}
func (f *consumePrefs) List(context.Context, string) ([]*models.PreferenceDoc, error) {
	return nil, nil
}
func (f *consumePrefs) Set(_ context.Context, user, category, channel string, optedIn bool) error {
	f.n++
	*f.ordered = append(*f.ordered, "pref")
	f.lastUser, f.lastCat, f.lastChan, f.lastOptedIn = user, category, channel, optedIn
	return f.err
}

// consumeSink records what the sink firer received.
type consumeSink struct {
	err     error
	ordered *[]string

	n                      int
	addr, category, refCtx string
}

func (f *consumeSink) fire(_ context.Context, address, category, refContext string) error {
	f.n++
	*f.ordered = append(*f.ordered, "sink")
	f.addr, f.category, f.refCtx = address, category, refContext
	return f.err
}

type consumeKit struct {
	svc     UnsubscribeService
	optouts *consumeOptouts
	store   *consumeTokenStore
	prefs   *consumePrefs
	sink    *consumeSink
	order   *[]string
}

func newConsumeKit(t *testing.T, deps consumeDeps) *consumeKit {
	t.Helper()
	order := &[]string{}

	var doc *models.UnsubscribeTokenDoc
	if !deps.noToken {
		doc = &models.UnsubscribeTokenDoc{
			UUID:      "token-uuid-1",
			TokenHash: hashToken("raw-token"),
			UserUUID:  deps.userUUID,
			Address:   "ada@example.test",
			Category:  deps.category,
			Context:   "ref-9",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}
		if deps.claimMiss {
			// The fixture a second click meets: the row is there, already spent.
			used := time.Now().Add(-time.Minute)
			doc.UsedAt = &used
		}
	}

	k := &consumeKit{
		optouts: &consumeOptouts{err: deps.optoutErr, on: deps.onOptout, ordered: order},
		store:   &consumeTokenStore{doc: doc, claimMiss: deps.claimMiss, on: deps.onClaim, ordered: order},
		prefs:   &consumePrefs{err: deps.prefErr, ordered: order},
		sink:    &consumeSink{err: deps.sinkErr, ordered: order},
		order:   order,
	}

	logger := discardLogger()
	if deps.logTo != nil {
		logger = slog.New(slog.NewTextHandler(deps.logTo, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	opts := []UnsubscribeOption{WithUnsubscribeLogger(logger)}
	if !deps.noOptoutSeam {
		opts = append(opts, WithOptouts(k.optouts))
	}
	if !deps.noPrefSeam {
		opts = append(opts, WithPreferences(k.prefs))
	}
	if !deps.noSink {
		opts = append(opts, WithUnsubscribeSink(k.sink.fire))
	}
	k.svc = NewUnsubscribeService(k.store, opts...)
	return k
}

func newConsumeTestService(t *testing.T, deps consumeDeps) UnsubscribeService {
	t.Helper()
	return newConsumeKit(t, deps).svc
}

func newConsumeTestServiceWithStore(t *testing.T, deps consumeDeps) (UnsubscribeService, *consumeTokenStore) {
	t.Helper()
	k := newConsumeKit(t, deps)
	return k.svc, k.store
}

// ---------------------------------------------------------------------------
// The sequence
// ---------------------------------------------------------------------------

func TestConsume_RecordsTheOptoutBeforeClaimingTheToken(t *testing.T) {
	// This order is what makes a crash safe: if the process dies after step
	// 1, the opt-out is already there and the token is still reusable.
	var order []string
	svc := newConsumeTestService(t, consumeDeps{
		onOptout: func() { order = append(order, "optout") },
		onClaim:  func() { order = append(order, "claim") },
	})

	if err := svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if len(order) < 2 || order[0] != "optout" || order[1] != "claim" {
		t.Fatalf("wrong order: %v", order)
	}
}

func TestConsume_OptoutFailureStopsBeforeClaiming(t *testing.T) {
	claimed := false
	svc := newConsumeTestService(t, consumeDeps{
		optoutErr: errors.New("mongo down"),
		onClaim:   func() { claimed = true },
	})
	if err := svc.Consume(context.Background(), "raw-token"); err == nil {
		t.Fatal("if the opt-out does not land, the sequence must stop")
	}
	if claimed {
		t.Fatal("the token must not be spent when the opt-out is not durable")
	}
}

func TestConsume_AlreadyUsedTokenStillRecordsTheOptout(t *testing.T) {
	// The second click on a link already used: the upsert is idempotent and
	// the answer to the recipient stays generic.
	opted := false
	svc := newConsumeTestService(t, consumeDeps{
		claimMiss: true,
		onOptout:  func() { opted = true },
	})
	if err := svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("a token already used is not an error for the caller: %v", err)
	}
	if !opted {
		t.Fatal("the opt-out has to be recorded anyway")
	}
}

func TestConsume_SinkFailureLeavesSinkPending(t *testing.T) {
	svc, store := newConsumeTestServiceWithStore(t, consumeDeps{sinkErr: errors.New("sink down")})
	if err := svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("a sink failure must not fail the public response: %v", err)
	}
	if !store.lastSinkPending {
		t.Fatal("sinkPending must stay up so the reconciler picks it up")
	}
}

func TestConsume_PreferenceFailureLeavesPrefPending(t *testing.T) {
	svc, store := newConsumeTestServiceWithStore(t, consumeDeps{userUUID: "u1", prefErr: errors.New("db down")})
	if err := svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !store.lastPrefPending {
		t.Fatal("prefPending must stay up: the error is replayed, not dropped in silence")
	}
}

// ---------------------------------------------------------------------------
// The other half of the pending contract, and the edges around the sequence
// ---------------------------------------------------------------------------

func TestConsume_SinkSuccessClearsSinkPending(t *testing.T) {
	// Symmetric to the test above: a flag left up after a success would have
	// the reconciler retrying forever.
	svc, store := newConsumeTestServiceWithStore(t, consumeDeps{})
	if err := svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if store.lastSinkPending {
		t.Fatal("sinkPending must go down once the sink accepts the opt-out")
	}
	if store.lastPrefPending {
		t.Fatal("prefPending must not go up when there is nothing to replay")
	}
}

func TestConsume_PreferenceSuccessClearsPrefPending(t *testing.T) {
	// The claim raises prefPending before the write is attempted — that is
	// what survives a crash — so the success is what has to lower it again.
	k := newConsumeKit(t, consumeDeps{userUUID: "u1"})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if k.prefs.n != 1 {
		t.Fatalf("the preference was written %d times, want 1", k.prefs.n)
	}
	if k.store.lastPrefPending {
		t.Fatal("prefPending must go down once the preference is written")
	}
	if k.store.lastSinkPending {
		t.Fatal("and so must sinkPending, the sink having accepted")
	}
}

func TestConsume_RunsTheFourStepsInOrder(t *testing.T) {
	k := newConsumeKit(t, consumeDeps{userUUID: "u1"})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	got := *k.order
	want := []string{"optout", "claim", "pref", "sink"}
	if len(got) != len(want) {
		t.Fatalf("sequence %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sequence %v, want %v", got, want)
		}
	}
}

func TestConsume_ClaimsTheTokenExactlyOnce(t *testing.T) {
	k := newConsumeKit(t, consumeDeps{userUUID: "u1"})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if k.store.claims != 1 {
		t.Fatalf("the claim ran %d times, want 1", k.store.claims)
	}
	if k.optouts.n != 1 {
		t.Fatalf("the upsert ran %d times, want 1", k.optouts.n)
	}
}

func TestConsume_UnknownTokenIsNotAnErrorAndRecordsNothing(t *testing.T) {
	// The public response is generic either way: an error here would turn
	// the endpoint into an oracle for whether a token ever existed.
	k := newConsumeKit(t, consumeDeps{noToken: true})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("an unknown token is not an error: %v", err)
	}
	if k.optouts.n != 0 {
		t.Fatal("with no token there is no address to opt out")
	}
	if k.store.claims != 0 {
		t.Fatal("there is nothing to consume")
	}
}

func TestConsume_EmptyTokenIsNotAnError(t *testing.T) {
	k := newConsumeKit(t, consumeDeps{})
	if err := k.svc.Consume(context.Background(), ""); err != nil {
		t.Fatalf("an empty token is not an error: %v", err)
	}
	if k.optouts.n != 0 || k.store.claims != 0 {
		t.Fatal("an empty token must touch nothing")
	}
}

func TestConsume_TokenWithoutAnAddressDoesNotBurnTheToken(t *testing.T) {
	// Upsert returns nil for an empty address — it reports success for a
	// write that never happened — so the address is checked here. Spending
	// the token now would burn it with nothing recorded.
	k := newConsumeKit(t, consumeDeps{})
	k.store.doc.Address = ""
	if err := k.svc.Consume(context.Background(), "raw-token"); err == nil {
		t.Fatal("with no address the opt-out cannot be recorded: that is an error")
	}
	if k.store.claims != 0 {
		t.Fatal("the token must not be spent")
	}
	if k.optouts.n != 0 {
		t.Fatal("the write must not even be attempted")
	}
}

func TestConsume_WithoutTheOptoutSeamFailsClosed(t *testing.T) {
	// An unwired seam must not read as "there is no opt-out to record":
	// without the durable fact, the token is not touched.
	k := newConsumeKit(t, consumeDeps{noOptoutSeam: true})
	if err := k.svc.Consume(context.Background(), "raw-token"); err == nil {
		t.Fatal("without the opt-out repository the sequence must fail")
	}
	if k.store.claims != 0 {
		t.Fatal("the token must not be spent")
	}
}

func TestConsume_WithoutAUserSkipsThePreference(t *testing.T) {
	k := newConsumeKit(t, consumeDeps{})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if k.prefs.n != 0 {
		t.Fatal("a token with no user has no preference to update")
	}
	if k.store.lastPrefPending {
		t.Fatal("and so has nothing to replay")
	}
}

func TestConsume_PassesTheTokensAttributionToTheSinkAndThePreference(t *testing.T) {
	k := newConsumeKit(t, consumeDeps{userUUID: "u1"}) // category deliberately empty
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if k.sink.addr != "ada@example.test" {
		t.Fatalf("sink addr = %q", k.sink.addr)
	}
	if k.sink.category != models.TypeMarketing {
		t.Fatalf("an empty category means all marketing, got %q", k.sink.category)
	}
	if k.sink.refCtx != "ref-9" {
		t.Fatalf("the producer's opaque context must reach the sink, got %q", k.sink.refCtx)
	}
	if k.prefs.lastUser != "u1" || k.prefs.lastCat != models.TypeMarketing ||
		k.prefs.lastChan != models.ChannelEmail || k.prefs.lastOptedIn {
		t.Fatalf("wrong preference: %+v", k.prefs)
	}
	if k.optouts.last.Address != "ada@example.test" || k.optouts.last.SourceTokenUUID != "token-uuid-1" {
		t.Fatalf("opt-out recorded wrong: %+v", k.optouts.last)
	}
	if k.optouts.last.At.IsZero() {
		t.Fatal("the opt-out must carry the instant it happened")
	}
}

func TestConsume_WithoutASinkThereIsNothingLeftPending(t *testing.T) {
	// The base ships no sink: leaving sinkPending up would have the
	// reconciler retrying forever against a consumer that does not exist.
	k := newConsumeKit(t, consumeDeps{noSink: true})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if k.store.lastSinkPending {
		t.Fatal("with no consumer there is nothing to mirror")
	}
}

func TestConsume_PreferenceFailureStillFiresTheSink(t *testing.T) {
	// Steps 3 and 4 are independent: a preference that will not write must
	// not keep the downstream consumer from learning about the opt-out.
	k := newConsumeKit(t, consumeDeps{userUUID: "u1", prefErr: errors.New("db down")})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if k.sink.n != 1 {
		t.Fatalf("the sink ran %d times, want 1", k.sink.n)
	}
	if k.store.lastSinkPending {
		t.Fatal("the sink accepted: its flag must go down")
	}
	if !k.store.lastPrefPending {
		t.Fatal("prefPending must stay up")
	}
}

func TestConsume_KeepsTheTokensOwnCategory(t *testing.T) {
	// The category is recorded and forwarded as the token carried it, rather
	// than widened to the "marketing" default. It is attribution, not a
	// filter: IsOptedOut ignores the category and $setOnInsert freezes
	// whichever one landed first, so any opt-out row suppresses all
	// marketing to that address. What this pins is that the three
	// consumers below are told what the link actually promised.
	k := newConsumeKit(t, consumeDeps{userUUID: "u1", category: "marketing.newsletter"})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if k.sink.category != "marketing.newsletter" {
		t.Fatalf("sink category = %q", k.sink.category)
	}
	if k.prefs.lastCat != "marketing.newsletter" {
		t.Fatalf("preference category = %q", k.prefs.lastCat)
	}
	if k.optouts.last.Category != "marketing.newsletter" {
		t.Fatalf("opt-out category = %q", k.optouts.last.Category)
	}
}

func TestConsume_WithoutThePreferenceSeamLeavesPrefPending(t *testing.T) {
	// An unwired seam is not "nothing to update": it is work still owed,
	// and the claim already marked it as such.
	k := newConsumeKit(t, consumeDeps{userUUID: "u1", noPrefSeam: true})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !k.store.lastPrefPending {
		t.Fatal("prefPending must stay up")
	}
	if k.store.lastSinkPending {
		t.Fatal("the sink, on the other hand, accepted: its flag must go down")
	}
}

// ---------------------------------------------------------------------------
// PII: neither the address nor the raw token may reach a log or an error
// ---------------------------------------------------------------------------

func TestConsume_NeverPutsTheAddressInTheErrorItReturns(t *testing.T) {
	// A MongoDB write error quotes what it failed on, so the address can
	// ride out inside an error string nobody classifies as PII. (Document
	// validation is the realistic case: a duplicate key on this collection
	// never reaches here, because the repository converts it to nil.)
	k := newConsumeKit(t, consumeDeps{
		optoutErr: errors.New(`write exception: write errors: [{code: 121, message: "Document failed validation", errInfo: {failingDocument: {address: "ada@example.test"}}}]`),
	})
	err := k.svc.Consume(context.Background(), "raw-token")
	if err == nil {
		t.Fatal("an opt-out that did not land must be an error")
	}
	if strings.Contains(err.Error(), "ada@example.test") {
		t.Fatalf("the address must not appear in the error: %v", err)
	}
	if !errors.Is(err, ErrOptoutNotRecorded) {
		t.Fatalf("the error must stay recognisable: %v", err)
	}
}

func TestConsume_NeverLogsTheAddressWhenTheSinkFails(t *testing.T) {
	// Core just handed this sink the address, so a sink that quotes it back
	// in its error is the ordinary case — and that error is logged.
	var buf bytes.Buffer
	k := newConsumeKit(t, consumeDeps{
		logTo:   &buf,
		sinkErr: errors.New("failed to upsert contact ada@example.test: timeout"),
	})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	logged := buf.String()
	if strings.Contains(logged, "ada@example.test") {
		t.Fatalf("the address reached the log: %s", logged)
	}
	if !strings.Contains(logged, "[address]") {
		t.Fatalf("the sink's error should still be logged, scrubbed: %s", logged)
	}
	if !strings.Contains(logged, "token-uuid-1") {
		t.Fatalf("the token's uuid is what identifies the line: %s", logged)
	}
}

func TestConsume_NeverLogsTheAddressWhenThePreferenceFails(t *testing.T) {
	var buf bytes.Buffer
	k := newConsumeKit(t, consumeDeps{
		logTo:    &buf,
		userUUID: "u1",
		prefErr:  errors.New("no preference row for ada@example.test"),
	})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if strings.Contains(buf.String(), "ada@example.test") {
		t.Fatalf("the address reached the log: %s", buf.String())
	}
}

func TestConsume_NeverLogsTheRawToken(t *testing.T) {
	// What identifies an unsubscribe in the logs is the token's uuid. The
	// raw token identifies nothing — it authorises.
	var buf bytes.Buffer
	k := newConsumeKit(t, consumeDeps{
		logTo:    &buf,
		userUUID: "u1",
		prefErr:  errors.New("db down"),
		sinkErr:  errors.New("sink down"),
	})
	if err := k.svc.Consume(context.Background(), "raw-token"); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if strings.Contains(buf.String(), "raw-token") {
		t.Fatalf("the raw token reached the log: %s", buf.String())
	}
	if k.optouts.last.SourceTokenUUID != "token-uuid-1" {
		t.Fatalf("the opt-out must cite the uuid, not the token: %+v", k.optouts.last)
	}
}

func TestScrubAddress(t *testing.T) {
	for _, tc := range []struct {
		name    string
		msg     string
		address string
		want    string
	}{
		{
			name:    "the address the token carries",
			msg:     `dup key: { address: "Ada@Example.test" }`,
			address: "Ada@Example.test",
			want:    `dup key: { address: "[address]" }`,
		},
		{
			name:    "the normalized form the collection stores",
			msg:     `dup key: { address: "ada@example.test" }`,
			address: "  Ada@Example.TEST ",
			want:    `dup key: { address: "[address]" }`,
		},
		{
			name:    "a value too short to be an address is left alone",
			msg:     "timeout after 5s",
			address: "5s",
			want:    "timeout after 5s",
		},
		{
			name:    "a value with no @ is left alone, diagnostic intact",
			msg:     "connection refused",
			address: "con",
			want:    "connection refused",
		},
		{
			name:    "no address, no change",
			msg:     "connection refused",
			address: "",
			want:    "connection refused",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scrubAddress(tc.msg, tc.address); got != tc.want {
				t.Fatalf("scrubAddress = %q, want %q", got, tc.want)
			}
		})
	}
}
