package jobs

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/orkestra/backend/internal/core/jobs/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// Config keys (ADR-0012 schema; see ConfigSchema).
const (
	keyTimezone          = "timezone"
	keyMaxConcurrentRuns = "maxConcurrentRuns"
	keyRunRetentionDays  = "runRetentionDays"

	defaultTimezone      = "UTC"
	defaultMaxConcurrent = 4
	minMaxConcurrent     = 1
	maxMaxConcurrent     = 32
	defaultRetentionDays = 30
	minRetentionDays     = 1
	maxRetentionDays     = 365

	// tzRefresh bounds how stale the cached module timezone may be: the
	// scheduler reads it on every poll, the config is re-read at most this
	// often.
	tzRefresh = 30 * time.Second
)

var _ module.HasConfigSnapshotValidator = (*Module)(nil)

func (m *Module) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "scheduling", Label: "Scheduling", Order: 1,
			Description: "The timezone cron schedules are read in, and how many jobs one replica runs at once."},
		{Key: "history", Label: "Run history", Order: 2,
			Description: "How long finished runs stay in the run log."},
	}
}

func (m *Module) ConfigSchema() []module.ConfigField {
	minC, maxC := minMaxConcurrent, maxMaxConcurrent
	minD, maxD := minRetentionDays, maxRetentionDays
	return []module.ConfigField{
		{Key: keyTimezone, Label: "Timezone", Group: "scheduling", Type: module.FieldString, Default: defaultTimezone, EnvVar: "JOBS_TIMEZONE",
			Description: "IANA zone name (e.g. Europe/Rome) cron schedules are evaluated in, unless a job has its own timezone override. A change is picked up within 30 seconds and reschedules the cron jobs from that moment, to their next time in the new zone; interval jobs keep their due time. Daylight saving: a time that does not exist is skipped, a repeated one runs once."},
		{Key: keyMaxConcurrentRuns, Label: "Concurrent runs per replica", Group: "scheduling", Type: module.FieldInt, Default: strconv.Itoa(defaultMaxConcurrent), EnvVar: "JOBS_MAX_CONCURRENT_RUNS", Min: &minC, Max: &maxC,
			Description: "Most jobs one backend replica runs at the same time (1–32). Takes effect at the next backend restart."},
		{Key: keyRunRetentionDays, Label: "Run retention (days)", Group: "history", Type: module.FieldInt, Default: strconv.Itoa(defaultRetentionDays), EnvVar: "JOBS_RUN_RETENTION_DAYS", Min: &minD, Max: &maxD,
			Description: "Runs older than this are deleted by the daily jobs.runs_cleanup job (1–365)."},
	}
}

// HotReloadConfig: timezone and retention are read live; maxConcurrentRuns
// at Start, which its description says.
func (m *Module) HotReloadConfig() bool { return true }

// ValidateConfigSnapshot rejects a timezone that is not an IANA name and
// integers out of range, on every config mutation surface.
func (m *Module) ValidateConfigSnapshot(_ context.Context, snap module.ConfigValidationSnapshot) error {
	eff := snap.EffectiveValues
	if tz := strings.TrimSpace(eff[keyTimezone]); tz != "" {
		if err := services.ValidateTimezone(tz); err != nil {
			return &module.ConfigValidationError{Field: keyTimezone, Code: errcode.JobsInvalidTimezone,
				Message: "timezone must be an IANA zone name such as UTC or Europe/Rome"}
		}
	}
	if err := intInRange(eff, keyMaxConcurrentRuns, minMaxConcurrent, maxMaxConcurrent); err != nil {
		return err
	}
	return intInRange(eff, keyRunRetentionDays, minRetentionDays, maxRetentionDays)
}

func intInRange(values map[string]string, key string, lo, hi int) error {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		return &module.ConfigValidationError{Field: key,
			Message: key + " must be a whole number between " + strconv.Itoa(lo) + " and " + strconv.Itoa(hi)}
	}
	return nil
}

func clamp(n, lo, hi, fallback int) int {
	switch {
	case n == 0:
		return fallback
	case n < lo:
		return lo
	case n > hi:
		return hi
	}
	return n
}

func (m *Module) configInt(key string, fallback int) int {
	if m.deps == nil {
		return fallback
	}
	return m.deps.GetConfigInt("jobs", key, fallback)
}

func (m *Module) maxConcurrent() int {
	return clamp(m.configInt(keyMaxConcurrentRuns, defaultMaxConcurrent), minMaxConcurrent, maxMaxConcurrent, defaultMaxConcurrent)
}

func (m *Module) retentionDays() int {
	return clamp(m.configInt(keyRunRetentionDays, defaultRetentionDays), minRetentionDays, maxRetentionDays, defaultRetentionDays)
}

// tzCache serves the module timezone to the scheduler (every poll) and the
// admin views, re-reading the config at most every tzRefresh. A stored name
// that does not load (an env seed the validator never saw) falls back to UTC
// and is logged once per refresh.
type tzCache struct {
	read   func() string
	now    func() time.Time
	logger *slog.Logger

	mu   sync.Mutex
	loc  *time.Location
	name string
	at   time.Time
}

func (c *tzCache) Location() *time.Location {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.loc != nil && now.Sub(c.at) < tzRefresh {
		return c.loc
	}
	name := defaultTimezone
	if c.read != nil {
		if v := strings.TrimSpace(c.read()); v != "" {
			name = v
		}
	}
	c.at = now
	if c.loc != nil && name == c.name {
		return c.loc
	}
	loc, err := time.LoadLocation(name)
	if err != nil || name == "Local" {
		if c.logger != nil {
			c.logger.Warn("jobs: module timezone is not an IANA zone; using UTC", slog.String("timezone", name))
		}
		loc = time.UTC
	}
	c.loc, c.name = loc, name
	return loc
}
