package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"github.com/orkestra/backend/pkg/sdk/jobs"
	"go.mongodb.org/mongo-driver/bson"
)

// fakeLegalHoldRepo is a hand-rolled stub of legalHoldRepo. Each method
// records its inputs and returns a pinned value so the service logic is
// exercised without MongoDB.
type fakeLegalHoldRepo struct {
	inserted     *models.LegalHold
	insertErr    error
	releaseErr   error
	listResult   []models.LegalHold
	listErr      error
	held         bool
	heldErr      error
	lastRelease  [3]string // uuid, releasedBy, reason
	lastListUUID string
	lastHeldUUID string

	getResult  *models.LegalHold
	updRow     *models.LegalHold
	updErr     error
	lastSet    bson.M
	lastUnset  []string
	lastNote   models.LegalHoldNote
	expired    []models.LegalHold
	expireErr  error
	lastExpire time.Time
}

func (f *fakeLegalHoldRepo) List(_ context.Context, u string, _ bool, _ time.Time) ([]models.LegalHold, error) {
	f.lastListUUID = u
	return f.listResult, f.listErr
}
func (f *fakeLegalHoldRepo) Get(_ context.Context, _ string) (*models.LegalHold, error) {
	if f.getResult == nil {
		return &models.LegalHold{UUID: "h-1", UserUUID: "u-1"}, nil
	}
	return f.getResult, nil
}
func (f *fakeLegalHoldRepo) Update(_ context.Context, _ string, set bson.M, unset []string) (*models.LegalHold, error) {
	f.lastSet, f.lastUnset = set, unset
	if f.updErr != nil {
		return nil, f.updErr
	}
	if f.updRow != nil {
		return f.updRow, nil
	}
	return &models.LegalHold{UUID: "h-1", UserUUID: "u-1", Active: true}, nil
}
func (f *fakeLegalHoldRepo) AddNote(_ context.Context, _ string, n models.LegalHoldNote) (*models.LegalHold, error) {
	f.lastNote = n
	return &models.LegalHold{UUID: "h-1", UserUUID: "u-1", Active: true, Notes: []models.LegalHoldNote{n}}, f.updErr
}
func (f *fakeLegalHoldRepo) ExpireDue(_ context.Context, now time.Time, _ int) ([]models.LegalHold, error) {
	f.lastExpire = now
	return f.expired, f.expireErr
}

type holdSink struct{ events []iface.AuditEvent }

func (k *holdSink) Emit(_ context.Context, e iface.AuditEvent) { k.events = append(k.events, e) }

func fixedHoldSvc(repo *fakeLegalHoldRepo, now time.Time) (*LegalHoldService, *holdSink) {
	svc := NewLegalHoldService(repo)
	sink := &holdSink{}
	svc.SetAuditSink(sink)
	svc.now = func() time.Time { return now }
	return svc, sink
}

func (f *fakeLegalHoldRepo) Insert(_ context.Context, h *models.LegalHold) error {
	f.inserted = h
	return f.insertErr
}
func (f *fakeLegalHoldRepo) Release(_ context.Context, uuid, releasedBy, reason string, _ time.Time) error {
	f.lastRelease = [3]string{uuid, releasedBy, reason}
	return f.releaseErr
}
func (f *fakeLegalHoldRepo) ListActive(_ context.Context, userUUID string, _ time.Time) ([]models.LegalHold, error) {
	f.lastListUUID = userUUID
	return f.listResult, f.listErr
}
func (f *fakeLegalHoldRepo) IsHeld(_ context.Context, userUUID string, _ time.Time) (bool, error) {
	f.lastHeldUUID = userUUID
	return f.held, f.heldErr
}

// TestPlacePopulatesActiveHold pins the Place contract: the service stamps a
// UUID + PlacedAt, marks the hold active, and forwards every operator-supplied
// field to the repo verbatim.
func TestPlacePopulatesActiveHold(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{}
	svc := NewLegalHoldService(repo)

	in := PlaceInput{
		UserUUID: "u-1",
		TenantID: "t-1",
		Reason:   "litigation",
		CaseRef:  "CASE-42",
		PlacedBy: "admin-9",
	}
	got, err := svc.Place(context.Background(), in)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if repo.inserted == nil {
		t.Fatal("Place should have inserted a hold")
	}
	if got.UUID == "" {
		t.Fatal("Place should stamp a UUID")
	}
	if !got.Active {
		t.Fatal("a freshly placed hold must be active")
	}
	if got.PlacedAt.IsZero() {
		t.Fatal("Place should stamp PlacedAt")
	}
	if got.UserUUID != "u-1" || got.TenantID != "t-1" || got.Reason != "litigation" ||
		got.CaseRef != "CASE-42" || got.PlacedBy != "admin-9" {
		t.Fatalf("placed hold fields not forwarded verbatim: %+v", got)
	}
}

// TestPlacePropagatesInsertError pins that a repo insert failure surfaces to
// the caller (and no hold object is returned).
func TestPlacePropagatesInsertError(t *testing.T) {
	t.Parallel()

	boom := errors.New("insert failed")
	svc := NewLegalHoldService(&fakeLegalHoldRepo{insertErr: boom})
	got, err := svc.Place(context.Background(), PlaceInput{UserUUID: "u-1", Reason: "x"})
	if !errors.Is(err, boom) {
		t.Fatalf("expected insert error to propagate, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil hold on insert error, got %+v", got)
	}
}

// TestReleaseForwardsArgs pins the pass-through to repo.Release.
func TestReleaseForwardsArgs(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{}
	svc := NewLegalHoldService(repo)
	if err := svc.Release(context.Background(), "h-1", "admin-9", "case closed"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if repo.lastRelease != [3]string{"h-1", "admin-9", "case closed"} {
		t.Fatalf("Release args not forwarded: %+v", repo.lastRelease)
	}
}

// TestListActiveForwardsFilter pins that ListActive passes the subject filter
// through and returns the repo rows unchanged.
func TestListActiveForwardsFilter(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{listResult: []models.LegalHold{{UUID: "h-1"}, {UUID: "h-2"}}}
	svc := NewLegalHoldService(repo)
	got, err := svc.ListActive(context.Background(), "u-7")
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if repo.lastListUUID != "u-7" {
		t.Fatalf("ListActive filter not forwarded: %q", repo.lastListUUID)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 holds, got %d", len(got))
	}
}

// TestIsHeldForwards pins the DSR-gate query: IsHeld delegates to the repo and
// returns its verdict for the right subject.
func TestIsHeldForwards(t *testing.T) {
	t.Parallel()

	repo := &fakeLegalHoldRepo{held: true}
	svc := NewLegalHoldService(repo)
	held, err := svc.IsHeld(context.Background(), "u-3")
	if err != nil {
		t.Fatalf("IsHeld: %v", err)
	}
	if !held {
		t.Fatal("IsHeld should report the held subject")
	}
	if repo.lastHeldUUID != "u-3" {
		t.Fatalf("IsHeld subject not forwarded: %q", repo.lastHeldUUID)
	}
}

var holdNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func TestPlaceValidatesAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeLegalHoldRepo{}
	svc, sink := fixedHoldSvc(repo, holdNow)
	ctx := context.Background()

	if _, err := svc.Place(ctx, PlaceInput{UserUUID: "u-1", Reason: "  "}); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("blank reason err = %v", err)
	}
	if _, err := svc.Place(ctx, PlaceInput{UserUUID: "u-1", Reason: "x", ExpiresAt: ptrTime(holdNow)}); !errors.Is(err, ErrLegalHoldExpiryPast) {
		t.Fatalf("past expiry err = %v", err)
	}
	if repo.inserted != nil || len(sink.events) != 0 {
		t.Fatal("a refused place must not insert or audit")
	}
	h, err := svc.Place(ctx, PlaceInput{UserUUID: "u-1", Reason: "dispute", PlacedBy: "op-1",
		ReviewAt: ptrTime(holdNow.Add(-time.Hour)), ExpiresAt: ptrTime(holdNow.Add(time.Hour))})
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if !h.ReviewDue {
		t.Fatal("a review date in the past on an active hold is reviewDue")
	}
	if len(sink.events) != 1 || sink.events[0].Action != "compliance.legal_hold.placed" ||
		sink.events[0].ActorUserID != "op-1" || sink.events[0].ResourceType != "legal_hold" ||
		sink.events[0].Metadata["subjectUserUuid"] != "u-1" {
		t.Fatalf("placed audit wrong: %+v", sink.events)
	}
}

func TestReleaseRequiresReasonAndAudits(t *testing.T) {
	t.Parallel()
	repo := &fakeLegalHoldRepo{}
	svc, sink := fixedHoldSvc(repo, holdNow)
	if err := svc.Release(context.Background(), "h-1", "op-1", " "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("blank release reason err = %v", err)
	}
	if err := svc.Release(context.Background(), "h-1", "op-1", strings.Repeat("x", 501)); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("long release reason err = %v", err)
	}
	if repo.lastRelease[0] != "" || len(sink.events) != 0 {
		t.Fatal("a refused release must not reach the repo or audit")
	}
	if err := svc.Release(context.Background(), "h-1", "op-1", "closed"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if len(sink.events) != 1 || sink.events[0].Action != "compliance.legal_hold.released" || sink.events[0].Metadata["subjectUserUuid"] != "u-1" {
		t.Fatalf("released audit wrong: %+v", sink.events)
	}
}

func TestUpdateValidationAndAudit(t *testing.T) {
	t.Parallel()
	repo := &fakeLegalHoldRepo{}
	svc, sink := fixedHoldSvc(repo, holdNow)
	ctx := context.Background()
	soon := ptrTime(holdNow.Add(time.Hour))

	if _, err := svc.Update(ctx, "h-1", "op", nil, nil, false, false); !errors.Is(err, ErrLegalHoldUpdateInvalid) {
		t.Fatalf("empty update err = %v", err)
	}
	if _, err := svc.Update(ctx, "h-1", "op", soon, nil, true, false); !errors.Is(err, ErrLegalHoldUpdateInvalid) {
		t.Fatalf("set+clear err = %v", err)
	}
	if _, err := svc.Update(ctx, "h-1", "op", nil, ptrTime(holdNow.Add(-time.Second)), false, false); !errors.Is(err, ErrLegalHoldExpiryPast) {
		t.Fatalf("past expiry err = %v", err)
	}
	if len(sink.events) != 0 {
		t.Fatal("refused updates must not audit")
	}
	if _, err := svc.Update(ctx, "h-1", "op", soon, nil, false, true); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if _, ok := repo.lastSet["reviewAt"]; !ok || len(repo.lastUnset) != 1 || repo.lastUnset[0] != "expiresAt" {
		t.Fatalf("set/unset wrong: %v %v", repo.lastSet, repo.lastUnset)
	}
	ev := sink.events[0]
	if ev.Action != "compliance.legal_hold.updated" {
		t.Fatalf("action = %q", ev.Action)
	}
	if ch, _ := ev.Metadata["changed"].([]string); len(ch) != 2 || ch[0] != "reviewAt" || ch[1] != "expiresAt" {
		t.Fatalf("changed = %v", ev.Metadata["changed"])
	}
	repo.updErr = errors.New("gone")
	if _, err := svc.Update(ctx, "h-1", "op", soon, nil, false, false); err == nil {
		t.Fatal("repo error must propagate")
	}
}

func TestAddNoteMasksAndNeverAuditsText(t *testing.T) {
	t.Parallel()
	repo := &fakeLegalHoldRepo{}
	svc, sink := fixedHoldSvc(repo, holdNow)
	ctx := context.Background()
	for _, bad := range []string{"", "   ", strings.Repeat("n", 1001)} {
		if _, err := svc.AddNote(ctx, "h-1", "op", bad); !errors.Is(err, ErrReasonRequired) {
			t.Fatalf("note %d chars err = %v", len(bad), err)
		}
	}
	text := "call back on IT60X0542811101000000123456 please"
	h, err := svc.AddNote(ctx, "h-1", "op-1", text)
	if err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if repo.lastNote.ID == "" || repo.lastNote.By != "op-1" || !repo.lastNote.At.Equal(holdNow) {
		t.Fatalf("note stamps wrong: %+v", repo.lastNote)
	}
	if strings.Contains(repo.lastNote.Text, "IT60X0542811101000000123456") || len(h.Notes) != 1 {
		t.Fatalf("note not masked: %q", repo.lastNote.Text)
	}
	if len(sink.events) != 1 || sink.events[0].Action != "compliance.legal_hold.note_added" {
		t.Fatalf("audit wrong: %+v", sink.events)
	}
	for k, v := range sink.events[0].Metadata {
		if str, ok := v.(string); ok && strings.Contains(str, "call back") {
			t.Fatalf("note text leaked into audit metadata %q", k)
		}
	}
}

func TestExpireDueAuditsOnlyWhatItReleased(t *testing.T) {
	t.Parallel()
	repo := &fakeLegalHoldRepo{expired: []models.LegalHold{
		{UUID: "h-1", UserUUID: "u-1", TenantID: "t-1"}, {UUID: "h-2", UserUUID: "u-2"}}}
	svc, sink := fixedHoldSvc(repo, holdNow)
	n, err := svc.ExpireDue(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("ExpireDue = %d, %v", n, err)
	}
	if !repo.lastExpire.Equal(holdNow) {
		t.Fatalf("expiry compared to %v", repo.lastExpire)
	}
	if len(sink.events) != 2 || sink.events[0].Action != "compliance.legal_hold.expired" ||
		sink.events[0].ActorUserID != models.LegalHoldExpiryActor || sink.events[0].ActorType != "system" ||
		sink.events[1].ResourceID != "h-2" {
		t.Fatalf("expired audit wrong: %+v", sink.events)
	}

	// Nothing released (another replica won): no audit, zero.
	repo.expired = nil
	sink.events = nil
	if n, _ := svc.ExpireDue(context.Background()); n != 0 || len(sink.events) != 0 {
		t.Fatalf("idle run = %d, audit %d", n, len(sink.events))
	}
}

func TestServiceWithoutSinkIsNilSafe(t *testing.T) {
	t.Parallel()
	repo := &fakeLegalHoldRepo{expired: []models.LegalHold{{UUID: "h-1"}}}
	svc := NewLegalHoldService(repo)
	if _, err := svc.Place(context.Background(), PlaceInput{UserUUID: "u", Reason: "r"}); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ExpireDue(context.Background()); err != nil || n != 1 {
		t.Fatalf("ExpireDue = %d, %v", n, err)
	}
}

func TestRunLegalHoldExpiryRecordsReleased(t *testing.T) {
	t.Parallel()
	repo := &fakeLegalHoldRepo{expired: []models.LegalHold{{UUID: "h-1"}, {UUID: "h-2"}, {UUID: "h-3"}}}
	svc, _ := fixedHoldSvc(repo, holdNow)
	run := jobs.NewRun("compliance.legal_hold_expiry", "r", jobs.TriggerSchedule, 1, holdNow)
	if err := RunLegalHoldExpiry(svc)(context.Background(), run); err != nil {
		t.Fatalf("run: %v", err)
	}
	d, _ := run.Details()
	if v, ok := d["released"].(int64); !ok || v != 3 {
		t.Fatalf("details = %v", d)
	}
	repo.expireErr = errors.New("db down")
	if err := RunLegalHoldExpiry(svc)(context.Background(), run); err == nil {
		t.Fatal("a repo error must fail the run")
	}
}
