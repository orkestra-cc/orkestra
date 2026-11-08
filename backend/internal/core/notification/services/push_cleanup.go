package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/internal/core/notification/repository"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	pushCleanupBatch       = 500
	pushStaleAfter         = 60 * 24 * time.Hour
	pushDeleteDisabledAged = 90 * 24 * time.Hour
)

// ErrPushCleanupNoReader is returned when no session reader is available, so
// the cleanup cannot tell a live session from an ended one. Nothing is
// disabled or deleted in that case.
var ErrPushCleanupNoReader = errors.New("notification: push cleanup has no session activity reader")

// PushCleanupResult is what one cleanup run did.
type PushCleanupResult struct {
	SessionEnded int64
	Stale        int64
	Deleted      int64
	// SkippedNoReader counts active tokens left alone because their tier's
	// reader was unavailable.
	SkippedNoReader int64
}

// PushCleanup retires device tokens: it disables those whose session has
// ended, disables those unseen for 60 days, and deletes rows disabled for
// more than 90 days.
type PushCleanup struct {
	Devices  repository.DeviceTokenRepository
	Sessions func(tier string) iface.SessionActivityReader
	Now      func() time.Time
}

// Run executes one pass. The result is filled in even when an error is
// returned, so the caller can report partial counts.
func (c *PushCleanup) Run(ctx context.Context) (PushCleanupResult, error) {
	var res PushCleanupResult
	if c == nil || c.Devices == nil || c.Sessions == nil {
		return res, ErrPushCleanupNoReader
	}
	readers := map[string]iface.SessionActivityReader{}
	for _, tier := range []string{iface.PushTierOperator, iface.PushTierClient} {
		if r := c.Sessions(tier); r != nil {
			readers[tier] = r
		}
	}
	if len(readers) == 0 {
		return res, ErrPushCleanupNoReader
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	now = now.UTC()

	after := ""
	for {
		batch, err := c.Devices.ListActiveBatch(ctx, after, pushCleanupBatch)
		if err != nil {
			return res, fmt.Errorf("list active device tokens: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		after = batch[len(batch)-1].UUID
		byTier := map[string][]models.DeviceToken{}
		for _, d := range batch {
			if d.SessionID == "" {
				continue // no session to check; the stale pass covers it
			}
			if readers[d.Tier] == nil {
				res.SkippedNoReader++
				continue
			}
			byTier[d.Tier] = append(byTier[d.Tier], d)
		}
		for tier, rows := range byTier {
			ids := make([]string, 0, len(rows))
			for _, d := range rows {
				ids = append(ids, d.SessionID)
			}
			active, err := readers[tier].ActiveSessions(ctx, ids)
			if err != nil {
				return res, fmt.Errorf("read session activity (%s): %w", tier, err)
			}
			for _, d := range rows {
				if active[d.SessionID] {
					continue
				}
				ok, err := c.Devices.Disable(ctx, d.Tier, d.UserUUID, d.DeviceID, models.DeviceDisabledSessionEnded, now)
				if err != nil {
					return res, fmt.Errorf("disable device token: %w", err)
				}
				if ok {
					res.SessionEnded++
				}
			}
		}
		if len(batch) < pushCleanupBatch {
			break
		}
	}

	n, err := c.Devices.DisableStale(ctx, now.Add(-pushStaleAfter), now)
	res.Stale = n
	if err != nil {
		return res, fmt.Errorf("disable stale device tokens: %w", err)
	}
	n, err = c.Devices.DeleteDisabledBefore(ctx, now.Add(-pushDeleteDisabledAged))
	res.Deleted = n
	if err != nil {
		return res, fmt.Errorf("delete old disabled device tokens: %w", err)
	}
	if res.SkippedNoReader > 0 {
		return res, fmt.Errorf("%w: %d token(s) of a tier without a reader were not checked", ErrPushCleanupNoReader, res.SkippedNoReader)
	}
	return res, nil
}
