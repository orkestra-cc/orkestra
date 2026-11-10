package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/repository"
	"github.com/orkestra/backend/internal/shared/blob"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// DSRExportBucket is the object-store domain that holds stored DSR exports.
const DSRExportBucket = "dsr-exports"

// DSRExportDownloadTTL is how long a minted download URL works.
const DSRExportDownloadTTL = 5 * time.Minute

const defaultExportRetentionDays = 30

// exportMarkerSkew is the clock skew a Create tolerates between replicas when
// it compares an erasure marker with its own start.
const exportMarkerSkew = 2 * time.Second

var (
	// ErrExportStorageUnavailable: no object store is configured.
	ErrExportStorageUnavailable = errors.New("compliance: object storage for DSR exports is not available")
	// ErrExportExpired: the export is past its expiry and is no longer served.
	ErrExportExpired = errors.New("compliance: the DSR export has expired")
	// ErrExportErasureInProgress: the subject is being erased, or was erased
	// while the export ran; no export is kept.
	ErrExportErasureInProgress = errors.New("compliance: the data subject is being erased")
	// ErrExportFormatInvalid: the format is neither json nor csv.
	ErrExportFormatInvalid = errors.New("compliance: export format must be json or csv")
)

// DSRExportRepo is the persistence seam of DSRExportService.
type DSRExportRepo interface {
	Insert(ctx context.Context, e *models.DSRExport) error
	Get(ctx context.Context, uuid string) (*models.DSRExport, error)
	ListByUser(ctx context.Context, userUUID string) ([]models.DSRExport, error)
	ListExpired(ctx context.Context, now time.Time, limit int) ([]models.DSRExport, error)
	Delete(ctx context.Context, uuid string) error
}

// DSRExportMarkers records when an erasure started deleting a subject's
// exports, so a Create that raced it can tell.
type DSRExportMarkers interface {
	Mark(ctx context.Context, userUUID string, at time.Time) error
	ErasedAt(ctx context.Context, userUUID string) (time.Time, bool, error)
	DeleteBefore(ctx context.Context, t time.Time) (int, error)
}

// DSRExportService generates a subject's export on behalf of an operator and
// keeps it in object storage until it expires.
type DSRExportService struct {
	repo          DSRExportRepo
	dsr           *DSRService
	store         iface.ObjectStore // nil = storage unavailable
	sink          iface.AuditSink
	retentionDays func() int
	markers       DSRExportMarkers
	hasIncomplete func(ctx context.Context, userUUID string) (bool, error)
	now           func() time.Time
	logger        *slog.Logger
}

// NewDSRExportService wires the service. store may be nil; retentionDays may be
// nil (30 days).
func NewDSRExportService(repo DSRExportRepo, dsr *DSRService, store iface.ObjectStore, sink iface.AuditSink, retentionDays func() int, logger *slog.Logger) *DSRExportService {
	if logger == nil {
		logger = slog.Default()
	}
	return &DSRExportService{repo: repo, dsr: dsr, store: store, sink: sink, retentionDays: retentionDays, now: time.Now, logger: logger}
}

// SetErasureGuard wires the erasure markers and the incomplete-erasure check.
// Either may be nil.
func (s *DSRExportService) SetErasureGuard(m DSRExportMarkers, hasIncomplete func(ctx context.Context, userUUID string) (bool, error)) {
	s.markers, s.hasIncomplete = m, hasIncomplete
}

// SetClock replaces the clock (tests).
func (s *DSRExportService) SetClock(now func() time.Time) { s.now = now }

func (s *DSRExportService) days() int {
	if s.retentionDays != nil {
		if d := s.retentionDays(); d > 0 {
			return d
		}
	}
	return defaultExportRetentionDays
}

func exportContentType(format string) string {
	if format == "csv" {
		return "text/csv; charset=utf-8"
	}
	return "application/json"
}

// Create runs the subject's export as actor (audited as gdpr.dsr.exported by
// ExportAs), stores the bytes and records the row.
func (s *DSRExportService) Create(ctx context.Context, userUUID, format, reason, actor string) (*models.DSRExport, error) {
	if format != "json" && format != "csv" {
		return nil, ErrExportFormatInvalid
	}
	note, ok := NormalizeReason(reason)
	if !ok {
		return nil, ErrReasonRequired
	}
	userUUID = NormalizeSubjectUUID(userUUID)
	if err := s.dsr.requireSubject(ctx, userUUID); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, ErrExportStorageUnavailable
	}

	if s.hasIncomplete != nil {
		busy, err := s.hasIncomplete(ctx, userUUID)
		if err != nil {
			return nil, fmt.Errorf("check erasure: %w", err)
		}
		if busy {
			return nil, ErrExportErasureInProgress
		}
	}
	started := s.now().UTC()

	res, err := s.dsr.ExportAs(ctx, userUUID, actor)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if format == "csv" {
		err = FlattenBundleCSV(&buf, res.Bundle)
	} else {
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		err = enc.Encode(res.Bundle)
	}
	if err != nil {
		return nil, fmt.Errorf("encode export: %w", err)
	}

	id := uuid.Must(uuid.NewV7()).String()
	now := s.now().UTC()
	row := &models.DSRExport{
		UUID:        id,
		UserUUID:    userUUID,
		Format:      format,
		RequestedBy: actor,
		Reason:      maskReason(note),
		CreatedAt:   now,
		ExpiresAt:   now.Add(time.Duration(s.days()) * 24 * time.Hour),
		SizeBytes:   int64(buf.Len()),
		Producers:   res.Producers,
		ObjectKey:   id + "." + format,
	}
	for k := range res.Errors {
		row.ErrorKeys = append(row.ErrorKeys, k)
	}
	sort.Strings(row.ErrorKeys)

	if err := s.store.Put(ctx, row.ObjectKey, exportContentType(format), bytes.NewReader(buf.Bytes())); err != nil {
		return nil, fmt.Errorf("store export: %w", err)
	}
	if err := s.repo.Insert(ctx, row); err != nil {
		if derr := s.store.Delete(ctx, row.ObjectKey); derr != nil {
			s.logger.Warn("DSR export: orphan object after a failed insert", slog.String("exportUuid", id))
		}
		return nil, fmt.Errorf("record export: %w", err)
	}
	// An erasure that started after this export began may have listed the
	// subject's exports before the row landed: remove it rather than let it
	// outlive the erasure.
	if s.markers != nil {
		at, ok, merr := s.markers.ErasedAt(ctx, userUUID)
		// Replicas' clocks differ a little: a marker written up to
		// exportMarkerSkew before this export began still counts as racing.
		if merr != nil || (ok && at.After(started.Add(-exportMarkerSkew))) {
			if derr := s.deleteOne(ctx, *row); derr != nil {
				s.logger.Warn("DSR export: could not remove an export that raced an erasure", slog.String("exportUuid", id))
			}
			if merr != nil {
				return nil, fmt.Errorf("check erasure marker: %w", merr)
			}
			return nil, ErrExportErasureInProgress
		}
	}
	if s.sink != nil {
		s.sink.Emit(ctx, iface.AuditEvent{
			ActorUserID:  actor,
			ActorType:    "user",
			Action:       "gdpr.dsr.export_stored",
			ResourceType: "user",
			ResourceID:   userUUID,
			Outcome:      "success",
			Metadata:     map[string]any{"exportUuid": id, "format": format, "subjectUserUuid": userUUID},
		})
	}
	return row, nil
}

// List returns the subject's exports, newest first.
func (s *DSRExportService) List(ctx context.Context, userUUID string) ([]models.DSRExport, error) {
	return s.repo.ListByUser(ctx, NormalizeSubjectUUID(userUUID))
}

// DownloadURL mints a 5-minute URL for the export. An export past its expiry is
// refused with ErrExportExpired even if the cleanup job has not run yet; no URL
// is minted.
func (s *DSRExportService) DownloadURL(ctx context.Context, exportUUID, actor string) (string, error) {
	row, err := s.repo.Get(ctx, exportUUID)
	if err != nil {
		return "", err
	}
	if !row.ExpiresAt.After(s.now()) {
		return "", ErrExportExpired
	}
	if s.store == nil {
		return "", ErrExportStorageUnavailable
	}
	var url string
	if dl, ok := s.store.(blob.ObjectDownloadPresigner); ok {
		name := fmt.Sprintf("dsr-export-%s-%s.%s", short(row.UserUUID), row.CreatedAt.UTC().Format("2006-01-02"), row.Format)
		url, err = dl.PresignGetDownload(ctx, row.ObjectKey, name, DSRExportDownloadTTL)
	} else {
		url, err = s.store.PresignGet(ctx, row.ObjectKey, DSRExportDownloadTTL)
	}
	if err != nil {
		return "", fmt.Errorf("presign export: %w", err)
	}
	if s.sink != nil {
		s.sink.Emit(ctx, iface.AuditEvent{
			ActorUserID:  actor,
			ActorType:    "user",
			Action:       "gdpr.dsr.export_downloaded",
			ResourceType: "user",
			ResourceID:   row.UserUUID,
			Outcome:      "success",
			Metadata:     map[string]any{"exportUuid": row.UUID, "subjectUserUuid": row.UserUUID},
		})
	}
	return url, nil
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// CleanupExpired deletes the objects and rows of the exports past their expiry
// and returns how many rows it removed. A row whose object cannot be deleted
// stays for the next run.
func (s *DSRExportService) CleanupExpired(ctx context.Context) (int, error) {
	const batch = 200
	deleted := 0
	if s.markers != nil {
		cutoff := s.now().Add(-time.Duration(s.days()) * 24 * time.Hour)
		if _, err := s.markers.DeleteBefore(ctx, cutoff); err != nil {
			s.logger.Warn("DSR export: could not drop old erasure markers", slog.String("error", err.Error()))
		}
	}
	for {
		rows, err := s.repo.ListExpired(ctx, s.now(), batch)
		if err != nil {
			return deleted, err
		}
		progress := 0
		var firstErr error
		for _, r := range rows {
			if err := s.deleteOne(ctx, r); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			progress++
		}
		deleted += progress
		if firstErr != nil {
			return deleted, firstErr
		}
		if len(rows) < batch || progress == 0 {
			return deleted, nil
		}
	}
}

// deleteOne removes the object, then the row. A missing object is fine.
func (s *DSRExportService) deleteOne(ctx context.Context, r models.DSRExport) error {
	if s.store == nil {
		return ErrExportStorageUnavailable
	}
	if err := s.store.Delete(ctx, r.ObjectKey); err != nil {
		return fmt.Errorf("delete export object: %w", err)
	}
	if err := s.repo.Delete(ctx, r.UUID); err != nil && !errors.Is(err, repository.ErrDSRExportNotFound) {
		return fmt.Errorf("delete export row: %w", err)
	}
	return nil
}

// PurgeUser deletes every stored export of the subject and returns the row count.
func (s *DSRExportService) PurgeUser(ctx context.Context, userUUID string) (int, error) {
	// Marker first: a Create still running re-reads it after its insert.
	if s.markers != nil {
		if err := s.markers.Mark(ctx, userUUID, s.now().UTC()); err != nil {
			return 0, fmt.Errorf("mark export erasure: %w", err)
		}
	}
	rows, err := s.repo.ListByUser(ctx, userUUID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if err := s.deleteOne(ctx, r); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
