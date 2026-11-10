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

var (
	// ErrExportStorageUnavailable: no object store is configured.
	ErrExportStorageUnavailable = errors.New("compliance: object storage for DSR exports is not available")
	// ErrExportExpired: the export is past its expiry and is no longer served.
	ErrExportExpired = errors.New("compliance: the DSR export has expired")
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

// DSRExportService generates a subject's export on behalf of an operator and
// keeps it in object storage until it expires.
type DSRExportService struct {
	repo          DSRExportRepo
	dsr           *DSRService
	store         iface.ObjectStore // nil = storage unavailable
	sink          iface.AuditSink
	retentionDays func() int
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
	if s.store == nil {
		return nil, ErrExportStorageUnavailable
	}

	res, err := s.dsr.ExportAs(ctx, userUUID, actor)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if format == "csv" {
		err = FlattenBundleCSV(&buf, res.Bundle)
	} else {
		enc := json.NewEncoder(&buf)
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
	return row, nil
}

// List returns the subject's exports, newest first.
func (s *DSRExportService) List(ctx context.Context, userUUID string) ([]models.DSRExport, error) {
	return s.repo.ListByUser(ctx, userUUID)
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
