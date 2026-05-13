package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/addons/billing/models"
	"github.com/orkestra/backend/internal/addons/billing/repository"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeNotifRepo is a minimal billing NotificationRepository used to drive
// the thin service-layer wrappers under test.
type fakeNotifRepo struct {
	getByUUIDErr error
	getByUUIDDoc *models.SDINotification

	byInvoice    []models.SDINotification
	byInvoiceErr error

	listResult []models.SDINotification
	listCount  int64
	listErr    error
	lastFilter *models.NotificationFilters
	lastPaging models.PaginationParams

	markErr      error
	lastMarkUUID string
	lastMarkBy   string

	summary    *models.NotificationSummary
	summaryErr error
	lastFrom   *time.Time
	lastTo     *time.Time
}

func (f *fakeNotifRepo) Create(_ context.Context, _ *models.SDINotification) error { return nil }
func (f *fakeNotifRepo) GetByID(_ context.Context, _ string) (*models.SDINotification, error) {
	return nil, nil
}
func (f *fakeNotifRepo) GetByUUID(_ context.Context, _ string) (*models.SDINotification, error) {
	return f.getByUUIDDoc, f.getByUUIDErr
}
func (f *fakeNotifRepo) GetByInvoiceUUID(_ context.Context, _ string) ([]models.SDINotification, error) {
	return f.byInvoice, f.byInvoiceErr
}
func (f *fakeNotifRepo) List(_ context.Context, filters *models.NotificationFilters, p models.PaginationParams) ([]models.SDINotification, int64, error) {
	f.lastFilter = filters
	f.lastPaging = p
	return f.listResult, f.listCount, f.listErr
}
func (f *fakeNotifRepo) GetUnprocessed(_ context.Context) ([]models.SDINotification, error) {
	return nil, nil
}
func (f *fakeNotifRepo) MarkAsProcessed(_ context.Context, uuid, by string) error {
	f.lastMarkUUID = uuid
	f.lastMarkBy = by
	return f.markErr
}
func (f *fakeNotifRepo) GetSummary(_ context.Context, from, to *time.Time) (*models.NotificationSummary, error) {
	f.lastFrom, f.lastTo = from, to
	return f.summary, f.summaryErr
}
func (f *fakeNotifRepo) CountUnprocessed(_ context.Context) (int64, error) { return 0, nil }
func (f *fakeNotifRepo) GetPollingState(_ context.Context) (*models.PollingState, error) {
	return nil, nil
}
func (f *fakeNotifRepo) UpdatePollingState(_ context.Context, _ *models.PollingState) error {
	return nil
}

func TestNotificationService_GetNotification_HappyPath(t *testing.T) {
	want := &models.SDINotification{UUID: "n-1"}
	repo := &fakeNotifRepo{getByUUIDDoc: want}
	svc := NewNotificationService(repo, discardLogger())
	got, err := svc.GetNotification(context.Background(), "n-1")
	if err != nil {
		t.Fatalf("GetNotification: %v", err)
	}
	if got != want {
		t.Fatalf("returned wrong pointer")
	}
}

func TestNotificationService_GetNotification_NotFoundMapped(t *testing.T) {
	repo := &fakeNotifRepo{getByUUIDErr: repository.ErrNotificationNotFound}
	svc := NewNotificationService(repo, discardLogger())
	_, err := svc.GetNotification(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "notification not found") {
		t.Fatalf("expected mapped not-found, got %v", err)
	}
}

func TestNotificationService_GetNotification_RawRepoErrorPassesThrough(t *testing.T) {
	repo := &fakeNotifRepo{getByUUIDErr: errors.New("mongo died")}
	svc := NewNotificationService(repo, discardLogger())
	_, err := svc.GetNotification(context.Background(), "x")
	if err == nil || err.Error() != "mongo died" {
		t.Fatalf("expected raw repo error to pass through, got %v", err)
	}
}

func TestNotificationService_GetNotificationsByInvoice_Passthrough(t *testing.T) {
	want := []models.SDINotification{{UUID: "n-1"}, {UUID: "n-2"}}
	repo := &fakeNotifRepo{byInvoice: want}
	svc := NewNotificationService(repo, discardLogger())
	got, err := svc.GetNotificationsByInvoice(context.Background(), "inv-uuid")
	if err != nil {
		t.Fatalf("GetNotificationsByInvoice: %v", err)
	}
	if len(got) != 2 || got[0].UUID != "n-1" {
		t.Fatalf("expected pass-through slice, got %+v", got)
	}

	repo2 := &fakeNotifRepo{byInvoiceErr: errors.New("repo boom")}
	svc2 := NewNotificationService(repo2, discardLogger())
	if _, err := svc2.GetNotificationsByInvoice(context.Background(), "x"); err == nil {
		t.Fatalf("expected error propagation")
	}
}

func TestNotificationService_ListNotifications_ForwardsArgsAndResults(t *testing.T) {
	repo := &fakeNotifRepo{
		listResult: []models.SDINotification{{UUID: "n-1"}},
		listCount:  7,
	}
	svc := NewNotificationService(repo, discardLogger())
	filter := &models.NotificationFilters{NotificationType: models.NotificationNS}
	paging := models.PaginationParams{Page: 2, PageSize: 50}
	got, count, err := svc.ListNotifications(context.Background(), filter, paging)
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if count != 7 || len(got) != 1 || got[0].UUID != "n-1" {
		t.Fatalf("unexpected list result: got=%+v count=%d", got, count)
	}
	if repo.lastFilter != filter {
		t.Errorf("filter was not forwarded to repo")
	}
	if repo.lastPaging != paging {
		t.Errorf("pagination was not forwarded to repo, got %+v", repo.lastPaging)
	}
}

func TestNotificationService_MarkAsProcessed_ForwardsArgs(t *testing.T) {
	repo := &fakeNotifRepo{}
	svc := NewNotificationService(repo, discardLogger())
	if err := svc.MarkAsProcessed(context.Background(), "n-1", "operator-uuid"); err != nil {
		t.Fatalf("MarkAsProcessed: %v", err)
	}
	if repo.lastMarkUUID != "n-1" || repo.lastMarkBy != "operator-uuid" {
		t.Fatalf("args not forwarded: uuid=%q by=%q", repo.lastMarkUUID, repo.lastMarkBy)
	}

	repo2 := &fakeNotifRepo{markErr: errors.New("write failed")}
	svc2 := NewNotificationService(repo2, discardLogger())
	if err := svc2.MarkAsProcessed(context.Background(), "x", "y"); err == nil {
		t.Fatalf("expected error from repo to propagate")
	}
}

func TestNotificationService_GetSummary_ForwardsDateRange(t *testing.T) {
	from := time.Now().Add(-30 * 24 * time.Hour)
	to := time.Now()
	want := &models.NotificationSummary{TotalCount: 5}
	repo := &fakeNotifRepo{summary: want}
	svc := NewNotificationService(repo, discardLogger())
	got, err := svc.GetSummary(context.Background(), &from, &to)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if got != want {
		t.Fatalf("expected pointer pass-through")
	}
	if repo.lastFrom == nil || !repo.lastFrom.Equal(from) {
		t.Errorf("from not forwarded")
	}
	if repo.lastTo == nil || !repo.lastTo.Equal(to) {
		t.Errorf("to not forwarded")
	}

	repo2 := &fakeNotifRepo{summaryErr: errors.New("agg failed")}
	svc2 := NewNotificationService(repo2, discardLogger())
	if _, err := svc2.GetSummary(context.Background(), nil, nil); err == nil {
		t.Fatalf("expected summary error to propagate")
	}
}
