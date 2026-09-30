package services

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/internal/core/compliance/retentionclass"
	"github.com/orkestra/backend/internal/shared/redact"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Validation error codes (compliance spec §1.4). Errors always block, even
// with four eyes on.
const (
	IssueInvalidEnum            = "invalid_enum"
	IssueRetentionOutOfRange    = "retention_out_of_range"
	IssueClassBelowMinimum      = "class_below_minimum"
	IssueClassNotTenantSettable = "class_not_tenant_settable"
	IssueSinksNotPlatform       = "sinks_not_platform"
	IssueSinksMissing           = "sinks_missing"
	IssueSinkInvalid            = "sink_invalid"
	IssueInvalidPIIKey          = "invalid_pii_key"
	IssueNameInvalid            = "name_invalid"
	IssueNameTaken              = "name_taken"
	IssueReasonRequired         = "reason_required"
)

// Warning codes: they need an explicit acknowledgement and, with four eyes
// on, a second operator's approval. less_restrictive_than_current is this
// plan's addition to spec §1.4 (an assignment that weakens a tenant).
const (
	WarnLessRestrictiveThanPlatform    = "less_restrictive_than_platform"
	WarnLessRestrictiveThanCurrent     = "less_restrictive_than_current"
	WarnRetentionLongerThanDefault     = "retention_longer_than_default"
	WarnRetentionShorterThanDefault    = "retention_shorter_than_default"
	WarnSinkRetentionLongerThanDefault = "sink_retention_longer_than_default"
	WarnExternalSinkChanged            = "external_sink_changed"
	WarnIPFull                         = "ip_full"
	WarnReviewOverdue                  = "review_overdue"
	WarnAccountabilityIncomplete       = "accountability_incomplete"
)

const (
	maxNameLen   = 80
	maxReasonLen = 500
	maxPIIKeys   = 100
)

var piiKeyRe = regexp.MustCompile(`^[a-z0-9]{2,40}$`)

type PolicyIssue struct {
	Code   string         `json:"code"`
	Field  string         `json:"field,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

type ValidationResult struct {
	Errors   []PolicyIssue `json:"errors"`
	Warnings []PolicyIssue `json:"warnings"`
}

func (r *ValidationResult) addError(code, field string, params map[string]any) {
	r.Errors = append(r.Errors, PolicyIssue{Code: code, Field: field, Params: params})
}

func (r ValidationResult) HasError(code string) bool {
	return slices.ContainsFunc(r.Errors, func(i PolicyIssue) bool { return i.Code == code })
}

func (r ValidationResult) ErrorCodes() []string { return uniqueCodes(r.Errors) }

func (r ValidationResult) WarningCodes() []string { return uniqueCodes(r.Warnings) }

func uniqueCodes(issues []PolicyIssue) []string {
	out := make([]string, 0, len(issues))
	for _, i := range issues {
		out = append(out, i.Code)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// NormalizePolicyInput trims the texts, normalizes the PII keys (letters and
// digits, lowercase, sorted, unique) and, on the platform policy, fills the
// classes the input leaves out with their defaults. It never drops an
// invalid value: the validator must still see it.
func NormalizePolicyInput(in models.PolicyInput, isPlatform bool) models.PolicyInput {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Description = strings.TrimSpace(in.Description)
	keys := make([]string, 0, len(in.LogContent.PIIKeys))
	for _, k := range in.LogContent.PIIKeys {
		keys = append(keys, redact.NormalizeKey(k))
	}
	slices.Sort(keys)
	out.LogContent.PIIKeys = slices.Compact(keys)
	out.Retention = maps.Clone(in.Retention)
	if out.Retention == nil {
		out.Retention = map[iface.RetentionClass]int{}
	}
	if isPlatform {
		for _, c := range retentionclass.All() {
			if _, ok := out.Retention[c.Key]; !ok {
				out.Retention[c.Key] = c.DefaultDays
			}
		}
	}
	out.Sinks = in.Sinks.Clone()
	out.Accountability.RoPARef = strings.TrimSpace(in.Accountability.RoPARef)
	out.Accountability.AssessmentRef = strings.TrimSpace(in.Accountability.AssessmentRef)
	out.Accountability.Owner = strings.TrimSpace(in.Accountability.Owner)
	return out
}

// NormalizeReason trims a change reason or decision note; ok=false when it
// is empty or longer than 500 characters (spec D9).
func NormalizeReason(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > maxReasonLen {
		return s, false
	}
	return s, true
}

// PolicyCheck is one validation: the normalized input, whether it is the
// platform policy, the current platform policy (nil when validating the
// platform policy itself), whether another policy already has the name.
type PolicyCheck struct {
	Input      models.PolicyInput
	IsPlatform bool
	Platform   *models.Policy
	// Current is the policy being edited, nil for a new one. Only the sinks
	// of the platform policy use it, to spot new or changed external sinks.
	Current   *models.Policy
	NameTaken bool
	Now       time.Time
}

func ValidatePolicy(c PolicyCheck) ValidationResult {
	var r ValidationResult
	in := c.Input
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > maxNameLen:
		r.addError(IssueNameInvalid, "name", map[string]any{"max": maxNameLen})
	case c.NameTaken:
		r.addError(IssueNameTaken, "name", nil)
	}
	lc := in.LogContent
	if !lc.IPAddress.Valid() {
		r.addError(IssueInvalidEnum, iface.FieldIPAddress, map[string]any{"value": string(lc.IPAddress)})
	}
	if !lc.UserAgent.Valid() {
		r.addError(IssueInvalidEnum, iface.FieldUserAgent, map[string]any{"value": string(lc.UserAgent)})
	}
	if !lc.SubjectIDs.Valid() {
		r.addError(IssueInvalidEnum, iface.FieldSubjectIDs, map[string]any{"value": string(lc.SubjectIDs)})
	}
	if len(lc.PIIKeys) > maxPIIKeys {
		r.addError(IssueInvalidPIIKey, iface.FieldPIIKeys, map[string]any{"max": maxPIIKeys})
	}
	for _, k := range lc.PIIKeys {
		if !piiKeyRe.MatchString(k) {
			r.addError(IssueInvalidPIIKey, iface.FieldPIIKeys, map[string]any{"key": k})
		}
	}
	if !models.ValidRole(in.Accountability.Role) {
		r.addError(IssueInvalidEnum, "accountability.role", map[string]any{"value": in.Accountability.Role})
	}
	for _, class := range sortedClasses(in.Retention) {
		days := in.Retention[class]
		field := "retention." + string(class)
		def, ok := retentionclass.Get(class)
		switch {
		case !ok:
			r.addError(IssueInvalidEnum, field, map[string]any{"value": string(class)})
		case !c.IsPlatform && !def.TenantSettable:
			r.addError(IssueClassNotTenantSettable, field, nil)
		case def.HardMinimum && days < def.MinDays:
			r.addError(IssueClassBelowMinimum, field, map[string]any{"min": def.MinDays})
		case days < def.MinDays || days > retentionclass.MaxDays:
			r.addError(IssueRetentionOutOfRange, field, map[string]any{"min": def.MinDays, "max": retentionclass.MaxDays})
		}
	}
	switch {
	case c.IsPlatform && in.Sinks == nil:
		r.addError(IssueSinksMissing, "sinks", nil)
	case c.IsPlatform:
		validateSinks(&r, in.Sinks)
	case in.Sinks != nil:
		r.addError(IssueSinksNotPlatform, "sinks", nil)
	}
	r.Warnings = PolicyWarnings(in, c.IsPlatform, c.Platform, c.Now)
	if c.IsPlatform && in.Sinks != nil {
		var current *models.SinkPolicy
		if c.Current != nil {
			current = c.Current.Sinks
		}
		r.Warnings = append(r.Warnings, sinkWarnings(in.Sinks, current)...)
	}
	return r
}

func validateSinks(r *ValidationResult, s *models.SinkPolicy) {
	rng := func(field string, v, lo, hi int) {
		if v < lo || v > hi {
			r.addError(IssueSinkInvalid, "sinks."+field, map[string]any{"min": lo, "max": hi})
		}
	}
	rng("loki.days", s.Loki.Days, 1, 3650)
	rng("loki.warnErrorDays", s.Loki.WarnErrorDays, max(1, s.Loki.Days), 3650)
	rng("tempo.days", s.Tempo.Days, 1, 365)
	rng("prometheus.days", s.Prometheus.Days, 1, 365)
	rng("containerLogs.maxSizeMb", s.ContainerLogs.MaxSizeMB, 1, 1024)
	rng("containerLogs.maxFiles", s.ContainerLogs.MaxFiles, 1, 100)
	rng("backups.days", s.Backups.Days, 1, 3650)
	rng("backups.minKeep", s.Backups.MinKeep, 1, 100)
	rng("dsrExportDays", s.DSRExportDays, 1, 90)
	rng("spool.deadLetterDays", s.Spool.DeadLetterDays, 1, 365)
	kinds := []string{models.ExternalKindOTLP, models.ExternalKindSyslog, models.ExternalKindBackupOffsite, models.ExternalKindOther}
	for i, e := range s.External {
		field := fmt.Sprintf("sinks.external[%d]", i)
		if strings.TrimSpace(e.Name) == "" || !slices.Contains(kinds, e.Kind) || strings.TrimSpace(e.DPARef) == "" ||
			e.DeclaredRetentionDays < 1 || e.DeclaredRetentionDays > retentionclass.MaxDays {
			r.addError(IssueSinkInvalid, field, nil)
		}
	}
}

// sinkWarnings are the warnings of the platform sink policy: values above the
// spec §5.1 defaults, and external sinks (new processors or transfers, GDPR
// art. 28 and 44) that current does not already have unchanged. current may
// be nil.
func sinkWarnings(next, current *models.SinkPolicy) []PolicyIssue {
	var w []PolicyIssue
	def := models.DefaultSinkPolicy()
	for _, v := range []struct {
		field      string
		val, deflt int
	}{
		{"loki.days", next.Loki.Days, def.Loki.Days},
		{"loki.warnErrorDays", next.Loki.WarnErrorDays, def.Loki.WarnErrorDays},
		{"tempo.days", next.Tempo.Days, def.Tempo.Days},
		{"prometheus.days", next.Prometheus.Days, def.Prometheus.Days},
		{"containerLogs.maxSizeMb", next.ContainerLogs.MaxSizeMB, def.ContainerLogs.MaxSizeMB},
		{"containerLogs.maxFiles", next.ContainerLogs.MaxFiles, def.ContainerLogs.MaxFiles},
		{"backups.days", next.Backups.Days, def.Backups.Days},
		{"dsrExportDays", next.DSRExportDays, def.DSRExportDays},
		{"spool.deadLetterDays", next.Spool.DeadLetterDays, def.Spool.DeadLetterDays},
	} {
		if v.val > v.deflt {
			w = append(w, PolicyIssue{Code: WarnSinkRetentionLongerThanDefault, Field: "sinks." + v.field,
				Params: map[string]any{"default": v.deflt}})
		}
	}
	var known []models.ExternalSink
	if current != nil {
		known = current.External
	}
	for i, e := range next.External {
		if !slices.ContainsFunc(known, func(k models.ExternalSink) bool { return sameExternalSink(e, k) }) {
			w = append(w, PolicyIssue{Code: WarnExternalSinkChanged, Field: fmt.Sprintf("sinks.external[%d]", i)})
		}
	}
	return w
}

// sameExternalSink compares two external sinks; the review date is compared
// as an instant, not as a struct (location pointers differ after a round trip).
func sameExternalSink(a, b models.ExternalSink) bool {
	if !a.ReviewDueAt.Equal(b.ReviewDueAt) {
		return false
	}
	a.ReviewDueAt, b.ReviewDueAt = time.Time{}, time.Time{}
	return a == b
}

// PolicyWarnings are the warnings of a policy on its own (spec §1.4);
// platform is the current platform policy, nil for the platform itself.
func PolicyWarnings(in models.PolicyInput, isPlatform bool, platform *models.Policy, now time.Time) []PolicyIssue {
	var w []PolicyIssue
	if !isPlatform && platform != nil {
		fields := lessRestrictiveFields(in.LogContent, platform.LogContent, in.Retention, platform.Retention)
		if len(fields) > 0 {
			w = append(w, PolicyIssue{Code: WarnLessRestrictiveThanPlatform, Params: map[string]any{"fields": fields}})
		}
	}
	for _, class := range sortedClasses(in.Retention) {
		def, ok := retentionclass.Get(class)
		if !ok {
			continue
		}
		code := ""
		switch days := in.Retention[class]; {
		case days > def.DefaultDays:
			code = WarnRetentionLongerThanDefault
		case days < def.DefaultDays:
			code = WarnRetentionShorterThanDefault
		}
		if code != "" {
			w = append(w, PolicyIssue{Code: code, Field: "retention." + string(class),
				Params: map[string]any{"default": def.DefaultDays}})
		}
	}
	if in.LogContent.IPAddress == iface.IPAddressFull {
		w = append(w, PolicyIssue{Code: WarnIPFull, Field: iface.FieldIPAddress})
	}
	if due := in.Accountability.ReviewDueAt; due.IsZero() || due.Before(now) {
		w = append(w, PolicyIssue{Code: WarnReviewOverdue, Field: "accountability.reviewDueAt"})
	}
	if in.Accountability.RoPARef == "" || in.Accountability.AssessmentRef == "" {
		w = append(w, PolicyIssue{Code: WarnAccountabilityIncomplete, Field: "accountability"})
	}
	return w
}

// EffectiveRetention is the retention a tenant under p gets: the classes of
// p itself when p is the platform policy, otherwise the platform classes
// overridden by the tenant-settable classes p sets.
func EffectiveRetention(p, platform *models.Policy) map[iface.RetentionClass]int {
	if p.IsPlatformDefault {
		return maps.Clone(p.Retention)
	}
	out := maps.Clone(platform.Retention)
	for class, days := range p.Retention {
		if def, ok := retentionclass.Get(class); ok && def.TenantSettable {
			out[class] = days
		}
	}
	return out
}

// LessRestrictiveThanCurrent warns when moving a tenant from current to next
// (either may be the platform policy) weakens its protection.
func LessRestrictiveThanCurrent(next, current, platform *models.Policy) []PolicyIssue {
	fields := lessRestrictiveFields(next.LogContent, current.LogContent,
		EffectiveRetention(next, platform), EffectiveRetention(current, platform))
	if len(fields) == 0 {
		return nil
	}
	return []PolicyIssue{{Code: WarnLessRestrictiveThanCurrent, Params: map[string]any{"fields": fields}}}
}

// lessRestrictiveFields lists the content fields and the retention classes
// (only those present in nextRet) on which next protects less than base.
func lessRestrictiveFields(next, base iface.LogContentPolicy, nextRet, baseRet map[iface.RetentionClass]int) []string {
	fields := iface.LessRestrictiveContentFields(next, base)
	for _, class := range sortedClasses(nextRet) {
		if b, ok := baseRet[class]; ok && nextRet[class] > b {
			fields = append(fields, "retention."+string(class))
		}
	}
	return fields
}

func sortedClasses(m map[iface.RetentionClass]int) []iface.RetentionClass {
	out := slices.Collect(maps.Keys(m))
	slices.Sort(out)
	return out
}
