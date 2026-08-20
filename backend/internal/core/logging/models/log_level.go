// Package models defines the persistence types for the logging core
// module — ADR-0005 Phase F. The single document in the `log_levels`
// collection holds the global threshold and per-module overrides,
// loaded into the LogLevelService's atomic snapshot at boot and
// refreshed on every admin mutation.
package models

import (
	"errors"
	"log/slog"
	"strings"
	"time"
)

// LogLevel is the persisted value-object. The accepted strings match
// what slog accepts plus "warning" as a synonym for "warn". Other
// values are rejected at the boundary — bad data never reaches the
// PerModuleLevelHandler.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// AllLevels returns the supported levels in increasing-severity order.
// Frontend uses this to build the dropdown without hardcoding.
func AllLevels() []LogLevel {
	return []LogLevel{LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError}
}

// Parse normalises the input string to a LogLevel or returns
// ErrInvalidLogLevel. Accepts both "warn" and "warning" for parity
// with slog and with the env-var parser in shared/utils.
func Parse(s string) (LogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case string(LogLevelDebug):
		return LogLevelDebug, nil
	case string(LogLevelInfo):
		return LogLevelInfo, nil
	case string(LogLevelWarn), "warning":
		return LogLevelWarn, nil
	case string(LogLevelError):
		return LogLevelError, nil
	default:
		return "", ErrInvalidLogLevel
	}
}

// Slog returns the slog.Level corresponding to this LogLevel.
// Panics on an unrecognised value because the parser guarantees the
// set is closed; an unknown value here would be a programmer error.
func (l LogLevel) Slog() slog.Level {
	switch l {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelInfo:
		return slog.LevelInfo
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		panic("models.LogLevel: unrecognised value " + string(l))
	}
}

// ErrInvalidLogLevel is returned by Parse for unrecognised input.
var ErrInvalidLogLevel = errors.New("invalid log level (expected debug | info | warn | error)")

// PermanentConfigInput is the complete desired permanent log-level
// configuration. ExpectedUpdatedAt provides optimistic concurrency against
// the server snapshot the operator edited.
type PermanentConfigInput struct {
	Global            LogLevel
	PerModule         map[string]LogLevel
	ExpectedUpdatedAt time.Time
}

// LogLevelDoc is the single-document shape persisted in the
// log_levels collection. There is exactly one document per
// deployment — keyed by ConfigKey — so admin mutations are simple
// upserts that overwrite the prior snapshot.
//
// PerModule keys are module names matching the lowercase directory
// convention (e.g. "rag", "billing", "auth"). Missing keys fall
// back to Global at lookup time.
type LogLevelDoc struct {
	// ConfigKey is a fixed sentinel ("default") that makes the
	// upsert a single-row replace regardless of how the collection
	// is queried. Indexed unique.
	ConfigKey string `bson:"_id" json:"-"`

	// Global threshold applied to records with no module attribute
	// or modules not listed in PerModule.
	Global LogLevel `bson:"global" json:"global"`

	// PerModule maps module name → level override. Modules absent
	// from this map inherit Global.
	PerModule map[string]LogLevel `bson:"perModule" json:"perModule"`

	// Diagnostics maps module name → temporary diagnostic override.
	// Expired entries are ignored by the resolver until cleanup removes
	// them from persistence.
	Diagnostics map[string]DiagnosticOverride `bson:"diagnostics" json:"diagnostics"`

	UpdatedAt  time.Time `bson:"updatedAt" json:"updatedAt"`
	UpdatedBy  string    `bson:"updatedBy,omitempty" json:"updatedBy,omitempty"`
	UpdateNote string    `bson:"updateNote,omitempty" json:"updateNote,omitempty"`
}

// DefaultConfigKey is the sentinel _id used for the single document
// in log_levels.
const DefaultConfigKey = "default"

// AdminView shapes the response of GET /v1/admin/observability/log-levels.
// It surfaces the persisted Global + PerModule alongside the catalog
// of known module names so the UI can render a row per module
// (including modules without overrides). EffectiveLevel resolves the
// per-module fallback for each module so the UI doesn't need to
// re-do the resolution.
type AdminView struct {
	Global      LogLevel               `json:"global"`
	Modules     []AdminModuleEntry     `json:"modules"`
	Diagnostics []AdminDiagnosticEntry `json:"diagnostics"`
	LogProvider LogProviderStatus      `json:"logProvider"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	UpdatedBy   string                 `json:"updatedBy,omitempty"`
}

// LogProviderStatus reports optional preview/deep-link capabilities without
// making the logging workspace depend on Loki or Grafana being deployed.
type LogProviderStatus struct {
	Available  bool   `json:"available"`
	GrafanaURL string `json:"grafanaUrl,omitempty"`
}

// LogEvent is the minimized preview projection returned to Tier-1 operators.
// Message is preserved for diagnostic usefulness and may still contain
// personal data; Attributes contains only explicitly allowlisted fields.
type LogEvent struct {
	Timestamp  time.Time      `json:"timestamp"`
	Level      LogLevel       `json:"level"`
	Message    string         `json:"message"`
	Module     string         `json:"module"`
	Attributes map[string]any `json:"attributes"`
}

// AdminModuleEntry is one row in the observability admin table.
// Effective is what the handler currently gates on for this module;
// Override is the durable per-module setting even when a diagnostic
// temporarily changes Effective. HasOverride remains true when Override
// is present so existing consumers can render the "revert to global"
// affordance correctly.
type AdminModuleEntry struct {
	Name        string    `json:"name"`
	Effective   LogLevel  `json:"effective"`
	Override    *LogLevel `json:"override,omitempty"`
	HasOverride bool      `json:"hasOverride"`
}

// DiagnosticOverride is a temporary per-module threshold persisted in
// the log_levels document. A nil ExpiresAt makes the diagnostic active
// until an operator stops it explicitly.
type DiagnosticOverride struct {
	Level     LogLevel   `bson:"level" json:"level"`
	StartedAt time.Time  `bson:"startedAt" json:"startedAt"`
	StartedBy string     `bson:"startedBy" json:"startedBy"`
	ExpiresAt *time.Time `bson:"expiresAt,omitempty" json:"expiresAt,omitempty"`
}

// AdminDiagnosticEntry is the active diagnostic projection returned to
// the operator UI. Module is supplied by the diagnostics map key.
type AdminDiagnosticEntry struct {
	Module    string     `json:"module"`
	Level     LogLevel   `json:"level"`
	StartedAt time.Time  `json:"startedAt"`
	StartedBy string     `json:"startedBy"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}
