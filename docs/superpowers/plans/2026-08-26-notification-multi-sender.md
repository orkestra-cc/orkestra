# Notification Multi-Sender Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the `notification` module operator-declared sender profiles — each its own transport and identity — selected per send by category pattern, behind a populated driver seam (`noop`, `smtp`, `mailup`), fail-closed, with legacy flat keys surviving as the synthesized default.

**Architecture:** Four code PRs and one docs-site PR against `dev`, each green alone. PR 1 replaces the single `EmailSender` with `SenderProfile` + `EmailDriver` + registry + a resolver that always returns the legacy profile, and routes `dispatchEmail` through it — plus two declared behaviour changes: the driver error contract and an SMTP connection deadline. PR 2 declares the `email.senders` record list, the save-time/activation validators, real roster resolution, the `CategoryConfiguredChecker` companion, `auth`'s eight guards, and `sender` on the test endpoint. PR 3 adds the MailUp driver. PR 4 adds `senderSlug` + a sender filter on the delivery log. PR 5 rewrites the two docs-site pages.

**Tech Stack:** Go 1.25.13, Huma v2, MongoDB 8, `net/smtp`, `net/http` + `httptest`, React 19 i18n JSON (EN/IT parity test), Docusaurus (`docs/site`).

**Spec:** `docs/superpowers/specs/2026-08-26-notification-multi-sender-design.md`
**ADR:** `docs/adr/0019-notification-multi-sender.md`

## Global Constraints

- **Sequencing:** PR 1 → PR 2 → PR 3 → PR 4 → PR 5, each branched off `dev` after the previous merged. Branches: `feat/adr-0019-pr1-driver-seam`, `feat/adr-0019-pr2-sender-profiles`, `feat/adr-0019-pr3-mailup-driver`, `feat/adr-0019-pr4-delivery-log-sender`, `docs/adr-0019-pr5-site-pages`.
- **Docs move in the same commit as the code** (repo rule): every task that changes behaviour updates `backend/internal/core/notification/CLAUDE.md` (and `auth`/`pkg/sdk` CLAUDE.md where touched) in that commit. No trailing docs PR except PR 5, which is the docs *site*.
- **`iface.NotificationSender` and its DTOs are unchanged** (D1). The only `pkg/sdk/iface` addition is the optional `CategoryConfiguredChecker` + `IsConfiguredForCategory` accessor (D7).
- **`EmailMessage` gains exactly one field: `Category`.** No `Type`, no `MessageUUID`.
- **`smtp` requires `smtp_host`, `smtp_port`, `from_address` — never credentials** (anonymous relay stays supported, D3/D6). `noop` requires nothing. `mailup` requires `from_address`, `mailup_user`, `mailup_secret`.
- **No string produced by a remote peer is ever persisted or logged** — not in `NotificationDoc.Error`, not on stdout, not at debug level. Drivers return a typed `*SendError` (no free-text field); the chokepoint renders the diagnostic from its fields with every string allowlisted (`[A-Za-z0-9._-]`, ≤64 chars per token; media types also admit `/` and `+`); the whole stored value is capped at 512.
- **Bounded reads:** every HTTP response body is read through `io.LimitReader(body, 64<<10 + 1)` before any parse; `http.Client{Timeout: 30 * time.Second}`.
- **MailUp success is an allowlist:** `2xx ∧ body ≤ 64 KiB ∧ parses ∧ Status == "done" ∧ Code == "0"`; everything else fails.
- **Validation states (D5):** roster empty → rules vacuous; roster non-empty but no profile declares a pattern → vacuous; otherwise all rules apply, and every per-profile rule (driver known, driver complete) applies only to profiles declaring ≥1 pattern; grammar applies to any profile whose normalized pattern list is non-empty.
- **Pattern grammar:** exactly `*`, exact `foo.bar`, or prefix `foo.*` (matches `foo.` + ≥1 char at any depth, never bare `foo`). A token is any non-empty run of characters other than `.`, `*` and `,` (the `stringList` separator) — `iface` does not restrict a fork's category charset (`crm/campaign`, `vendor:event`), so neither does the pattern. Patterns are trimmed + lowercased on read; the category is lowercased, **not trimmed**, for comparison. Precedence = longest required literal; ties impossible.
- **Error codes** are `notification.sender_*` constants in `internal/shared/errcode/codes.go` with matching rows in `codes_test.go`'s `goldenCodes`.
- **New Mongo query sites need an inline `//tenantscope:allow` comment**; never add baseline rows by hand (PR 4 removes three rows whose line numbers move).
- **No new `go.mod` / `replace` / `go.work`** (ADR-0006).
- Before each commit: `cd backend && go test ./internal/core/notification/... ./pkg/sdk/... -count=1` (plus `./internal/core/auth/...` in PR 2). Before opening each PR: `make ci-backend` (PR 2 also `make ci-frontend-admin`).

## Declared deviations from the spec (read before executing)

1. **`EmailDriver` exposes `Requires() []ProfileRequirement` instead of a `Validate(profile)` method** — **recorded in ADR-0019 D3 and the spec's §Components / §Driver validation contract on this branch (PR 0)**, not a silent plan-level choice. `ValidateProfile(driver, profile, view)` is a package function over that declaration. Reason: the save-time gate (secret-blind) and the runtime check (secret-aware) must read *one* declaration or they drift, and the spec's schema/driver agreement test needs the requirement list as data. `Requires()` expresses only "field non-empty"; a driver needing a richer check later adds a method then, with a recorded decision — not a `Validate` carried empty now.
2. **Empty `email.provider` normalizes to `noop` in the synthesized legacy profile.** Today `Send("")` behaves as noop but `IsConfigured("")` returns `false`. The synthesized profile cannot carry that inconsistency; `deps.GetConfig` never returns `""` for `email.provider` (schema default `noop`), so no installation observes the change.
3. **PR 1 carries two behaviour changes, both accounted for here.** (a) The driver error contract: `NotificationDoc.Error` for a failed SMTP send changes from the Go error text to the bounded diagnostic (`smtp op=auth code=535`), and the error a caller receives is the same bounded text, never the driver's own. The SMTP driver is rewritten in PR 1 anyway; writing its error path twice would be waste. (b) The SMTP connection deadline (deviation 6). Everything else in PR 1 is behaviour-neutral and the golden MIME test proves it on the wire.
4. **Three `auth` sends carry bare categories today** (`verify_email`, `reset_password` — the `EmailTokenPurpose*` constants — not `auth.verify_email` / `auth.reset_password` as the spec's table states). PR 2 Task 8 aligns them to the `notifModels.CategoryAuth*` constants so an operator's `auth.*` pattern actually captures verification and reset mail. Delivery-log rows created before the change keep the old category string.
5. **The console has no delivery-log page** (verified: nothing under `frontend-admin/src` consumes `GET /v1/notifications`), so PR 4's "delivery-log column" has nowhere to land. PR 4 ships the API side (`senderSlug` field + `sender` filter); the column follows whenever a page exists.
6. **The SMTP driver sets a connection deadline** (context deadline, else 30s). Today a relay that accepts and never answers hangs the send goroutine forever; the spec's test table requires `err=timeout` for that case.

## Corrections found while executing PR 1 (apply to later PRs)

7. **`EmailMessage` must leave `email_service.go` in PR 1 Task 1, not Task 4.** Task 1 declares `EmailMessage` in `email_driver.go` while `email_service.go` still owns it, so the package does *not* still compile as Task 1 Step 5 claims — it fails with `EmailMessage redeclared`. Delete the old declaration in Task 1: the new one is a superset (adds `Category`), so the legacy transport compiles unchanged until Task 4 rewrites it.
8. **The plan's `fakeResolver` violates the resolver contract.** It returns `f.profile, f.err` together, but `senderResolver` returns the **zero** profile on failure — and `TestNotificationService_Dispatch_FailClosedPaths` asserts exactly that (`sender=-`, empty `Provider`), so those two subtests fail against the plan's fake. The fake must return `SenderProfile{}` when `err != nil`, in both `Resolve` and `Default`. Already fixed in the committed test file; PR 2 extends the same fake and must keep it.

## Spec / ADR amendments made on this branch (PR 0) while planning

- **D6 cutover is the routing map, not roster emptiness.** The spec and ADR said the legacy profile is synthesized "when the roster is empty"; taken literally, saving the first pattern-less profile would strand every send (no profile matches, `Default` fails, `auth` refuses signups) — contradicting the draft state both documents describe. Amended to: the flat keys carry mail **until some profile declares a pattern**; the resolver and `ValidateSenderConfig` share that predicate; a draft stays reachable by slug for the test send.
- **D3 driver contract** is `Name / Requires / Send` + package-level `ValidateProfile` (deviation 1).
- **`from_name` and `reply_to` are gated on `provider ∈ {smtp, mailup}`** like `from_address`: `noop` reads none of them, and D2 says every field a driver does not read is gated off. The spec's config-shape table showed them unconditional.
- **The send error contract is typed at the driver boundary.** `SendError` carries no free-text field; the chokepoint renders the diagnostic from typed fields through the allowlists, so no driver — ours or a fork's — can hand it a remote string.
- **Pattern tokens are charset-free** (any non-empty run without `.`/`*`), stated explicitly; the category is lowercased but not trimmed.
- **MailUp sets `CampaignCode` only**; `CampaignName` is a distinct vendor field the spec does not decide.
- **The chokepoint returns only sanitized errors.** `dispatchEmail`/`SendTest` return a `*DispatchError` (text = the stored reason; `errors.Is` for trusted sentinels only); the driver's raw error is dropped, not wrapped, because `auth` logs `err.Error()`.
- **Malformed categories fail closed at the resolver — once a routing map exists**: empty, or untrimmed, categories match nothing, not even `*`. **Declared exception:** while no profile declares a pattern the category is not inspected at all, because an empty `Category` is legal today and D6 promises byte-identical behaviour until a profile routes.
- **`TenantID` is read from the request context** (`ctxauth.GetTenantID`) and passed to the resolver by both the chokepoint and `IsConfiguredFor`, as D4 intends; the resolver still ignores it.
- **Configuration is decoded from one snapshot per send.** Today's settings closure makes nine separate reads (a TOCTOU across an environment switch); the loader now reads the document once and decodes legacy keys, roster values and secrets from that document's active environment via `module.UnmarshalConfig`, never through a second `GetSecret`.
- **The legacy slug is `_legacy`**, outside the record-list slug grammar, so it can never collide with a roster profile named "Default" (the spec's earlier `default` could).
- **`,` is excluded from pattern tokens** — it is the `stringList` separator.

## File Structure

**PR 1 — driver seam** (`backend/internal/core/notification/services/` unless noted)

| File | Responsibility |
|---|---|
| `sender_profile.go` (create) | `SenderProfile`, the `Sub*` sub-field keys, `Field`/`setField`, `LegacyProfile` |
| `email_driver.go` (create) | `EmailMessage` (+`Category`), `EmailDriver`, `ProfileRequirement`, `ValidateProfile`, `ProfileIncompleteError`, `DriverRegistry`, `CoreDrivers`, sentinels |
| `send_error.go` (create) | `SendError`, `failureKind`, `safeToken`/`safeMediaType`/`capString`, `describeSendError` |
| `driver_noop.go` (create) | `noopDriver`, `truncate` |
| `driver_smtp.go` (rename from `email_service.go`) | `smtpDriver`, `sendSMTP`, `buildMIMEMessage`, `encodeQuotedPrintable`, `smtpError` |
| `sender_resolver.go` (create) | `ResolveInput`, `SenderConfig`, `SenderConfigLoader`, `SenderResolver`, `NewSenderResolver` (legacy-only in PR 1) |
| `sender_loader.go` (create) | `SnapshotGetter`, `NewSnapshotLoader` — one document read per send; legacy (PR 1) and roster (PR 2) decoded from the same active-environment snapshot |
| `notification_service.go` (modify) | `resolver` + `drivers` replace `email`; `dispatchEmail` resolve → validate → send; `SendTest`; `Drivers()` |
| `../module.go` (modify) | Build legacy loader, registry, resolver |
| `../handlers/notification_handler.go` (modify) | Test endpoint calls `SendTest` |
| `../CLAUDE.md` (modify) | Drivers, resolver, error contract |

**PR 2 — profiles, validation, pre-flight**

| File | Responsibility |
|---|---|
| `backend/internal/shared/errcode/codes.go` + `codes_test.go` (modify) | 8 `notification.*` codes |
| `services/sender_patterns.go` (create) | `NormalizePatterns`, `ValidatePattern`, `MatchPattern` |
| `services/sender_config.go` (create) | `SendersField`, `SenderItems()`, `DecodeSenderProfiles` |
| `services/sender_resolver.go` (modify) | Roster matching, `Default`, `BySlug`, config-unavailable fail-closed |
| `services/sender_validation.go` (create) | `ValidateSenderConfig` (three states) |
| `internal/core/notification/config_validation.go` (create) | `ValidateConfig` / `ValidateConfigActivation` |
| `internal/core/notification/module.go` (modify) | `email.senders` field, `senders` group |
| `services/sender_loader.go` (modify) | roster decoded from the same snapshot as the legacy keys |
| `pkg/sdk/iface/interfaces.go` (modify) | `CategoryConfiguredChecker`, `IsConfiguredForCategory` |
| `services/notification_service.go` (modify) | `IsConfiguredFor`, `SendTest.Sender` |
| `internal/core/auth/services/password_auth_service.go`, `suspicious_login_notifier.go` (modify) | 8 guards → accessor; 3 categories aligned |
| `handlers/notification_handler.go` (modify) | `sender` field, 404/422/502 mapping |
| `frontend-admin/src/locales/{en,it}.json` (modify) | `senders` group + `email.senders` field |
| `backend/openapi/enterprise.json` (regenerate) | `sender` on the test endpoint |
| `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/shared-iface.mdx` (modify) | the companion interface row — `docs/site/**` changes must be rendered locally before merge, so PR 2's gate includes a render |

**PR 3 — MailUp:** `services/driver_mailup.go` (+test), `sender_profile.go`, `sender_config.go` (schema), `email_driver.go` (`CoreDrivers`), `send_error.go` (`readBounded`).

**PR 4 — delivery log:** `models/notification.go`, `repository/notification_repository.go`, `handlers/notification_handler.go`, `services/notification_service.go`, `module.go` (index), `tools/tenantscope/baseline.txt`, `openapi/enterprise.json`.

**PR 5 — docs site:** `docs/site/modules/core/notification.mdx`, `docs/site/operating/notifications.mdx`.

---

# PR 1 — Driver seam, legacy-only resolver

Branch: `git checkout dev && git pull && git checkout -b feat/adr-0019-pr1-driver-seam`.

### Task 1: `SenderProfile` and the driver contract

**Files:**
- Create: `backend/internal/core/notification/services/sender_profile.go`
- Create: `backend/internal/core/notification/services/email_driver.go`
- Test: `backend/internal/core/notification/services/email_driver_test.go`

**Interfaces produced:** `SenderProfile`, `Sub*` constants, `(SenderProfile).Field(key) string`, `(*SenderProfile).setField(key, v)`, `LegacyProfile(p) SenderProfile`, `EmailMessage`, `EmailDriver{Name, Requires, Send}`, `ProfileRequirement{Key, Secret}`, `RequirementView{RuntimeView, SaveTimeView}`, `ValidateProfile(d, p, view) error`, `*ProfileIncompleteError{Driver, Missing}`, `DriverRegistry{Get, Names}`, `NewDriverRegistry(...EmailDriver)`, sentinels `ErrNoSenderForCategory`, `ErrUnknownDriver`, `ErrSenderNotConfigured`, `ErrSenderConfigUnavailable`, `ErrSenderNotFound`.

- [ ] **Step 1: Write the failing tests**

```go
package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

type reqDriver struct {
	name string
	reqs []ProfileRequirement
}

func (d *reqDriver) Name() string                      { return d.name }
func (d *reqDriver) Requires() []ProfileRequirement    { return d.reqs }
func (d *reqDriver) Send(context.Context, SenderProfile, EmailMessage) error { return nil }

func TestValidateProfile_ViewsAndMissingFields(t *testing.T) {
	d := &reqDriver{name: "x", reqs: []ProfileRequirement{
		{Key: SubFromAddress}, {Key: SubSMTPHost}, {Key: SubSMTPPassword, Secret: true},
	}}
	p := SenderProfile{FromAddress: "a@b", SMTPHost: "h"}

	if err := ValidateProfile(d, p, SaveTimeView); err != nil {
		t.Fatalf("save-time view must ignore the missing secret, got %v", err)
	}
	err := ValidateProfile(d, p, RuntimeView)
	var inc *ProfileIncompleteError
	if !errors.As(err, &inc) || !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("want *ProfileIncompleteError wrapping ErrSenderNotConfigured, got %v", err)
	}
	if len(inc.Missing) != 1 || inc.Missing[0] != SubSMTPPassword || inc.Driver != "x" {
		t.Fatalf("Missing = %v Driver = %q", inc.Missing, inc.Driver)
	}
	if err := ValidateProfile(d, SenderProfile{}, RuntimeView); err == nil {
		t.Fatal("empty profile must fail")
	}
}

func TestSenderProfile_FieldRoundTrip(t *testing.T) {
	var p SenderProfile
	for _, key := range []string{SubProvider, SubFromAddress, SubFromName, SubReplyTo, SubSMTPHost, SubSMTPTLSMode, SubSMTPUsername, SubSMTPPassword} {
		p.setField(key, "v-"+key)
		if got := p.Field(key); got != "v-"+key {
			t.Errorf("Field(%q) = %q", key, got)
		}
	}
	p.setField(SubSMTPPort, "2525")
	if p.SMTPPort != 2525 || p.Field(SubSMTPPort) != "2525" {
		t.Fatalf("port round trip: %d %q", p.SMTPPort, p.Field(SubSMTPPort))
	}
	if (SenderProfile{}).Field(SubSMTPPort) != "" {
		t.Fatal("zero port must read as unset")
	}
	if (SenderProfile{}).Field("nope") != "" {
		t.Fatal("unknown key must read empty")
	}
}

func TestLegacyProfile_SynthesizesDefault(t *testing.T) {
	p := LegacyProfile(SenderProfile{Provider: "", SMTPHost: "h"})
	if p.Slug != LegacySlug || p.Provider != "noop" || len(p.Categories) != 1 || p.Categories[0] != "*" {
		t.Fatalf("unexpected legacy profile %+v", p)
	}
	// Reserved by construction: the console can never mint this slug.
	if module.ValidSlug(LegacySlug) {
		t.Fatalf("LegacySlug %q must be outside the record-list slug grammar", LegacySlug)
	}
	if p.SMTPHost != "h" {
		t.Fatal("transport fields must be preserved")
	}
	if LegacyProfile(SenderProfile{Provider: "smtp"}).Provider != "smtp" {
		t.Fatal("a set provider must be kept")
	}
}

func TestDriverRegistry(t *testing.T) {
	r := NewDriverRegistry(&reqDriver{name: "b"}, &reqDriver{name: "a"})
	if _, ok := r.Get("a"); !ok {
		t.Fatal("registered driver not found")
	}
	if _, ok := r.Get("ses"); ok {
		t.Fatal("unknown driver must not resolve")
	}
	if n := r.Names(); len(n) != 2 || n[0] != "a" || n[1] != "b" {
		t.Fatalf("Names() = %v, want sorted", n)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'TestValidateProfile|TestSenderProfile|TestLegacyProfile|TestDriverRegistry' -count=1`
Expected: FAIL — `undefined: ProfileRequirement`, `undefined: SenderProfile`.

- [ ] **Step 3: Write `sender_profile.go`**

```go
package services

import "strconv"

// SenderProfile is one configured sender: transport and identity together
// (ADR-0019 D2). PR 2 decodes it from the email.senders record list; while
// the roster is empty it is synthesized from the flat legacy keys (D6).
type SenderProfile struct {
	Slug       string   // element key segment; LegacySlug for the legacy profile
	Label      string   // operator display name
	Provider   string   // driver name: "noop" | "smtp" (| "mailup", PR 3)
	Categories []string // normalized routing patterns; "*" marks the default

	FromAddress string
	FromName    string
	ReplyTo     string

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPTLSMode  string // "starttls" | "tls" | "none"
}

// Sub-field keys of one email.senders element. They are the record-list
// item keys (PR 2), the names a driver's Requires() speaks, and the
// argument Field accepts — one vocabulary, three readers.
const (
	SubProvider     = "provider"
	SubCategories   = "categories"
	SubFromAddress  = "from_address"
	SubFromName     = "from_name"
	SubReplyTo      = "reply_to"
	SubSMTPHost     = "smtp_host"
	SubSMTPPort     = "smtp_port"
	SubSMTPTLSMode  = "smtp_tls_mode"
	SubSMTPUsername = "smtp_username"
	SubSMTPPassword = "smtp_password"
)

// LegacySlug names the profile synthesized from the flat email.* keys. The
// leading underscore is outside the record-list slug grammar
// (^[a-z0-9]+(-[a-z0-9]+)*$), so no roster element can ever mint it: the
// legacy profile and a profile an operator labels "Default" cannot collide,
// in the resolver or in the delivery log.
const LegacySlug = "_legacy"

// Field returns the value stored under a sub-field key; "" when unset or
// unknown. A zero port is unset.
func (p SenderProfile) Field(key string) string {
	switch key {
	case SubProvider:
		return p.Provider
	case SubFromAddress:
		return p.FromAddress
	case SubFromName:
		return p.FromName
	case SubReplyTo:
		return p.ReplyTo
	case SubSMTPHost:
		return p.SMTPHost
	case SubSMTPPort:
		if p.SMTPPort == 0 {
			return ""
		}
		return strconv.Itoa(p.SMTPPort)
	case SubSMTPTLSMode:
		return p.SMTPTLSMode
	case SubSMTPUsername:
		return p.SMTPUsername
	case SubSMTPPassword:
		return p.SMTPPassword
	}
	return ""
}

// setField is Field's inverse, used by the record-list decoder (PR 2).
// Categories are not a scalar and are set by the decoder directly.
func (p *SenderProfile) setField(key, v string) {
	switch key {
	case SubProvider:
		p.Provider = v
	case SubFromAddress:
		p.FromAddress = v
	case SubFromName:
		p.FromName = v
	case SubReplyTo:
		p.ReplyTo = v
	case SubSMTPHost:
		p.SMTPHost = v
	case SubSMTPPort:
		p.SMTPPort, _ = strconv.Atoi(v)
	case SubSMTPTLSMode:
		p.SMTPTLSMode = v
	case SubSMTPUsername:
		p.SMTPUsername = v
	case SubSMTPPassword:
		p.SMTPPassword = v
	}
}

// LegacyProfile stamps the identity of the profile synthesized from the
// flat keys: LegacySlug, the "*" pattern, and — the one normalization —
// an empty provider reads as noop, which is how Send has always treated it.
func LegacyProfile(p SenderProfile) SenderProfile {
	p.Slug = LegacySlug
	p.Label = "Legacy default (flat email.* keys)"
	p.Categories = []string{"*"}
	if p.Provider == "" {
		p.Provider = "noop"
	}
	return p
}
```

- [ ] **Step 4: Write `email_driver.go`**

```go
package services

import (
	"context"
	"errors"
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
```

- [ ] **Step 5: Run to verify pass**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'TestValidateProfile|TestSenderProfile|TestLegacyProfile|TestDriverRegistry' -count=1`
Expected: PASS (the rest of the package still compiles because nothing was removed yet).

- [ ] **Step 6: Commit**

```bash
git add backend/internal/core/notification/services/sender_profile.go backend/internal/core/notification/services/email_driver.go backend/internal/core/notification/services/email_driver_test.go
git commit -m "feat(notification): SenderProfile and the EmailDriver contract (ADR-0019 PR 1)"
```

### Task 2: The send error contract

**Files:**
- Create: `backend/internal/core/notification/services/send_error.go`
- Test: `backend/internal/core/notification/services/send_error_test.go`

**Interfaces produced:** `*SendError{Driver, Op, Kind, Code, HTTP, Status, Vendor, Body, Bytes, Media, Cause}` — typed fields only, `Error()` renders the diagnostic through the allowlists, `Unwrap()` returns `Cause`; constructors `transportError(driver, op, err)`, `rejectionError(driver, op, code, err)`, `vendorEnvelopeError(driver, http, status, code)`, `vendorBodyError(driver, http, body, bytes, media)`; `failureKind(err) string` ∈ `{dial,tls,timeout,canceled,io}`, `safeToken(s)`, `safeMediaType(s)`, `safeKind(s)`, `capString(s, n)`, `describeSendError(p, err) string`.

The chokepoint never copies a string a driver hands it. `SendError` has no free-text field: a driver states *what* failed (step, numeric code, kind, HTTP status, vendor status/code) and the text is rendered from those fields with every string allowlisted — so a future driver cannot smuggle a response body into the delivery log even by writing `&SendError{Status: body}`.

- [ ] **Step 1: Write the failing tests**

```go
package services

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strings"
	"testing"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestFailureKind(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"canceled", context.Canceled, "canceled"},
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"net timeout", timeoutErr{}, "timeout"},
		{"dial", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, "dial"},
		{"other", errors.New("boom"), "io"},
	}
	for _, c := range cases {
		if got := failureKind(c.err); got != c.want {
			t.Errorf("%s: failureKind = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSafeToken(t *testing.T) {
	cases := map[string]string{
		"":                             "",
		"done":                         "done",
		"E_1.2-x":                      "E_1.2-x",
		"has space":                    "invalid",
		"<html>":                       "invalid",
		strings.Repeat("a", 65):        "invalid",
		strings.Repeat("a", 64):        strings.Repeat("a", 64),
	}
	for in, want := range cases {
		if got := safeToken(in); got != want {
			t.Errorf("safeToken(%q) = %q, want %q", in, got, want)
		}
	}
	if safeToken(LegacySlug) != LegacySlug {
		t.Fatalf("LegacySlug %q must survive the diagnostic allowlist", LegacySlug)
	}
	if got := safeMediaType("application/json; charset=utf-8"); got != "invalid" {
		t.Errorf("parameters are not a media type: %q", got)
	}
	if got := safeMediaType("application/vnd.api+json"); got != "application/vnd.api+json" {
		t.Errorf("real media type mangled: %q", got)
	}
}

func TestSendError_Shapes(t *testing.T) {
	cause := errors.New("535 5.7.8 AHVzZXIAcGFzcw==")
	cases := []struct {
		name string
		err  *SendError
		want string
	}{
		{"server rejection", rejectionError("smtp", "auth", 535, cause), "smtp op=auth code=535"},
		{"local failure with op", transportError("smtp", "dial", &net.OpError{Op: "dial", Err: errors.New("refused")}), "smtp op=dial err=dial"},
		{"local failure without op", transportError("mailup", "", context.DeadlineExceeded), "mailup err=timeout"},
		{"vendor envelope", vendorEnvelopeError("mailup", 401, "error", "401"), "http=401 status=error code=401"},
		{"vendor envelope empty fields", vendorEnvelopeError("mailup", 200, "", ""), "http=200 status= code="},
		{"oversized body", vendorBodyError("mailup", 200, bodyTooLarge, 0, ""), "http=200 body=too_large"},
		{"unparseable body", vendorBodyError("mailup", 502, bodyUnparseable, 20, "text/html"), "http=502 body=unparseable bytes=20 type=text/html"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if !errors.Is(rejectionError("smtp", "auth", 535, cause), cause) {
		t.Fatal("Unwrap must expose the cause for errors.Is")
	}
}

// TestSendError_FreeTextCannotReachTheDiagnostic is the chokepoint guarantee:
// a driver — a fork's, or a buggy one of ours — that stuffs remote text into
// every string field of SendError still produces a diagnostic of the fixed
// shape with the marker in place of the text. There is no field it could
// use to pass a body through.
func TestSendError_FreeTextCannotReachTheDiagnostic(t *testing.T) {
	secret := "hunter2 s3cr=t!"
	hostile := &SendError{Driver: "mailup " + secret, Op: "send " + secret, Kind: secret, HTTP: 200,
		Status: "error " + secret, Vendor: secret, Body: secret, Bytes: 3, Media: "text/html; " + secret}
	got := describeSendError(SenderProfile{Slug: "s"}, hostile)
	if strings.Contains(got, secret) {
		t.Fatalf("remote text reached the diagnostic: %q", got)
	}
	if got != "sender=s http=200 status=invalid code=invalid" {
		t.Fatalf("unexpected shape %q", got)
	}
	hostile2 := &SendError{Driver: secret, Op: secret, Kind: secret}
	if got := hostile2.Error(); got != "invalid op=invalid err=io" {
		t.Fatalf("unexpected shape %q", got)
	}
	// An unknown Body marker never selects a body shape.
	if got := (&SendError{HTTP: 200, Body: "<html>"}).Error(); got != "http=200 status= code=" {
		t.Fatalf("unexpected shape %q", got)
	}
}

func TestDescribeSendError_Shapes(t *testing.T) {
	p := SenderProfile{Slug: "esp-campagne", Provider: "smtp"}
	cases := []struct {
		name string
		p    SenderProfile
		err  error
		want string
	}{
		{"no sender", SenderProfile{}, ErrNoSenderForCategory, "sender=- err=no_sender_for_category"},
		{"config unavailable", SenderProfile{}, ErrSenderConfigUnavailable, "sender=- err=config_unavailable"},
		{"unknown driver", SenderProfile{Slug: "x", Provider: "ses"}, ErrUnknownDriver, "sender=x driver=ses err=unknown_driver"},
		{"incomplete", p, &ProfileIncompleteError{Driver: "smtp", Missing: []string{SubSMTPHost, SubFromAddress}},
			"sender=esp-campagne driver=smtp err=not_configured missing=smtp_host,from_address"},
		{"driver error", p, rejectionError("smtp", "auth", 535, errors.New("535 secret")),
			"sender=esp-campagne smtp op=auth code=535"},
		{"deadline", p, context.DeadlineExceeded, "sender=esp-campagne err=timeout"},
		{"canceled", p, context.Canceled, "sender=esp-campagne err=canceled"},
		{"unknown text is dropped", p, errors.New("mailgun: user=s1_2 secret=hunter2"), "sender=esp-campagne err=unknown"},
	}
	for _, c := range cases {
		if got := describeSendError(c.p, c.err); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	if !regexp.MustCompile(`^sender=[A-Za-z0-9._-]+ smtp op=[a-z_]+ code=\d{3}$`).MatchString(describeSendError(p, rejectionError("smtp", "auth", 535, nil))) {
		t.Fatal("shape")
	}
}

func TestDescribeSendError_MissingKeysAreAllowlisted(t *testing.T) {
	inc := &ProfileIncompleteError{Driver: "x", Missing: []string{SubSMTPHost, "api key hunter2", strings.Repeat("a", 65)}}
	got := describeSendError(SenderProfile{Slug: "s", Provider: "x"}, inc)
	if got != "sender=s driver=x err=not_configured missing=smtp_host,invalid,invalid" {
		t.Fatalf("got %q", got)
	}
}

func TestDescribeSendError_IsCapped(t *testing.T) {
	many := make([]string, 200)
	for i := range many {
		many[i] = strings.Repeat("a", 64)
	}
	long := &ProfileIncompleteError{Driver: "x", Missing: many}
	got := describeSendError(SenderProfile{Slug: "s"}, long)
	if len(got) > 512 {
		t.Fatalf("len = %d, want ≤ 512", len(got))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'TestFailureKind|TestSafeToken|TestDescribeSendError|TestSendError' -count=1`
Expected: FAIL — `undefined: failureKind`.

- [ ] **Step 3: Write `send_error.go`**

```go
package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// SendError is the only error shape a driver may return for a transport or
// vendor failure. It carries TYPED fields — the step that failed, a numeric
// reply code, a failure kind, an HTTP status, a vendor status/code — and no
// free text. The diagnostic is rendered by Error() from those fields, every
// string through an allowlist, so the chokepoint never copies a string a
// driver produced and a driver cannot hand it a remote body even by writing
// &SendError{Status: body}. Cause is for errors.Is/As and is never persisted
// or logged. Drivers use the constructors below rather than the literal.
type SendError struct {
	Driver string // registry name
	Op     string // the step that failed — a constant the driver owns; "" when unknown
	Kind   string // local failure kind, one of failureKinds; anything else renders "io"
	Code   int    // numeric server reply code (SMTP); 0 when none
	HTTP   int    // HTTP status of a vendor response; 0 when none
	Status string // vendor envelope status — allowlisted on render
	Vendor string // vendor envelope code — allowlisted on render
	Body   string // bodyTooLarge | bodyUnparseable; any other value renders the envelope shape
	Bytes  int    // bytes read of an unparseable body
	Media  string // Content-Type of an unparseable body — allowlisted on render
	Cause  error
}

const (
	bodyTooLarge    = "too_large"
	bodyUnparseable = "unparseable"
)

// failureKinds is the closed set a local failure may be reported as.
var failureKinds = map[string]bool{"dial": true, "tls": true, "timeout": true, "canceled": true, "io": true}

func safeKind(k string) string {
	if failureKinds[k] {
		return k
	}
	return "io"
}

// Error renders the diagnostic. Shapes, all tokens allowlisted, ints formatted:
//
//	vendor envelope    http=<n> status=<tok> code=<tok>
//	oversized body     http=<n> body=too_large
//	unparseable body   http=<n> body=unparseable bytes=<n> type=<media>
//	server rejection   <driver> op=<op> code=<nnn>
//	local failure      <driver> op=<op> err=<kind>   or   <driver> err=<kind>
func (e *SendError) Error() string {
	driver := safeToken(e.Driver)
	if driver == "" {
		driver = "driver"
	}
	switch {
	case e.HTTP > 0:
		switch e.Body {
		case bodyTooLarge:
			return fmt.Sprintf("http=%d body=too_large", e.HTTP)
		case bodyUnparseable:
			return fmt.Sprintf("http=%d body=unparseable bytes=%d type=%s", e.HTTP, e.Bytes, safeMediaType(e.Media))
		default:
			return fmt.Sprintf("http=%d status=%s code=%s", e.HTTP, safeToken(e.Status), safeToken(e.Vendor))
		}
	case e.Op != "" && e.Code > 0:
		return fmt.Sprintf("%s op=%s code=%d", driver, safeToken(e.Op), e.Code)
	case e.Op != "":
		return fmt.Sprintf("%s op=%s err=%s", driver, safeToken(e.Op), safeKind(e.Kind))
	default:
		return fmt.Sprintf("%s err=%s", driver, safeKind(e.Kind))
	}
}

func (e *SendError) Unwrap() error { return e.Cause }

// transportError reports a local failure (dial, TLS, timeout, …) at step op
// of driver; op may be "" for a driver whose exchange has no steps.
func transportError(driver, op string, err error) *SendError {
	return &SendError{Driver: driver, Op: op, Kind: failureKind(err), Cause: err}
}

// rejectionError keeps only the numeric reply code of a server rejection.
func rejectionError(driver, op string, code int, err error) *SendError {
	return &SendError{Driver: driver, Op: op, Code: code, Cause: err}
}

// vendorEnvelopeError records a parsed vendor envelope by its status and code.
func vendorEnvelopeError(driver string, httpStatus int, status, code string) *SendError {
	return &SendError{Driver: driver, HTTP: httpStatus, Status: status, Vendor: code}
}

// vendorBodyError records a body that was not parsed: body is bodyTooLarge
// (bytes and media ignored) or bodyUnparseable.
func vendorBodyError(driver string, httpStatus int, body string, bytes int, media string) *SendError {
	return &SendError{Driver: driver, HTTP: httpStatus, Body: body, Bytes: bytes, Media: media}
}

const (
	maxTokenLen   = 64
	maxErrorValue = 512
)

var (
	tokenAllow     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	mediaTypeAllow = regexp.MustCompile(`^[A-Za-z0-9._/+-]+$`)
)

// safeToken admits [A-Za-z0-9._-] up to 64 chars. Anything else becomes the
// marker "invalid" — the allowlist is the protection; the cap is a backstop.
func safeToken(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > maxTokenLen || !tokenAllow.MatchString(s) {
		return "invalid"
	}
	return s
}

// safeMediaType is safeToken with '/' and '+' admitted so real media types
// survive; parameters ("; charset=") do not.
func safeMediaType(s string) string {
	if s == "" {
		return ""
	}
	if len(s) > maxTokenLen || !mediaTypeAllow.MatchString(s) {
		return "invalid"
	}
	return s
}

// safeTokens allowlists each element (a fork's driver may name its own
// requirement keys) and joins them; the count is capped so a hostile list
// cannot pad the diagnostic to the value cap.
func safeTokens(keys []string) string {
	if len(keys) > 16 {
		keys = keys[:16]
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, safeToken(k))
	}
	return strings.Join(out, ",")
}

func capString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// failureKind classifies a local or transport failure into a fixed set.
// The Go error string never leaves this function.
func failureKind(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	var (
		recHdr   tls.RecordHeaderError
		certErr  *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
	)
	if errors.As(err, &recHdr) || errors.As(err, &certErr) || errors.As(err, &unknown) || errors.As(err, &hostname) {
		return "tls"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "dial"
	}
	return "io"
}

// describeSendError renders what the chokepoint stores for a failed send.
// Every branch is built from constants; an error of unknown shape — a fork's
// driver returning fmt.Errorf with a vendor body — is recorded as "unknown",
// not persisted. The sender slug is included so the delivery log names the
// profile before PR 4 adds a dedicated field.
func describeSendError(p SenderProfile, err error) string {
	slug := p.Slug
	if slug == "" {
		slug = "-"
	}
	prefix := "sender=" + safeToken(slug)
	var (
		se  *SendError
		inc *ProfileIncompleteError
		out string
	)
	switch {
	case errors.Is(err, ErrNoSenderForCategory):
		out = prefix + " err=no_sender_for_category"
	case errors.Is(err, ErrSenderConfigUnavailable):
		out = prefix + " err=config_unavailable"
	case errors.Is(err, ErrUnknownDriver):
		out = prefix + " driver=" + safeToken(p.Provider) + " err=unknown_driver"
	case errors.As(err, &inc):
		out = prefix + " driver=" + safeToken(p.Provider) + " err=not_configured missing=" + safeTokens(inc.Missing)
	case errors.As(err, &se):
		out = prefix + " " + se.Error() // rendered from typed fields through the allowlists — never a driver's string
	case errors.Is(err, context.DeadlineExceeded):
		out = prefix + " err=timeout"
	case errors.Is(err, context.Canceled):
		out = prefix + " err=canceled"
	default:
		out = prefix + " err=unknown"
	}
	return capString(out, maxErrorValue)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'TestFailureKind|TestSafeToken|TestDescribeSendError|TestSendError' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/notification/services/send_error.go backend/internal/core/notification/services/send_error_test.go
git commit -m "feat(notification): typed, peer-free send error contract rendered at the chokepoint (ADR-0019)"
```

### Task 3: The `noop` driver

**Files:**
- Create: `backend/internal/core/notification/services/driver_noop.go`
- Test: `backend/internal/core/notification/services/driver_noop_test.go`

**Interfaces produced:** `NewNoopDriver(logger *slog.Logger) EmailDriver` (name `"noop"`, `Requires() == nil`), `truncate(s, n)`.

- [ ] **Step 1: Write the failing test**

```go
package services

import (
	"context"
	"testing"
)

func TestNoopDriver_RequiresNothingAndNeverFails(t *testing.T) {
	d := NewNoopDriver(nil)
	if d.Name() != "noop" || len(d.Requires()) != 0 {
		t.Fatalf("noop must be named noop and require nothing: %v", d.Requires())
	}
	if err := ValidateProfile(d, SenderProfile{}, RuntimeView); err != nil {
		t.Fatalf("a noop profile with nothing but a slug must validate, got %v", err)
	}
	if err := d.Send(context.Background(), SenderProfile{}, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"}); err != nil {
		t.Fatalf("noop send must never error, got %v", err)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abcdef", 3, "abc..."},
		{"", 5, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
```

Delete `TestTruncate` from `email_service_test.go` (it moves here); leave the rest of that file for Task 4.

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'TestNoopDriver|TestTruncate' -count=1`
Expected: FAIL — `undefined: NewNoopDriver`.

- [ ] **Step 3: Write `driver_noop.go`**

```go
package services

import (
	"context"
	"log/slog"
)

// noopDriver logs the rendered message instead of sending it — the dev /
// bootstrap transport every fresh install boots with.
type noopDriver struct{ logger *slog.Logger }

func NewNoopDriver(logger *slog.Logger) EmailDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &noopDriver{logger: logger}
}

func (d *noopDriver) Name() string                   { return "noop" }
func (d *noopDriver) Requires() []ProfileRequirement { return nil }

func (d *noopDriver) Send(_ context.Context, _ SenderProfile, msg EmailMessage) error {
	d.logger.Info("notification.email noop send",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
	)
	d.logger.Debug("notification.email body",
		slog.String("text", truncate(msg.BodyText, 500)),
	)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
```

Remove `truncate` from `email_service.go` (it now lives here) so the package compiles.

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'TestNoopDriver|TestTruncate' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/notification/services/driver_noop.go backend/internal/core/notification/services/driver_noop_test.go backend/internal/core/notification/services/email_service.go backend/internal/core/notification/services/email_service_test.go
git commit -m "feat(notification): noop driver behind the EmailDriver seam"
```

### Task 4: The `smtp` driver — `emailService` retires inside it

**Files:**
- Rename: `backend/internal/core/notification/services/email_service.go` → `driver_smtp.go` (`git mv`, then rewrite)
- Rename: `backend/internal/core/notification/services/email_service_test.go` → `driver_smtp_test.go` (`git mv`, then rewrite)
- Modify: `backend/internal/core/notification/services/email_driver.go` (add `CoreDrivers`)

**Interfaces produced:** `NewSMTPDriver(logger) EmailDriver` (name `"smtp"`, requires `smtp_host`, `smtp_port`, `from_address`), `CoreDrivers(logger) []EmailDriver`, `buildMIMEMessage(p SenderProfile, msg) string`, `buildMIMEMessageAt(p, msg, now time.Time) string`, `encodeQuotedPrintable`.

**Interfaces removed:** `EmailSettings`, `SettingsLoader`, `EmailSender`, `NewEmailService`, `emailService`, `isSMTPConfigured`, `ErrEmailNotConfigured`. (`notification_service.go` and `module.go` still reference `EmailSender` — the package will not compile until Task 6/7. Use `go test -run` on `driver_smtp_test.go` via `go vet`-free compile of only these files is not possible, so **Tasks 4–7 are committed together after Task 7's build passes**; each task still has its own test step run with `cd backend && go test ./internal/core/notification/services/` once Task 6 lands.)

- [ ] **Step 0: Capture the byte-identical golden from TODAY's code, before any rewrite**

`buildMIMEMessage` reads `time.Now()` twice (the `Date:` header and the boundary), so it cannot be compared across the refactor as-is. Extract the clock first — a mechanical change to the OLD code — and prove the golden against it:

In `email_service.go` rename `buildMIMEMessage(cfg EmailSettings, msg EmailMessage) string` to `buildMIMEMessageAt(cfg EmailSettings, msg EmailMessage, now time.Time) string`, replace both `time.Now()` inside it with `now`, and add:

```go
func buildMIMEMessage(cfg EmailSettings, msg EmailMessage) string {
	return buildMIMEMessageAt(cfg, msg, time.Now())
}
```

Append to `email_service_test.go`:

```go
// mimeGolden is the exact wire output of today's transport for one fixed
// message at one fixed instant. The smtp driver must reproduce it byte for
// byte — that is what "byte-identical under D6" means.
const mimeGolden = "From: Orkestra <no-reply@example.com>\r\n" +
	"To: Alice <alice@example.com>\r\n" +
	"Subject: Hello\r\n" +
	"Reply-To: support@example.com\r\n" +
	"Date: Wed, 26 Aug 2026 12:00:00 +0000\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=\"orkestra_boundary_1787745600000000000\"\r\n\r\n" +
	"--orkestra_boundary_1787745600000000000\r\n" +
	"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"Body with =3D sign\r\n" +
	"--orkestra_boundary_1787745600000000000\r\n" +
	"Content-Type: text/html; charset=\"utf-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"<p>html</p>\r\n" +
	"--orkestra_boundary_1787745600000000000--\r\n"

var mimeGoldenAt = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

func TestBuildMIMEMessage_GoldenLegacy(t *testing.T) {
	cfg := EmailSettings{FromAddress: "no-reply@example.com", FromName: "Orkestra", ReplyTo: "support@example.com"}
	msg := EmailMessage{To: "alice@example.com", ToName: "Alice", Subject: "Hello", BodyText: "Body with = sign", BodyHTML: "<p>html</p>"}
	if got := buildMIMEMessageAt(cfg, msg, mimeGoldenAt); got != mimeGolden {
		t.Fatalf("golden does not match today's output:\n%q", got)
	}
}
```

Run: `cd backend && go test ./internal/core/notification/services/ -run TestBuildMIMEMessage_GoldenLegacy -count=1` → **must PASS against the old code**. If it does not, the golden literal is wrong — fix the literal to what today's code produces (print `got`), never the other way round. Commit this alone:

```bash
git add backend/internal/core/notification/services/email_service.go backend/internal/core/notification/services/email_service_test.go
git commit -m "test(notification): pin today's MIME wire output as a golden before the driver refactor"
```

- [ ] **Step 1: `git mv` both files**

```bash
cd backend/internal/core/notification/services
git mv email_service.go driver_smtp.go
git mv email_service_test.go driver_smtp_test.go
```

- [ ] **Step 2: Write the tests (`driver_smtp_test.go`, full replacement)**

```go
package services

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEncodeQuotedPrintable_Empty(t *testing.T) {
	if got := encodeQuotedPrintable(""); got != "" {
		t.Fatalf("empty input should return empty, got %q", got)
	}
}

func TestEncodeQuotedPrintable_EscapesEquals(t *testing.T) {
	got := encodeQuotedPrintable("url?token=ABC")
	if !strings.Contains(got, "=3D") || strings.Contains(got, "token=ABC") {
		t.Fatalf("expected literal '=' escaped as =3D, got %q", got)
	}
}

func TestEncodeQuotedPrintable_PlainASCIIPassesThrough(t *testing.T) {
	if got := encodeQuotedPrintable("hello world"); got != "hello world" {
		t.Fatalf("plain ASCII should pass through unchanged, got %q", got)
	}
}

func TestEncodeQuotedPrintable_LongLineWrapping(t *testing.T) {
	if got := encodeQuotedPrintable(strings.Repeat("a", 200)); !strings.Contains(got, "=\r\n") {
		t.Fatalf("expected soft line break in long QP output, got %q", got)
	}
}

// TestSMTPDriver_Requires is the D6 regression test: an anonymous relay —
// host + port + from, no credentials — must validate exactly as
// isSMTPConfigured accepted it; missing host, port or from must not.
func TestSMTPDriver_Requires(t *testing.T) {
	d := NewSMTPDriver(nil)
	complete := SenderProfile{Provider: "smtp", SMTPHost: "mail.example.com", SMTPPort: 587, FromAddress: "no-reply@example.com"}
	cases := []struct {
		name string
		p    SenderProfile
		ok   bool
	}{
		{"anonymous relay", complete, true},
		{"username without password", SenderProfile{Provider: "smtp", SMTPHost: "h", SMTPPort: 587, FromAddress: "f", SMTPUsername: "u"}, true},
		{"missing host", SenderProfile{Provider: "smtp", SMTPPort: 587, FromAddress: "f"}, false},
		{"zero port", SenderProfile{Provider: "smtp", SMTPHost: "h", FromAddress: "f"}, false},
		{"missing from", SenderProfile{Provider: "smtp", SMTPHost: "h", SMTPPort: 587}, false},
		{"empty", SenderProfile{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateProfile(d, c.p, RuntimeView)
			if (err == nil) != c.ok {
				t.Fatalf("ValidateProfile(%+v) = %v, want ok=%v", c.p, err, c.ok)
			}
			if !c.ok && !errors.Is(err, ErrSenderNotConfigured) {
				t.Fatalf("want ErrSenderNotConfigured, got %v", err)
			}
		})
	}
	for _, r := range d.Requires() {
		if r.Secret {
			t.Fatalf("smtp must not require a secret: %+v", r)
		}
	}
}

func TestSMTPDriver_SendRefusesIncompleteProfile(t *testing.T) {
	err := NewSMTPDriver(nil).Send(context.Background(), SenderProfile{Provider: "smtp"}, EmailMessage{To: "a@example.com"})
	if !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("expected ErrSenderNotConfigured, got %v", err)
	}
}

func TestBuildMIMEMessage_TextOnly(t *testing.T) {
	p := SenderProfile{FromAddress: "no-reply@example.com", FromName: "Orkestra", ReplyTo: "support@example.com"}
	msg := EmailMessage{To: "alice@example.com", ToName: "Alice", Subject: "Hello", BodyText: "Body with = sign", Category: "auth.verify_email"}
	out := buildMIMEMessage(p, msg)
	for _, want := range []string{
		"From: Orkestra <no-reply@example.com>\r\n",
		"To: Alice <alice@example.com>\r\n",
		"Subject: Hello\r\n",
		"Reply-To: support@example.com\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=\"utf-8\"\r\n",
		"Content-Transfer-Encoding: quoted-printable\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing header/line %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "=3D") {
		t.Fatalf("expected QP-escaped body, got:\n%s", out)
	}
	if strings.Contains(out, "multipart/alternative") {
		t.Fatalf("text-only message should not declare multipart/alternative")
	}
	// The wire output ignores Category — what makes extending EmailMessage safe under D6.
	if strings.Contains(out, "auth.verify_email") {
		t.Fatalf("Category must not reach the wire: %s", out)
	}
}

// mimeGolden is the exact wire output captured from the pre-refactor
// transport in Step 0 (see the commit "pin today's MIME wire output"). The
// driver must reproduce it byte for byte, with and without Category set.
const mimeGolden = "From: Orkestra <no-reply@example.com>\r\n" +
	"To: Alice <alice@example.com>\r\n" +
	"Subject: Hello\r\n" +
	"Reply-To: support@example.com\r\n" +
	"Date: Wed, 26 Aug 2026 12:00:00 +0000\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=\"orkestra_boundary_1787745600000000000\"\r\n\r\n" +
	"--orkestra_boundary_1787745600000000000\r\n" +
	"Content-Type: text/plain; charset=\"utf-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"Body with =3D sign\r\n" +
	"--orkestra_boundary_1787745600000000000\r\n" +
	"Content-Type: text/html; charset=\"utf-8\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n\r\n" +
	"<p>html</p>\r\n" +
	"--orkestra_boundary_1787745600000000000--\r\n"

var mimeGoldenAt = time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)

// TestBuildMIMEMessage_ByteIdentical is the D6 wire test: the profile-based
// builder produces exactly the bytes the EmailSettings-based one did, and
// EmailMessage.Category changes nothing.
func TestBuildMIMEMessage_ByteIdentical(t *testing.T) {
	p := SenderProfile{FromAddress: "no-reply@example.com", FromName: "Orkestra", ReplyTo: "support@example.com"}
	msg := EmailMessage{To: "alice@example.com", ToName: "Alice", Subject: "Hello", BodyText: "Body with = sign", BodyHTML: "<p>html</p>"}
	if got := buildMIMEMessageAt(p, msg, mimeGoldenAt); got != mimeGolden {
		t.Fatalf("wire output drifted from the golden:\n%q", got)
	}
	msg.Category = "auth.verify_email"
	if got := buildMIMEMessageAt(p, msg, mimeGoldenAt); got != mimeGolden {
		t.Fatalf("Category must not change the wire output:\n%q", got)
	}
}

func TestBuildMIMEMessage_MultipartWhenHTMLPresent(t *testing.T) {
	out := buildMIMEMessage(SenderProfile{FromAddress: "no-reply@example.com"},
		EmailMessage{To: "alice@example.com", Subject: "Hello", BodyText: "plain", BodyHTML: "<p>html</p>"})
	for _, want := range []string{
		"Content-Type: multipart/alternative;",
		"Content-Type: text/plain; charset=\"utf-8\"",
		"Content-Type: text/html; charset=\"utf-8\"",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestBuildMIMEMessage_OmitsFromNameAndReplyToWhenBlank(t *testing.T) {
	out := buildMIMEMessage(SenderProfile{FromAddress: "no-reply@example.com"}, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	if !strings.Contains(out, "From: no-reply@example.com\r\n") || strings.Contains(out, "Reply-To:") {
		t.Fatalf("bare From expected and no Reply-To, got:\n%s", out)
	}
}

// ---- scripted SMTP server ----------------------------------------------

// scriptedSMTP is a one-connection SMTP server with fixed replies keyed by
// command verb. It scripts rejections — including a 535 that echoes the AUTH
// argument back — without a real MTA. greet=false accepts and never speaks.
type scriptedSMTP struct {
	ln      net.Listener
	replies map[string]string
	greet   bool
	mu      sync.Mutex
	got     []string
}

func startScriptedSMTP(t *testing.T, greet bool, replies map[string]string) *scriptedSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &scriptedSMTP{ln: ln, replies: replies, greet: greet}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *scriptedSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *scriptedSMTP) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	if !s.greet {
		time.Sleep(2 * time.Second) // longer than any test deadline; the goroutine ends with the test binary
		return
	}
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	w.WriteString("220 scripted ESMTP\r\n")
	w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.got = append(s.got, line)
		s.mu.Unlock()
		verb := line
		if i := strings.IndexByte(line, ' '); i >= 0 {
			verb = line[:i]
		}
		verb = strings.ToUpper(verb)
		reply, ok := s.replies[verb]
		if !ok {
			switch verb {
			case "EHLO", "HELO":
				reply = "250-scripted\r\n250 AUTH PLAIN\r\n"
			case "QUIT":
				reply = "221 bye\r\n"
			default:
				reply = "250 ok\r\n"
			}
		}
		w.WriteString(reply)
		w.Flush()
		if verb == "QUIT" {
			return
		}
	}
}

func scriptedProfile(port int) SenderProfile {
	return SenderProfile{Provider: "smtp", SMTPHost: "127.0.0.1", SMTPPort: port, SMTPTLSMode: "none", FromAddress: "no-reply@example.com"}
}

// TestSMTPDriver_AuthRejectionKeepsOnlyCode is the regression test for the
// credential path inherited from sendErr.Error(): a 535 line that echoes the
// base64 AUTH argument must leave "smtp op=auth code=535" and nothing else.
func TestSMTPDriver_AuthRejectionKeepsOnlyCode(t *testing.T) {
	user, pass := "s12345_67", "hunter2-secret"
	echo := base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
	srv := startScriptedSMTP(t, true, map[string]string{"AUTH": "535 5.7.8 rejected " + echo + "\r\n"})

	p := scriptedProfile(srv.port())
	p.SMTPUsername, p.SMTPPassword = user, pass
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), p, EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})

	var se *SendError
	if !errors.As(err, &se) {
		t.Fatalf("want *SendError, got %T %v", err, err)
	}
	if se.Error() != "smtp op=auth code=535" {
		t.Fatalf("diagnostic = %q", se.Error())
	}
	if s := err.Error(); strings.Contains(s, echo) || strings.Contains(s, pass) || strings.Contains(s, "rejected") {
		t.Fatalf("server text leaked: %q", s)
	}
}

func TestSMTPDriver_RcptRejectionKeepsOnlyCode(t *testing.T) {
	srv := startScriptedSMTP(t, true, map[string]string{"RCPT": "550 5.1.1 <a@example.com> user unknown\r\n"})
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(srv.port()), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "smtp op=rcpt_to code=550" {
		t.Fatalf("got %v", err)
	}
}

func TestSMTPDriver_AcceptedMessage(t *testing.T) {
	srv := startScriptedSMTP(t, true, map[string]string{"DATA": "354 go ahead\r\n", ".": "250 queued\r\n"})
	err := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(srv.port()), EmailMessage{To: "a@example.com", Subject: "s", BodyText: "b"})
	if err != nil {
		t.Fatalf("expected accepted send, got %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	joined := strings.Join(srv.got, "\n")
	if !strings.Contains(joined, "MAIL FROM:<no-reply@example.com>") || !strings.Contains(joined, "RCPT TO:<a@example.com>") {
		t.Fatalf("envelope not sent: %s", joined)
	}
	if strings.Contains(joined, "AUTH") {
		t.Fatalf("anonymous relay must not authenticate: %s", joined)
	}
}

func TestSMTPDriver_DialRefusedIsKindDial(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	sendErr := NewSMTPDriver(discardLogger()).Send(context.Background(), scriptedProfile(port), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(sendErr, &se) || se.Error() != "smtp op=dial err=dial" {
		t.Fatalf("got %v", sendErr)
	}
}

func TestSMTPDriver_HungServerIsKindTimeout(t *testing.T) {
	srv := startScriptedSMTP(t, false, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := NewSMTPDriver(discardLogger()).Send(ctx, scriptedProfile(srv.port()), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "smtp op=greeting err=timeout" {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("Go error string leaked: %q", err.Error())
	}
}
```

- [ ] **Step 3: Write `driver_smtp.go` (full replacement)**

```go
package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

const (
	smtpDialTimeout = 15 * time.Second
	// smtpIOTimeout bounds the whole exchange when the caller's context has
	// no deadline. A relay that accepts the connection and never answers
	// would otherwise hold the send goroutine forever.
	smtpIOTimeout = 30 * time.Second
)

// SMTP steps, named by us. They are the only "op" values a diagnostic carries.
const (
	smtpOpDial     = "dial"
	smtpOpGreeting = "greeting"
	smtpOpStartTLS = "starttls"
	smtpOpAuth     = "auth"
	smtpOpMailFrom = "mail_from"
	smtpOpRcptTo   = "rcpt_to"
	smtpOpData     = "data"
	smtpOpWrite    = "write"
	smtpOpClose    = "close"
)

// smtpDriver is the pre-ADR-0019 emailService, retired inside the driver
// seam: sendSMTP, the TLS-mode handling and the quoted-printable encoding
// are unchanged; only the source of the credentials moved to the profile.
type smtpDriver struct{ logger *slog.Logger }

func NewSMTPDriver(logger *slog.Logger) EmailDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &smtpDriver{logger: logger}
}

func (d *smtpDriver) Name() string { return "smtp" }

// Requires reproduces isSMTPConfigured exactly: host, port and from — and
// NOT credentials. An unauthenticated internal relay is a supported
// configuration (D3/D6); sendSMTP authenticates only when a username is set.
func (d *smtpDriver) Requires() []ProfileRequirement {
	return []ProfileRequirement{{Key: SubSMTPHost}, {Key: SubSMTPPort}, {Key: SubFromAddress}}
}

func (d *smtpDriver) Send(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return err
	}
	return d.sendSMTP(ctx, p, msg)
}

func (d *smtpDriver) sendSMTP(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	addr := fmt.Sprintf("%s:%d", p.SMTPHost, p.SMTPPort)

	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	var conn net.Conn
	var err error
	if p.SMTPTLSMode == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: p.SMTPHost})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return smtpError(smtpOpDial, err)
	}
	defer conn.Close()

	deadline := time.Now().Add(smtpIOTimeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, p.SMTPHost)
	if err != nil {
		return smtpError(smtpOpGreeting, err)
	}
	defer client.Quit()

	if p.SMTPTLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: p.SMTPHost}); err != nil {
				return smtpError(smtpOpStartTLS, err)
			}
		}
	}

	if p.SMTPUsername != "" {
		auth := smtp.PlainAuth("", p.SMTPUsername, p.SMTPPassword, p.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return smtpError(smtpOpAuth, err)
		}
	}

	if err := client.Mail(p.FromAddress); err != nil {
		return smtpError(smtpOpMailFrom, err)
	}
	if err := client.Rcpt(msg.To); err != nil {
		return smtpError(smtpOpRcptTo, err)
	}

	wc, err := client.Data()
	if err != nil {
		return smtpError(smtpOpData, err)
	}
	if _, err := wc.Write([]byte(buildMIMEMessage(p, msg))); err != nil {
		wc.Close()
		return smtpError(smtpOpWrite, err)
	}
	if err := wc.Close(); err != nil {
		return smtpError(smtpOpClose, err)
	}

	d.logger.Info("notification.email sent",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("provider", "smtp"),
	)
	return nil
}

// smtpError keeps the numeric reply code of a server rejection and drops its
// text: a hostile or merely broken MTA may echo the AUTH argument —
// base64(\0user\0pass) — into a 5xx line, and *textproto.Error.Msg IS that
// line. A local failure keeps only a kind from failureKind's fixed set.
func smtpError(op string, err error) error {
	var tp *textproto.Error
	if errors.As(err, &tp) {
		return rejectionError("smtp", op, tp.Code, err)
	}
	return transportError("smtp", op, err)
}

// buildMIMEMessage formats the message as multipart/alternative when both
// text and HTML bodies are provided, or text/plain when only text is.
func buildMIMEMessage(p SenderProfile, msg EmailMessage) string {
	return buildMIMEMessageAt(p, msg, time.Now())
}

// buildMIMEMessageAt is buildMIMEMessage with the clock injected, so the
// wire output can be pinned byte for byte in tests.
func buildMIMEMessageAt(p SenderProfile, msg EmailMessage, now time.Time) string {
	var b strings.Builder

	from := p.FromAddress
	if p.FromName != "" {
		from = fmt.Sprintf("%s <%s>", p.FromName, p.FromAddress)
	}
	to := msg.To
	if msg.ToName != "" {
		to = fmt.Sprintf("%s <%s>", msg.ToName, msg.To)
	}

	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	if p.ReplyTo != "" {
		fmt.Fprintf(&b, "Reply-To: %s\r\n", p.ReplyTo)
	}
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")

	if msg.BodyHTML != "" {
		boundary := "orkestra_boundary_" + fmt.Sprint(now.UnixNano())
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(encodeQuotedPrintable(msg.BodyText))
		b.WriteString("\r\n")

		fmt.Fprintf(&b, "--%s\r\n", boundary)
		b.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(encodeQuotedPrintable(msg.BodyHTML))
		b.WriteString("\r\n")

		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	} else {
		b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		b.WriteString(encodeQuotedPrintable(msg.BodyText))
	}

	return b.String()
}

// encodeQuotedPrintable encodes s with RFC 2045 quoted-printable so that
// '=' bytes (and any non-printable / >0x7E) become '=XX' hex sequences
// and long lines are wrapped at 76 chars with soft '=' line endings.
// Strict decoders (Stalwart among them) drop an unescaped '=' and mangle
// URLs in flight.
func encodeQuotedPrintable(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}
```

- [ ] **Step 4: Add `CoreDrivers` to `email_driver.go`**

```go
// CoreDrivers builds every driver the base ships. module.go registers them;
// tests register fakes instead.
func CoreDrivers(logger *slog.Logger) []EmailDriver {
	return []EmailDriver{NewNoopDriver(logger), NewSMTPDriver(logger)}
}
```
(add `"log/slog"` to the imports.)

- [ ] **Step 5: Continue to Task 5** — the package does not compile until `notification_service.go` is rewired in Task 6. Do not commit yet.

### Task 5: The resolver (legacy-only in PR 1)

**Files:**
- Create: `backend/internal/core/notification/services/sender_resolver.go`
- Test: `backend/internal/core/notification/services/sender_resolver_test.go`

**Files (in addition):**
- Create: `backend/internal/core/notification/services/sender_loader.go`
- Test: `backend/internal/core/notification/services/sender_loader_test.go`

**Interfaces produced:** `ResolveInput{Category, Type, TenantID}`, `SenderConfig{Profiles, Legacy, Err}`, `SenderConfigLoader func(ctx) SenderConfig`, `SenderResolver{Resolve, Default}`, `NewSenderResolver(load) SenderResolver` (consumes `ErrSenderConfigUnavailable` from Task 1); `SnapshotGetter func(ctx) (*module.ModuleConfig, error)`, `NewSnapshotLoader(get SnapshotGetter) SenderConfigLoader`, `legacyFromSnapshot(doc) (SenderProfile, error)`.

**Why a snapshot loader.** Today's settings closure issues nine `deps.GetConfig`/`GetSecret` calls per send, each a separate `FindByName`; an environment activation landing between two of them pairs one environment's host with the other's password. Per-environment configuration (D4) only holds if values **and** secrets are decoded from **one** document read — `doc.ActiveConfigValues()` and `doc.ActiveEncryptedValues()` of the same `*module.ModuleConfig`, decrypted through the exported `module.UnmarshalConfig`, never through a second `GetSecret`. PR 2 decodes the roster from that same snapshot.

- [ ] **Step 1: Write the failing test**

```go
package services

import (
	"context"
	"errors"
	"testing"
)

func fixedLoader(cfg SenderConfig) SenderConfigLoader {
	return func(context.Context) SenderConfig { return cfg }
}

// TestSenderResolver_EmptyRosterReturnsLegacy: with no routing map the
// category is not inspected at all — "" included, which is legal today
// (D6 byte-identical). The malformed-category rule belongs to routing.
func TestSenderResolver_EmptyRosterReturnsLegacy(t *testing.T) {
	legacy := LegacyProfile(SenderProfile{Provider: "smtp", SMTPHost: "h", SMTPPort: 25, FromAddress: "f"})
	r := NewSenderResolver(fixedLoader(SenderConfig{Legacy: legacy}))

	for _, cat := range []string{"auth.verify_email", "crm.campaign", "marketing", "", " padded "} {
		got, err := r.Resolve(context.Background(), ResolveInput{Category: cat, Type: "transactional", TenantID: "t-1"})
		if err != nil || got.Slug != LegacySlug || got.Provider != "smtp" {
			t.Fatalf("Resolve(%q) = %+v, %v", cat, got, err)
		}
	}
	got, err := r.Default(context.Background())
	if err != nil || got.Slug != LegacySlug {
		t.Fatalf("Default = %+v, %v", got, err)
	}
}

func TestSenderResolver_ConfigErrorFailsClosed(t *testing.T) {
	r := NewSenderResolver(fixedLoader(SenderConfig{Err: errors.New("mongo down"), Legacy: LegacyProfile(SenderProfile{})}))
	if _, err := r.Resolve(context.Background(), ResolveInput{Category: "auth.verify_email"}); !errors.Is(err, ErrSenderConfigUnavailable) {
		t.Fatalf("want ErrSenderConfigUnavailable, got %v", err)
	}
	if _, err := r.Default(context.Background()); !errors.Is(err, ErrSenderConfigUnavailable) {
		t.Fatalf("want ErrSenderConfigUnavailable, got %v", err)
	}
}
```

- [ ] **Step 2: Write `sender_resolver.go`**

```go
package services

import "context"

// ResolveInput identifies one send for routing. TenantID is reserved (D4)
// and ignored today.
type ResolveInput struct {
	Category string
	Type     string
	TenantID string
}

// SenderConfig is the resolver's view of configuration at one instant:
// the decoded roster (empty until PR 2 reads email.senders) and the profile
// synthesized from the flat legacy keys (D6). Err marks a roster that could
// not be read at all.
type SenderConfig struct {
	Profiles []SenderProfile
	Legacy   SenderProfile
	Err      error
}

// SenderConfigLoader supplies the current configuration at send time so
// admin UI changes take effect without a restart.
type SenderConfigLoader func(ctx context.Context) SenderConfig

// SenderResolver picks the profile that carries one send.
type SenderResolver interface {
	// Resolve returns the most specific matching profile, or
	// ErrNoSenderForCategory when nothing matches (fail-closed, D5).
	Resolve(ctx context.Context, in ResolveInput) (SenderProfile, error)
	// Default returns the profile declaring "*" — what IsConfigured reports on.
	Default(ctx context.Context) (SenderProfile, error)
}

type senderResolver struct{ load SenderConfigLoader }

func NewSenderResolver(load SenderConfigLoader) SenderResolver {
	return &senderResolver{load: load}
}

// PR 1: the roster is never populated, so every send resolves to the legacy
// profile. PR 2 adds pattern matching over cfg.Profiles.
func (r *senderResolver) Resolve(ctx context.Context, _ ResolveInput) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	return cfg.Legacy, nil
}

func (r *senderResolver) Default(ctx context.Context) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	return cfg.Legacy, nil
}
```

- [ ] **Step 3: Write the failing loader tests (`sender_loader_test.go`)**

```go
package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// testKeyHex is a 32-byte AES key for OAUTH_TOKEN_ENCRYPTION_KEY.
const testKeyHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

// encryptForTest produces ciphertext in the SDK's stored form
// (base64(nonce || AES-256-GCM ciphertext)). The SDK does not export its
// encryptor; if its scheme ever changes, decoding these fixtures fails
// loudly (cfg.Err), which is the right way for this test to break.
func encryptForTest(t *testing.T, plain string) string {
	t.Helper()
	key, _ := hex.DecodeString(testKeyHex)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

// legacyTestSchema is the subset of the flat schema these tests read; the
// real one is module.go's ConfigSchema (a key absent here stays zero).
func legacyTestSchema() []module.ConfigField {
	return []module.ConfigField{
		{Key: "email.provider", Type: module.FieldEnum, Options: []string{"noop", "smtp"}, Default: "noop"},
		{Key: "email.smtp.host", Type: module.FieldString},
		{Key: "email.smtp.port", Type: module.FieldInt, Default: "587"},
		{Key: "email.smtp.password", Type: module.FieldSecret},
		{Key: "email.from_address", Type: module.FieldString},
	}
}

func twoEnvDoc(t *testing.T) *module.ModuleConfig {
	t.Helper()
	return &module.ModuleConfig{
		ConfigSchema:      legacyTestSchema(),
		ActiveEnvironment: "production",
		Environments: map[string]module.EnvironmentConfig{
			"production": {
				ConfigValues:    map[string]string{"email.provider": "smtp", "email.smtp.host": "prod.relay", "email.from_address": "p@example.com"},
				EncryptedValues: map[string]string{"email.smtp.password": encryptForTest(t, "prod-secret")},
			},
			"sandbox": {
				ConfigValues:    map[string]string{"email.provider": "smtp", "email.smtp.host": "sand.relay", "email.from_address": "s@example.com"},
				EncryptedValues: map[string]string{"email.smtp.password": encryptForTest(t, "sand-secret")},
			},
		},
	}
}

// TestSnapshotLoader_ValuesAndSecretsComeFromOneEnvironment simulates an
// environment switch between two loads. Each load must be internally
// consistent — host and password from the SAME environment — and the
// getter must be called exactly once per load, so there is no second read a
// switch could land between.
func TestSnapshotLoader_ValuesAndSecretsComeFromOneEnvironment(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	doc := twoEnvDoc(t)
	calls := 0
	get := func(context.Context) (*module.ModuleConfig, error) {
		calls++
		if calls > 1 {
			doc.ActiveEnvironment = "sandbox" // the switch
		}
		return doc, nil
	}
	load := NewSnapshotLoader(get)

	first := load(context.Background())
	second := load(context.Background())
	if first.Err != nil || second.Err != nil {
		t.Fatalf("errs: %v %v", first.Err, second.Err)
	}
	if first.Legacy.SMTPHost != "prod.relay" || first.Legacy.SMTPPassword != "prod-secret" || first.Legacy.FromAddress != "p@example.com" {
		t.Fatalf("first load mixed environments: %+v", first.Legacy)
	}
	if second.Legacy.SMTPHost != "sand.relay" || second.Legacy.SMTPPassword != "sand-secret" {
		t.Fatalf("second load mixed environments: %+v", second.Legacy)
	}
	if first.Legacy.SMTPPort != 587 || first.Legacy.Slug != LegacySlug || first.Legacy.Provider != "smtp" {
		t.Fatalf("defaults/identity: %+v", first.Legacy)
	}
	if calls != 2 {
		t.Fatalf("one document read per load, got %d for 2 loads", calls)
	}
}

func TestSnapshotLoader_FailsClosed(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	boom := errors.New("mongo down")
	if cfg := NewSnapshotLoader(func(context.Context) (*module.ModuleConfig, error) { return nil, boom })(context.Background()); !errors.Is(cfg.Err, boom) {
		t.Fatalf("getter error must surface as cfg.Err, got %v", cfg.Err)
	}
	doc := twoEnvDoc(t)
	env := doc.Environments["production"]
	env.EncryptedValues["email.smtp.password"] = "not-ciphertext"
	doc.Environments["production"] = env
	if cfg := NewSnapshotLoader(func(context.Context) (*module.ModuleConfig, error) { return doc, nil })(context.Background()); cfg.Err == nil {
		t.Fatal("undecryptable secret must fail closed, not degrade to an empty password")
	}
}

func TestSnapshotLoader_NoDocumentIsNoop(t *testing.T) {
	cfg := NewSnapshotLoader(func(context.Context) (*module.ModuleConfig, error) { return nil, nil })(context.Background())
	if cfg.Err != nil || cfg.Legacy.Provider != "noop" || cfg.Legacy.Slug != LegacySlug {
		t.Fatalf("no document ⇒ noop legacy profile, got %+v %v", cfg.Legacy, cfg.Err)
	}
	if cfg := NewSnapshotLoader(nil)(context.Background()); cfg.Legacy.Provider != "noop" {
		t.Fatalf("nil getter ⇒ noop, got %+v", cfg.Legacy)
	}
}
```

- [ ] **Step 4: Write `sender_loader.go`**

```go
package services

import (
	"context"
	"fmt"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// SnapshotGetter returns the module's configuration document — ONE read.
// module.go wires it to ModuleConfigService.GetConfig.
type SnapshotGetter func(ctx context.Context) (*module.ModuleConfig, error)

// legacySettings are the flat email.* keys, decoded by the SDK against the
// document's own schema so value → env var → default resolution matches
// GetValue/GetSecret exactly — from the snapshot, not from further reads.
type legacySettings struct {
	Provider    string `module:"email.provider"`
	Host        string `module:"email.smtp.host"`
	Port        int    `module:"email.smtp.port"`
	Username    string `module:"email.smtp.username"`
	Password    string `module:"email.smtp.password"`
	FromAddress string `module:"email.from_address"`
	FromName    string `module:"email.from_name"`
	ReplyTo     string `module:"email.reply_to"`
	TLSMode     string `module:"email.smtp.tls_mode"`
}

// NewSnapshotLoader builds the SenderConfigLoader every send calls. Values
// and secrets are decoded from the active environment of ONE document, so
// an environment activation between reads can never pair one environment's
// host with the other's password (D4). A read or decode failure is reported
// as cfg.Err and the resolver fails closed on it.
func NewSnapshotLoader(get SnapshotGetter) SenderConfigLoader {
	return func(ctx context.Context) SenderConfig {
		if get == nil {
			return SenderConfig{Legacy: LegacyProfile(SenderProfile{})}
		}
		doc, err := get(ctx)
		if err != nil {
			return SenderConfig{Err: fmt.Errorf("notification: read config snapshot: %w", err)}
		}
		if doc == nil {
			return SenderConfig{Legacy: LegacyProfile(SenderProfile{})}
		}
		legacy, err := legacyFromSnapshot(doc)
		if err != nil {
			return SenderConfig{Err: err}
		}
		return SenderConfig{Legacy: legacy}
	}
}

func legacyFromSnapshot(doc *module.ModuleConfig) (SenderProfile, error) {
	var ls legacySettings
	if err := module.UnmarshalConfig(doc.ConfigSchema, doc.ActiveConfigValues(), doc.ActiveEncryptedValues(), &ls); err != nil {
		return SenderProfile{}, fmt.Errorf("notification: decode legacy email settings: %w", err)
	}
	if ls.Port == 0 {
		ls.Port = 587
	}
	return LegacyProfile(SenderProfile{
		Provider: ls.Provider, SMTPHost: ls.Host, SMTPPort: ls.Port, SMTPUsername: ls.Username, SMTPPassword: ls.Password,
		FromAddress: ls.FromAddress, FromName: ls.FromName, ReplyTo: ls.ReplyTo, SMTPTLSMode: ls.TLSMode,
	}), nil
}
```

- [ ] **Step 5: Continue to Task 6** (package still does not compile; tests run after Task 6).

### Task 6: Route `dispatchEmail` through resolver → validate → driver

**Files:**
- Modify: `backend/internal/core/notification/services/notification_service.go`
- Modify: `backend/internal/core/notification/services/notification_service_test.go`

**Interfaces produced:** `NewNotificationService(logRepo, tmplService, prefService, unsubService, resolver SenderResolver, drivers *DriverRegistry, logger, opts)`, `ErrSendFailed`, `*DispatchError{Reason}` (the **only** error type `dispatchEmail`/`SendTest` return: `Error()` is the sanitized reason, `Is` matches a trusted sentinel only, the raw driver error is never wrapped), `(*NotificationService).SendTest(ctx, TestSendInput) (TestSendResult, error)`, `TestSendInput{To, Subject, BodyText}`, `TestSendResult{Provider, SenderSlug, Diagnostic}`, `(*NotificationService).Drivers() *DriverRegistry`, `(*NotificationService).Resolver() SenderResolver`.
**Interfaces removed:** `(*NotificationService).EmailSender()`.

**Why the chokepoint drops the raw error instead of wrapping it.** `auth` logs `err.Error()` of a failed send (`password_auth_service.go:1709` and siblings). Returning a fork driver's `fmt.Errorf("vendor: %s", body)` up the chain would put a remote string into the backend log even though the delivery log is clean. So `dispatchEmail` and `SendTest` return a `*DispatchError` whose text is the same bounded reason that was stored and whose `Is` answers only for the five trusted sentinels plus `ErrSendFailed`. `errors.Is(err, boom)` for a driver's own error is **false by design**.

- [ ] **Step 1: Replace the fakes and the kit in `notification_service_test.go`**

Replace the `fakeSender` type (lines ~163–172) with:

```go
type fakeDriver struct {
	name     string
	requires []ProfileRequirement
	sendErr  error
	sent     []EmailMessage
	profiles []SenderProfile // the profile handed to each Send
}

func (f *fakeDriver) Name() string                   { return f.name }
func (f *fakeDriver) Requires() []ProfileRequirement { return f.requires }
func (f *fakeDriver) Send(_ context.Context, p SenderProfile, msg EmailMessage) error {
	f.sent = append(f.sent, msg)
	f.profiles = append(f.profiles, p)
	return f.sendErr
}

type fakeResolver struct {
	profile SenderProfile
	err     error
	inputs  []ResolveInput
}

func (f *fakeResolver) Resolve(_ context.Context, in ResolveInput) (SenderProfile, error) {
	f.inputs = append(f.inputs, in)
	return f.profile, f.err
}
func (f *fakeResolver) Default(context.Context) (SenderProfile, error) { return f.profile, f.err }
```

Replace the `kit` struct and `newKit`:

```go
type kit struct {
	logRepo  *fakeNotifRepo
	tmpl     *fakeTemplateService
	pref     *fakePrefService
	unsub    *fakeUnsubService
	driver   *fakeDriver
	resolver *fakeResolver
	drivers  *DriverRegistry
	svc      *NotificationService
}

func newKit(opts Options) *kit {
	k := &kit{
		logRepo:  newFakeNotifRepo(),
		tmpl:     &fakeTemplateService{},
		pref:     &fakePrefService{can: true},
		unsub:    &fakeUnsubService{token: "raw-token"},
		driver:   &fakeDriver{name: "noop"},
		resolver: &fakeResolver{profile: SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}}},
	}
	k.rewire(opts)
	return k
}

// rewire rebuilds the registry and service after a test changed the fake
// driver's name or the resolver's profile.
func (k *kit) rewire(opts Options) {
	k.drivers = NewDriverRegistry(k.driver)
	k.svc = NewNotificationService(k.logRepo, k.tmpl, k.pref, k.unsub, k.resolver, k.drivers, discardLogger(), opts)
}
```

Then apply these mechanical edits (grep `k.email` — 15 sites):

| Old | New |
|---|---|
| `k.email.configured = false` (IsConfigured test) | `k.resolver.err = ErrNoSenderForCategory` |
| `NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{}, nil, discardLogger(), Options{})` | `NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{}, nil, nil, discardLogger(), Options{})` |
| every `k.email.sent` | `k.driver.sent` |
| `k.email.sendErr = errors.New("smtp boom")` | `k.driver.sendErr = errors.New("smtp boom")` |
| `k.email.provider = "smtp"` | `k.driver.name = "smtp"; k.resolver.profile.Provider = "smtp"; k.rewire(Options{})` |
| `if k.svc.EmailSender() != k.email {` … | `if k.svc.Drivers() != k.drivers {` with message `"Drivers() accessor mismatch"` |

In `TestNotificationService_Send_TransportFailureLogsAndReturnsError` replace the three assertions on the error text:

```go
	boom := errors.New("smtp boom")
	k.driver.sendErr = boom
	// ...
	if !errors.Is(err, ErrSendFailed) || errors.Is(err, boom) {
		t.Fatalf("the caller gets ErrSendFailed and never the raw driver error, got %v", err)
	}
	if err.Error() != "sender=default err=unknown" {
		t.Fatalf("err.Error() must be the bounded reason (auth logs it), got %q", err.Error())
	}
	if res == nil || res.Status != models.StatusFailed {
		t.Fatalf("expected failed result, got %+v", res)
	}
	// An error of unknown shape is never persisted: only its kind is.
	if res.Error != "sender=default err=unknown" {
		t.Fatalf("result.Error = %q", res.Error)
	}
	if len(k.logRepo.created) != 1 || k.logRepo.created[0].Status != models.StatusFailed || k.logRepo.created[0].Error != "sender=default err=unknown" {
		t.Fatalf("expected one failed log doc with the bounded reason, got %+v", k.logRepo.created)
	}
```

- [ ] **Step 2: Add the new tests to `notification_service_test.go`**

```go
func TestNotificationService_IsConfigured_DefaultProfileMustBeUsable(t *testing.T) {
	k := newKit(Options{})
	k.driver.requires = []ProfileRequirement{{Key: SubSMTPHost}}
	if k.svc.IsConfigured(context.Background()) {
		t.Fatal("a default profile missing a required field must not report configured")
	}
	k.resolver.profile.SMTPHost = "h"
	if !k.svc.IsConfigured(context.Background()) {
		t.Fatal("a complete default profile must report configured")
	}
	k.resolver.profile.Provider = "ses" // no such driver registered
	if k.svc.IsConfigured(context.Background()) {
		t.Fatal("an unregistered driver must not report configured")
	}
}

// (test file imports: add "fmt" and "github.com/orkestra/backend/pkg/sdk/ctxauth".)

func sendOne(t *testing.T, k *kit) (*iface.NotificationResult, error) {
	t.Helper()
	return k.svc.Send(context.Background(), iface.NotificationRequest{
		Type:       models.TypeTransactional,
		Category:   "crm.campaign",
		Recipients: []iface.Recipient{{Address: "a@example.com"}},
		Subject:    "S",
		Body:       "B",
	})
}

func TestNotificationService_Dispatch_FailClosedPaths(t *testing.T) {
	cases := []struct {
		name      string
		arrange   func(k *kit)
		wantErr   error
		wantError string
		wantProv  string
	}{
		{"no sender for category", func(k *kit) { k.resolver.err = ErrNoSenderForCategory },
			ErrNoSenderForCategory, "sender=- err=no_sender_for_category", ""},
		{"config unavailable", func(k *kit) { k.resolver.err = ErrSenderConfigUnavailable },
			ErrSenderConfigUnavailable, "sender=- err=config_unavailable", ""},
		{"unknown driver", func(k *kit) { k.resolver.profile.Provider = "ses" },
			ErrUnknownDriver, "sender=default driver=ses err=unknown_driver", "ses"},
		{"incomplete profile", func(k *kit) { k.driver.requires = []ProfileRequirement{{Key: SubSMTPHost}, {Key: SubFromAddress}} },
			ErrSenderNotConfigured, "sender=default driver=noop err=not_configured missing=smtp_host,from_address", "noop"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := newKit(Options{})
			c.arrange(k)
			res, err := sendOne(t, k)
			if !errors.Is(err, c.wantErr) || !errors.Is(err, ErrSendFailed) {
				t.Fatalf("err = %v, want %v and the ErrSendFailed umbrella", err, c.wantErr)
			}
			if res == nil || res.Status != models.StatusFailed || res.Error != c.wantError || res.Provider != c.wantProv {
				t.Fatalf("res = %+v", res)
			}
			if len(k.driver.sent) != 0 {
				t.Fatal("a fail-closed path must never reach the driver")
			}
			if len(k.logRepo.created) != 1 || k.logRepo.created[0].Error != c.wantError || k.logRepo.created[0].Status != models.StatusFailed {
				t.Fatalf("every fail-closed path writes a failed log row naming the reason: %+v", k.logRepo.created)
			}
		})
	}
}

func TestNotificationService_Dispatch_CarriesCategoryAndResolveInput(t *testing.T) {
	k := newKit(Options{})
	if _, err := sendOne(t, k); err != nil {
		t.Fatal(err)
	}
	if len(k.driver.sent) != 1 || k.driver.sent[0].Category != "crm.campaign" {
		t.Fatalf("Category must ride on EmailMessage: %+v", k.driver.sent)
	}
	if len(k.resolver.inputs) != 1 || k.resolver.inputs[0].Category != "crm.campaign" || k.resolver.inputs[0].Type != models.TypeTransactional {
		t.Fatalf("resolver input = %+v", k.resolver.inputs)
	}
	if k.resolver.inputs[0].TenantID != "" {
		t.Fatalf("no tenant in ctx ⇒ empty TenantID, got %q", k.resolver.inputs[0].TenantID)
	}

	// D4: the tenant on the request context reaches the resolver unchanged.
	ctx := context.WithValue(context.Background(), ctxauth.KeyTenantID, "t-42")
	if _, err := k.svc.Send(ctx, iface.NotificationRequest{Type: models.TypeTransactional, Category: "crm.campaign",
		Recipients: []iface.Recipient{{Address: "b@example.com"}}, Subject: "S", Body: "B"}); err != nil {
		t.Fatal(err)
	}
	if k.resolver.inputs[1].TenantID != "t-42" {
		t.Fatalf("TenantID must be propagated from ctx, got %q", k.resolver.inputs[1].TenantID)
	}
	if k.driver.profiles[0].Slug != "default" {
		t.Fatalf("driver must receive the resolved profile, got %+v", k.driver.profiles[0])
	}
	if k.logRepo.created[0].Provider != "noop" {
		t.Fatalf("log row provider = %q", k.logRepo.created[0].Provider)
	}
}

// TestNotificationService_Dispatch_HostileDriverErrorNeverEscapes is the
// end-to-end containment test: a driver (a fork's, say) that returns a raw
// error carrying a vendor body must leave no trace of it in the stored row,
// in the result, or in the error a caller receives and logs.
func TestNotificationService_Dispatch_HostileDriverErrorNeverEscapes(t *testing.T) {
	const secret = "s3cr=t hunter2"
	k := newKit(Options{})
	k.driver.sendErr = fmt.Errorf("vendor response: 401 user=s12345_67 secret=%s <html>", secret)
	res, err := sendOne(t, k)
	for what, text := range map[string]string{
		"stored row":     k.logRepo.created[0].Error,
		"result.Error":   res.Error,
		"returned error": err.Error(),
	} {
		if strings.Contains(text, secret) || strings.Contains(text, "vendor response") || strings.Contains(text, "<html>") {
			t.Fatalf("%s leaked the driver's text: %q", what, text)
		}
		if text != "sender=default err=unknown" {
			t.Fatalf("%s = %q", what, text)
		}
	}
	if !errors.Is(err, ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	var de *DispatchError
	if !errors.As(err, &de) {
		t.Fatalf("the chokepoint returns only *DispatchError, got %T", err)
	}
}

func TestNotificationService_Dispatch_DriverDiagnosticIsStored(t *testing.T) {
	k := newKit(Options{})
	k.driver.sendErr = rejectionError("smtp", smtpOpAuth, 535, errors.New("535 AHVzZXIAcGFzcw=="))
	res, _ := sendOne(t, k)
	if res.Error != "sender=default smtp op=auth code=535" {
		t.Fatalf("res.Error = %q", res.Error)
	}
}

func TestNotificationService_SendTest(t *testing.T) {
	k := newKit(Options{})
	res, err := k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com", Subject: "T", BodyText: "B"})
	if err != nil || res.Provider != "noop" || res.SenderSlug != "default" {
		t.Fatalf("SendTest = %+v, %v", res, err)
	}
	if len(k.driver.sent) != 1 || k.driver.sent[0].To != "a@example.com" || k.driver.sent[0].Category != "" {
		t.Fatalf("test send not delivered to the driver: %+v", k.driver.sent)
	}
	if len(k.logRepo.created) != 0 {
		t.Fatal("a test send must not write a delivery-log row (unchanged from today)")
	}

	k.driver.sendErr = errors.New("boom secret=hunter2")
	res, err = k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com"})
	if err == nil || res.Diagnostic != "sender=default err=unknown" || res.Provider != "noop" {
		t.Fatalf("SendTest failure = %+v, %v", res, err)
	}
	if err.Error() != res.Diagnostic || !errors.Is(err, ErrSendFailed) {
		t.Fatalf("SendTest must return the sanitized DispatchError, got %v", err)
	}
}
```

- [ ] **Step 3: Rewrite the service**

In `notification_service.go` (add `"github.com/orkestra/backend/pkg/sdk/ctxauth"` to the imports):

Struct + constructor:

```go
type NotificationService struct {
	logRepo      repository.NotificationRepository
	tmplService  TemplateService
	prefService  PreferenceService
	unsubService UnsubscribeService
	resolver     SenderResolver
	drivers      *DriverRegistry
	logger       *slog.Logger
	opts         Options
}

func NewNotificationService(
	logRepo repository.NotificationRepository,
	tmplService TemplateService,
	prefService PreferenceService,
	unsubService UnsubscribeService,
	resolver SenderResolver,
	drivers *DriverRegistry,
	logger *slog.Logger,
	opts Options,
) *NotificationService {
	if opts.DefaultLocale == "" {
		opts.DefaultLocale = "en"
	}
	if opts.IdempotencyTTL == 0 {
		opts.IdempotencyTTL = 1 * time.Hour
	}
	return &NotificationService{
		logRepo:      logRepo,
		tmplService:  tmplService,
		prefService:  prefService,
		unsubService: unsubService,
		resolver:     resolver,
		drivers:      drivers,
		logger:       logger,
		opts:         opts,
	}
}
```

`IsConfigured` + the shared driver lookup:

```go
// IsConfigured keeps its pre-ADR-0019 meaning: the default ("*") profile
// resolves and its driver accepts it. It is deliberately coarse — a caller
// about to send should ask IsConfiguredFor (PR 2, D7).
func (s *NotificationService) IsConfigured(ctx context.Context) bool {
	if s.resolver == nil || s.drivers == nil {
		return false
	}
	p, err := s.resolver.Default(ctx)
	if err != nil {
		return false
	}
	_, err = s.usableDriver(p)
	return err == nil
}

// usableDriver is the second and third fail-closed step: the profile's
// driver must be registered and must accept the profile with secrets in view.
func (s *NotificationService) usableDriver(p SenderProfile) (EmailDriver, error) {
	d, ok := s.drivers.Get(p.Provider)
	if !ok {
		return nil, ErrUnknownDriver
	}
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return nil, err
	}
	return d, nil
}
```

Replace the body of `dispatchEmail` from the comment `// Dispatch via the email sender.` to the end of the function with:

```go
	// Resolve → validate → send. Every failure before the driver is
	// fail-closed (D5) and still writes a failed log row, so the delivery
	// log answers which profile failed and why.
	// TenantID rides to the resolver (D4) so per-tenant routing later needs
	// no change at this chokepoint; the resolver ignores it today.
	tenantID, _ := ctxauth.GetTenantID(ctx)
	profile, err := s.resolver.Resolve(ctx, ResolveInput{Category: in.Category, Type: in.Type, TenantID: tenantID})
	if err != nil {
		return s.failSend(ctx, logDoc, profile, err)
	}
	driver, err := s.usableDriver(profile)
	if err != nil {
		return s.failSend(ctx, logDoc, profile, err)
	}

	sendErr := driver.Send(ctx, profile, EmailMessage{
		To:       in.Recipient.Address,
		ToName:   in.Recipient.Name,
		Subject:  in.Subject,
		BodyText: in.BodyText,
		BodyHTML: in.BodyHTML,
		Category: in.Category,
	})
	if sendErr != nil {
		return s.failSend(ctx, logDoc, profile, sendErr)
	}

	now := time.Now()
	logDoc.Status = models.StatusSent
	logDoc.Provider = profile.Provider
	logDoc.SentAt = &now
	_ = s.logRepo.Create(ctx, logDoc)

	return &iface.NotificationResult{
		ID:       logDoc.UUID,
		Status:   logDoc.Status,
		Provider: profile.Provider,
	}, nil
}

// ErrSendFailed: the driver refused or could not deliver. The reason is in
// the bounded diagnostic; the driver's own error is never in the chain.
var ErrSendFailed = errors.New("notification: send failed")

// trustedSentinels are the only errors a caller may test with errors.Is.
var trustedSentinels = []error{ErrNoSenderForCategory, ErrSenderConfigUnavailable, ErrSenderNotFound, ErrUnknownDriver, ErrSenderNotConfigured}

// DispatchError is the only error dispatchEmail and SendTest return. Error()
// is the sanitized reason (the same string the delivery log stores). Is
// answers true for ErrSendFailed on EVERY dispatch failure, and for the one
// trusted sentinel that names the cause (ErrNoSenderForCategory, …) when
// there is one — never for the raw driver error, which is dropped rather
// than wrapped: auth logs err.Error() of a failed send, so a fork driver's
// fmt.Errorf("vendor: %s", body) must not be reachable there.
type DispatchError struct {
	Reason   string
	sentinel error
}

func (e *DispatchError) Error() string { return e.Reason }

func (e *DispatchError) Is(target error) bool { return target == ErrSendFailed || e.sentinel == target }

func newDispatchError(profile SenderProfile, err error) *DispatchError {
	sentinel := ErrSendFailed
	for _, s := range trustedSentinels {
		if errors.Is(err, s) {
			sentinel = s
			break
		}
	}
	return &DispatchError{Reason: describeSendError(profile, err), sentinel: sentinel}
}

// failSend records a failed attempt with the bounded reason and returns the
// matching DispatchError. Nothing the driver wrote leaves this function.
func (s *NotificationService) failSend(ctx context.Context, logDoc *models.NotificationDoc, profile SenderProfile, err error) (*iface.NotificationResult, error) {
	de := newDispatchError(profile, err)
	logDoc.Status = models.StatusFailed
	logDoc.Provider = profile.Provider
	logDoc.Error = de.Reason
	_ = s.logRepo.Create(ctx, logDoc)
	s.logger.Warn("notification: send failed",
		slog.String("category", logDoc.Category),
		slog.String("reason", de.Reason),
	)
	return &iface.NotificationResult{
		ID:       logDoc.UUID,
		Status:   logDoc.Status,
		Provider: profile.Provider,
		Error:    de.Reason,
	}, de
}
```

Replace the `EmailSender()` accessor with:

```go
// Drivers exposes the driver registry (tests, admin diagnostics).
func (s *NotificationService) Drivers() *DriverRegistry { return s.drivers }

// Resolver exposes the sender resolver.
func (s *NotificationService) Resolver() SenderResolver { return s.resolver }

// TestSendInput is one operator-initiated test message.
type TestSendInput struct {
	To       string
	Subject  string
	BodyText string
}

// TestSendResult reports which profile carried (or refused) a test send.
// Diagnostic is the bounded reason on failure — safe to show an operator.
type TestSendResult struct {
	Provider   string
	SenderSlug string
	Diagnostic string
}

// SendTest sends through the default profile, bypassing preferences,
// idempotency and the delivery log exactly as the /test endpoint always has.
func (s *NotificationService) SendTest(ctx context.Context, in TestSendInput) (TestSendResult, error) {
	profile, err := s.resolver.Default(ctx)
	if err != nil {
		de := newDispatchError(profile, err)
		return TestSendResult{Diagnostic: de.Reason}, de
	}
	res := TestSendResult{Provider: profile.Provider, SenderSlug: profile.Slug}
	driver, err := s.usableDriver(profile)
	if err != nil {
		de := newDispatchError(profile, err)
		res.Diagnostic = de.Reason
		return res, de
	}
	if err := driver.Send(ctx, profile, EmailMessage{To: in.To, Subject: in.Subject, BodyText: in.BodyText}); err != nil {
		de := newDispatchError(profile, err)
		res.Diagnostic = de.Reason
		return res, de
	}
	return res, nil
}
```

- [ ] **Step 4: Run the package tests**

Run: `cd backend && go test ./internal/core/notification/services/ -count=1`
Expected: FAIL only in `../notification` (module.go) — the `services` package itself passes, including the scripted-SMTP tests from Task 4 and the resolver tests from Task 5. If any `services` test fails, fix before continuing.

### Task 7: Wire the module and the test endpoint

**Files:**
- Modify: `backend/internal/core/notification/module.go` (`Init`, lines ~174–200)
- Modify: `backend/internal/core/notification/handlers/notification_handler.go` (`SendTestEmail`)
- Modify: `backend/internal/core/notification/CLAUDE.md`

- [ ] **Step 1: Rewire `Init`**

Replace the block from `// Settings loader:` through `emailSender := services.NewEmailService(loader, deps.Logger)` with:

```go
	// One document read per send: values and secrets of the active
	// environment come from the same snapshot (D4), and admin UI changes
	// still propagate without a restart. Until PR 2 reads the email.senders
	// roster the legacy profile is the only one (ADR-0019 D6).
	snapshot := func(ctx context.Context) (*module.ModuleConfig, error) {
		if deps.ConfigService == nil {
			return nil, nil
		}
		return deps.ConfigService.GetConfig(ctx, "notification")
	}
	loader := services.NewSnapshotLoader(snapshot)

	m.drivers = services.NewDriverRegistry(services.CoreDrivers(deps.Logger)...)
	resolver := services.NewSenderResolver(loader)
```

Add `drivers *services.DriverRegistry` to the `NotificationModule` struct, pass `resolver, m.drivers,` where `emailSender,` was passed to `NewNotificationService`, and drop the now-unused `"strconv"` import.

- [ ] **Step 2: Rewrite `SendTestEmail` in the handler**

```go
func (h *NotificationHandler) SendTestEmail(ctx context.Context, req *testEmailRequest) (*testEmailResponse, error) {
	if req.Body.To == "" {
		return nil, huma.Error400BadRequest("recipient required", nil)
	}
	subject := req.Body.Subject
	if subject == "" {
		subject = "Orkestra test email"
	}
	body := req.Body.BodyText
	if body == "" {
		body = "This is a test email sent from the Orkestra notification module at " + time.Now().Format(time.RFC3339)
	}
	res, err := h.svc.SendTest(ctx, services.TestSendInput{To: req.Body.To, Subject: subject, BodyText: body})
	resp := &testEmailResponse{}
	resp.Body.Provider = res.Provider
	if err != nil {
		resp.Body.Success = false
		resp.Body.Message = res.Diagnostic
		return resp, huma.Error500InternalServerError("send failed", err)
	}
	resp.Body.Success = true
	resp.Body.Message = "Test email dispatched"
	return resp, nil
}
```

(The status codes stay as today; PR 2 introduces the typed codes together with the `sender` field, which is the wire change.)

- [ ] **Step 2b: Handler containment test** — create `backend/internal/core/notification/handlers/notification_handler_test.go`:

```go
package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/orkestra/backend/internal/core/notification/services"
)

const hostileSecret = "s3cr=t hunter2"

// hostileDriver is what a careless fork driver looks like: it returns the
// vendor's response verbatim.
type hostileDriver struct{}

func (hostileDriver) Name() string                            { return "hostile" }
func (hostileDriver) Requires() []services.ProfileRequirement { return nil }
func (hostileDriver) Send(context.Context, services.SenderProfile, services.EmailMessage) error {
	return fmt.Errorf("vendor response: 401 user=s12345_67 secret=%s <html>", hostileSecret)
}

func hostileService() *services.NotificationService {
	loader := func(context.Context) services.SenderConfig {
		return services.SenderConfig{Legacy: services.LegacyProfile(services.SenderProfile{Provider: "hostile"})}
	}
	return services.NewNotificationService(nil, nil, nil, nil,
		services.NewSenderResolver(loader), services.NewDriverRegistry(hostileDriver{}), nil, services.Options{})
}

// TestSendTestEmail_HostileDriverTextNeverReachesTheResponse: the HTTP
// detail, and every message huma attaches from the error chain, carry only
// the bounded reason.
func TestSendTestEmail_HostileDriverTextNeverReachesTheResponse(t *testing.T) {
	h := NewNotificationHandler(hostileService())
	req := &testEmailRequest{}
	req.Body.To = "a@example.com"
	_, err := h.SendTestEmail(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error")
	}
	texts := []string{err.Error()}
	var em *huma.ErrorModel
	if errors.As(err, &em) {
		texts = append(texts, em.Detail)
		for _, d := range em.Errors {
			texts = append(texts, d.Message)
		}
	}
	for _, text := range texts {
		if strings.Contains(text, hostileSecret) || strings.Contains(text, "<html>") || strings.Contains(text, "vendor response") {
			t.Fatalf("driver text reached the response: %q", text)
		}
	}
}
```

(`NewNotificationService` with nil collaborators is fine for `SendTest`, which touches only the resolver and the registry; `discardLogger` is not exported, so pass `nil` — the service must tolerate a nil logger: in `NewNotificationService` add `if logger == nil { logger = slog.Default() }`.)

- [ ] **Step 3: Build and run every notification test**

Run: `cd backend && go build ./... && go test ./internal/core/notification/... ./cmd/server/ -count=1`
Expected: PASS. `TestConfigDeclarationsAreValid` and the notification `config_groups_test.go` are unaffected (no schema change in PR 1).

- [ ] **Step 4: Update `backend/internal/core/notification/CLAUDE.md`**

In the **What it owns** table replace the `SMTP transport` row with:

```
| Sender profiles + resolver | `services/sender_profile.go`, `services/sender_resolver.go` |
| Driver seam + registry (`noop`, `smtp`) | `services/email_driver.go`, `services/driver_noop.go`, `services/driver_smtp.go` |
| Send error contract     | `services/send_error.go`                   |
```

Add a section after **Settings (loaded lazily per send)**:

```markdown
## Sender profiles and drivers (ADR-0019)

`dispatchEmail` no longer talks to a single transport. Per send it runs
**resolve → validate → send**:

1. `SenderResolver.Resolve({Category, Type, TenantID})` picks a `SenderProfile`
   (transport **and** identity). Until some profile declares a pattern — every
   install today — the resolver returns the profile **synthesized from the flat
   `email.*` keys** (`slug=_legacy`, pattern `*`) without inspecting the
   category, so behaviour is byte-identical to before. `TenantID` is read from
   the request context and passed through; the resolver ignores it (D4).
2. `DriverRegistry.Get(profile.Provider)` → `EmailDriver`; `ValidateProfile(driver,
   profile, RuntimeView)` checks the driver's `Requires()`.
3. `driver.Send(ctx, profile, msg)`; `msg.Category` carries the routing category.

Every failure is **fail-closed** and still writes a `failed` log row whose `error`
names the profile and the reason (`sender=default driver=smtp err=not_configured
missing=smtp_host`). Nothing is silently rerouted.

**Driver requirements** (`Requires()`): `noop` nothing; `smtp` `smtp_host`,
`smtp_port`, `from_address` and **never credentials** — an anonymous relay is a
supported configuration; `sendSMTP` authenticates only when a username is set.

**Error contract.** `NotificationDoc.Error` is served to operators and rides the
GDPR export, so **no string produced by a remote peer is ever persisted or
logged**. Drivers return `*SendError`; it has no free-text field, and the diagnostic is rendered from its typed fields with
allowlisted tokens (`[A-Za-z0-9._-]`, ≤64 chars; ≤512 overall). An SMTP
rejection keeps only `smtp op=<step> code=<nnn>` — the server's text is dropped
because a broken MTA can echo the `AUTH` argument (`base64(\0user\0pass)`) into
its 5xx line. A local failure keeps a kind from a fixed set
(`dial|tls|timeout|canceled|io`). An error of unknown shape (a fork's driver
returning `fmt.Errorf` with a vendor body) is recorded as `err=unknown`.
The SMTP driver bounds the exchange with the context deadline or 30 s.

`IsConfigured(ctx)` means exactly what it did: the default (`*`) profile resolves
and its driver accepts it.
```

Update the **Admin** endpoint line: `POST /v1/notifications/test` — "send a test email through the default sender profile; bypasses preferences, idempotency and the delivery log".

- [ ] **Step 5: Commit Tasks 4–7 together**

```bash
cd backend && gofmt -l ./internal/core/notification && go vet ./internal/core/notification/...
git add backend/internal/core/notification
git commit -m "refactor(notification): retire emailService inside the smtp driver; route dispatchEmail through resolver → validate → driver (ADR-0019 PR 1)"
```

### Task 8: Compatibility test and the CI gate

**Files:**
- Create: `backend/internal/core/notification/legacy_compat_test.go`

- [ ] **Step 1: Write the compatibility test (module-level, no infrastructure)**

```go
package notification

import (
	"context"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/services"
)

// TestLegacyKeysAloneResolveToTheSynthesizedProfile pins ADR-0019 D6: with no
// routing map, the flat keys become one profile with LegacySlug routing "*",
// and its provider/host/port/from are exactly what the flat keys said.
func TestLegacyKeysAloneResolveToTheSynthesizedProfile(t *testing.T) {
	loader := func(context.Context) services.SenderConfig {
		return services.SenderConfig{Legacy: services.LegacyProfile(services.SenderProfile{
			Provider: "smtp", SMTPHost: "relay.internal", SMTPPort: 25, SMTPTLSMode: "none", FromAddress: "no-reply@example.com",
		})}
	}
	r := services.NewSenderResolver(loader)
	drivers := services.NewDriverRegistry(services.CoreDrivers(nil)...)

	for _, cat := range []string{"auth.verify_email", "auth.reset_password", "crm.campaign", "marketing"} {
		p, err := r.Resolve(context.Background(), services.ResolveInput{Category: cat})
		if err != nil || p.Slug != services.LegacySlug || p.Provider != "smtp" || p.SMTPHost != "relay.internal" {
			t.Fatalf("Resolve(%q) = %+v, %v", cat, p, err)
		}
		d, ok := drivers.Get(p.Provider)
		if !ok {
			t.Fatalf("smtp driver not registered")
		}
		// Anonymous relay: no credentials, and it validates — as isSMTPConfigured did.
		if err := services.ValidateProfile(d, p, services.RuntimeView); err != nil {
			t.Fatalf("anonymous relay must validate: %v", err)
		}
	}
}
```

- [ ] **Step 2: Run the gate**

Run: `make ci-backend`
Expected: all green. `backend-openapi-check`: **no diff expected** (PR 1 changes no wire shape). If a diff appears, stop — something moved that should not have.

- [ ] **Step 3: Commit and open PR 1**

```bash
git add backend/internal/core/notification/legacy_compat_test.go
git commit -m "test(notification): pin legacy flat-key compatibility under the driver seam"
git push -u origin feat/adr-0019-pr1-driver-seam
gh pr create --base dev --title "feat(notification): driver seam + sender profile resolver (ADR-0019 PR 1)" --body "Implements PR 1 of docs/superpowers/plans/2026-08-26-notification-multi-sender.md. No routing change: no roster yet ⇒ synthesized legacy profile; wire output pinned byte-for-byte. Two declared behaviour changes: (1) send errors are bounded diagnostics — stored, returned and logged — never server/vendor text; (2) the SMTP exchange has a deadline (ctx or 30s) instead of hanging forever."
```

---

# PR 2 — Sender profiles, validation, category-aware pre-flight

Branch: after PR 1 merges, `git checkout dev && git pull && git checkout -b feat/adr-0019-pr2-sender-profiles`.

### Task 1: Error codes

**Files:**
- Modify: `backend/internal/shared/errcode/codes.go`
- Modify: `backend/internal/shared/errcode/codes_test.go` (`goldenCodes`)

- [ ] **Step 1: Add golden rows first (the snapshot test fails until the consts exist)**

In `goldenCodes` add:

```go
	"NotificationSenderPatternInvalid":   "notification.sender_pattern_invalid",
	"NotificationSenderNoDefault":        "notification.sender_no_default",
	"NotificationSenderDuplicateDefault": "notification.sender_duplicate_default",
	"NotificationSenderPatternConflict":  "notification.sender_pattern_conflict",
	"NotificationSenderUnknownDriver":    "notification.sender_unknown_driver",
	"NotificationSenderIncomplete":       "notification.sender_incomplete",
	"NotificationSenderNotFound":         "notification.sender_not_found",
	"NotificationSendFailed":             "notification.send_failed",
```

Run: `cd backend && go test ./internal/shared/errcode/ -count=1` → FAIL (`TestCodesMatchGoldenSnapshot`: const missing).

- [ ] **Step 2: Declare the constants** — a new `// --- notification ---` block in `codes.go`:

```go
// --- notification ---

// NotificationSenderPatternInvalid: a sender profile declares a category
// pattern outside the grammar (exact "foo.bar", prefix "foo.*", or "*"). 422.
const NotificationSenderPatternInvalid = "notification.sender_pattern_invalid"

// NotificationSenderNoDefault: patterns are declared but no profile claims
// "*", so an unmatched category would fail closed. 422.
const NotificationSenderNoDefault = "notification.sender_no_default"

// NotificationSenderDuplicateDefault: more than one profile claims "*". 422.
const NotificationSenderDuplicateDefault = "notification.sender_duplicate_default"

// NotificationSenderPatternConflict: the same pattern is declared by two
// profiles, so the send would be ambiguous. 422.
const NotificationSenderPatternConflict = "notification.sender_pattern_conflict"

// NotificationSenderUnknownDriver: a routing profile's provider names no
// registered driver. 422.
const NotificationSenderUnknownDriver = "notification.sender_unknown_driver"

// NotificationSenderIncomplete: a profile lacks a non-secret field its
// driver requires (save time), or any required field (test send). 422.
const NotificationSenderIncomplete = "notification.sender_incomplete"

// NotificationSenderNotFound: a test send named a profile slug that is not
// in the roster. 404.
const NotificationSenderNotFound = "notification.sender_not_found"

// NotificationSendFailed: the sender's transport or vendor refused a test
// message. The detail carries the bounded diagnostic, never vendor text. 502.
const NotificationSendFailed = "notification.send_failed"
```

- [ ] **Step 3: Verify and commit**

Run: `cd backend && go test ./internal/shared/errcode/ -count=1` → PASS.

```bash
git add backend/internal/shared/errcode
git commit -m "feat(errcode): notification.sender_* codes (ADR-0019)"
```

### Task 2: Pattern grammar, normalization and matching

**Files:**
- Create: `backend/internal/core/notification/services/sender_patterns.go`
- Test: `backend/internal/core/notification/services/sender_patterns_test.go`

**Interfaces produced:** `NormalizePatterns([]string) []string`, `ValidatePattern(string) error`, `MatchPattern(pattern, category string) (matched bool, literal int)`, `ErrPatternInvalid`.

- [ ] **Step 1: Write the failing tests**

```go
package services

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizePatterns(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{"auth.*", "", "crm.*"}, []string{"auth.*", "crm.*"}},          // an empty entry never becomes a match-everything pattern
		{[]string{" Auth.* ", "auth.*", "AUTH.*"}, []string{"auth.*"}},          // trim + lowercase + within-profile dedup
		{[]string{"*", "auth.x", "*"}, []string{"*", "auth.x"}},                 // order kept, first wins
		{nil, []string{}},
		{[]string{"  "}, []string{}},
	}
	for _, c := range cases {
		if got := NormalizePatterns(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("NormalizePatterns(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidatePattern(t *testing.T) {
	valid := []string{"*", "auth.verify_email", "auth.*", "auth.oauth.*", "marketing", "crm-2.x_1.*", "crm/campaign", "vendor:event.*", "événement.*"}
	invalid := []string{"", ".", ".*", "auth*", "auth.*.google", "*.auth", "a..b", "auth.", "auth.**", "*auth", "au*th.x", "a,b", "a,b.*"}
	for _, p := range valid {
		if err := ValidatePattern(p); err != nil {
			t.Errorf("ValidatePattern(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range invalid {
		if err := ValidatePattern(p); !errors.Is(err, ErrPatternInvalid) {
			t.Errorf("ValidatePattern(%q) = %v, want ErrPatternInvalid", p, err)
		}
	}
}

func TestMatchPattern(t *testing.T) {
	cases := []struct {
		pattern, category string
		matched           bool
		literal           int
	}{
		{"*", "anything", true, 0},
		{"*", "", true, 0},
		{"auth.x", "auth.x", true, 6},
		{"auth.x", "auth.x.y", false, 6},
		{"auth.*", "auth.x", true, 5},
		{"auth.*", "auth.oauth.google", true, 5},   // any depth
		{"auth.*", "auth", false, 5},               // never the bare token
		{"auth.*", "auth.", false, 5},              // at least one further character
		{"auth.*", "authx.y", false, 5},
		{"auth.oauth.*", "auth.oauth.google", true, 11},
		{"marketing", "marketing", true, 9},
		{"marketing.*", "marketing", false, 10},    // a category with no dot is never captured by a prefix rule
	}
	for _, c := range cases {
		m, lit := MatchPattern(c.pattern, c.category)
		if m != c.matched || lit != c.literal {
			t.Errorf("MatchPattern(%q, %q) = (%v, %d), want (%v, %d)", c.pattern, c.category, m, lit, c.matched, c.literal)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd backend && go test ./internal/core/notification/services/ -run 'Pattern' -count=1` → FAIL `undefined: NormalizePatterns`.

- [ ] **Step 3: Write `sender_patterns.go`**

```go
package services

import (
	"errors"
	"fmt"
	"strings"
)

// ErrPatternInvalid: a category pattern outside the grammar.
var ErrPatternInvalid = errors.New("notification: sender pattern is not valid")

// NormalizePatterns trims and lowercases each entry, drops empties and
// collapses duplicates within one list (first occurrence wins, order kept).
// Dedup happens here, BEFORE the cross-profile uniqueness check, so a
// within-profile repeat can never be reported as a conflict.
func NormalizePatterns(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		p := strings.ToLower(strings.TrimSpace(r))
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ValidatePattern accepts exactly one of: "*", an exact category "foo.bar",
// or a prefix "foo.*". Nothing else: no "*" inside a token, no "*" mid-
// pattern, no empty token. A token is otherwise unrestricted — iface does
// not constrain a fork's category charset, so the pattern does not either.
// Call on normalized input.
func ValidatePattern(p string) error {
	if p == "*" {
		return nil
	}
	lit := strings.TrimSuffix(p, ".*")
	if lit == "" {
		return fmt.Errorf("%w: %q", ErrPatternInvalid, p)
	}
	for _, tok := range strings.Split(lit, ".") {
		// "," is the FieldStringList separator, so it can never be part of a
		// stored pattern; rejecting it makes the impossibility explicit.
		if tok == "" || strings.ContainsAny(tok, "*,") {
			return fmt.Errorf("%w: %q", ErrPatternInvalid, p)
		}
	}
	return nil
}

// MatchPattern reports whether pattern matches category and the length of
// the literal the pattern requires — the precedence key: among matching
// patterns the longest literal wins, and ties are impossible (two prefixes
// of one category with equal literals are the same string; an exact match
// requires the whole category while a prefix requires strictly less).
func MatchPattern(pattern, category string) (matched bool, literal int) {
	switch {
	case pattern == "*":
		return true, 0
	case strings.HasSuffix(pattern, ".*"):
		prefix := pattern[:len(pattern)-1] // keep the dot: "auth."
		return len(category) > len(prefix) && strings.HasPrefix(category, prefix), len(prefix)
	default:
		return category == pattern, len(pattern)
	}
}
```

- [ ] **Step 4: Verify and commit**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'Pattern' -count=1` → PASS.

```bash
git add backend/internal/core/notification/services/sender_patterns.go backend/internal/core/notification/services/sender_patterns_test.go
git commit -m "feat(notification): sender category pattern grammar, normalization and precedence"
```

### Task 3: The element schema and the roster decoder

**Files:**
- Create: `backend/internal/core/notification/services/sender_config.go`
- Test: `backend/internal/core/notification/services/sender_config_test.go`

**Interfaces produced:** `SendersField = "email.senders"`, `SenderItems() []module.ConfigItemField`, `DecodeSenderProfiles(values, encrypted map[string]string) ([]SenderProfile, error)` — both maps from one snapshot; `encrypted == nil` is the save-time view; a decrypt failure is an error (the loader fails closed on it).

- [ ] **Step 1: Write the failing tests**

```go
package services

import (
	"reflect"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func rosterValues() map[string]string {
	k := func(slug, sub string) string { return module.ItemKey(SendersField, slug, sub) }
	return map[string]string{
		module.RosterKey(SendersField):              "mailup-sistema, esp-campagne",
		module.LabelKey(SendersField, "mailup-sistema"): "MailUp sistema",
		k("mailup-sistema", SubProvider):            "smtp",
		k("mailup-sistema", SubCategories):          " Auth.*,,*, auth.* ",
		k("mailup-sistema", SubFromAddress):         "sys@example.com",
		k("mailup-sistema", SubSMTPHost):            "relay",
		k("esp-campagne", SubProvider):              "noop",
		k("esp-campagne", SubSMTPPort):              "2525",
	}
}

func TestDecodeSenderProfiles_RosterOrderDefaultsAndNormalization(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex) // encryptForTest: sender_loader_test.go
	encrypted := map[string]string{module.ItemKey(SendersField, "mailup-sistema", SubSMTPPassword): encryptForTest(t, "s3cret")}
	got, err := DecodeSenderProfiles(rosterValues(), encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Slug != "mailup-sistema" || got[1].Slug != "esp-campagne" {
		t.Fatalf("roster order lost: %+v", got)
	}
	a := got[0]
	if a.Label != "MailUp sistema" || a.Provider != "smtp" || a.FromAddress != "sys@example.com" || a.SMTPHost != "relay" {
		t.Fatalf("scalar fields: %+v", a)
	}
	if !reflect.DeepEqual(a.Categories, []string{"auth.*", "*"}) {
		t.Fatalf("categories must be normalized and deduplicated: %q", a.Categories)
	}
	if a.SMTPPort != 587 || a.SMTPTLSMode != "starttls" {
		t.Fatalf("absent sub-fields resolve to the item Default: port=%d tls=%q", a.SMTPPort, a.SMTPTLSMode)
	}
	if a.SMTPPassword != "s3cret" {
		t.Fatalf("secret must be decrypted from the snapshot's encrypted map, got %q", a.SMTPPassword)
	}
	b := got[1]
	if b.Provider != "noop" || b.SMTPPort != 2525 || len(b.Categories) != 0 || b.SMTPPassword != "" {
		t.Fatalf("second profile: %+v", b)
	}
}

func TestDecodeSenderProfiles_SaveTimeViewHasNoSecrets(t *testing.T) {
	got, err := DecodeSenderProfiles(rosterValues(), nil)
	if err != nil || got[0].SMTPPassword != "" {
		t.Fatalf("nil encrypted map must leave every secret empty: %v %+v", err, got)
	}
}

func TestDecodeSenderProfiles_UndecryptableSecretIsAnError(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	encrypted := map[string]string{module.ItemKey(SendersField, "mailup-sistema", SubSMTPPassword): "not-ciphertext"}
	if _, err := DecodeSenderProfiles(rosterValues(), encrypted); err == nil {
		t.Fatal("a secret that cannot be decrypted must fail the decode, never read as empty")
	}
}

func TestDecodeSenderProfiles_EmptyRoster(t *testing.T) {
	if got, err := DecodeSenderProfiles(map[string]string{}, nil); err != nil || len(got) != 0 {
		t.Fatalf("want empty, got %+v %v", got, err)
	}
	if got, err := DecodeSenderProfiles(nil, nil); err != nil || len(got) != 0 {
		t.Fatalf("nil map: %+v %v", got, err)
	}
}

func TestDecodeSenderProfiles_StaleEnumValueIsPreservedNotRejected(t *testing.T) {
	v := rosterValues()
	v[module.ItemKey(SendersField, "esp-campagne", SubProvider)] = "sendgrid"
	got, err := DecodeSenderProfiles(v, nil)
	if err != nil || len(got) != 2 || got[1].Provider != "sendgrid" {
		t.Fatalf("a provider the enum no longer lists must surface on its own profile as an unknown driver, not fail the decode: %+v %v", got, err)
	}
}

func TestSenderItems_DeclarationIsValid(t *testing.T) {
	field := module.ConfigField{Key: SendersField, Label: "Sender profiles", Type: module.FieldRecordList, Items: SenderItems()}
	if err := module.ValidateConfigDeclarations([]module.ConfigField{field}, nil); err != nil {
		t.Fatal(err)
	}
	// Every sub-field key is one SenderProfile.Field understands (or categories).
	for _, it := range SenderItems() {
		if it.Key == SubCategories {
			continue
		}
		var p SenderProfile
		p.setField(it.Key, "1")
		if p.Field(it.Key) == "" {
			t.Errorf("sub-field %q is not mapped by SenderProfile.Field/setField", it.Key)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd backend && go test ./internal/core/notification/services/ -run 'DecodeSender|SenderItems' -count=1` → FAIL.

- [ ] **Step 3: Write `sender_config.go`**

```go
package services

import (
	"strings"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// SendersField is the record-list key that holds sender profiles.
const SendersField = "email.senders"

// SenderItems is the element schema of email.senders. It lives next to the
// decoder that reads it and module.go's ConfigSchema references it, so the
// two cannot drift.
//
// Every Required is scoped by the DependsOn that governs its driver — a
// correctness rule, not tidiness: the console blocks Save on an empty
// visible required field, so from_address required outside smtp would
// make a noop profile unsavable.
func SenderItems() []module.ConfigItemField {
	smtpOnly := []module.FieldCondition{{Key: SubProvider, In: []string{"smtp"}}}
	identity := []module.FieldCondition{{Key: SubProvider, In: []string{"smtp"}}}
	return []module.ConfigItemField{
		{Key: SubProvider, Label: "Provider", Type: module.FieldEnum, Options: []string{"noop", "smtp"}, Required: true, Default: "noop"},
		{Key: SubCategories, Label: "Categories", Type: module.FieldStringList, Placeholder: "auth.*, *",
			Description: "Routing patterns this profile serves: an exact category (auth.verify_email), a prefix (auth.*), or * for the default. Leave empty to keep the profile as a draft that receives no mail."},
		{Key: SubFromAddress, Label: "From address", Type: module.FieldString, Required: true, DependsOn: identity},
		{Key: SubFromName, Label: "From name", Type: module.FieldString, DependsOn: identity},
		{Key: SubReplyTo, Label: "Reply-To address", Type: module.FieldString, DependsOn: identity},
		{Key: SubSMTPHost, Label: "SMTP host", Type: module.FieldString, Required: true, DependsOn: smtpOnly},
		{Key: SubSMTPPort, Label: "SMTP port", Type: module.FieldInt, Default: "587", DependsOn: smtpOnly},
		{Key: SubSMTPTLSMode, Label: "TLS mode", Type: module.FieldEnum, Options: []string{"starttls", "tls", "none"}, Default: "starttls", DependsOn: smtpOnly},
		{Key: SubSMTPUsername, Label: "SMTP username", Type: module.FieldString, DependsOn: smtpOnly,
			Description: "Leave username and password empty for an unauthenticated relay."},
		{Key: SubSMTPPassword, Label: "SMTP password", Type: module.FieldSecret, DependsOn: smtpOnly},
	}
}

// DecodeSenderProfiles reads every element of email.senders out of ONE
// snapshot — the flat value map and the encrypted map of the same
// environment — in roster order. encrypted == nil is the save-time view, in
// which every secret reads "". A secret that cannot be decrypted is an
// error: the loader fails closed on it rather than sending with an empty
// password.
//
// Sub-field resolution is stored value → item Default, and the provider
// enum is NOT enforced here: a stale value must surface as ErrUnknownDriver
// on the profile that carries it, never as a decode failure that takes
// every other profile down with it.
func DecodeSenderProfiles(values, encrypted map[string]string) ([]SenderProfile, error) {
	items := SenderItems()
	roster := module.ParseRoster(values, SendersField)
	out := make([]SenderProfile, 0, len(roster))
	for _, slug := range roster {
		p := SenderProfile{Slug: slug, Label: values[module.LabelKey(SendersField, slug)]}
		for _, it := range items {
			key := module.ItemKey(SendersField, slug, it.Key)
			var v string
			switch {
			case it.Type == module.FieldSecret:
				plain, err := decryptSnapshotSecret(encrypted[key])
				if err != nil {
					return nil, fmt.Errorf("notification: sender %q: decrypt %s: %w", slug, it.Key, err)
				}
				v = plain
			default:
				v = strings.TrimSpace(values[key])
				if v == "" {
					v = it.Default
				}
			}
			switch it.Key {
			case SubCategories:
				p.Categories = NormalizePatterns(strings.Split(v, ","))
			case SubProvider:
				p.setField(it.Key, strings.ToLower(v))
			default:
				p.setField(it.Key, v)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

// decryptSnapshotSecret decrypts one stored ciphertext through the SDK's
// exported decoder — the same AES-256-GCM path GetSecret uses — without a
// repository read. "" decrypts to "".
func decryptSnapshotSecret(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	var v struct {
		S string `module:"s"`
	}
	schema := []module.ConfigField{{Key: "s", Type: module.FieldSecret}}
	if err := module.UnmarshalConfig(schema, nil, map[string]string{"s": enc}, &v); err != nil {
		return "", err
	}
	return v.S, nil
}
```
(add `"fmt"` to the imports.)

- [ ] **Step 4: Verify and commit**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'DecodeSender|SenderItems' -count=1` → PASS.

```bash
git add backend/internal/core/notification/services/sender_config.go backend/internal/core/notification/services/sender_config_test.go
git commit -m "feat(notification): email.senders element schema and roster decoder"
```

### Task 4: Roster resolution — most specific match wins

**Files:**
- Modify: `backend/internal/core/notification/services/sender_resolver.go`
- Modify: `backend/internal/core/notification/services/sender_resolver_test.go`
- Modify: `backend/internal/core/notification/services/notification_service_test.go` (`fakeResolver` gains `BySlug`)

**Interfaces produced:** `SenderResolver.BySlug(ctx, slug) (SenderProfile, error)` (returns `ErrSenderNotFound`, declared in PR 1 Task 1).

- [ ] **Step 1: Add the failing tests to `sender_resolver_test.go`**

```go
func rosterCfg(profiles ...SenderProfile) SenderConfig {
	return SenderConfig{Profiles: profiles, Legacy: LegacyProfile(SenderProfile{Provider: "noop"})}
}

func TestSenderResolver_MostSpecificWins(t *testing.T) {
	r := NewSenderResolver(fixedLoader(rosterCfg(
		SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}},
		SenderProfile{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}},
		SenderProfile{Slug: "auth-x", Provider: "smtp", Categories: []string{"auth.x"}},
		SenderProfile{Slug: "oauth", Provider: "smtp", Categories: []string{"auth.oauth.*"}},
		SenderProfile{Slug: "mkt", Provider: "smtp", Categories: []string{"marketing"}},
		SenderProfile{Slug: "draft", Provider: "ses"}, // no patterns: never selected
	)))
	cases := map[string]string{
		"auth.x":            "auth-x",  // exact (6) beats prefix (5)
		"AUTH.X":            "auth-x",  // category lowercased
		"auth.y":            "auth",
		"auth.oauth.google": "oauth",   // auth.oauth.* (11) beats auth.* (5)
		"auth":              "default", // auth.* never matches the bare token
		"marketing":         "mkt",
		"marketing.promo":   "default", // exact "marketing" does not match deeper
		"crm.campaign":      "default",
	}
	for cat, want := range cases {
		got, err := r.Resolve(context.Background(), ResolveInput{Category: cat})
		if err != nil || got.Slug != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", cat, got.Slug, err, want)
		}
	}
}

// TestSenderResolver_MalformedCategoryFailsClosed: with a "*" profile present,
// an empty or untrimmed category must NOT ride the default.
func TestSenderResolver_MalformedCategoryFailsClosed(t *testing.T) {
	r := NewSenderResolver(fixedLoader(rosterCfg(
		SenderProfile{Slug: "default", Provider: "noop", Categories: []string{"*"}},
		SenderProfile{Slug: "mkt", Provider: "noop", Categories: []string{"marketing"}},
	)))
	for _, cat := range []string{"", " marketing", "marketing ", "\tmarketing", " "} {
		if p, err := r.Resolve(context.Background(), ResolveInput{Category: cat}); !errors.Is(err, ErrNoSenderForCategory) {
			t.Errorf("Resolve(%q) = %+v, %v; want ErrNoSenderForCategory", cat, p, err)
		}
	}
	if p, err := r.Resolve(context.Background(), ResolveInput{Category: "MARKETING"}); err != nil || p.Slug != "mkt" {
		t.Fatalf("case is normalized, whitespace is not: %+v, %v", p, err)
	}
	// The rule starts with the routing map: an all-draft roster still sends "" through the legacy profile.
	drafts := rosterCfg(SenderProfile{Slug: "a", Provider: "noop"})
	if p, err := NewSenderResolver(fixedLoader(drafts)).Resolve(context.Background(), ResolveInput{Category: ""}); err != nil || p.Slug != LegacySlug {
		t.Fatalf("legacy mode must not inspect the category: %+v, %v", p, err)
	}
}

func TestSenderResolver_NoMatchFailsClosed(t *testing.T) {
	r := NewSenderResolver(fixedLoader(rosterCfg(
		SenderProfile{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}},
	)))
	if _, err := r.Resolve(context.Background(), ResolveInput{Category: "crm.campaign"}); !errors.Is(err, ErrNoSenderForCategory) {
		t.Fatalf("want ErrNoSenderForCategory, got %v", err)
	}
	if _, err := r.Default(context.Background()); !errors.Is(err, ErrNoSenderForCategory) {
		t.Fatalf("Default without a * profile must fail closed, got %v", err)
	}
}

// TestSenderResolver_DraftsOnlyKeepsLegacy pins the D6 cutover: the flat
// keys carry every send until some profile declares a pattern. Creating the
// first (pattern-less) profile — or removing the last pattern — must not
// strand mail; a draft is still reachable by slug for the test send.
func TestSenderResolver_DraftsOnlyKeepsLegacy(t *testing.T) {
	drafts := rosterCfg(
		SenderProfile{Slug: "a", Provider: "smtp"},
		SenderProfile{Slug: "b", Provider: "sendgrid"},
	)
	drafts.Legacy = LegacyProfile(SenderProfile{Provider: "smtp", SMTPHost: "relay", SMTPPort: 25, FromAddress: "f@x"})
	r := NewSenderResolver(fixedLoader(drafts))
	for _, cat := range []string{"auth.verify_email", "crm.campaign"} {
		if p, err := r.Resolve(context.Background(), ResolveInput{Category: cat}); err != nil || p.Slug != LegacySlug {
			t.Fatalf("Resolve(%q) with an all-draft roster = %+v, %v; want the legacy profile", cat, p, err)
		}
	}
	if p, err := r.Default(context.Background()); err != nil || p.Slug != LegacySlug {
		t.Fatalf("Default = %+v, %v", p, err)
	}
	if p, err := r.BySlug(context.Background(), "a"); err != nil || p.Provider != "smtp" {
		t.Fatalf("a draft must be reachable by slug for the test send: %+v, %v", p, err)
	}
	if p, err := r.BySlug(context.Background(), LegacySlug); err != nil || p.Slug != LegacySlug {
		t.Fatalf("the legacy slug resolves while it is what carries mail: %+v, %v", p, err)
	}

	// Once one profile routes, the legacy profile is out — including by slug.
	live := rosterCfg(SenderProfile{Slug: "a", Provider: "noop", Categories: []string{"*"}})
	rl := NewSenderResolver(fixedLoader(live))
	if p, _ := rl.Resolve(context.Background(), ResolveInput{Category: "x"}); p.Slug != "a" {
		t.Fatalf("routing map present: got %+v", p)
	}
	if _, err := rl.BySlug(context.Background(), LegacySlug); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("legacy slug must not resolve once a routing map exists, got %v", err)
	}
}

func TestSenderResolver_DefaultAndBySlug(t *testing.T) {
	roster := rosterCfg(
		SenderProfile{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}},
		SenderProfile{Slug: "fallback", Provider: "noop", Categories: []string{"*"}},
	)
	r := NewSenderResolver(fixedLoader(roster))
	if p, err := r.Default(context.Background()); err != nil || p.Slug != "fallback" {
		t.Fatalf("Default = %+v, %v", p, err)
	}
	if p, err := r.BySlug(context.Background(), "auth"); err != nil || p.Provider != "smtp" {
		t.Fatalf("BySlug = %+v, %v", p, err)
	}
	if _, err := r.BySlug(context.Background(), "nope"); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("want ErrSenderNotFound, got %v", err)
	}

	// Empty roster: the legacy slug resolves and nothing else does.
	legacyOnly := NewSenderResolver(fixedLoader(SenderConfig{Legacy: LegacyProfile(SenderProfile{})}))
	if p, err := legacyOnly.BySlug(context.Background(), LegacySlug); err != nil || p.Slug != LegacySlug {
		t.Fatalf("legacy BySlug = %+v, %v", p, err)
	}
	if _, err := legacyOnly.BySlug(context.Background(), "auth"); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("want ErrSenderNotFound on an empty roster, got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd backend && go test ./internal/core/notification/services/ -run 'TestSenderResolver' -count=1` → FAIL (`BySlug` undefined; match cases return legacy).

- [ ] **Step 3: Implement**

In `sender_resolver.go` add the routing-map predicate and the interface method:

```go
// hasRoutingMap reports whether any profile declares a pattern. This — not
// roster emptiness — is the D6 cutover: until some profile routes, the flat
// keys carry every send, so creating a first draft (or removing the last
// pattern) never strands mail. It is the same predicate ValidateSenderConfig
// uses to decide whether the routing rules apply.
func hasRoutingMap(profiles []SenderProfile) bool {
	for _, p := range profiles {
		if len(p.Categories) > 0 {
			return true
		}
	}
	return false
}
```

```go
	// BySlug returns one profile by its slug — the explicit-sender test send.
	BySlug(ctx context.Context, slug string) (SenderProfile, error)
```

Replace `Resolve` and `Default`, and add `BySlug`:

```go
func (r *senderResolver) Resolve(ctx context.Context, in ResolveInput) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	if !hasRoutingMap(cfg.Profiles) {
		// Legacy mode never inspects the category: today an empty Category
		// is legal and sends, and D6 promises byte-identical behaviour
		// until a profile routes. The malformed-category rule below is a
		// property of routing, and starts with the routing map.
		return cfg.Legacy, nil
	}
	// Lowercased for comparison (patterns are lowercased on read) — never
	// repaired: an empty category, or one carrying leading/trailing
	// whitespace, is malformed and fails closed. Without this, "*" would
	// match "" and " marketing " would ride the default sender.
	if in.Category == "" || strings.TrimSpace(in.Category) != in.Category {
		return SenderProfile{}, ErrNoSenderForCategory
	}
	category := strings.ToLower(in.Category)
	best := -1
	var found SenderProfile
	for _, p := range cfg.Profiles {
		for _, pat := range p.Categories {
			if ok, lit := MatchPattern(pat, category); ok && lit > best {
				best, found = lit, p
			}
		}
	}
	if best < 0 {
		return SenderProfile{}, ErrNoSenderForCategory
	}
	return found, nil
}

func (r *senderResolver) Default(ctx context.Context) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	if !hasRoutingMap(cfg.Profiles) {
		return cfg.Legacy, nil
	}
	for _, p := range cfg.Profiles {
		for _, pat := range p.Categories {
			if pat == "*" {
				return p, nil
			}
		}
	}
	return SenderProfile{}, ErrNoSenderForCategory
}

// BySlug sees drafts — that is how the test send proves a profile before
// any pattern routes traffic to it. The legacy slug resolves only while the
// legacy profile is the one carrying mail.
func (r *senderResolver) BySlug(ctx context.Context, slug string) (SenderProfile, error) {
	cfg := r.load(ctx)
	if cfg.Err != nil {
		return SenderProfile{}, ErrSenderConfigUnavailable
	}
	for _, p := range cfg.Profiles {
		if p.Slug == slug {
			return p, nil
		}
	}
	if slug == cfg.Legacy.Slug && !hasRoutingMap(cfg.Profiles) {
		return cfg.Legacy, nil
	}
	return SenderProfile{}, ErrSenderNotFound
}
```
(add `"strings"` to the imports; the file still needs no `"errors"` — every sentinel it returns is declared in `email_driver.go`.) In `notification_service_test.go` give `fakeResolver` a `BySlug`:

```go
func (f *fakeResolver) BySlug(_ context.Context, slug string) (SenderProfile, error) {
	if f.err != nil {
		return SenderProfile{}, f.err
	}
	if slug != f.profile.Slug {
		return SenderProfile{}, ErrSenderNotFound
	}
	return f.profile, nil
}
```

- [ ] **Step 4: Verify and commit**

Run: `cd backend && go test ./internal/core/notification/services/ -count=1` → PASS.

```bash
git add backend/internal/core/notification/services/sender_resolver.go backend/internal/core/notification/services/sender_resolver_test.go backend/internal/core/notification/services/notification_service_test.go
git commit -m "feat(notification): resolve senders by most-specific category pattern; legacy keys carry mail until a profile routes (D6)"
```

### Task 5: Save-time validation — the three states

**Files:**
- Create: `backend/internal/core/notification/services/sender_validation.go`
- Test: `backend/internal/core/notification/services/sender_validation_test.go`

**Interfaces produced:** `ValidateSenderConfig(values map[string]string, drivers *DriverRegistry) error` (returns `*module.ConfigValidationError` with `errcode.NotificationSender*` codes).

- [ ] **Step 1: Write the failing tests**

```go
package services

import (
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// profileValues builds a flat map for a roster. Each entry is slug → sub-field values.
func profileValues(order []string, elems map[string]map[string]string) map[string]string {
	v := map[string]string{module.RosterKey(SendersField): module.FormatRoster(order)}
	for slug, subs := range elems {
		for k, val := range subs {
			v[module.ItemKey(SendersField, slug, k)] = val
		}
	}
	return v
}

func validationDrivers() *DriverRegistry { return NewDriverRegistry(CoreDrivers(nil)...) }

func TestValidateSenderConfig_ThreeStates(t *testing.T) {
	smtpOK := map[string]string{SubProvider: "smtp", SubFromAddress: "f@x", SubSMTPHost: "h", SubSMTPPort: "25"}
	cases := []struct {
		name      string
		values    map[string]string
		wantCode  string
		wantField string
	}{
		// State 1 — empty roster: a legacy install; a PATCH touching only app.name must pass.
		{"empty roster passes", map[string]string{"app.name": "Orkestra", "email.provider": "smtp"}, "", ""},
		{"nil map passes", nil, "", ""},
		// State 2 — roster of drafts: the first save of the first profile carries no patterns.
		{"drafts only pass even with an unknown driver",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop"}, "b": {SubProvider: "sendgrid"}}), "", ""},
		// State 3 — routing map.
		{"one default routing profile passes",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}}), "", ""},
		{"pattern without a default",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "auth.*"}}),
			errcode.NotificationSenderNoDefault, SendersField},
		{"two defaults",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "noop", SubCategories: "*"}}),
			errcode.NotificationSenderDuplicateDefault, module.ItemKey(SendersField, "b", SubCategories)},
		{"same pattern on two profiles",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*, auth.*"}, "b": {SubProvider: "noop", SubCategories: "Auth.*"}}),
			errcode.NotificationSenderPatternConflict, module.ItemKey(SendersField, "b", SubCategories)},
		{"within-profile repeat is not a conflict",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*, auth.*, auth.*, AUTH.*"}}), "", ""},
		{"malformed pattern",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*, auth*"}}),
			errcode.NotificationSenderPatternInvalid, module.ItemKey(SendersField, "a", SubCategories)},
		{"a profile whose only pattern is malformed is not a draft",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "noop", SubCategories: "auth.*.google"}}),
			errcode.NotificationSenderPatternInvalid, module.ItemKey(SendersField, "b", SubCategories)},
		{"draft with unknown driver beside a live profile saves",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "sendgrid"}}), "", ""},
		{"the same profile once it declares a pattern is rejected",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "sendgrid", SubCategories: "crm.*"}}),
			errcode.NotificationSenderUnknownDriver, module.ItemKey(SendersField, "b", SubProvider)},
		{"routing smtp profile missing host",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "smtp", SubCategories: "*", SubFromAddress: "f@x"}}),
			errcode.NotificationSenderIncomplete, module.ItemKey(SendersField, "a", SubSMTPHost)},
		{"anonymous smtp relay is complete", profileValues([]string{"a"}, map[string]map[string]string{"a": mergeSubs(smtpOK, SubCategories, "*")}), "", ""},
		{"draft smtp profile missing host saves",
			profileValues([]string{"a", "b"}, map[string]map[string]string{"a": {SubProvider: "noop", SubCategories: "*"}, "b": {SubProvider: "smtp"}}), "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSenderConfig(c.values, validationDrivers())
			if c.wantCode == "" {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			var ve *module.ConfigValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("want *ConfigValidationError, got %v", err)
			}
			if ve.Code != c.wantCode || ve.Field != c.wantField {
				t.Fatalf("code=%q field=%q, want %q %q (message %q)", ve.Code, ve.Field, c.wantCode, c.wantField, ve.Message)
			}
		})
	}
}

func mergeSubs(base map[string]string, kv ...string) map[string]string {
	out := make(map[string]string, len(base)+len(kv)/2)
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// TestValidateSenderConfig_IsSecretBlind documents the D5 limit rather than
// leaving it implicit: a routing profile whose only gap is a secret saves
// cleanly here and is caught by IsConfiguredFor at request time instead.
func TestValidateSenderConfig_IsSecretBlind(t *testing.T) {
	secretDriver := &reqDriver{name: "vendor", reqs: []ProfileRequirement{{Key: SubFromAddress}, {Key: SubSMTPPassword, Secret: true}}}
	values := profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "vendor", SubCategories: "*", SubFromAddress: "f@x"}})
	if err := ValidateSenderConfig(values, NewDriverRegistry(secretDriver)); err != nil {
		t.Fatalf("the save-time gate cannot see secrets and must not reject on one: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure** — `cd backend && go test ./internal/core/notification/services/ -run 'ValidateSenderConfig' -count=1` → FAIL.

- [ ] **Step 3: Write `sender_validation.go`**

```go
package services

import (
	"fmt"
	"strings"

	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// ValidateSenderConfig is the save-time and activation-time gate (ADR-0019
// D5). It sees the module's merged non-secret map — every PATCH, including
// one that touches only app.name — so the routing rules are scoped to the
// states in which a routing map exists:
//
//	roster empty                     → vacuous (a legacy install)
//	roster non-empty, no patterns    → vacuous (every profile is a draft)
//	≥1 profile declares a pattern    → all rules apply
//
// In the third state every per-profile rule applies only to profiles that
// declare at least one pattern: a draft cannot route, so nothing it gets
// wrong can reach a send, and rejecting it would block a PATCH the operator
// did not intend to be about that profile. Grammar is the exception —
// checked wherever a pattern is declared at all, well-formed or not.
func ValidateSenderConfig(values map[string]string, drivers *DriverRegistry) error {
	profiles, err := DecodeSenderProfiles(values, nil) // save-time view: no secrets, so no decrypt can fail
	if err != nil {
		return fmt.Errorf("notification: decode sender profiles: %w", err)
	}
	if !hasRoutingMap(profiles) {
		return nil // states 1 and 2: no routing map, nothing to judge
	}
	var routing []SenderProfile
	for _, p := range profiles {
		if len(p.Categories) > 0 {
			routing = append(routing, p)
		}
	}

	claimedBy := make(map[string]string) // pattern → slug
	defaults := 0
	for _, p := range routing {
		catField := module.ItemKey(SendersField, p.Slug, SubCategories)
		for _, pat := range p.Categories {
			if err := ValidatePattern(pat); err != nil {
				return &module.ConfigValidationError{
					Field:   catField,
					Message: fmt.Sprintf("%q is not a valid pattern: use an exact category (auth.verify_email), a prefix (auth.*), or *", capString(pat, maxTokenLen)),
					Code:    errcode.NotificationSenderPatternInvalid,
				}
			}
			if pat == "*" {
				defaults++
				if defaults > 1 {
					return &module.ConfigValidationError{
						Field:   catField,
						Message: "only one sender profile may declare * as its pattern",
						Code:    errcode.NotificationSenderDuplicateDefault,
					}
				}
			}
			if other, dup := claimedBy[pat]; dup {
				return &module.ConfigValidationError{
					Field:   catField,
					Message: fmt.Sprintf("pattern %q is already declared by profile %q", pat, other),
					Code:    errcode.NotificationSenderPatternConflict,
				}
			}
			claimedBy[pat] = p.Slug
		}

		d, ok := drivers.Get(p.Provider)
		if !ok {
			return &module.ConfigValidationError{
				Field:   module.ItemKey(SendersField, p.Slug, SubProvider),
				Message: "provider must be one of: " + strings.Join(drivers.Names(), ", "),
				Code:    errcode.NotificationSenderUnknownDriver,
			}
		}
		if err := ValidateProfile(d, p, SaveTimeView); err != nil {
			inc := err.(*ProfileIncompleteError)
			return &module.ConfigValidationError{
				Field:   module.ItemKey(SendersField, p.Slug, inc.Missing[0]),
				Message: fmt.Sprintf("the %s provider needs: %s", p.Provider, strings.Join(inc.Missing, ", ")),
				Code:    errcode.NotificationSenderIncomplete,
			}
		}
	}
	if defaults == 0 {
		return &module.ConfigValidationError{
			Field:   SendersField,
			Message: "one sender profile must declare * so every category has a sender",
			Code:    errcode.NotificationSenderNoDefault,
		}
	}
	return nil
}
```

- [ ] **Step 4: Verify and commit**

Run: `cd backend && go test ./internal/core/notification/services/ -run 'ValidateSenderConfig' -count=1` → PASS.

```bash
git add backend/internal/core/notification/services/sender_validation.go backend/internal/core/notification/services/sender_validation_test.go
git commit -m "feat(notification): save-time sender validation scoped to the three roster states (ADR-0019 D5)"
```

### Task 6: Declare `email.senders`, wire the validators and the roster loader

**Files:**
- Create: `backend/internal/core/notification/config_validation.go`
- Test: `backend/internal/core/notification/config_validation_test.go`
- Modify: `backend/internal/core/notification/module.go` (`ConfigGroups`, `ConfigSchema`, `Init`)
- Modify: `backend/internal/core/notification/config_groups_test.go` (`TestSMTPFields_GatedOnProvider` unchanged; add the agreement test)

**Interfaces produced:** `(*NotificationModule).ValidateConfig`, `(*NotificationModule).ValidateConfigActivation`, `(*NotificationModule).driverRegistry()`.

- [ ] **Step 1: Write the failing tests (`config_validation_test.go`)**

```go
package notification

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/internal/shared/errcode"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func TestNotificationModuleImplementsConfigValidatorHooks(t *testing.T) {
	var m module.Module = NewModule()
	if _, ok := m.(module.HasConfigValidator); !ok {
		t.Fatal("notification must implement HasConfigValidator")
	}
	if _, ok := m.(module.HasConfigActivationValidator); !ok {
		t.Fatal("notification must implement HasConfigActivationValidator")
	}
}

// Both hooks must share one policy function: a map broken in the third
// state must be rejected at PATCH time AND must not be promotable.
func TestNotificationConfigValidation_BothHooksAgree(t *testing.T) {
	broken := map[string]string{
		module.RosterKey(services.SendersField):                          "a",
		module.ItemKey(services.SendersField, "a", services.SubProvider):   "noop",
		module.ItemKey(services.SendersField, "a", services.SubCategories): "auth.*",
	}
	legacy := map[string]string{"app.name": "Orkestra", "email.provider": "smtp"}
	m := NewModule()
	hooks := []struct {
		name string
		call func(map[string]string) error
	}{
		{"ValidateConfig", func(v map[string]string) error { return m.ValidateConfig(context.Background(), v) }},
		{"ValidateConfigActivation", func(v map[string]string) error { return m.ValidateConfigActivation(context.Background(), v) }},
	}
	for _, h := range hooks {
		if err := h.call(legacy); err != nil {
			t.Errorf("%s: legacy map must pass, got %v", h.name, err)
		}
		var ve *module.ConfigValidationError
		if err := h.call(broken); !errors.As(err, &ve) || ve.Code != errcode.NotificationSenderNoDefault {
			t.Errorf("%s: want sender_no_default, got %v", h.name, err)
		}
	}
	// A zero-value module (as the declaration tests build it) must still validate.
	if err := (&NotificationModule{}).ValidateConfig(context.Background(), broken); err == nil {
		t.Error("zero-value module must build its registry lazily and still reject")
	}
}
```

Add to `config_groups_test.go`:

```go
// TestSenderItems_SchemaDriverAgreement: for every driver, every sub-field
// the schema marks Required AND visible under that provider must be one the
// driver requires (secret or not — a required secret is a console-side
// hint, D5); and every field the driver requires must be visible under that
// provider and either Required or defaulted. A noop profile with
// nothing but a slug and a label must be savable.
func TestSenderItems_SchemaDriverAgreement(t *testing.T) {
	items := services.SenderItems()
	visible := func(it module.ConfigItemField, provider string) bool {
		if len(it.DependsOn) == 0 {
			return true
		}
		for _, c := range it.DependsOn {
			if c.Key != services.SubProvider {
				t.Fatalf("sub-field %q depends on %q; only provider is expected", it.Key, c.Key)
			}
			for _, v := range c.In {
				if v == provider {
					return true
				}
			}
		}
		return false
	}
	for _, d := range services.CoreDrivers(nil) {
		required := map[string]bool{} // every requirement, secrets included
		for _, r := range d.Requires() {
			required[r.Key] = true
		}
		for _, it := range items {
			if it.Key == services.SubProvider {
				continue // the selector itself is required and never gated
			}
			schemaRequired := it.Required && visible(it, d.Name())
			if schemaRequired && !required[it.Key] {
				t.Errorf("driver %s: schema requires %q but the driver does not — the console would block Save on a field the driver never reads", d.Name(), it.Key)
			}
			if required[it.Key] && !visible(it, d.Name()) {
				t.Errorf("driver %s: requires %q but the schema hides it under that provider", d.Name(), it.Key)
			}
			if required[it.Key] && !it.Required && it.Default == "" {
				t.Errorf("driver %s: requires %q but the schema neither marks it required nor defaults it", d.Name(), it.Key)
			}
		}
	}
	// noop: nothing visible is required.
	for _, it := range items {
		if it.Key != services.SubProvider && it.Required && visible(it, "noop") {
			t.Errorf("a noop profile must be savable with nothing but a slug and a label; %q is required and visible", it.Key)
		}
	}
}

func TestSendersField_Declared(t *testing.T) {
	for _, f := range (&NotificationModule{}).ConfigSchema() {
		if f.Key == services.SendersField {
			if f.Type != module.FieldRecordList || f.Group != "senders" || len(f.Items) == 0 {
				t.Fatalf("email.senders must be a recordList in group senders with items: %+v", f)
			}
			return
		}
	}
	t.Fatal("email.senders not declared")
}
```
(add `"github.com/orkestra/backend/internal/core/notification/services"` to that file's imports.)

- [ ] **Step 2: Run to verify failure** — `cd backend && go test ./internal/core/notification/ -count=1` → FAIL (`ValidateConfig` undefined, `email.senders` not declared).

- [ ] **Step 3: Write `config_validation.go`**

```go
package notification

import (
	"context"
	"log/slog"

	"github.com/orkestra/backend/internal/core/notification/services"
	"github.com/orkestra/backend/pkg/sdk/module"
)

var (
	_ module.HasConfigValidator           = (*NotificationModule)(nil)
	_ module.HasConfigActivationValidator = (*NotificationModule)(nil)
)

// ValidateConfig rejects a broken sender routing map at the PATCH boundary
// (active-config and named-environment PATCH both funnel here). The rules
// are vacuous while no profile declares a pattern — see
// services.ValidateSenderConfig for the three states (ADR-0019 D5).
func (m *NotificationModule) ValidateConfig(_ context.Context, merged map[string]string) error {
	return services.ValidateSenderConfig(merged, m.driverRegistry())
}

// ValidateConfigActivation applies the same policy to the whole target
// profile before an environment switch, so sandbox → production cannot
// activate a map that is broken in the third state.
func (m *NotificationModule) ValidateConfigActivation(_ context.Context, target map[string]string) error {
	return services.ValidateSenderConfig(target, m.driverRegistry())
}

// driverRegistry returns the registry Init built, or a default one for a
// module that was never initialized (declaration tests, validation before
// Init). Both carry the same driver names and requirements; only the
// logger differs.
func (m *NotificationModule) driverRegistry() *services.DriverRegistry {
	if m.drivers == nil {
		m.drivers = services.NewDriverRegistry(services.CoreDrivers(slog.Default())...)
	}
	return m.drivers
}
```

- [ ] **Step 4: Declare the field and group in `module.go`**

`ConfigGroups()` becomes:

```go
	return []module.ConfigGroup{
		{Key: "delivery", Label: "Delivery", Order: 1,
			Description: "The default transport. With the noop provider, rendered mail is logged to the backend instead of sent — set the provider to SMTP to reveal the connection settings. Ignored once at least one sender profile below routes a category."},
		{Key: "senders", Label: "Sender profiles", Order: 2,
			Description: "Several senders, each with its own transport and identity, selected per message by category patterns (auth.*, crm.*, * for the default). Until one profile declares a pattern, the Delivery and Sender settings remain the single default."},
		{Key: "sender", Label: "Sender", Order: 3,
			Description: "The addresses recipients see when the default transport is used."},
		{Key: "branding", Label: "Branding & templates", Order: 4,
			Description: "Values injected into every templated email."},
	}
```

In `ConfigSchema()` append, after `email.reply_to`:

```go
		{Key: services.SendersField, Label: "Sender profiles", Group: "senders", Type: module.FieldRecordList, Items: services.SenderItems(),
			Description: "Each profile is a transport and an identity. Patterns decide which categories it carries; the most specific pattern wins and * is the default. A profile without patterns is a draft and receives no mail. Once any profile declares a pattern, exactly one must declare *."},
```

`Init` is unchanged: `NewSnapshotLoader` already receives the whole document. Teach the loader to decode the roster from that same snapshot — in `sender_loader.go`, replace the tail of `NewSnapshotLoader`'s closure:

```go
		legacy, err := legacyFromSnapshot(doc)
		if err != nil {
			return SenderConfig{Err: err}
		}
		// Roster and legacy keys from the SAME active-environment snapshot:
		// the profile that carries a send and the secret it authenticates
		// with can never come from different environments.
		profiles, err := DecodeSenderProfiles(doc.ActiveConfigValues(), doc.ActiveEncryptedValues())
		if err != nil {
			return SenderConfig{Err: err} // fail closed (D5): a roster we cannot read must not be guessed
		}
		return SenderConfig{Legacy: legacy, Profiles: profiles}
```

Add to `sender_loader_test.go`:

```go
// TestSnapshotLoader_RosterFollowsTheSameSnapshot: a profile's host and its
// secret come from the environment the document says is active — for every
// load, across a switch — and never from a second read.
func TestSnapshotLoader_RosterFollowsTheSameSnapshot(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", testKeyHex)
	doc := twoEnvDoc(t)
	k := func(sub string) string { return module.ItemKey(SendersField, "auth", sub) }
	for env, host := range map[string]string{"production": "prod.esp", "sandbox": "sand.esp"} {
		e := doc.Environments[env]
		e.ConfigValues[module.RosterKey(SendersField)] = "auth"
		e.ConfigValues[k(SubProvider)] = "smtp"
		e.ConfigValues[k(SubCategories)] = "*"
		e.ConfigValues[k(SubFromAddress)] = "a@example.com"
		e.ConfigValues[k(SubSMTPHost)] = host
		e.EncryptedValues[k(SubSMTPPassword)] = encryptForTest(t, host+"-secret")
		doc.Environments[env] = e
	}
	calls := 0
	load := NewSnapshotLoader(func(context.Context) (*module.ModuleConfig, error) {
		calls++
		if calls > 1 {
			doc.ActiveEnvironment = "sandbox"
		}
		return doc, nil
	})
	for _, want := range []string{"prod.esp", "sand.esp"} {
		cfg := load(context.Background())
		if cfg.Err != nil || len(cfg.Profiles) != 1 {
			t.Fatalf("cfg = %+v", cfg)
		}
		if p := cfg.Profiles[0]; p.SMTPHost != want || p.SMTPPassword != want+"-secret" {
			t.Fatalf("host %q with secret %q — environments mixed", p.SMTPHost, p.SMTPPassword)
		}
	}
	if calls != 2 {
		t.Fatalf("one read per load, got %d", calls)
	}
}
```

- [ ] **Step 5: Verify**

Run: `cd backend && go test ./internal/core/notification/... ./cmd/server/ -count=1` → PASS, including `TestConfigDeclarationsAreValid` over the real catalog (a `DependsOn` naming an option outside `Options` would fail here).

- [ ] **Step 6: Update `CLAUDE.md` and commit**

In `backend/internal/core/notification/CLAUDE.md`, under **Sender profiles and drivers**, replace the sentence about the empty roster with:

```markdown
Profiles are declared in the `email.senders` record list (`FieldRecordList`,
group **Sender profiles**). Storage is the flat map every setting uses:
`email.senders.__items` (roster), `email.senders.<slug>.provider`,
`.categories`, `.from_address`, `.from_name`, `.reply_to`, `.smtp_host`,
`.smtp_port`, `.smtp_tls_mode`, `.smtp_username`, `.smtp_password` (secret,
AES-256-GCM at its ordinary key). Element sub-fields carry **no `EnvVar`** by
construction, so the flat `email.*` keys stay as the environment-bootstrap
path: **until some profile declares a pattern** — the roster is empty, or holds
only drafts — the resolver synthesizes `slug=_legacy`, pattern `*`, from them
(D6). The leading underscore is outside the slug grammar, so a roster profile
named "Default" can never collide with it; `BySlug` searches the roster first
and answers `_legacy` only while the legacy profile carries mail. Creating a first draft, or removing the last pattern, therefore never
strands mail; the cutover is the existence of a routing map, the same predicate
the validator uses. No migration, no boot-time write.

**Routing.** A pattern is exactly `*`, an exact category `foo.bar`, or a prefix
`foo.*` (matches `foo.` + ≥1 char at any depth, never bare `foo`). Entries are
trimmed, lowercased, empties dropped, within-profile duplicates collapsed. The
most specific match (longest required literal) wins; `*` last. No match ⇒ the
send **fails closed** (`ErrNoSenderForCategory`). A profile with no patterns is a
draft: never selected, never validated beyond grammar.

**Validation** (`config_validation.go` → `services.ValidateSenderConfig`, both
`HasConfigValidator` and `HasConfigActivationValidator`). Rules apply only once
the roster is non-empty **and** ≥1 profile declares a pattern; below that they
are vacuous (a legacy install's PATCH to `app.name` must pass; the first save of
the first profile carries no patterns). Then: every declared pattern is
well-formed (`notification.sender_pattern_invalid`), exactly one profile declares
`*` (`_no_default` / `_duplicate_default`), no pattern is claimed twice
(`_pattern_conflict`), and — for profiles declaring ≥1 pattern only — the
provider is a registered driver (`_unknown_driver`) and its **non-secret**
requirements are present (`_incomplete`). The gate never sees secrets; a `mailup`
profile missing only its secret saves and fails at send — `IsConfiguredFor`
and the explicit-sender test send cover that gap.
```

Update the **Settings** table: add a row `email.senders` — "(record list, see Sender profiles)" — no env var.

```bash
git add backend/internal/core/notification
git commit -m "feat(notification): declare email.senders, validate the routing map at save and activation, read the roster from the same snapshot as the legacy keys (ADR-0019 PR 2)"
```

### Task 7: `CategoryConfiguredChecker` in `pkg/sdk/iface` and `IsConfiguredFor`

**Files:**
- Modify: `backend/pkg/sdk/iface/interfaces.go` (after `NotificationSender`)
- Create: `backend/pkg/sdk/iface/notification_category_test.go`
- Modify: `backend/internal/core/notification/services/notification_service.go`
- Modify: `backend/internal/core/notification/services/notification_service_test.go`
- Modify: `backend/pkg/sdk/CLAUDE.md`, `docs/site/sdk/shared-iface.mdx` (one row each)

**Interfaces produced:** `iface.CategoryConfiguredChecker`, `iface.IsConfiguredForCategory(ctx, s NotificationSender, category string) bool`, `(*NotificationService).IsConfiguredFor(ctx, category) bool`.

- [ ] **Step 1: Write the failing accessor test**

```go
package iface

import (
	"context"
	"testing"
)

type coarseSender struct{ configured bool }

func (c coarseSender) IsConfigured(context.Context) bool { return c.configured }
func (c coarseSender) Send(context.Context, NotificationRequest) (*NotificationResult, error) {
	return nil, nil
}
func (c coarseSender) SendTemplated(context.Context, TemplatedNotificationRequest) (*NotificationResult, error) {
	return nil, nil
}

type exactSender struct {
	coarseSender
	perCategory map[string]bool
	asked       []string
}

func (e *exactSender) IsConfiguredFor(_ context.Context, category string) bool {
	e.asked = append(e.asked, category)
	return e.perCategory[category]
}

// A sender implementing the companion gets the exact answer; one that does
// not falls back to IsConfigured — the fork-compatibility guarantee (D7).
func TestIsConfiguredForCategory(t *testing.T) {
	ctx := context.Background()
	if IsConfiguredForCategory(ctx, nil, "auth.verify_email") {
		t.Fatal("nil sender must be not configured")
	}
	if !IsConfiguredForCategory(ctx, coarseSender{configured: true}, "auth.verify_email") {
		t.Fatal("a sender without the companion falls back to IsConfigured")
	}
	if IsConfiguredForCategory(ctx, coarseSender{configured: false}, "auth.verify_email") {
		t.Fatal("fallback must honour IsConfigured=false")
	}
	e := &exactSender{coarseSender: coarseSender{configured: true}, perCategory: map[string]bool{"auth.verify_email": false, "crm.campaign": true}}
	if IsConfiguredForCategory(ctx, e, "auth.verify_email") {
		t.Fatal("exact answer must win over the coarse true")
	}
	if !IsConfiguredForCategory(ctx, e, "crm.campaign") {
		t.Fatal("exact answer must win")
	}
	if len(e.asked) != 2 || e.asked[0] != "auth.verify_email" {
		t.Fatalf("companion must be asked for the category: %v", e.asked)
	}
}
```

- [ ] **Step 2: Add to `interfaces.go` immediately after the `NotificationSender` interface**

```go
// CategoryConfiguredChecker is an OPTIONAL companion to NotificationSender
// (ADR-0019 D7). With sender profiles routed by category, IsConfigured's
// single boolean is wrong in both directions for a caller about to send one
// category; this answers for that category. A sender that does not
// implement it keeps working — IsConfiguredForCategory falls back.
type CategoryConfiguredChecker interface {
	IsConfiguredFor(ctx context.Context, category string) bool
}

// IsConfiguredForCategory is the accessor every pre-flight guard should use:
// the exact answer when s implements CategoryConfiguredChecker, the coarse
// IsConfigured otherwise, false for a nil sender. Follows the
// HasConfigGroups/ConfigGroupsOf idiom for extending a frozen contract.
func IsConfiguredForCategory(ctx context.Context, s NotificationSender, category string) bool {
	if s == nil {
		return false
	}
	if c, ok := s.(CategoryConfiguredChecker); ok {
		return c.IsConfiguredFor(ctx, category)
	}
	return s.IsConfigured(ctx)
}
```

- [ ] **Step 3: Add `IsConfiguredFor` to the service and its tests**

In `notification_service.go` after `IsConfigured`:

```go
// IsConfiguredFor answers for one category: it resolves the profile that
// would carry the send and checks its driver with secrets in view — which
// the save-time gate cannot do. No match ⇒ false (fail-closed, D7).
func (s *NotificationService) IsConfiguredFor(ctx context.Context, category string) bool {
	if s.resolver == nil || s.drivers == nil {
		return false
	}
	// Same input the dispatch path builds — TenantID included — so the
	// pre-flight can never check a different profile than the send uses.
	tenantID, _ := ctxauth.GetTenantID(ctx)
	p, err := s.resolver.Resolve(ctx, ResolveInput{Category: category, TenantID: tenantID})
	if err != nil {
		return false
	}
	_, err = s.usableDriver(p)
	return err == nil
}
```

In `notification_service_test.go`:

```go
func TestNotificationService_IsConfiguredFor(t *testing.T) {
	auth := &fakeDriver{name: "smtp", requires: []ProfileRequirement{{Key: SubSMTPHost}}}
	def := &fakeDriver{name: "noop"}
	cfg := SenderConfig{Profiles: []SenderProfile{
		{Slug: "default", Provider: "noop", Categories: []string{"*"}},
		{Slug: "auth", Provider: "smtp", Categories: []string{"auth.*"}}, // broken: no host
	}}
	svc := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		NewSenderResolver(fixedLoader(cfg)), NewDriverRegistry(auth, def), discardLogger(), Options{})
	ctx := context.Background()

	// Today's global boolean gets both of these wrong.
	if svc.IsConfiguredFor(ctx, "auth.verify_email") {
		t.Fatal("valid default + broken auth.* must be false for auth.*")
	}
	if !svc.IsConfiguredFor(ctx, "crm.campaign") || !svc.IsConfigured(ctx) {
		t.Fatal("the default profile is fine")
	}

	// Secret-only gap: invisible to ValidateConfig, caught here.
	secretDriver := &fakeDriver{name: "vendor", requires: []ProfileRequirement{{Key: SubSMTPPassword, Secret: true}}}
	cfg2 := SenderConfig{Profiles: []SenderProfile{{Slug: "v", Provider: "vendor", Categories: []string{"*"}}}}
	svc2 := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		NewSenderResolver(fixedLoader(cfg2)), NewDriverRegistry(secretDriver), discardLogger(), Options{})
	if svc2.IsConfiguredFor(ctx, "anything") {
		t.Fatal("a profile missing only its secret must be reported unconfigured at request time")
	}

	// Malformed category ⇒ false, even with a "*" profile.
	if svc.IsConfiguredFor(ctx, "") || svc.IsConfiguredFor(ctx, " crm.campaign") {
		t.Fatal("an empty or untrimmed category must not ride the default")
	}

	// D4: the pre-flight hands the resolver the same TenantID the dispatch does.
	k := newKit(Options{})
	tctx := context.WithValue(context.Background(), ctxauth.KeyTenantID, "t-42")
	if !k.svc.IsConfiguredFor(tctx, "crm.campaign") {
		t.Fatal("fake default profile is usable")
	}
	if len(k.resolver.inputs) != 1 || k.resolver.inputs[0].TenantID != "t-42" || k.resolver.inputs[0].Category != "crm.campaign" {
		t.Fatalf("pre-flight resolver input = %+v", k.resolver.inputs)
	}

	// Unmatched category ⇒ false.
	cfg3 := SenderConfig{Profiles: []SenderProfile{{Slug: "auth", Provider: "noop", Categories: []string{"auth.*"}}}}
	svc3 := NewNotificationService(newFakeNotifRepo(), &fakeTemplateService{}, &fakePrefService{can: true}, &fakeUnsubService{},
		NewSenderResolver(fixedLoader(cfg3)), NewDriverRegistry(def), discardLogger(), Options{})
	if svc3.IsConfiguredFor(ctx, "crm.campaign") {
		t.Fatal("no match must be false")
	}
	var _ iface.CategoryConfiguredChecker = svc3
}
```

- [ ] **Step 4: Verify, document, commit**

Run: `cd backend && go test ./pkg/sdk/iface/ ./internal/core/notification/... -count=1` → PASS.

`backend/pkg/sdk/CLAUDE.md` (the `iface/` row): append "`CategoryConfiguredChecker` (optional companion to `NotificationSender`, ADR-0019) + `IsConfiguredForCategory` accessor". `docs/site/sdk/shared-iface.mdx`: after the `NotificationSender` row add a row `CategoryConfiguredChecker | notification | auth pre-flight guards — optional; IsConfiguredForCategory falls back to IsConfigured`.

```bash
git add backend/pkg/sdk backend/internal/core/notification docs/site/sdk/shared-iface.mdx
git commit -m "feat(sdk): optional CategoryConfiguredChecker + IsConfiguredForCategory; notification answers per category (ADR-0019 D7)"
```

### Task 8: Migrate `auth`'s eight guards; align the three bare categories

**Files:**
- Modify: `backend/internal/core/auth/services/password_auth_service.go` (guards at ~256, 969, 1280, 1660, 1767, 1839; categories at ~978, 1288, 1847)
- Modify: `backend/internal/core/auth/services/suspicious_login_notifier.go` (guards at ~216, 295)
- Modify: `backend/internal/core/auth/services/suspicious_login_notifier_test.go` (`stubNotifier`)
- Create: `backend/internal/core/auth/services/preflight_guards_test.go`
- Modify: `backend/internal/core/auth/CLAUDE.md` (line ~645)

- [ ] **Step 1: Write the failing source-shape test**

```go
package services

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestPreflightGuards_AskForTheCategoryTheySend is the ADR-0019 D7
// checklist: every pre-flight guard asks for the category of the send that
// follows it, and no coarse IsConfigured(ctx) guard remains. Counted from
// source because the eight call sites live inside methods whose fixtures
// are large; the runtime behaviour of the accessor is tested in pkg/sdk/iface.
func TestPreflightGuards_AskForTheCategoryTheySend(t *testing.T) {
	want := map[string]map[string]int{
		"password_auth_service.go": {
			"CategoryAuthVerifyEmail":    2, // signup admission + resend verification
			"CategoryAuthResetPassword":  2, // forgot password + admin-initiated reset
			"CategoryAuthNewDeviceLogin": 1,
			"CategoryAuthAdminInvite":    1,
		},
		"suspicious_login_notifier.go": {
			"CategoryAuthSuspiciousLogin":      1,
			"CategoryAuthAdminSuspiciousLogin": 1,
		},
	}
	total := 0
	for file, cats := range want {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(src), ".IsConfigured(ctx)"); n != 0 {
			t.Errorf("%s: %d coarse IsConfigured(ctx) guard(s) remain", file, n)
		}
		for c, n := range cats {
			re := regexp.MustCompile(`IsConfiguredForCategory\(ctx, \w+\.notifier, notifModels\.` + c + `\)`)
			if got := len(re.FindAllIndex(src, -1)); got != n {
				t.Errorf("%s: %d guard(s) for %s, want %d", file, got, c, n)
			}
			total += n
		}
	}
	if total != 8 {
		t.Fatalf("checklist covers %d guards, want 8", total)
	}

	// The categories the sends carry must be the auth.* constants, or an
	// operator's auth.* pattern silently misses verification and reset mail.
	src, _ := os.ReadFile("password_auth_service.go")
	for _, bare := range []string{"Category:   authModels.EmailTokenPurposeResetPassword", "Category:   authModels.EmailTokenPurposeVerifyEmail"} {
		if strings.Contains(string(src), bare) {
			t.Errorf("a send still carries a bare category: %s", bare)
		}
	}
}
```

Run: `cd backend && go test ./internal/core/auth/services/ -run TestPreflightGuards -count=1` → FAIL (8 coarse guards remain).

- [ ] **Step 2: Replace the eight guards**

Each `s.notifier == nil || !s.notifier.IsConfigured(ctx)` (or `n.notifier`) becomes `!iface.IsConfiguredForCategory(ctx, <recv>.notifier, notifModels.<Category>)` — the accessor handles nil. Exact edits:

| Site | New condition |
|---|---|
| `password_auth_service.go:256` (signup) | `if s.requireEmailVerification && !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthVerifyEmail) {` |
| `:969` (forgot password) | `if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthResetPassword) {` |
| `:1280` (resend verification) | `if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthVerifyEmail) {` |
| `:1660` (new device) | `if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthNewDeviceLogin) {` |
| `:1767` (admin invite) | `if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthAdminInvite) {` |
| `:1839` (admin-initiated reset) | `if !iface.IsConfiguredForCategory(ctx, s.notifier, notifModels.CategoryAuthResetPassword) {` |
| `suspicious_login_notifier.go:216` | `if !iface.IsConfiguredForCategory(ctx, n.notifier, notifModels.CategoryAuthSuspiciousLogin) {` |
| `:295` | `if n.policy == nil || !iface.IsConfiguredForCategory(ctx, n.notifier, notifModels.CategoryAuthAdminSuspiciousLogin) {` |

Both files already import `iface` and `notifModels`.

- [ ] **Step 3: Align the three categories** (`password_auth_service.go` ~978, ~1288, ~1847):

`Category:   authModels.EmailTokenPurposeResetPassword,` → `Category:   notifModels.CategoryAuthResetPassword,` (two sites)
`Category:   authModels.EmailTokenPurposeVerifyEmail,` → `Category:   notifModels.CategoryAuthVerifyEmail,`

(`TemplateID` values are already `auth.*`; the idempotency keys are unchanged.)

- [ ] **Step 4: Give `stubNotifier` the companion and assert the category at runtime**

In `suspicious_login_notifier_test.go` extend the stub:

```go
type stubNotifier struct {
	configured    bool
	configuredFor map[string]bool // when non-nil, the exact per-category answer
	askedFor      []string
	sends         []iface.TemplatedNotificationRequest
	sendErr       error
}

func (s *stubNotifier) IsConfiguredFor(_ context.Context, category string) bool {
	s.askedFor = append(s.askedFor, category)
	if s.configuredFor != nil {
		return s.configuredFor[category]
	}
	return s.configured
}
```

In the existing test that sets `configured: false` and asserts no send, add after the assertion:

```go
	if len(stub.askedFor) != 1 || stub.askedFor[0] != notifModels.CategoryAuthSuspiciousLogin {
		t.Fatalf("guard must ask for the category it is about to send, asked %v", stub.askedFor)
	}
```
(use the stub variable name that test declares; import `notifModels` if the test file lacks it.)

- [ ] **Step 5: Verify**

Run: `cd backend && go test ./internal/core/auth/... -count=1` → PASS (the `gateNotifier` fake needs no change: the accessor falls back to its `IsConfigured`).

- [ ] **Step 6: Document and commit**

`backend/internal/core/auth/CLAUDE.md` line ~645: replace `reports \`IsConfigured() == false\`` with `reports \`iface.IsConfiguredForCategory(ctx, notifier, "auth.verify_email") == false\` — every auth pre-flight asks for the category it is about to send (ADR-0019 D7); a fork's sender without the companion interface falls back to \`IsConfigured\``. Add one sentence: "Verification and reset sends carry `auth.verify_email` / `auth.reset_password` as their category (aligned in ADR-0019 PR 2; before that they carried the bare token-purpose strings)."

```bash
git add backend/internal/core/auth
git commit -m "feat(auth): category-aware notification pre-flight on all eight guards; auth.* categories on verify/reset sends (ADR-0019 D7)"
```

### Task 9: `sender` on `POST /v1/notifications/test` and typed errors

**Files:**
- Modify: `backend/internal/core/notification/services/notification_service.go` (`TestSendInput.Sender`, `SendTest`)
- Modify: `backend/internal/core/notification/services/notification_service_test.go`
- Modify: `backend/internal/core/notification/handlers/notification_handler.go`
- Regenerate: `backend/openapi/enterprise.json`

- [ ] **Step 1: Failing service test**

```go
func TestNotificationService_SendTest_ExplicitSender(t *testing.T) {
	k := newKit(Options{})
	res, err := k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com", Sender: "default"})
	if err != nil || res.SenderSlug != "default" || len(k.driver.sent) != 1 {
		t.Fatalf("explicit default: %+v %v", res, err)
	}
	if _, err := k.svc.SendTest(context.Background(), TestSendInput{To: "a@example.com", Sender: "nope"}); !errors.Is(err, ErrSenderNotFound) {
		t.Fatalf("unknown slug must be ErrSenderNotFound, got %v", err)
	}
	if len(k.driver.sent) != 1 {
		t.Fatal("nothing must be sent for an unknown slug")
	}
}
```

- [ ] **Step 2: Implement** — add `Sender string // profile slug; empty = the default (*) profile` to `TestSendInput`; in `SendTest` replace the first lines with:

```go
	var (
		profile SenderProfile
		err     error
	)
	if in.Sender != "" {
		profile, err = s.resolver.BySlug(ctx, in.Sender)
	} else {
		profile, err = s.resolver.Default(ctx)
	}
	if err != nil {
		de := newDispatchError(profile, err)
		return TestSendResult{Diagnostic: de.Reason}, de
	}
```

- [ ] **Step 3: Handler**

```go
type testEmailRequest struct {
	Body struct {
		To       string `json:"to" doc:"Recipient email address"`
		Subject  string `json:"subject,omitempty" doc:"Optional subject override"`
		BodyText string `json:"bodyText,omitempty" doc:"Optional body override"`
		Sender   string `json:"sender,omitempty" doc:"Sender profile slug to test; empty uses the default (*) profile"`
	}
}

type testEmailResponse struct {
	Body struct {
		Success  bool   `json:"success"`
		Provider string `json:"provider"`
		Sender   string `json:"sender" doc:"Slug of the profile that carried the message"`
		Message  string `json:"message"`
	}
}

func (h *NotificationHandler) SendTestEmail(ctx context.Context, req *testEmailRequest) (*testEmailResponse, error) {
	if req.Body.To == "" {
		return nil, huma.Error400BadRequest("recipient required", nil)
	}
	subject := req.Body.Subject
	if subject == "" {
		subject = "Orkestra test email"
	}
	body := req.Body.BodyText
	if body == "" {
		body = "This is a test email sent from the Orkestra notification module at " + time.Now().Format(time.RFC3339)
	}
	res, err := h.svc.SendTest(ctx, services.TestSendInput{To: req.Body.To, Subject: subject, BodyText: body, Sender: req.Body.Sender})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrSenderNotFound):
			return nil, errcode.NotFound(errcode.NotificationSenderNotFound, "No sender profile has that slug.")
		case errors.Is(err, services.ErrNoSenderForCategory), errors.Is(err, services.ErrUnknownDriver), errors.Is(err, services.ErrSenderNotConfigured):
			return nil, errcode.UnprocessableEntity(errcode.NotificationSenderIncomplete, "The sender profile cannot send yet: "+res.Diagnostic)
		default:
			return nil, errcode.New(http.StatusBadGateway, errcode.NotificationSendFailed, "The sender did not accept the test message: "+res.Diagnostic)
		}
	}
	resp := &testEmailResponse{}
	resp.Body.Success = true
	resp.Body.Provider = res.Provider
	resp.Body.Sender = res.SenderSlug
	resp.Body.Message = "Test email dispatched"
	return resp, nil
}
```
(imports: add `"errors"` and `"github.com/orkestra/backend/internal/shared/errcode"`.) `res.Diagnostic` is the bounded string from `describeSendError` — never vendor text — so it is safe in the detail; errquality R1 is satisfied because no `err.Error()` reaches a detail argument.

Extend `handlers/notification_handler_test.go` (PR 1) so the typed error is checked too — append to `TestSendTestEmail_HostileDriverTextNeverReachesTheResponse`, before the loop:

```go
	var ee *errcode.Error
	if !errors.As(err, &ee) || ee.Code != errcode.NotificationSendFailed || ee.Status != http.StatusBadGateway {
		t.Fatalf("want 502 %s, got %v", errcode.NotificationSendFailed, err)
	}
	texts = append(texts, ee.Detail)
	if ee.Detail != "The sender did not accept the test message: sender=_legacy err=unknown" {
		t.Fatalf("detail = %q", ee.Detail)
	}
```

- [ ] **Step 4: Regenerate the spec and verify**

Run (infra up: `cd docker && docker compose -f docker-compose.infra.yml up -d`): `cd backend && make openapi-dump && git diff --stat openapi/` → only the `notifications-test` request/response schemas change (`sender` in, `sender` out). **Declaring `email.senders` produces no spec diff** (ADR-0012 frozen shapes); any other diff is a signal to stop.

Run: `cd backend && go test ./internal/core/notification/... -count=1 && make -C .. backend-errquality` → PASS.

- [ ] **Step 5: Document and commit** — `CLAUDE.md` Admin endpoint line: `POST /v1/notifications/test` — "`{to, subject?, bodyText?, sender?}`; `sender` names a profile slug (default: the `*` profile). 404 `notification.sender_not_found`, 422 `notification.sender_incomplete` (driver unknown or a required field — secret included — missing), 502 `notification.send_failed` with the bounded diagnostic. This is the only way to prove a profile whose gap is a secret."

```bash
git add backend/internal/core/notification backend/openapi
git commit -m "feat(notification): explicit sender on the test send with typed error codes (ADR-0019 PR 2)"
```

### Task 10: Console i18n (EN/IT)

**Files:**
- Modify: `frontend-admin/src/locales/en.json` (`moduleConfig.notification`, line ~2758)
- Modify: `frontend-admin/src/locales/it.json` (same block)

Element sub-field labels resolve to the literal `Label` from the schema by construction (`helpers/configLabel.ts` derives the key from the full field key, which carries the slug), so only the group and the list field itself get entries.

- [ ] **Step 1: EN** — in `groups` add after `delivery`:

```json
        "senders": {
          "label": "Sender profiles",
          "desc": "Several senders, each with its own transport and identity, selected per message by category patterns (auth.*, crm.*, * for the default). Until one profile declares a pattern, the Delivery and Sender settings remain the single default."
        },
```
update `delivery.desc` to `"The default transport. With the noop provider, rendered mail is logged to the backend instead of sent — set the provider to SMTP to reveal the connection settings. Ignored once at least one sender profile routes a category."` and `sender.desc` to `"The addresses recipients see when the default transport is used."`. In `fields.email` add:

```json
          "senders": {
            "label": "Sender profiles",
            "desc": "Each profile is a transport and an identity. Patterns decide which categories it carries; the most specific pattern wins and * is the default. A profile without patterns is a draft and receives no mail. Once any profile declares a pattern, exactly one must declare *."
          }
```

- [ ] **Step 2: IT** — same keys:

```json
        "senders": {
          "label": "Profili mittente",
          "desc": "Più mittenti, ognuno con il proprio trasporto e la propria identità, scelti per ogni messaggio in base a pattern di categoria (auth.*, crm.*, * per il predefinito). Finché nessun profilo dichiara un pattern, le impostazioni Consegna e Mittente restano l'unico predefinito."
        },
```
`delivery.desc`: `"Il trasporto predefinito. Con il provider noop la mail renderizzata viene registrata nel backend invece di essere inviata — imposta il provider su SMTP per mostrare le impostazioni di connessione. Ignorato quando almeno un profilo mittente instrada una categoria."`; `sender.desc`: `"Gli indirizzi che i destinatari vedono quando si usa il trasporto predefinito."`

```json
          "senders": {
            "label": "Profili mittente",
            "desc": "Ogni profilo è un trasporto e un'identità. I pattern decidono quali categorie trasporta; vince il pattern più specifico e * è il predefinito. Un profilo senza pattern è una bozza e non riceve posta. Quando un profilo dichiara un pattern, esattamente uno deve dichiarare *."
          }
```

- [ ] **Step 3: Verify and commit**

Run: `cd frontend-admin && npx vitest run src/locales/parity.test.ts src/pages/admin/modules/moduleConfigI18n.dotted.test.ts` → PASS.

```bash
git add frontend-admin/src/locales/en.json frontend-admin/src/locales/it.json
git commit -m "feat(frontend-admin): i18n for the notification sender-profiles group and field (ADR-0019)"
```

### Task 11: PR 2 gate

- [ ] **Step 1:** `make ci-backend` → green. `backend-openapi-check` passes because Task 9 committed the dump. `backend-errquality` passes (no `err.Error()` in any detail argument).
- [ ] **Step 2:** `make ci-frontend-admin` → green.
- [ ] **Step 2b:** Render `docs/site` locally (Task 7 touched `docs/site/sdk/shared-iface.mdx`; nothing in CI builds the site), exactly as `docs/site/README.md` prescribes: in a fresh clone of `orkestra-docs`, `npm ci && MONOREPO_LOCAL_PATH=/path/to/orkestra npm run sync:site && CI=true npm run build`; open the SDK "shared iface" page.
- [ ] **Step 3:** Smoke against a running stack (see the `orkestra-stack` and `orkestra-api-test` skills). The 422 must be provoked **through the API**: the console blocks Save client-side on a visible required field (`smtp_host`), so it can never reach the server-side gate.

**Get a token first — the method depends on the stack's `ENV`** (`grep "^ENV=" docker/.env`; this checkout's own stack is `staging`):

```bash
API=http://localhost:3000   # or the HOST_BIND_ADDRESS the stack publishes on

# development stack: /dev/token exists. --quiet is load-bearing: without it
# T captures the script's banner and metadata, not just the JWT.
T=$(ORKESTRA_API_URL="$API" ./scripts/devtoken.sh administrator --quiet)

# staging (or any production-like ENV): /dev/token is deliberately NOT
# registered (cmd/server/main.go, internal/shared/devtoken RegisterRoutes),
# so log in as a real administrator instead. An MFA-enrolled account answers
# with an mfaToken rather than accessToken — use an admin without MFA, or
# complete the MFA step per the orkestra-api-test skill.
T=$(curl -s -X POST "$API/v1/auth/login" -H "Content-Type: application/json" \
     -d '{"email":"<administrator email>","password":"<password>"}' | jq -r .accessToken)

[ -n "$T" ] && [ "$T" != null ] || { echo "no token"; exit 1; }
H=(-H "Authorization: Bearer $T" -H "Content-Type: application/json")
E=$API/v1/admin/modules/notification/environments/production

# 1. First save of a first profile: a draft (no patterns) with smtp and no host — must SAVE (state 2).
curl -s -o /dev/null -w '%{http_code}\n' -X PATCH "$E" "${H[@]}" -d '{
  "config": {"email.senders.esp.__label": "Esp", "email.senders.esp.provider": "smtp"},
  "recordLists": [{"field": "email.senders", "create": ["esp"]}]}'          # → 200

# 2. Declare a pattern on the incomplete profile — must be REJECTED (state 3).
curl -s -X PATCH "$E" "${H[@]}" -d '{"config": {"email.senders.esp.categories": "*"}}'
#   → 422 {"code":"notification.sender_incomplete", ...}  (field email.senders.esp.smtp_host)

# 3. Complete it — saves.
curl -s -o /dev/null -w '%{http_code}\n' -X PATCH "$E" "${H[@]}" -d '{"config": {
  "email.senders.esp.categories": "*", "email.senders.esp.smtp_host": "relay.internal",
  "email.senders.esp.from_address": "no-reply@example.com"}}'                # → 200

# 4. Prove the profile end to end with an explicit sender.
curl -s -X POST $API/v1/notifications/test "${H[@]}" -d '{"to": "you@example.com", "sender": "esp"}'
#   → 200 {"success":true,"provider":"smtp","sender":"esp",...}  (or 502 notification.send_failed with a bounded reason if relay.internal is not reachable — that is also a pass for this gate)
```

Then open `/admin/modules/notification` and confirm the profile card renders with the rail badge showing nothing left to fill.
- [ ] **Step 4:** Push and open the PR:

```bash
git push -u origin feat/adr-0019-pr2-sender-profiles
gh pr create --base dev --title "feat(notification): sender profiles, routing validation, category-aware pre-flight (ADR-0019 PR 2)" --body "PR 2 of docs/superpowers/plans/2026-08-26-notification-multi-sender.md. First behavioural change: email.senders record list, three-state validation, resolver reads the roster (legacy fallback), CategoryConfiguredChecker + auth's 8 guards, sender on the test send. Also aligns verify/reset send categories to auth.* (plan deviation 4)."
```

---

# PR 3 — The `mailup` driver

Branch: after PR 2 merges, `git checkout dev && git pull && git checkout -b feat/adr-0019-pr3-mailup-driver`.

**Reference reading, done at Task 2 before writing the struct (not a spike):** the `SendMessage` request/response field names on <https://helpmailup.atlassian.net/wiki/spaces/mailupapi/pages/36342655/Transactional+Emails+using+APIs>. The endpoint (`POST https://send.mailup.com/API/v2.0/messages/sendmessage`), the auth model (SMTP+ credentials in the request body's `User` field, **not** a bearer header) and the method are settled. The struct below uses the field names as currently documented; if the page differs, change the JSON tags and the test fixture only — the success predicate and the error contract do not move.

### Task 1: Bounded response reading

**Files:**
- Modify: `backend/internal/core/notification/services/send_error.go`
- Modify: `backend/internal/core/notification/services/send_error_test.go`

**Interfaces produced:** `const maxResponseBody = 64 << 10`, `readBounded(r io.Reader, max int) (body []byte, tooLarge bool, err error)`.

- [ ] **Step 1: Failing test**

```go
// countingReader counts bytes handed out so the test can assert how much
// was READ, not merely how much was stored.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func TestReadBounded(t *testing.T) {
	// 5 MB HTML error page: rejected as too large, and at most max+1 bytes were read.
	big := &countingReader{r: strings.NewReader(strings.Repeat("<html>", 1<<20))}
	body, tooLarge, err := readBounded(big, maxResponseBody)
	if err != nil || !tooLarge || body != nil {
		t.Fatalf("want tooLarge with no body, got %v %v %d", tooLarge, err, len(body))
	}
	if big.n > maxResponseBody+1 {
		t.Fatalf("read %d bytes, must stop at %d", big.n, maxResponseBody+1)
	}

	// Exactly at the cap is fine.
	exact := strings.Repeat("a", maxResponseBody)
	body, tooLarge, err = readBounded(strings.NewReader(exact), maxResponseBody)
	if err != nil || tooLarge || len(body) != maxResponseBody {
		t.Fatalf("exactly at the cap must be accepted: %v %v %d", tooLarge, err, len(body))
	}

	// One over the cap is not.
	_, tooLarge, _ = readBounded(strings.NewReader(exact+"a"), maxResponseBody)
	if !tooLarge {
		t.Fatal("max+1 bytes must be rejected")
	}
}
```
(add `"io"` to the test imports.)

- [ ] **Step 2: Implement** (append to `send_error.go`, add `"io"` import):

```go
// maxResponseBody bounds every vendor response — success path included —
// before any parse. Generous for a JSON acknowledgement, harmless if a proxy
// answers with a page of HTML.
const maxResponseBody = 64 << 10

// readBounded reads at most max+1 bytes. Reading the one extra byte is what
// distinguishes "exactly at the cap" from "over it"; an oversized body costs
// max bytes of memory rather than an allocation the size of whatever was
// sent, and is never handed to a decoder.
func readBounded(r io.Reader, max int) (body []byte, tooLarge bool, err error) {
	b, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > max {
		return nil, true, nil
	}
	return b, false, nil
}
```

- [ ] **Step 3: Verify and commit**

Run: `cd backend && go test ./internal/core/notification/services/ -run TestReadBounded -count=1` → PASS.

```bash
git add backend/internal/core/notification/services/send_error.go backend/internal/core/notification/services/send_error_test.go
git commit -m "feat(notification): bounded vendor response reader"
```

### Task 2: The driver

**Files:**
- Modify: `backend/internal/core/notification/services/sender_profile.go` (`MailUpUser`, `MailUpSecret`, `SubMailUpUser`, `SubMailUpSecret`, `Field`/`setField` cases)
- Create: `backend/internal/core/notification/services/driver_mailup.go`
- Test: `backend/internal/core/notification/services/driver_mailup_test.go`
- Modify: `backend/internal/core/notification/services/email_driver.go` (`CoreDrivers`)

**Interfaces produced:** `NewMailUpDriver(logger) EmailDriver`, `newMailUpDriver(logger, endpoint string, client *http.Client) EmailDriver` (test seam), `mailUpSendURL`.

- [ ] **Step 1: Extend the profile**

In `sender_profile.go` add to the struct:

```go
	MailUpUser   string // SMTP+ username, sNNNNN_NN
	MailUpSecret string // SMTP+ secret
```
constants `SubMailUpUser = "mailup_user"`, `SubMailUpSecret = "mailup_secret"`; and `Field`/`setField` cases for both. Extend `TestSenderProfile_FieldRoundTrip`'s key list with the two.

- [ ] **Step 2: Write the failing driver tests**

```go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func mailUpProfile() SenderProfile {
	return SenderProfile{Slug: "mailup-sistema", Provider: "mailup", FromAddress: "sys@example.com", FromName: "Sistema",
		ReplyTo: "help@example.com", MailUpUser: "s12345_67", MailUpSecret: "hunter2-secret"}
}

func mailUpServer(t *testing.T, handler http.HandlerFunc) (EmailDriver, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return newMailUpDriver(discardLogger(), srv.URL, &http.Client{Timeout: 2 * time.Second}), srv
}

func TestMailUpDriver_Requires(t *testing.T) {
	d := NewMailUpDriver(nil)
	if d.Name() != "mailup" {
		t.Fatal(d.Name())
	}
	want := map[string]bool{SubFromAddress: false, SubMailUpUser: false, SubMailUpSecret: true}
	for _, r := range d.Requires() {
		secret, ok := want[r.Key]
		if !ok || secret != r.Secret {
			t.Fatalf("unexpected requirement %+v", r)
		}
		delete(want, r.Key)
	}
	if len(want) != 0 {
		t.Fatalf("missing requirements: %v", want)
	}
	if err := ValidateProfile(d, SenderProfile{FromAddress: "f", MailUpUser: "u"}, SaveTimeView); err != nil {
		t.Fatalf("save-time view must accept a secret-only gap: %v", err)
	}
	if err := ValidateProfile(d, SenderProfile{FromAddress: "f", MailUpUser: "u"}, RuntimeView); !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("runtime view must reject a missing secret: %v", err)
	}
}

func TestMailUpDriver_RequestShapeAndSuccess(t *testing.T) {
	var got mailUpRequest
	var method, path, ctype, auth string
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, ctype, auth = r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"done","Code":"0","Message":"","Data":{"Id":42}}`))
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{
		To: "alice@example.com", ToName: "Alice", Subject: "Hi", BodyText: "text", BodyHTML: "<p>html</p>", Category: "crm.campaign",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if method != http.MethodPost || path != "/" || ctype != "application/json" || auth != "" {
		t.Fatalf("method=%s path=%s ctype=%s auth=%q — auth rides in the body, never a header", method, path, ctype, auth)
	}
	if got.User.Username != "s12345_67" || got.User.Secret != "hunter2-secret" {
		t.Fatalf("SMTP+ credentials must be in the body's User field: %+v", got.User)
	}
	if got.Subject != "Hi" || got.Text != "text" || got.Html.Body != "<p>html</p>" {
		t.Fatalf("content: %+v", got)
	}
	if got.From.Email != "sys@example.com" || got.From.Name != "Sistema" || got.ReplyTo != "help@example.com" {
		t.Fatalf("identity: %+v", got)
	}
	if len(got.To) != 1 || got.To[0].Email != "alice@example.com" || got.To[0].Name != "Alice" {
		t.Fatalf("recipient: %+v", got.To)
	}
	if got.XSmtpAPI.CampaignCode != "crm.campaign" {
		t.Fatalf("CampaignCode must carry the category through: %+v", got.XSmtpAPI)
	}
}

// Success is an allowlist: everything that is not (2xx ∧ within limit ∧
// parses ∧ Status==done ∧ Code==0) fails with a bounded diagnostic.
func TestMailUpDriver_FailureTable(t *testing.T) {
	secret := "hunter2-secret"
	cases := []struct {
		name     string
		status   int
		ctype    string
		body     string
		wantDiag string
	}{
		{"non-2xx envelope", 401, "application/json", `{"Status":"error","Code":"401","Message":"Unauthorized user s12345_67 secret ` + secret + `"}`, "http=401 status=error code=401"},
		{"2xx with non-done status", 200, "application/json", `{"Status":"error","Code":"5","Message":"bad"}`, "http=200 status=error code=5"},
		{"2xx with non-zero code", 200, "application/json", `{"Status":"done","Code":"12","Message":""}`, "http=200 status=done code=12"},
		{"missing fields", 200, "application/json", `{"Data":{"Id":1}}`, "http=200 status= code="},
		{"empty body", 200, "application/json", ``, "http=200 body=unparseable bytes=0 type=application/json"},
		{"html error page", 502, "text/html; charset=utf-8", "<html>gateway</html>", "http=502 body=unparseable bytes=20 type=invalid"},
		{"code carrying a sentence", 200, "application/json", `{"Status":"done","Code":"the user ` + secret + ` is wrong"}`, "http=200 status=done code=invalid"},
		{"status over 64 chars", 200, "application/json", `{"Status":"` + strings.Repeat("s", 65) + `","Code":"0"}`, "http=200 status=invalid code=0"},
	}
	envelope := regexp.MustCompile(`^http=\d+ status=[A-Za-z0-9._-]{0,64} code=[A-Za-z0-9._-]{0,64}$`)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", c.ctype)
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com"})
			var se *SendError
			if !errors.As(err, &se) {
				t.Fatalf("want *SendError, got %v", err)
			}
			if se.Error() != c.wantDiag {
				t.Fatalf("diagnostic = %q, want %q", se.Error(), c.wantDiag)
			}
			if strings.Contains(c.body, "Status") && !strings.Contains(c.wantDiag, "unparseable") && !envelope.MatchString(se.Error()) {
				t.Fatalf("envelope diagnostics must match the shape, got %q", se.Error())
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "Unauthorized") || strings.Contains(err.Error(), "<html>") {
				t.Fatalf("remote text leaked: %q", err.Error())
			}
		})
	}
}

func TestMailUpDriver_OversizedBodyIsNotParsed(t *testing.T) {
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		chunk := []byte(strings.Repeat("<b>", 1024))
		for i := 0; i < 5*1024; i++ { // ~15 MB
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	err := d.Send(context.Background(), mailUpProfile(), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "http=200 body=too_large" {
		t.Fatalf("got %v", err)
	}
}

func TestMailUpDriver_TimeoutAndRefusedProfile(t *testing.T) {
	d, _ := mailUpServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := d.Send(ctx, mailUpProfile(), EmailMessage{To: "a@example.com"})
	var se *SendError
	if !errors.As(err, &se) || se.Error() != "mailup err=timeout" {
		t.Fatalf("got %v", err)
	}
	p := mailUpProfile()
	p.MailUpSecret = ""
	if err := d.Send(context.Background(), p, EmailMessage{To: "a@example.com"}); !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("incomplete profile must be refused before any request: %v", err)
	}
}
```

- [ ] **Step 3: Run to verify failure** — `cd backend && go test ./internal/core/notification/services/ -run MailUp -count=1` → FAIL (`newMailUpDriver` undefined).

- [ ] **Step 4: Write `driver_mailup.go`**

```go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// mailUpSendURL is MailUp's transactional SendMessage endpoint (SMTP+ REST).
const mailUpSendURL = "https://send.mailup.com/API/v2.0/messages/sendmessage"

// mailUpTimeout bounds the whole request. Go's default client has none, so a
// vendor that accepts a connection and never answers would hold the send
// goroutine indefinitely.
const mailUpTimeout = 30 * time.Second

// Request shape of SendMessage. Authentication rides in the body's User
// field — SMTP+ credentials — not in an Authorization header (that belongs
// to the management APIs). Field names as documented on the vendor page;
// confirm them there when touching this struct.
type mailUpRequest struct {
	User     mailUpUser      `json:"User"`
	Subject  string          `json:"Subject"`
	Html     mailUpHTML      `json:"Html"`
	Text     string          `json:"Text"`
	From     mailUpAddress   `json:"From"`
	To       []mailUpAddress `json:"To"`
	ReplyTo  string          `json:"ReplyTo,omitempty"`
	CharSet  string          `json:"CharSet"`
	XSmtpAPI mailUpXSmtpAPI  `json:"XSmtpAPI"`
}

type mailUpUser struct {
	Username string `json:"Username"`
	Secret   string `json:"Secret"`
}

type mailUpHTML struct {
	Body string `json:"Body"`
}

type mailUpAddress struct {
	Name  string `json:"Name,omitempty"`
	Email string `json:"Email"`
}

// mailUpXSmtpAPI carries the extras that ride as the X-SMTPAPI header over
// the relay. CampaignCode is what MailUp aggregates statistics by — mapped
// from EmailMessage.Category so the vendor's reporting lines up with the
// routing this design introduces. CampaignName is a distinct vendor field
// and is deliberately not set: the spec decides CampaignCode only.
type mailUpXSmtpAPI struct {
	CampaignCode string `json:"CampaignCode,omitempty"`
}

// mailUpResponse is the envelope. Only Status and Code are ever read;
// Message is deliberately not declared so it cannot be persisted by accident.
type mailUpResponse struct {
	Status string `json:"Status"`
	Code   string `json:"Code"`
}

type mailUpDriver struct {
	logger   *slog.Logger
	endpoint string
	client   *http.Client
}

// NewMailUpDriver sends through MailUp's SendMessage endpoint with an explicit client timeout.
func NewMailUpDriver(logger *slog.Logger) EmailDriver {
	return newMailUpDriver(logger, mailUpSendURL, &http.Client{Timeout: mailUpTimeout})
}

// newMailUpDriver is the test seam: an httptest.Server endpoint and a client.
func newMailUpDriver(logger *slog.Logger, endpoint string, client *http.Client) EmailDriver {
	if logger == nil {
		logger = slog.Default()
	}
	return &mailUpDriver{logger: logger, endpoint: endpoint, client: client}
}

func (d *mailUpDriver) Name() string { return "mailup" }

// Requires: identity plus both SMTP+ credentials — the API cannot function
// without them. The secret is invisible to the save-time gate (D5).
func (d *mailUpDriver) Requires() []ProfileRequirement {
	return []ProfileRequirement{{Key: SubFromAddress}, {Key: SubMailUpUser}, {Key: SubMailUpSecret, Secret: true}}
}

func (d *mailUpDriver) Send(ctx context.Context, p SenderProfile, msg EmailMessage) error {
	if err := ValidateProfile(d, p, RuntimeView); err != nil {
		return err
	}
	payload := mailUpRequest{
		User:    mailUpUser{Username: p.MailUpUser, Secret: p.MailUpSecret},
		Subject: msg.Subject,
		Html:    mailUpHTML{Body: msg.BodyHTML},
		Text:    msg.BodyText,
		From:    mailUpAddress{Name: p.FromName, Email: p.FromAddress},
		To:      []mailUpAddress{{Name: msg.ToName, Email: msg.To}},
		ReplyTo: p.ReplyTo,
		CharSet: "utf-8",
		XSmtpAPI: mailUpXSmtpAPI{CampaignCode: msg.Category},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return transportError("mailup", "", err)
	}
	// The request payload never reaches an error path below: only the
	// response is inspected, and only through the bounded, typed route.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, bytes.NewReader(body))
	if err != nil {
		return transportError("mailup", "", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return transportError("mailup", "", err)
	}
	defer resp.Body.Close()

	// Read under a bound before deciding anything. A body over the limit is
	// not parsed; its connection is closed without draining — losing
	// keep-alive on one connection is the cheaper half of that trade.
	raw, tooLarge, err := readBounded(resp.Body, maxResponseBody)
	if err != nil {
		return transportError("mailup", "read", err)
	}
	if tooLarge {
		return vendorBodyError("mailup", resp.StatusCode, bodyTooLarge, 0, "")
	}

	var env mailUpResponse
	if len(raw) == 0 || json.Unmarshal(raw, &env) != nil {
		return vendorBodyError("mailup", resp.StatusCode, bodyUnparseable, len(raw), strings.TrimSpace(resp.Header.Get("Content-Type")))
	}

	// Success is an allowlist, not the absence of an error: MailUp's WCF-
	// derived surface can answer 200 with an error envelope in the body.
	// Every other shape — including ones nobody anticipated — fails.
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300 && env.Status == "done" && env.Code == "0"
	if !ok {
		return vendorEnvelopeError("mailup", resp.StatusCode, env.Status, env.Code)
	}
	d.logger.Info("notification.email accepted",
		slog.String("to", msg.To),
		slog.String("subject", msg.Subject),
		slog.String("provider", "mailup"),
	)
	return nil
}
```

Register it: `CoreDrivers` returns `[]EmailDriver{NewNoopDriver(logger), NewSMTPDriver(logger), NewMailUpDriver(logger)}`.

- [ ] **Step 5: Verify**

Run: `cd backend && go test ./internal/core/notification/services/ -count=1` → PASS. If a JSON field name on the vendor page differs from the struct, change the tag and the corresponding assertion in `TestMailUpDriver_RequestShapeAndSuccess`; nothing else.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/core/notification/services
git commit -m "feat(notification): mailup driver — SendMessage over SMTP+ credentials, allowlisted success, bounded diagnostics (ADR-0019 PR 3)"
```

### Task 3: Schema, agreement test, docs

**Files:**
- Modify: `backend/internal/core/notification/services/sender_config.go` (`SenderItems`)
- Modify: `backend/internal/core/notification/services/sender_validation_test.go` (one case)
- Modify: `backend/internal/core/notification/CLAUDE.md`

- [ ] **Step 1: Failing validation case** — add to `TestValidateSenderConfig_ThreeStates`:

```go
		{"routing mailup profile missing user",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "mailup", SubCategories: "*", SubFromAddress: "f@x"}}),
			errcode.NotificationSenderIncomplete, module.ItemKey(SendersField, "a", SubMailUpUser)},
		{"routing mailup profile missing only its secret saves (secret-blind)",
			profileValues([]string{"a"}, map[string]map[string]string{"a": {SubProvider: "mailup", SubCategories: "*", SubFromAddress: "f@x", SubMailUpUser: "s1_2"}}), "", ""},
```
Run → the first fails with `sender_unknown_driver`? No — `CoreDrivers` already includes mailup; it fails because `mailup_user` is not a schema item (`DecodeSenderProfiles` never sets it). Good: that is what Step 2 fixes.

- [ ] **Step 2: Extend `SenderItems`**

`provider` options → `[]string{"noop", "smtp", "mailup"}`; `identity` condition → `In: []string{"smtp", "mailup"}` (one condition, two values — an OR within the entry per ADR-0012, no `DependsOnMatch` needed); append:

```go
		{Key: SubMailUpUser, Label: "MailUp SMTP+ username", Type: module.FieldString, Required: true, Placeholder: "s12345_67",
			DependsOn: []module.FieldCondition{{Key: SubProvider, In: []string{"mailup"}}},
			Description: "An SMTP+ user created in the MailUp console after authorizing a trusted sender.",
			HelpURL:     "https://helpmailup.atlassian.net/wiki/spaces/mailupapi/pages/36342655/Transactional+Emails+using+APIs"},
		{Key: SubMailUpSecret, Label: "MailUp SMTP+ secret", Type: module.FieldSecret, Required: true,
			DependsOn: []module.FieldCondition{{Key: SubProvider, In: []string{"mailup"}}}},
```
`Required` on the secret is a console-side hint only — the rail badges it as "to fill", which is one of D5's three mitigations.

- [ ] **Step 3: Verify** — `cd backend && go test ./internal/core/notification/... ./cmd/server/ -count=1` → PASS, including `TestSenderItems_SchemaDriverAgreement` (mailup: `from_address`, `mailup_user` required-and-visible; the secret is excluded from the non-secret set) and `TestConfigDeclarationsAreValid` (the `In: ["mailup"]` value is in `Options`).

- [ ] **Step 4: Docs + gate + PR**

`CLAUDE.md`, driver requirements line: add "`mailup` `from_address`, `mailup_user`, `mailup_secret` (secret). Sends `POST https://send.mailup.com/API/v2.0/messages/sendmessage` with the SMTP+ credentials in the body's `User` field; `CampaignCode = Category`. **Success ⇔ 2xx ∧ body ≤ 64 KiB ∧ parses ∧ `Status=="done"` ∧ `Code=="0"`** — anything else fails with `http=<n> status=<tok> code=<tok>`, `body=too_large`, or `body=unparseable bytes=<n> type=<media>`; the vendor's `Message` is never read. A success means *accepted*, not delivered." Add to the Settings table the two `email.senders.<slug>.mailup_*` sub-fields.

Run `make ci-backend` → green; `backend-openapi-check`: no diff (data against frozen shapes).

```bash
git add backend/internal/core/notification
git commit -m "feat(notification): mailup provider in the sender-profile schema"
git push -u origin feat/adr-0019-pr3-mailup-driver
gh pr create --base dev --title "feat(notification): mailup driver (ADR-0019 PR 3)" --body "PR 3 of docs/superpowers/plans/2026-08-26-notification-multi-sender.md. One file behind the registry; success is an allowlist; no vendor text persisted."
```

---

# PR 4 — `senderSlug` on the delivery log, sender filter

Branch: after PR 3 merges, `git checkout dev && git pull && git checkout -b feat/adr-0019-pr4-delivery-log-sender`.

### Task 1: Persist and serve the sender slug; filter by it

**Files:**
- Modify: `backend/internal/core/notification/models/notification.go` (`NotificationDoc`)
- Modify: `backend/internal/core/notification/repository/notification_repository.go` (`Filter`, `List`)
- Modify: `backend/internal/core/notification/services/notification_service.go` (`dispatchEmail`, `failSend`)
- Modify: `backend/internal/core/notification/services/notification_service_test.go`
- Modify: `backend/internal/core/notification/handlers/notification_handler.go` (`listNotificationsRequest`)
- Modify: `backend/internal/core/notification/module.go` (index)
- Modify: `backend/tools/tenantscope/baseline.txt` (remove three rows)
- Regenerate: `backend/openapi/enterprise.json`

- [ ] **Step 1: Failing test** — in `notification_service_test.go`:

```go
func TestNotificationService_Dispatch_StampsSenderSlug(t *testing.T) {
	k := newKit(Options{})
	k.resolver.profile.Slug = "esp-campagne"
	if _, err := sendOne(t, k); err != nil {
		t.Fatal(err)
	}
	if k.logRepo.created[0].SenderSlug != "esp-campagne" {
		t.Fatalf("sent row must carry the sender slug: %+v", k.logRepo.created[0])
	}
	k.driver.sendErr = errors.New("boom")
	_, _ = sendOne(t, k)
	if k.logRepo.created[1].SenderSlug != "esp-campagne" || !strings.HasPrefix(k.logRepo.created[1].Error, "sender=esp-campagne ") {
		t.Fatalf("failed row must carry the slug in both the field and the reason: %+v", k.logRepo.created[1])
	}
	k.resolver.err = ErrNoSenderForCategory
	_, _ = sendOne(t, k)
	if k.logRepo.created[2].SenderSlug != "" {
		t.Fatalf("no resolved profile ⇒ no slug: %+v", k.logRepo.created[2])
	}
}
```
Run → FAIL (`SenderSlug` undefined).

- [ ] **Step 2: Model + service**

`NotificationDoc`, after `Provider`:

```go
	SenderSlug        string             `bson:"senderSlug,omitempty" json:"senderSlug,omitempty"`
```

`dispatchEmail` success branch: add `logDoc.SenderSlug = profile.Slug` next to `logDoc.Provider = profile.Provider`. `failSend`: add `logDoc.SenderSlug = profile.Slug` (empty when no profile resolved).

- [ ] **Step 3: Repository filter**

`Filter` gains `SenderSlug string`; in `List` add after the `Channel` branch:

```go
	if filter.SenderSlug != "" {
		q["senderSlug"] = filter.SenderSlug
	}
```

The three Mongo calls in this file are in `tools/tenantscope/baseline.txt` **by line number**, which this edit shifts. Per `tools/tenantscope/CLAUDE.md`, do not edit the baseline by hand except to delete rows; add inline allow-comments on the line immediately above each call and delete the three rows (`notification_repository.go:56:FindOne`, `:86:Find`, `:106:FindOne`):

```go
	//tenantscope:allow system: the delivery log is platform-global; idempotency keys are minted per send, not per tenant
	err := r.coll.FindOne(ctx, bson.M{...}).Decode(&doc)      // FindByIdempotencyKey
```
```go
	//tenantscope:allow admin-view: operator delivery log spans every tenant (notification.log.read)
	cursor, err := r.coll.Find(ctx, q, ...)                     // List
```
```go
	//tenantscope:allow admin-view: operator delivery log lookup by message UUID
	err := r.coll.FindOne(ctx, bson.M{"uuid": uuid}).Decode(&doc)  // GetByUUID
```

`module.go` `Collections()`, `notification_messages` indexes: add `{Keys: map[string]int{"senderSlug": 1}, Sparse: true},`.

- [ ] **Step 4: Handler**

```go
type listNotificationsRequest struct {
	Category string `query:"category" doc:"Filter by category"`
	Status   string `query:"status" doc:"Filter by status"`
	Sender   string `query:"sender" doc:"Filter by sender profile slug"`
	Limit    int64  `query:"limit" doc:"Max rows (default 100)"`
}
```
and pass `SenderSlug: req.Sender` in the `repository.Filter`.

- [ ] **Step 5: Verify, dump, gate**

Run: `cd backend && go test ./internal/core/notification/... -count=1 && make -C .. backend-tenantscope` → PASS.
Run: `cd backend && make openapi-dump && git diff --stat openapi/` → `notifications-list` gains the `sender` query parameter; `NotificationDoc` gains `senderSlug`. Nothing else.
Run: `make ci-backend` → green.

- [ ] **Step 6: Docs + commit + PR**

`CLAUDE.md`: collections table — `notification_messages` indexes add `senderSlug` (sparse); Admin endpoints — `GET /v1/notifications` "filters: `category`, `status`, `sender` (profile slug); every row carries `provider` and `senderSlug`, so *which* profile sent or refused a message is answerable per row". Sender profiles section: "`logDoc.SenderSlug = profile.Slug` (empty when no profile resolved)".

```bash
git add backend/internal/core/notification backend/openapi backend/tools/tenantscope/baseline.txt
git commit -m "feat(notification): senderSlug on the delivery log + sender filter (ADR-0019 PR 4)"
git push -u origin feat/adr-0019-pr4-delivery-log-sender
gh pr create --base dev --title "feat(notification): sender slug on the delivery log (ADR-0019 PR 4)" --body "PR 4 of docs/superpowers/plans/2026-08-26-notification-multi-sender.md. Diagnostics only: senderSlug field + ?sender= filter. The console has no delivery-log page, so the column lands when one exists (plan deviation 5)."
```

---

# PR 5 — Docs site

Branch: after PR 4 merges, `git checkout dev && git pull && git checkout -b docs/adr-0019-pr5-site-pages`. Nothing in this repo's CI builds the site: **render locally before merging**, per `docs/site/README.md` — in a fresh clone of `orkestra-docs`: `npm ci && MONOREPO_LOCAL_PATH=/path/to/orkestra npm run sync:site && CI=true npm run build` (`onBrokenLinks: 'throw'` is the check).

### Task 1: `docs/site/modules/core/notification.mdx`

- [ ] **Step 1:** Replace the `## Config` section with:

```markdown
## Config

Thirteen fields: twelve seeded from env vars on first boot and owned by the ConfigService afterwards, plus one record list. `/admin/modules/notification` renders them as a four-group rail:

| Group | Keys |
|---|---|
| **Delivery** | `email.provider` plus the five `email.smtp.*` fields — the **default transport**, used while no sender profile routes a category |
| **Sender profiles** | `email.senders` — a repeatable list of senders, each with its own transport and identity ([ADR-0019](/adr/0019-notification-multi-sender)) |
| **Sender** | `email.from_address`, `email.from_name`, `email.reply_to` — the identity of the default transport |
| **Branding & templates** | `app.name`, `app.support_email`, `app.default_locale` |

The SMTP fields carry a `DependsOn` on `email.provider`, so a default `noop` install shows **one** visible Delivery field until you switch the provider. `email.smtp.password` and every profile's secret are `FieldSecret`: AES-256-GCM at rest, never read from plain env after bootstrap.

## Sender profiles

Email reputation is scored per domain and per IP, and bulk marketing and password resets do not belong on the same one. A **sender profile** is a transport **and** an identity — provider, credentials, from-address — and each profile declares the **category patterns** it carries:

| Pattern | Matches |
|---|---|
| `*` | everything — the default; exactly one profile must declare it once any profile routes |
| `auth.verify_email` | that category only |
| `auth.*` | any category beginning `auth.` at any depth — never the bare `auth` |

The most specific match wins (`auth.verify_email` beats `auth.*` beats `*`); a profile with no patterns is a **draft** that receives no mail. Resolution is **fail-closed**: a category no profile matches is not silently rerouted through the default — the send fails and the delivery log names the reason. Callers never see any of this: `iface.NotificationSender` is unchanged, and which sender carries which mail is an operator decision made on a screen.

Three drivers ship in the base: `noop`, `smtp` (an unauthenticated internal relay is a supported configuration — username and password are optional), and `mailup` (MailUp's SMTP+ REST API; the SMTP+ user and secret are required, and the category becomes the vendor's `CampaignCode`).

**The flat keys are still the environment-bootstrap path.** Record-list elements are never seeded from env vars, so a stack configured through `SMTP_HOST` keeps working: until some profile declares a pattern — the list is empty, or holds only drafts — the module synthesizes a profile (slug `_legacy`, a name no list element can take) from `email.provider`, `email.smtp.*` and `email.from_*`, routing `*`. Creating a first draft, or removing the last pattern, never changes which sender carries mail. Nothing migrates and nothing needs rolling back.

**Validation** runs on every save and before an environment is activated, but only once a profile actually declares a pattern — a legacy install and a first, pattern-less profile are never blocked. It rejects a malformed pattern, a missing or duplicate `*`, a pattern claimed by two profiles, an unknown provider, and a routing profile missing a non-secret field its driver needs (`422`, codes `notification.sender_*`). It **cannot see secrets**: a MailUp profile missing only its secret saves and fails at send. Prove a profile with `POST /v1/notifications/test` and an explicit `sender`.

**Pre-flight.** `IsConfigured` still answers only for the default profile. A consumer about to send should ask `iface.IsConfiguredForCategory(ctx, sender, category)`, which is exact for the core sender and falls back to `IsConfigured` for a fork's own implementation. Every `auth` guard does this.

**Delivery-log errors are bounded.** No string produced by a remote peer is ever stored: an SMTP rejection keeps only `smtp op=auth code=535`, a MailUp refusal `http=200 status=error code=5` — the server's text is dropped because a relay can echo the `AUTH` argument, and MailUp carries its credentials in the request body. Every row also carries `provider` and `senderSlug`, so "which sender failed" is answerable per message.
```

- [ ] **Step 2:** In `## Routes`, the Admin row: `GET /v1/notifications` (delivery log; filters `category`, `status`, `sender`), `POST /v1/notifications/test` (`{to, sender?}` — proves one profile end to end). Replace the sentence below the table with: "`POST /v1/notifications/test` sends through the default profile, or the profile named by `sender` — the only check that sees a profile's secret before real mail depends on it."

### Task 2: `docs/site/operating/notifications.mdx`

- [ ] **Step 1:** Replace everything from `## Switching to real delivery` through the end of `## Provider settings` (keep the provider table) with:

```markdown
## Switching to real delivery

The quickest path is still one relay for everything:

1. Open `/admin/modules/notification`.
2. Under **Delivery**, change **Email provider** from `noop` to `smtp`; the SMTP connection fields appear only at this point.
3. Fill in host, port and TLS mode; username and password only if your relay authenticates.
4. Under **Sender**, set the from-address and display name.
5. Save — it takes effect immediately — and send yourself a message with `POST /v1/notifications/test`.

This configures the **legacy default sender**: until a profile under **Sender profiles** declares a pattern, these settings are the one profile every message uses (it appears in the delivery log as `senderSlug=_legacy`), and a stack that sets `SMTP_HOST` in `docker-compose` is configured the same way. Adding a profile without patterns changes nothing yet.

## Separating transactional from marketing mail

Reputation is scored per domain and per IP, and vendors enforce the split contractually (MailUp's SMTP+ terms: *do not use it for promotional emails*). Once a fork sends campaigns, give each workload its own sender:

1. Under **Sender profiles**, add a profile — say *Transactional* — pick its provider, fill its identity and credentials, and give it the pattern `*`. It is now the default for everything.
2. Add a second profile — *Campaigns* — on its own domain and relay, with the patterns your campaign categories use: `crm.*`, `marketing`. (A category with no dot, like `marketing`, is matched only by the exact pattern; `marketing.*` would not capture it.)
3. Optionally pin the access path explicitly: a third profile with `auth.*` carries verification, reset and security mail even if someone later edits the default.
4. Save. The most specific pattern wins, so `auth.verify_email` goes to the `auth.*` profile, `crm.campaign` to `crm.*`, and anything else to `*`.
5. Prove each profile: `POST /v1/notifications/test` with `{"to": "you@example.com", "sender": "<slug>"}`. This is the only check that exercises a profile's secret before real mail depends on it.

A profile with no patterns is a **draft** — it receives nothing and is not validated beyond pattern grammar, so you can prepare a sender before routing traffic to it. Once any profile declares a pattern, exactly one must declare `*`; saving is refused otherwise (`notification.sender_no_default`).

**Nothing is silently rerouted.** If a category matches no profile, or its profile is incomplete, the send **fails** and the delivery log row says why (`sender=campaigns driver=mailup err=not_configured missing=mailup_user`). Falling back to the default would push promotional mail through the transactional sender — the thing this feature exists to prevent.

## Providers

`smtp` covers every hosted relay below and any internal MTA — credentials are optional, so an unauthenticated relay on a private network is a first-class setup. `mailup` talks to MailUp's SMTP+ API directly: create an SMTP+ user in the MailUp console after authorizing a trusted sender, and enter its username (`s12345_67`) and secret; the notification category is reported to MailUp as the `CampaignCode`, so their statistics line up with your routing. A MailUp "success" means *accepted*, not delivered — the same guarantee an SMTP `250` gives.

Every hosted relay maps onto the same SMTP fields. Confirm the host against your provider's current documentation — these are stable but not ours to guarantee.
```

- [ ] **Step 2:** In `## When mail does not arrive`, replace item 2 with:

```markdown
2. **Check the delivery log** at `GET /v1/notifications` — filter by `category`, `status`, or `sender`. A `failed` row names the profile and the reason (`sender=… smtp op=auth code=535`, `http=401 status=error code=401`, `err=no_sender_for_category`); Orkestra tried and the sender refused. No row at all means the send was never attempted — usually a pre-flight guard found the category's profile unusable, which for `auth` mail surfaces as a 503 on signup.
```
and add item 6: "**With several profiles, check which one carried it.** `senderSlug` on the row tells you; a category that matched a draft-less default when you expected `crm.*` means the pattern is misspelled — a pattern with no dot matches exactly, and `crm.*` never matches the bare `crm`."

### Task 3: Render, review, commit

- [ ] **Step 1:** Render locally per `docs/site/README.md`; open both pages and the ADR page; fix any MDX error.
- [ ] **Step 2:** Commit and PR:

```bash
git add docs/site/modules/core/notification.mdx docs/site/operating/notifications.mdx
git commit -m "docs(site): notification sender profiles, routing, drivers and diagnostics (ADR-0019 PR 5)"
git push -u origin docs/adr-0019-pr5-site-pages
gh pr create --base dev --title "docs(site): notification multi-sender pages (ADR-0019 PR 5)" --body "PR 5 of docs/superpowers/plans/2026-08-26-notification-multi-sender.md. Rendered locally; publishes on the next push to main via docs-dispatch."
```

---

## Self-review against the spec

| Spec section | Where |
|---|---|
| Components (`SenderProfile`, `EmailDriver` + registry, `SenderResolver`, modified `dispatchEmail`) | PR 1 Tasks 1, 5, 6 |
| `EmailMessage.Category` only; no `Type`/`MessageUUID` | PR 1 Task 1; Global Constraints |
| `emailService` retires inside the smtp driver; credentials from the profile | PR 1 Task 4 |
| Config shape (every `Required` scoped by its driver's `DependsOn`; `from_address` one condition two values; `noop` needs no from) | PR 2 Task 3, PR 3 Task 3, agreement test PR 2 Task 6 |
| Send flow, every fail-closed path writes a failed row | PR 1 Task 6 (`failSend`) |
| Matching grammar, normalization, dedup before conflict check, precedence, no ties | PR 2 Tasks 2, 4, 5 |
| Legacy flat keys as bootstrap; no routing map ⇒ synthesized `default`; drafts never strand mail | PR 1 Tasks 5, 7, 8; PR 2 Task 4 (`hasRoutingMap`, `TestSenderResolver_DraftsOnlyKeepsLegacy`), Task 6 loader |
| Byte-identical wire output under D6 | PR 1 Task 4 Step 0 golden + `TestBuildMIMEMessage_ByteIdentical` |
| Validation: declaration / save / send; three states; drafts exempt; grammar exception; secret-blind limit documented | PR 2 Tasks 5, 6 (`TestValidateSenderConfig_IsSecretBlind`) |
| Driver validation contract (anonymous relay) | PR 1 Task 4 (`TestSMTPDriver_Requires`), PR 1 Task 8 |
| Driver error contract: typed `SendError`, chokepoint renders through allowlists, bounded read, timeout, caps, SMTP `Msg` dropped, nothing logged; raw driver errors never returned to callers | PR 1 Task 2 (`TestSendError_FreeTextCannotReachTheDiagnostic`), Task 6 (`DispatchError`, `TestNotificationService_Dispatch_HostileDriverErrorNeverEscapes`), Task 7 handler test; Task 4; PR 3 Tasks 1, 2 |
| D4 `TenantID` through the chokepoint and the pre-flight | PR 1 Task 6 (`ctxauth.GetTenantID`, dispatch-path test); PR 2 Task 7 |
| Per-environment consistency of values and secrets (D4) | PR 1 Task 5 (`NewSnapshotLoader`, `TestSnapshotLoader_ValuesAndSecretsComeFromOneEnvironment`); PR 2 Task 6 (`TestSnapshotLoader_RosterFollowsTheSameSnapshot`) |
| Malformed category fails closed | PR 2 Task 4 (`TestSenderResolver_MalformedCategoryFailsClosed`), Task 7 |
| Pre-flight: companion interface, accessor fallback, eight `auth` guards | PR 2 Tasks 7, 8 |
| MailUp: endpoint, body auth, `CampaignCode`, success allowlist, failure table, timeout | PR 3 Task 2 |
| `sender` on the test send | PR 2 Task 9 |
| `senderSlug` + filter | PR 4 |
| Docs site pages | PR 5 |
| Gates: OpenAPI (no diff PR 1/3; dump PR 2/4), errquality, coverage, docs render | each PR's gate task |

Type consistency checked: `ValidateProfile(d, p, view)`, `ProfileRequirement{Key, Secret}`, `SenderConfig{Profiles, Legacy, Err}`, `SenderResolver{Resolve, Default, BySlug}`, `hasRoutingMap(profiles)`, `SendError` typed fields + the four constructors, `TestSendInput{To, Subject, BodyText, Sender}`, `TestSendResult{Provider, SenderSlug, Diagnostic}`, `describeSendError(p, err)`, `newDispatchError(p, err)`, `ErrSendFailed`, `NewSnapshotLoader(get)`, `DecodeSenderProfiles(values, encrypted) ([]SenderProfile, error)`, `LegacySlug = "_legacy"`, `readBounded(r, max)`, `buildMIMEMessageAt(p, msg, now)`, `Sub*` keys and `SendersField` are used with the same names in every PR.
