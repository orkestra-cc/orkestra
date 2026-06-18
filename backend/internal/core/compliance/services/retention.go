package services

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// RetentionConfig is read fresh on every run so admin toggles take effect
// without a backend restart.
type RetentionConfig struct {
	Enabled bool
	Years   int
}

// userTombstoneCollections are the tier-split identity collections retention
// scans. Inlined (the cross-module collection-naming contract) so compliance
// stays import-decoupled from the user module.
var userTombstoneCollections = []string{"operator_users", "client_users"}

// RetentionService hard-deletes anonymized user tombstones once they pass the
// configured retention window. A tombstone is the row the user PII producer
// leaves behind under EraseAnonymize (deletedAt set, email aliased, UUID
// kept). Off by default — RunOnce is a no-op unless RetentionConfig.Enabled.
// Erasure runs through the DSR pipeline, so the legal-hold gate is honoured.
type RetentionService struct {
	db         *mongo.Database
	dsr        *DSRService
	logger     *slog.Logger
	loadConfig func() RetentionConfig
}

// NewRetentionService wires the retention job. loadConfig is consulted on
// every run so the enable flag and window are live-editable.
func NewRetentionService(db *mongo.Database, dsr *DSRService, logger *slog.Logger, loadConfig func() RetentionConfig) *RetentionService {
	return &RetentionService{db: db, dsr: dsr, logger: logger, loadConfig: loadConfig}
}

// Preview returns the userUUIDs whose anonymized tombstone has passed the
// retention window, plus the cutoff — a dry run that mutates nothing.
func (s *RetentionService) Preview(ctx context.Context) ([]string, time.Time, error) {
	cfg := s.loadConfig()
	cutoff := retentionCutoff(cfg.Years)
	ids, err := s.listExpired(ctx, cutoff)
	return ids, cutoff, err
}

// RunOnce hard-deletes every expired tombstone, skipping subjects under an
// active legal hold (the DSR erase gate enforces that — a held subject errors
// and is logged-and-skipped). No-op when disabled. Returns the purge count.
func (s *RetentionService) RunOnce(ctx context.Context) (int, error) {
	cfg := s.loadConfig()
	if !cfg.Enabled {
		return 0, nil
	}
	ids, err := s.listExpired(ctx, retentionCutoff(cfg.Years))
	if err != nil {
		return 0, err
	}
	purged := 0
	for _, id := range ids {
		if _, err := s.dsr.Erase(ctx, id, iface.EraseHardDelete); err != nil {
			s.logger.Warn("retention: skip subject",
				slog.String("userUuid", id),
				slog.String("reason", err.Error()),
			)
			continue
		}
		purged++
	}
	return purged, nil
}

// Loop runs RunOnce on a daily cadence until stop or ctx cancellation. The
// module's Start() launches it only when the module is enabled.
func (s *RetentionService) Loop(ctx context.Context, stop <-chan struct{}) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := s.RunOnce(context.Background()); err != nil {
				s.logger.Warn("retention cleanup failed", slog.String("error", err.Error()))
			} else if n > 0 {
				s.logger.Info("retention cleanup purged tombstones", slog.Int("count", n))
			}
		}
	}
}

func (s *RetentionService) listExpired(ctx context.Context, cutoff time.Time) ([]string, error) {
	var ids []string
	for _, coll := range userTombstoneCollections {
		//tenantscope:allow platform retention scans anonymized tombstones across tenants.
		cur, err := s.db.Collection(coll).Find(ctx, bson.M{"deletedAt": bson.M{"$lt": cutoff}})
		if err != nil {
			return nil, err
		}
		var rows []struct {
			UUID string `bson:"uuid"`
		}
		if err := cur.All(ctx, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.UUID != "" {
				ids = append(ids, r.UUID)
			}
		}
	}
	return ids, nil
}

func retentionCutoff(years int) time.Time {
	if years <= 0 {
		years = 5
	}
	return time.Now().UTC().Add(-time.Duration(years) * 365 * 24 * time.Hour)
}
