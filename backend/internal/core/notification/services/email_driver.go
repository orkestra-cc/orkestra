package services

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
)

var (
	// ErrNoSenderForCategory: no profile pattern matches the category. Fail-closed (D5).
	ErrNoSenderForCategory = errors.New("notification: no sender profile matches the category")
	// ErrUnknownDriver: the profile's provider names no registered driver.
	ErrUnknownDriver = errors.New("notification: sender profile names an unregistered driver")
	// ErrSenderNotConfigured: the profile lacks a field its driver requires.
	ErrSenderNotConfigured = errors.New("notification: sender profile is incomplete")
	// ErrSenderConfigUnavailable: the roster could not be read. Fail-closed —
	// routing on a guessed roster could push campaign mail through the
	// transactional sender, the failure this design exists to prevent.
	ErrSenderConfigUnavailable = errors.New("notification: sender configuration unavailable")
	// ErrSenderNotFound: a slug named no profile (the explicit-sender test
	// send, PR 2). Declared here with its siblings so the chokepoint's
	// trusted-sentinel list is complete from PR 1.
	ErrSenderNotFound = errors.New("notification: sender profile not found")
)

// EmailMessage is a fully rendered message ready to hand to a driver.
type EmailMessage struct {
	To       string
	ToName   string
	Subject  string
	BodyText string
	BodyHTML string

	// Category is the routing category ("auth.verify_email"). It already
	// exists at the chokepoint and is what MailUp's CampaignCode needs; the
	// smtp and noop drivers ignore it.
	Category string
}

// ProfileRequirement names one sub-field a driver cannot send without.
type ProfileRequirement struct {
	Key    string
	Secret bool // a FieldSecret — invisible to the save-time gate (D5)
}

// EmailDriver is one transport behind the registry (ADR-0019 D3).
type EmailDriver interface {
	Name() string
	// Requires is the single declaration both ValidateProfile views read,
	// so the save-time gate and the runtime check cannot drift.
	Requires() []ProfileRequirement
	Send(ctx context.Context, p SenderProfile, msg EmailMessage) error
}

// RequirementView selects which requirements ValidateProfile enforces.
type RequirementView int

const (
	// RuntimeView enforces every requirement: the send path, IsConfigured, IsConfiguredFor.
	RuntimeView RequirementView = iota
	// SaveTimeView skips secrets: ValidateConfig never sees decrypted material.
	SaveTimeView
)

// ProfileIncompleteError lists the sub-field keys a profile is missing.
// The keys are ours (constants), never operator text.
type ProfileIncompleteError struct {
	Driver  string
	Missing []string
}

func (e *ProfileIncompleteError) Error() string {
	return "notification: sender profile is incomplete: missing " + strings.Join(e.Missing, ",")
}

func (e *ProfileIncompleteError) Is(target error) bool { return target == ErrSenderNotConfigured }

// ValidateProfile reports whether p satisfies d's requirements under view.
func ValidateProfile(d EmailDriver, p SenderProfile, view RequirementView) error {
	var missing []string
	for _, r := range d.Requires() {
		if view == SaveTimeView && r.Secret {
			continue
		}
		if strings.TrimSpace(p.Field(r.Key)) == "" {
			missing = append(missing, r.Key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &ProfileIncompleteError{Driver: d.Name(), Missing: missing}
}

// DriverRegistry maps provider names to drivers.
type DriverRegistry struct{ byName map[string]EmailDriver }

func NewDriverRegistry(drivers ...EmailDriver) *DriverRegistry {
	r := &DriverRegistry{byName: make(map[string]EmailDriver, len(drivers))}
	for _, d := range drivers {
		r.byName[d.Name()] = d
	}
	return r
}

func (r *DriverRegistry) Get(name string) (EmailDriver, bool) {
	if r == nil {
		return nil, false
	}
	d, ok := r.byName[name]
	return d, ok
}

// Names lists registered drivers, sorted, for diagnostics and tests.
func (r *DriverRegistry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// CoreDrivers builds every driver the base ships. module.go registers them;
// tests register fakes instead.
func CoreDrivers(logger *slog.Logger) []EmailDriver {
	return []EmailDriver{NewNoopDriver(logger), NewSMTPDriver(logger)}
}
