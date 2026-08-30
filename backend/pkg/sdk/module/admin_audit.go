package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Module-config audit vocabulary. Dotted so a reader filters by prefix;
// outcomes reuse the compliance model's existing "success" / "failure".
const (
	ActionModuleConfigUpdated        = "module.config.updated"
	ActionModuleEnvironmentActivated = "module.config.environment_activated"
	ActionModuleEnabled              = "module.enabled"
	ActionModuleDisabled             = "module.disabled"

	auditResourceTypeModule = "module"
	auditOutcomeSuccess     = "success"
	auditOutcomeFailure     = "failure"
	// auditMaxKeys bounds the key lists an event carries; auditMaxUserAgent
	// bounds the one free-text header the event keeps.
	auditMaxKeys      = 64
	auditMaxUserAgent = 256
)

// AdminActor is the request principal recorded on a module-config audit
// event. The host resolves it (SetActorResolver) because pkg/sdk cannot
// import the backend's auth middleware. Email is deliberately absent: the
// immutable user UUID is sufficient attribution and avoids duplicating
// mutable PII into a two-year audit store.
type AdminActor struct {
	UserID     string
	TenantID   string
	TenantKind string
	IP         string
	UserAgent  string
	RequestID  string
}

// SetAuditSink installs the platform audit sink. Nil-tolerant: SDK
// embedders and isolated tests may run without one; the in-tree server
// wiring requires it (cmd/server/admin_wiring.go).
func (h *ModuleAdminHandler) SetAuditSink(sink iface.AuditSink) { h.auditSink = sink }

// SetActorResolver installs the host's principal resolver. Nil-tolerant;
// without it events carry a "system" actor.
func (h *ModuleAdminHandler) SetActorResolver(fn func(context.Context) AdminActor) {
	h.actorResolver = fn
}

// auditRecord is one mutation attempt's outcome as the handler observed it.
type auditRecord struct {
	action      string
	module      string
	env         string
	err         error                // nil on success
	config      map[string]string    // submitted non-secret values — names only are recorded
	secrets     map[string]string    // submitted secrets — names only are recorded
	recordLists []RecordListMutation // membership intents — field + counts only are recorded
}

// emitAudit hands one event to the sink. Best-effort by contract: Emit
// returns nothing, the compliance sink logs its own insert failures, and a
// sink that panics is recovered here so the HTTP result of a mutation that
// already happened never changes. Values never enter the event — only
// schema-derived key names, the stable error code and request provenance.
func (h *ModuleAdminHandler) emitAudit(ctx context.Context, rec auditRecord) {
	if h.auditSink == nil {
		return
	}
	outcome := auditOutcomeSuccess
	if rec.err != nil {
		outcome = auditOutcomeFailure
	}
	defer func() {
		if r := recover(); r != nil {
			// Registered before anything else runs: the host's actor resolver
			// and a fork module's ConfigSchema() are foreign code on a path
			// where the mutation has already committed; none of them may
			// change the HTTP result. The panic value is foreign text; only
			// its type is logged.
			h.logger().Warn("module admin audit: sink failed",
				slog.String("action", rec.action),
				slog.String("module", rec.module),
				slog.String("outcome", outcome),
				slog.String("panic", fmt.Sprintf("%T", r)))
		}
	}()
	var actor AdminActor
	if h.actorResolver != nil {
		actor = h.actorResolver(ctx)
	}
	schema := h.schemaFor(rec.module)
	keys, unknown := auditKeyNames(schema, rec.config, false)
	secretKeys, unknownSecrets := auditKeyNames(schema, rec.secrets, true)
	lists, listFields, unknownLists := auditRecordLists(schema, rec.recordLists)
	if len(listFields) > 0 {
		// A membership-only save touches no value key; the record-list field
		// itself is what changed.
		merged := map[string]bool{}
		for _, k := range append(keys, listFields...) {
			merged[k] = true
		}
		keys = boundedNames(merged)
	}

	meta := map[string]any{"keys": keys, "secretKeys": secretKeys}
	if len(lists) > 0 {
		meta["recordLists"] = lists
	}
	if rec.env != "" {
		meta["env"] = rec.env
	}
	if n := unknown + unknownSecrets + unknownLists; n > 0 {
		meta["unknownKeyCount"] = n
	}
	if code := auditErrorCode(rec.err); code != "" {
		meta["code"] = code
	}
	if actor.RequestID != "" {
		meta["requestId"] = actor.RequestID
	}
	actorType := "system"
	if actor.UserID != "" {
		actorType = "user"
	}
	ev := iface.AuditEvent{
		TenantID:     actor.TenantID,
		TenantKind:   actor.TenantKind,
		ActorUserID:  actor.UserID,
		ActorType:    actorType,
		Action:       rec.action,
		ResourceType: auditResourceTypeModule,
		ResourceID:   rec.module,
		Outcome:      outcome,
		IPAddress:    actor.IP,
		UserAgent:    boundString(actor.UserAgent, auditMaxUserAgent),
		Metadata:     meta,
	}
	h.auditSink.Emit(ctx, ev)
}

func boundString(v string, max int) string {
	if len(v) <= max {
		return v
	}
	return v[:max]
}

// auditKeyNames reduces a submitted key set to schema-derived names. secret
// selects which half of the schema the keys must belong to: a key submitted
// in the config block must be declared as a non-secret field (or a
// record-list roster / label / non-secret sub-field); a key in the secrets
// block must be a declared secret (or a secret sub-field). Anything else —
// undeclared, or declared with the other type — contributes only to the
// returned count, so a misfiled key is never reported as if it were the
// field it names. An element key <field>.<slug>.<sub> collapses to
// <field>.<sub>, the roster key to <field>, a label key to <field>.__label.
// Sorted, de-duplicated, capped — operator-supplied text never reaches the
// audit store.
func auditKeyNames(schema []ConfigField, submitted map[string]string, secret bool) ([]string, int) {
	if len(submitted) == 0 {
		return []string{}, 0
	}
	set := map[string]bool{}
	declared := map[string]bool{}
	var lists []ConfigField
	for _, f := range schema {
		switch {
		case f.Type == FieldRecordList:
			lists = append(lists, f)
		case (f.Type == FieldSecret) == secret:
			declared[f.Key] = true
		}
	}
	unknown := 0
	for key := range submitted {
		if declared[key] {
			set[key] = true
			continue
		}
		if !collapseRecordListKey(lists, key, secret, set) {
			unknown++
		}
	}
	return boundedNames(set), unknown
}

func collapseRecordListKey(lists []ConfigField, key string, secret bool, set map[string]bool) bool {
	for _, f := range lists {
		if !secret && key == RosterKey(f.Key) {
			set[f.Key] = true
			return true
		}
		_, sub, ok := SplitElementKey(f.Key, key)
		if !ok {
			continue
		}
		if !secret && sub == labelSuffix {
			set[f.Key+"."+labelSuffix] = true
			return true
		}
		for _, it := range f.Items {
			if it.Key == sub && (it.Type == FieldSecret) == secret {
				set[f.Key+"."+sub] = true
				return true
			}
		}
	}
	return false
}

// auditRecordLists summarizes membership intents: per declared record-list
// field, how many elements were created and removed. Slugs are operator
// text and are never recorded; an intent on an undeclared field counts as
// unknown.
//
// One row per FIELD, the first one seen: a duplicate intent is refused by
// the service, but the failure is audited too, and two rows for one field
// would read as two changes that never happened. Both lists are capped at
// auditMaxKeys, like every other key list on an event — a request is
// operator-supplied and must not size the audit document.
func auditRecordLists(schema []ConfigField, mutations []RecordListMutation) (summary []map[string]any, fields []string, unknown int) {
	declared := map[string]bool{}
	for _, f := range schema {
		if f.Type == FieldRecordList {
			declared[f.Key] = true
		}
	}
	seen := map[string]bool{}
	summary = []map[string]any{}
	for _, m := range mutations {
		if !declared[m.Field] {
			unknown++
			continue
		}
		if seen[m.Field] {
			continue
		}
		seen[m.Field] = true
		if len(summary) >= auditMaxKeys {
			continue
		}
		fields = append(fields, m.Field)
		summary = append(summary, map[string]any{"field": m.Field, "created": len(m.Create), "removed": len(m.Remove)})
	}
	return summary, fields, unknown
}

func boundedNames(set map[string]bool) []string {
	names := sortedKeys(set)
	if len(names) > auditMaxKeys {
		names = names[:auditMaxKeys]
	}
	return names
}

func auditErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var invalid *ConfigValidationError
	if errors.As(err, &invalid) {
		return invalid.Code
	}
	if errors.Is(err, ErrRevisionStale) {
		return CodeConfigRevisionStale
	}
	return ""
}

// schemaFor reads the declared schema from the registered module — the
// binary's schema, not the stored copy — so key collapsing never depends on
// a document read that may itself have failed.
func (h *ModuleAdminHandler) schemaFor(name string) []ConfigField {
	for _, m := range h.registry.AllModules() {
		if m.Name() == name {
			return ConfigSchemaOf(m)
		}
	}
	return nil
}

func (h *ModuleAdminHandler) logger() *slog.Logger {
	if h.configService != nil && h.configService.logger != nil {
		return h.configService.logger
	}
	return slog.Default()
}
