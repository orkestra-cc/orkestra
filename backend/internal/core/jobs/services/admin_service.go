// Package services holds the Tier-1 admin surface of the jobs module: the job
// list and detail views, run history, operator overrides, run requests and
// the schedule preview. Scheduling itself is the scheduler package's.
package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/jobs/models"
	"github.com/orkestra/backend/internal/core/jobs/repository"
	"github.com/orkestra/backend/internal/core/jobs/scheduler"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
	sdkjobs "github.com/orkestra/backend/pkg/sdk/jobs"
	"github.com/orkestra/backend/pkg/sdk/tenantrepo"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Errors the handlers map to error codes. The repository's are re-exported
// so handlers depend on this package only.
var (
	ErrNotFound             = repository.ErrNotFound
	ErrRevisionConflict     = repository.ErrRevisionConflict
	ErrAlreadyRunning       = repository.ErrAlreadyRunning
	ErrAlreadyRequested     = repository.ErrAlreadyRequested
	ErrModuleStopped        = errors.New("jobs: module is not started")
	ErrInvalidSchedule      = errors.New("jobs: invalid schedule")
	ErrManualScheduleLocked = errors.New("jobs: manual job schedule cannot be overridden")
	ErrInvalidTimezone      = errors.New("jobs: invalid timezone")
	ErrInvalidQuery         = errors.New("jobs: invalid run query")
)

// InputError is a rejected input. Kind is one of the Err* above; Msg is a
// sentence for the caller (for a cron expression it names the field).
type InputError struct {
	Kind error
	Msg  string
}

func (e *InputError) Error() string { return e.Msg }
func (e *InputError) Unwrap() error { return e.Kind }

// InputMessage returns the caller-facing sentence of an InputError in err's
// chain, else fallback.
func InputMessage(err error, fallback string) string {
	var ie *InputError
	if errors.As(err, &ie) && ie.Msg != "" {
		return ie.Msg
	}
	return fallback
}

func inputErr(kind error, format string, a ...any) error {
	return &InputError{Kind: kind, Msg: clipMessage(fmt.Sprintf(format, a...))}
}

// maxMessageRunes bounds an input error message: some quote the operator's
// input (the cron field, the zone name).
const maxMessageRunes = 200

// clipMessage cuts s to maxMessageRunes runes, ending in "…" when cut.
func clipMessage(s string) string {
	if utf8.RuneCountInString(s) <= maxMessageRunes {
		return s
	}
	return string([]rune(s)[:maxMessageRunes-1]) + "…"
}

// Audit actions. Metadata carries the job key and the override before and
// after; never personal data.
const (
	AuditOverrideUpdated = "jobs.override_updated"
	AuditOverrideCleared = "jobs.override_cleared"
	AuditRunRequested    = "jobs.run_requested"
)

const (
	// PreviewCount is how many due times a schedule preview returns.
	PreviewCount = 5
	// MaxEvery bounds an every override (and keeps seconds→Duration exact).
	MaxEvery = 366 * 24 * time.Hour
	// DefaultRunsLimit and MaxRunsLimit bound one page of run history.
	DefaultRunsLimit = 25
	MaxRunsLimit     = 100
)

// runStatuses are the statuses the run-history filter accepts.
var runStatuses = map[string]bool{
	models.StatusRunning: true, models.StatusSucceeded: true, models.StatusFailed: true,
	models.StatusTimedOut: true, models.StatusAbandoned: true, models.StatusInterrupted: true,
	models.StatusSkipped: true,
}

// Options wire the admin service. Every func field may be nil.
type Options struct {
	// Catalog returns the job catalog (filled by the registry after Init);
	// nil or a nil result is an empty catalog.
	Catalog func() *sdkjobs.Catalog
	States  repository.StateRepository
	Runs    repository.RunRepository
	// DefaultLocation is the module timezone (nil: UTC).
	DefaultLocation func() *time.Location
	// ModuleStarted reports whether a module runs its jobs (nil: all do).
	ModuleStarted func(module string) bool
	// AuditSink is resolved per event; nil or a nil result skips auditing.
	AuditSink func() iface.AuditSink
	// Wake asks the local scheduler to poll now.
	Wake   func()
	Now    func() time.Time
	Logger *slog.Logger
}

// AdminService serves the /v1/admin/jobs surface.
type AdminService struct{ o Options }

// New builds the admin service.
func New(o Options) *AdminService {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &AdminService{o: o}
}

// --- views ------------------------------------------------------------------

// JobsScheduleView is a schedule on the wire.
type JobsScheduleView struct {
	Kind         string `json:"kind" enum:"every,cron,manual"`
	EverySeconds int64  `json:"everySeconds,omitempty"`
	Cron         string `json:"cron,omitempty"`
}

// JobsLastRunView summarises the most recent finished run.
type JobsLastRunView struct {
	RunID      string    `json:"runId"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	DurationMs int64     `json:"durationMs"`
	Error      string    `json:"error,omitempty"`
}

// JobsJobView is one job: its declaration, the effective schedule after the
// operator override, and its live state.
type JobsJobView struct {
	Key             string           `json:"key"`
	Module          string           `json:"module"`
	Name            string           `json:"name"`
	Title           string           `json:"title"`
	Description     string           `json:"description,omitempty"`
	DefaultSchedule JobsScheduleView `json:"defaultSchedule"`
	Schedule        JobsScheduleView `json:"schedule"`
	Timezone        string           `json:"timezone"`
	Enabled         bool             `json:"enabled"`
	DefaultEnabled  bool             `json:"defaultEnabled"`
	Overridden      bool             `json:"overridden"`
	ModuleStarted   bool             `json:"moduleStarted"`
	Running         bool             `json:"running"`
	NextRunAt       *time.Time       `json:"nextRunAt,omitempty"`
	RetryAt         *time.Time       `json:"retryAt,omitempty"`
	Attempt         int              `json:"attempt"`
	RunRequestedAt  *time.Time       `json:"runRequestedAt,omitempty"`
	LastRun         *JobsLastRunView `json:"lastRun,omitempty"`
	TimeoutSeconds  int64            `json:"timeoutSeconds"`
	MaxAttempts     int              `json:"maxAttempts"`
	Revision        int64            `json:"revision"`
}

// JobsRunView is one run record.
type JobsRunView struct {
	ID             string         `json:"id"`
	Key            string         `json:"key"`
	Trigger        string         `json:"trigger" enum:"schedule,manual,retry,catchup"`
	Status         string         `json:"status"`
	Error          string         `json:"error,omitempty"`
	Replica        string         `json:"replica,omitempty"`
	RequestedBy    string         `json:"requestedBy,omitempty"`
	Attempt        int            `json:"attempt"`
	ScheduledFor   *time.Time     `json:"scheduledFor,omitempty"`
	StartedAt      time.Time      `json:"startedAt"`
	FinishedAt     *time.Time     `json:"finishedAt,omitempty"`
	DurationMs     int64          `json:"durationMs"`
	Details        map[string]any `json:"details,omitempty"`
	DroppedDetails int            `json:"droppedDetails,omitempty"`
}

func scheduleView(s sdkjobs.Schedule) JobsScheduleView {
	v := JobsScheduleView{Kind: string(s.Kind())}
	switch s.Kind() {
	case sdkjobs.ScheduleEvery:
		v.EverySeconds = int64(s.Every() / time.Second)
	case sdkjobs.ScheduleCron:
		v.Cron = s.Expr()
	}
	return v
}

func (s *AdminService) catalog() *sdkjobs.Catalog {
	if s.o.Catalog != nil {
		if c := s.o.Catalog(); c != nil {
			return c
		}
	}
	return sdkjobs.NewCatalog()
}

func (s *AdminService) defaultLocation() *time.Location {
	if s.o.DefaultLocation != nil {
		if l := s.o.DefaultLocation(); l != nil {
			return l
		}
	}
	return time.UTC
}

func (s *AdminService) moduleStarted(module string) bool {
	return s.o.ModuleStarted == nil || s.o.ModuleStarted(module)
}

func (s *AdminService) view(entry sdkjobs.Entry, st *models.State, loc *time.Location, now time.Time) JobsJobView {
	def := entry.Def
	eff, err := scheduler.EffectiveOf(entry, st, loc)
	if err != nil {
		// A stored override that does not parse: the engine does not
		// schedule the job; show the declared default.
		s.o.Logger.Warn("jobs: stored override is invalid", "job_key", entry.Key, "error", err)
		eff, _ = scheduler.EffectiveOf(entry, nil, loc)
	}
	v := JobsJobView{
		Key: entry.Key, Module: entry.Module, Name: def.Name, Title: def.Title, Description: def.Description,
		DefaultSchedule: scheduleView(def.Schedule),
		Schedule:        scheduleView(eff.Schedule),
		Timezone:        eff.Location.String(),
		Enabled:         eff.Enabled,
		DefaultEnabled:  !def.DisabledByDefault,
		ModuleStarted:   s.moduleStarted(entry.Module),
		TimeoutSeconds:  int64(def.Timeout / time.Second),
		MaxAttempts:     def.Retry.MaxAttempts,
	}
	if st == nil {
		return v
	}
	v.Overridden = st.Override != nil
	v.Running = st.Lease != nil && st.Lease.Trigger != "" && st.Lease.Until.After(now)
	v.NextRunAt, v.RetryAt, v.RunRequestedAt = st.NextRunAt, st.RetryAt, st.RunRequestedAt
	v.Attempt = st.Attempt
	v.Revision = st.Revision
	if lr := st.LastRun; lr != nil {
		v.LastRun = &JobsLastRunView{RunID: lr.RunID, Status: lr.Status, StartedAt: lr.StartedAt,
			FinishedAt: lr.FinishedAt, DurationMs: lr.DurationMs, Error: lr.Error}
	}
	return v
}

func runView(r models.RunRecord) JobsRunView {
	v := JobsRunView{
		ID: r.ID, Key: r.Key, Trigger: r.Trigger, Status: r.Status, Error: r.Error, Replica: r.Replica,
		RequestedBy: r.RequestedBy, Attempt: r.Attempt, ScheduledFor: r.ScheduledFor, StartedAt: r.StartedAt,
		FinishedAt: r.FinishedAt, DurationMs: r.DurationMs, DroppedDetails: r.DroppedDetails,
	}
	if len(r.Details) > 0 {
		v.Details = make(map[string]any, len(r.Details))
		for k, val := range r.Details {
			if dt, ok := val.(primitive.DateTime); ok {
				val = dt.Time().UTC()
			}
			v.Details[k] = val
		}
	}
	return v
}

// --- reads ------------------------------------------------------------------

// List returns every registered job, sorted by key.
func (s *AdminService) List(ctx context.Context) ([]JobsJobView, error) {
	entries := s.catalog().List()
	states, err := s.o.States.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("jobs: list states: %w", err)
	}
	byKey := make(map[string]*models.State, len(states))
	for i := range states {
		byKey[states[i].Key] = &states[i]
	}
	loc, now := s.defaultLocation(), s.o.Now()
	out := make([]JobsJobView, 0, len(entries))
	for _, e := range entries {
		out = append(out, s.view(e, byKey[e.Key], loc, now))
	}
	return out, nil
}

// entryAndState resolves a registered job and its state. A job whose state is
// not created yet (the scheduler reconciles at start) has a nil state.
func (s *AdminService) entryAndState(ctx context.Context, key string) (sdkjobs.Entry, *models.State, error) {
	entry, ok := s.catalog().Get(key)
	if !ok {
		return sdkjobs.Entry{}, nil, ErrNotFound
	}
	st, err := s.o.States.Get(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return entry, nil, nil
	}
	if err != nil {
		return sdkjobs.Entry{}, nil, fmt.Errorf("jobs: get state of %s: %w", key, err)
	}
	return entry, st, nil
}

// Get returns one registered job.
func (s *AdminService) Get(ctx context.Context, key string) (JobsJobView, error) {
	entry, st, err := s.entryAndState(ctx, key)
	if err != nil {
		return JobsJobView{}, err
	}
	return s.view(entry, st, s.defaultLocation(), s.o.Now()), nil
}

// Runs returns one page of a job's run history, newest first. status filters
// by run status ("" = all); cursor is the nextCursor of the previous page.
func (s *AdminService) Runs(ctx context.Context, key, status, cursor string, limit int) ([]JobsRunView, string, error) {
	if _, ok := s.catalog().Get(key); !ok {
		return nil, "", ErrNotFound
	}
	if status != "" && !runStatuses[status] {
		return nil, "", inputErr(ErrInvalidQuery, "Unknown run status %q", status)
	}
	var before *repository.RunCursor
	if cursor != "" {
		c, err := repository.DecodeRunCursor(cursor)
		if err != nil {
			return nil, "", inputErr(ErrInvalidQuery, "The cursor is not one this API returned")
		}
		before = c
	}
	switch {
	case limit <= 0:
		limit = DefaultRunsLimit
	case limit > MaxRunsLimit:
		limit = MaxRunsLimit
	}
	recs, next, err := s.o.Runs.List(ctx, key, status, before, limit)
	if err != nil {
		return nil, "", fmt.Errorf("jobs: list runs of %s: %w", key, err)
	}
	out := make([]JobsRunView, 0, len(recs))
	for _, r := range recs {
		out = append(out, runView(r))
	}
	return out, repository.EncodeRunCursor(next), nil
}

// --- overrides --------------------------------------------------------------

// ScheduleInput is a schedule given by an operator ("every" or "cron").
type ScheduleInput struct {
	Kind         string
	EverySeconds int64
	Cron         string
}

// UpdateInput is a partial override change; nil fields are left as they are.
// Timezone "" clears the timezone override.
type UpdateInput struct {
	Revision int64
	Enabled  *bool
	Schedule *ScheduleInput
	Timezone *string
}

// parseSchedule validates an operator schedule and returns its persisted
// spec and the schedule.
func parseSchedule(in ScheduleInput) (models.ScheduleSpec, sdkjobs.Schedule, error) {
	var spec models.ScheduleSpec
	switch sdkjobs.ScheduleKind(in.Kind) {
	case sdkjobs.ScheduleEvery:
		minS, maxS := int64(sdkjobs.MinEvery/time.Second), int64(MaxEvery/time.Second)
		if in.EverySeconds < minS || in.EverySeconds > maxS {
			return spec, sdkjobs.Schedule{}, inputErr(ErrInvalidSchedule,
				"An every schedule needs everySeconds between %d and %d", minS, maxS)
		}
		spec = models.ScheduleSpec{Kind: in.Kind, EverySeconds: in.EverySeconds}
	case sdkjobs.ScheduleCron:
		spec = models.ScheduleSpec{Kind: in.Kind, Cron: in.Cron}
	default:
		return spec, sdkjobs.Schedule{}, inputErr(ErrInvalidSchedule, "Schedule kind must be every or cron")
	}
	sched, err := scheduler.ScheduleOf(spec)
	if err != nil {
		// The parser's message names the offending cron field and value,
		// e.g. `cron: minute field "61": value 61 out of range 0-59`.
		return spec, sdkjobs.Schedule{}, &InputError{Kind: ErrInvalidSchedule, Msg: clipMessage("Invalid schedule: " + err.Error())}
	}
	return spec, sched, nil
}

func parseTimezone(name string) (*time.Location, error) {
	// time.LoadLocation accepts "" and "Local" (the process zone); an
	// operator must name a zone.
	if name == "" || name == "Local" {
		return nil, inputErr(ErrInvalidTimezone, "Timezone must be an IANA zone name such as Europe/Rome")
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, inputErr(ErrInvalidTimezone, "Unknown timezone %q: use an IANA zone name such as Europe/Rome", name)
	}
	return loc, nil
}

// ValidateTimezone reports whether name is an accepted IANA zone name (the
// module config uses it too).
func ValidateTimezone(name string) error {
	_, err := parseTimezone(name)
	return err
}

func cloneOverride(o *models.Override) *models.Override {
	if o == nil {
		return &models.Override{}
	}
	c := *o
	if o.Enabled != nil {
		v := *o.Enabled
		c.Enabled = &v
	}
	if o.Schedule != nil {
		v := *o.Schedule
		c.Schedule = &v
	}
	return &c
}

// overrideMeta is the audit form of an override: nil (no override) or a map
// of the fields set.
func overrideMeta(o *models.Override) any {
	if o == nil {
		return nil
	}
	m := map[string]any{}
	if o.Enabled != nil {
		m["enabled"] = *o.Enabled
	}
	if o.Schedule != nil {
		sm := map[string]any{"kind": o.Schedule.Kind}
		if o.Schedule.EverySeconds > 0 {
			sm["everySeconds"] = o.Schedule.EverySeconds
		}
		if o.Schedule.Cron != "" {
			sm["cron"] = o.Schedule.Cron
		}
		m["schedule"] = sm
	}
	if o.Timezone != "" {
		m["timezone"] = o.Timezone
	}
	return m
}

// Update applies a partial override to a job, with optimistic concurrency on
// revision. A change of the effective schedule (fingerprint) reschedules the
// job from now; disabling it clears its due times.
func (s *AdminService) Update(ctx context.Context, key string, in UpdateInput) (JobsJobView, error) {
	entry, st, err := s.entryAndState(ctx, key)
	if err != nil {
		return JobsJobView{}, err
	}
	if st == nil || st.Revision != in.Revision {
		return JobsJobView{}, ErrRevisionConflict
	}
	next := cloneOverride(st.Override)
	if in.Enabled != nil {
		v := *in.Enabled
		next.Enabled = &v
	}
	if in.Schedule != nil {
		if entry.Def.Schedule.Kind() == sdkjobs.ScheduleManual {
			return JobsJobView{}, inputErr(ErrManualScheduleLocked, "Job %s runs only on request; its schedule cannot be changed", key)
		}
		spec, _, err := parseSchedule(*in.Schedule)
		if err != nil {
			return JobsJobView{}, err
		}
		next.Schedule = &spec
	}
	if in.Timezone != nil {
		if *in.Timezone == "" {
			next.Timezone = ""
		} else {
			if _, err := parseTimezone(*in.Timezone); err != nil {
				return JobsJobView{}, err
			}
			next.Timezone = *in.Timezone
		}
	}
	if next.Enabled == nil && next.Schedule == nil && next.Timezone == "" {
		next = nil
	}
	return s.write(ctx, entry, st, in.Revision, next, AuditOverrideUpdated)
}

// ClearOverride restores a job's declared defaults.
func (s *AdminService) ClearOverride(ctx context.Context, key string, revision int64) (JobsJobView, error) {
	entry, st, err := s.entryAndState(ctx, key)
	if err != nil {
		return JobsJobView{}, err
	}
	if st == nil || st.Revision != revision {
		return JobsJobView{}, ErrRevisionConflict
	}
	return s.write(ctx, entry, st, revision, nil, AuditOverrideCleared)
}

func (s *AdminService) write(ctx context.Context, entry sdkjobs.Entry, st *models.State, revision int64, o *models.Override, action string) (JobsJobView, error) {
	loc, now := s.defaultLocation(), s.o.Now()
	eff, err := scheduler.EffectiveOf(entry, &models.State{Override: o}, loc)
	if err != nil {
		return JobsJobView{}, inputErr(ErrInvalidSchedule, "The override does not produce a valid schedule")
	}
	// Same fingerprint: the due time still belongs to this schedule. A
	// different one reschedules from now (the slot due under the old
	// schedule is skipped); disabled yields nil, clearing the due times.
	nextRunAt := st.NextRunAt
	if eff.Fingerprint != st.Fingerprint {
		nextRunAt = scheduler.NextRun(eff, now)
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	updated, err := s.o.States.SetOverride(ctx, entry.Key, revision, o, eff.Fingerprint, nextRunAt, actor, now)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrRevisionConflict) {
			return JobsJobView{}, err
		}
		return JobsJobView{}, fmt.Errorf("jobs: set override of %s: %w", entry.Key, err)
	}
	s.audit(ctx, action, entry.Key, map[string]any{
		"before": overrideMeta(st.Override), "after": overrideMeta(o),
		"revision": updated.Revision,
	})
	s.wake()
	return s.view(entry, updated, loc, now), nil
}

// RequestRun asks for one run as soon as a replica is free (spec J12). It is
// allowed on a disabled job, not on a job of a stopped module.
func (s *AdminService) RequestRun(ctx context.Context, key string) (JobsJobView, error) {
	entry, ok := s.catalog().Get(key)
	if !ok {
		return JobsJobView{}, ErrNotFound
	}
	if !s.moduleStarted(entry.Module) {
		return JobsJobView{}, ErrModuleStopped
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	now := s.o.Now()
	st, err := s.o.States.RequestRun(ctx, key, actor, now)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound), errors.Is(err, repository.ErrAlreadyRunning),
			errors.Is(err, repository.ErrAlreadyRequested), errors.Is(err, repository.ErrRevisionConflict):
			return JobsJobView{}, err
		}
		return JobsJobView{}, fmt.Errorf("jobs: request run of %s: %w", key, err)
	}
	s.audit(ctx, AuditRunRequested, key, nil)
	s.wake()
	return s.view(entry, st, s.defaultLocation(), now), nil
}

// --- preview ----------------------------------------------------------------

// Preview returns the next PreviewCount due times of a schedule in tz ("" =
// the module timezone) and a one-line description.
func (s *AdminService) Preview(_ context.Context, in ScheduleInput, tz string) ([]time.Time, string, error) {
	_, sched, err := parseSchedule(in)
	if err != nil {
		return nil, "", err
	}
	loc := s.defaultLocation()
	if tz != "" {
		if loc, err = parseTimezone(tz); err != nil {
			return nil, "", err
		}
	}
	after := s.o.Now().Truncate(time.Second)
	out := make([]time.Time, 0, PreviewCount)
	for len(out) < PreviewCount {
		t, ok := sched.Next(after, loc)
		if !ok {
			break
		}
		out = append(out, t.In(loc))
		after = t
	}
	return out, describe(sched, loc), nil
}

func describe(s sdkjobs.Schedule, loc *time.Location) string {
	switch s.Kind() {
	case sdkjobs.ScheduleEvery:
		return "Every " + humanDuration(s.Every())
	case sdkjobs.ScheduleCron:
		return fmt.Sprintf("Cron %q in %s", s.Expr(), loc)
	}
	return s.String()
}

func humanDuration(d time.Duration) string {
	unit := func(n int64, name string) string {
		if n == 1 {
			return "1 " + name
		}
		return strconv.FormatInt(n, 10) + " " + name + "s"
	}
	switch {
	case d%(24*time.Hour) == 0:
		return unit(int64(d/(24*time.Hour)), "day")
	case d%time.Hour == 0:
		return unit(int64(d/time.Hour), "hour")
	case d%time.Minute == 0:
		return unit(int64(d/time.Minute), "minute")
	}
	return unit(int64(d/time.Second), "second")
}

// --- side effects -------------------------------------------------------------

func (s *AdminService) wake() {
	if s.o.Wake != nil {
		s.o.Wake()
	}
}

// audit emits one admin event; best-effort, a panicking sink is recovered.
func (s *AdminService) audit(ctx context.Context, action, key string, meta map[string]any) {
	if s.o.AuditSink == nil {
		return
	}
	sink := s.o.AuditSink()
	if sink == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			s.o.Logger.Warn("jobs: audit sink failed", "action", action, "job_key", key, "panic", fmt.Sprintf("%T", r))
		}
	}()
	tenantID, _ := tenantrepo.CurrentTenantID(ctx)
	kind := tenantrepo.CurrentTenantKind(ctx)
	if kind == "" {
		kind = "internal"
	}
	actor, _ := ctxauth.GetUserUUID(ctx)
	ip, _ := ctxauth.GetClientIP(ctx)
	if meta == nil {
		meta = map[string]any{}
	}
	meta["job"] = key
	sink.Emit(ctx, iface.AuditEvent{
		TenantID: tenantID, TenantKind: kind, ActorUserID: actor, ActorType: "user",
		Action: action, ResourceType: "job", ResourceID: key, Outcome: "success", IPAddress: ip, Metadata: meta,
	})
}
