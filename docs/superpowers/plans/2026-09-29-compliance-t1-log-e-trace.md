# Compliance T1 — Log e trace: piano di implementazione

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mascherare ogni record di log e ogni span in uscita secondo una policy di contenuto (per tenant quando il tenant è noto, la più restrittiva altrimenti), con tenant, utente e route template sulla riga `http_request`, e un gate di CI contro le chiamate `slog` non mascherabili.

**Architecture:** I tipi della policy di contenuto vivono in `pkg/sdk/iface`. `internal/shared/utils` aggiunge un `PolicyHandler` nella catena slog (dopo il filtro di livello, prima del fan-out) con un risolutore sostituibile a caldo, come `SwapLevelResolver`. `internal/shared/telemetry` avvolge l'exporter OTLP con lo stesso mascheratore. `RequestLogger` riceve il principal tramite annotazioni di richiesta in `ctxauth`, riempite dai middleware di autenticazione. In questa tappa il risolutore è statico, con i default di piattaforma; la policy viva per tenant arriva con la T2.

**Tech Stack:** Go 1.26.8, `log/slog`, `crypto/hkdf`, OpenTelemetry SDK (`sdk/trace`, `tracetest`), `golang.org/x/tools/go/packages`, Prometheus client.

**Spec:** [`docs/superpowers/specs/2026-09-29-compliance-policy-engine-design.md`](../specs/2026-09-29-compliance-policy-engine-design.md), revisione 2: §2 (log e trace), §9 (metrica dei panic), tappa **T1** di §13. Chi esegue legge la spec.

## Global Constraints

- Un solo modulo Go (`backend/go.mod`): niente `go.mod` satellite, `replace` o `go.work` (ADR-0006). **Nessuna nuova dipendenza** (`tracetest` fa parte di `go.opentelemetry.io/otel/sdk`, `x/tools` è già richiesto).
- Valori della spec da non cambiare: default `ipAddress=truncated`, `userAgent=full`, `subjectIds=uuid`, `scanFreeText=true`; `truncated` = IPv4 /24, IPv6 /48; hash `h:` + 16 caratteri esadecimali di HMAC-SHA256 con domini `ip:` e `sub:`; chiave HKDF-SHA256 da `OAUTH_TOKEN_ENCRYPTION_KEY` con info `orkestra/log-hash/v1`; cache degli handler derivati al massimo 32 voci; costo < 2 µs per record con 10 attributi senza `scanFreeText`.
- Le chiavi di dati personali si confrontano **esattamente** (dopo normalizzazione); i segreti per «contiene».
- Metriche con namespace `orkestra`, senza tenant né utente nelle label (ADR-0002).
- Si lavora sul branch `feat/compliance-t1-log-trace`, creato da `docs/compliance-policy-engine` (`git switch -c feat/compliance-t1-log-trace` prima del Task 1).
- Ogni commit: Conventional Commit in italiano; in fondo le righe `Prop: upstream` e `Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU`; mai `[skip ci]` letterale.

## Review Focus

1. **`RemoteAddr` con porta** (`203.0.113.7:51234`, `[2001:db8::1]:443`): in `truncated` e `hashed` si toglie la porta prima; mai `[REDACTED]` su un indirizzo valido. Test nel Task 5.
2. **Riga di log senza contesto**: usa la policy più restrittiva, non quella di piattaforma. Test nel Task 6.
3. **Un nuovo percorso di autenticazione che dimentica le annotazioni**: la riga `http_request` perderebbe di nuovo il tenant. Test AST nel Task 3.
4. **Path reale con un'email o un UUID quando esiste un template**: deve comparire solo `route`. Test nel Task 4.
5. **Span con URL completo o query string**: non devono mai uscire dal processo. Test nel Task 7.

## Mappa dei file

- Create: `backend/pkg/sdk/iface/compliance_policy.go` — tipi e ordine di restrittività (Task 1)
- Create: `backend/internal/shared/redact/redact.go` — chiavi e segreti condivisi (Task 2)
- Modify: `backend/internal/core/logging/logquery/redact.go` (Task 2)
- Create: `backend/pkg/sdk/ctxauth/annotations.go` (Task 3)
- Create: `backend/internal/shared/middleware/request_annotations.go`; Modify: `request_logger.go`, `auth.go`, `jwt_validator.go`, `audience.go` (Task 3, 4)
- Create: `backend/internal/shared/utils/log_masker.go` (Task 5, 7)
- Create: `backend/internal/shared/utils/policy_handler.go`; Modify: `logger.go` (Task 6)
- Modify: `backend/pkg/sdk/metrics/metrics.go`, `backend/cmd/server/main.go` (Task 6)
- Create: `backend/internal/shared/telemetry/masking_exporter.go`; Modify: `tracer.go` (Task 7)
- Create: `backend/tools/logscope/` (Task 8); Modify: `Makefile` (Task 8)
- Modify: `docker/grafana/provisioning/dashboards/logs-explorer.json` (Task 4)
- Modify: `backend/internal/core/logging/AGENTS.md`, `docs/site/modules/core/logging.mdx`, `AGENTS.md` (Task 9)

## Tappe successive

T2 (motore di policy), T3 (evidenza di audit), T4 (retention), T5 (sink), T6 (evidenze firmate, monitoraggio, documentazione) hanno ciascuna un piano proprio, scritto quando la tappa precedente da cui dipendono è approvata (spec §13).

---

### Task 1: Tipi della policy di contenuto e ordine di restrittività (`iface`)

**Files:**
- Create: `backend/pkg/sdk/iface/compliance_policy.go`
- Test: `backend/pkg/sdk/iface/compliance_policy_test.go`

**Interfaces:**
- Consumes: niente.
- Produces:
  - `type IPAddressMode string` con `IPAddressFull|IPAddressTruncated|IPAddressHashed|IPAddressOmitted`, `func (IPAddressMode) Valid() bool`
  - `type UserAgentMode string` con `UserAgentFull|UserAgentOmitted`, `Valid()`
  - `type SubjectIDMode string` con `SubjectIDUUID|SubjectIDHashed|SubjectIDOmitted`, `Valid()`
  - `type LogContentPolicy struct{ IPAddress IPAddressMode; UserAgent UserAgentMode; SubjectIDs SubjectIDMode; PIIKeys []string; ScanFreeText bool }` (tag bson/json come nella spec §1.1)
  - `func DefaultPIIKeys() []string`, `func DefaultLogContentPolicy() LogContentPolicy`
  - `func MostRestrictive(a, b LogContentPolicy) LogContentPolicy`
  - `func LessRestrictiveContentFields(candidate, baseline LogContentPolicy) []string` (la T2 aggiunge il confronto sulla retention)
  - costanti di campo `FieldIPAddress="logContent.ipAddress"`, `FieldUserAgent`, `FieldSubjectIDs`, `FieldPIIKeys`, `FieldScanFreeText`
  - (`CompliancePolicyProvider` e i tipi di retention arrivano con la T2)

- [ ] **Step 1: Scrivere il test che fallisce**

```go
package iface

import (
	"slices"
	"testing"
)

func TestModesValid(t *testing.T) {
	if !IPAddressHashed.Valid() || IPAddressMode("partial").Valid() {
		t.Fatal("IPAddressMode.Valid")
	}
	if !UserAgentOmitted.Valid() || UserAgentMode("").Valid() {
		t.Fatal("UserAgentMode.Valid")
	}
	if !SubjectIDHashed.Valid() || SubjectIDMode("email").Valid() {
		t.Fatal("SubjectIDMode.Valid")
	}
}

func TestDefaultLogContentPolicy(t *testing.T) {
	p := DefaultLogContentPolicy()
	if p.IPAddress != IPAddressTruncated || p.UserAgent != UserAgentFull ||
		p.SubjectIDs != SubjectIDUUID || !p.ScanFreeText {
		t.Fatalf("unexpected defaults: %+v", p)
	}
	if !slices.IsSorted(p.PIIKeys) || !slices.Contains(p.PIIKeys, "email") ||
		slices.Contains(p.PIIKeys, "name") {
		t.Fatalf("pii keys must be sorted, include email, exclude bare name: %v", p.PIIKeys)
	}
	// The slice must be a fresh copy: mutating it cannot leak into the next call.
	p.PIIKeys[0] = "zzz"
	if DefaultLogContentPolicy().PIIKeys[0] == "zzz" {
		t.Fatal("DefaultLogContentPolicy shares its PIIKeys slice")
	}
}

func TestMostRestrictive(t *testing.T) {
	a := LogContentPolicy{IPAddress: IPAddressHashed, UserAgent: UserAgentFull, SubjectIDs: SubjectIDUUID, PIIKeys: []string{"email", "phone"}, ScanFreeText: false}
	b := LogContentPolicy{IPAddress: IPAddressTruncated, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDOmitted, PIIKeys: []string{"iban", "email"}, ScanFreeText: true}
	got := MostRestrictive(a, b)
	want := LogContentPolicy{IPAddress: IPAddressHashed, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDOmitted, PIIKeys: []string{"email", "iban", "phone"}, ScanFreeText: true}
	if got.IPAddress != want.IPAddress || got.UserAgent != want.UserAgent ||
		got.SubjectIDs != want.SubjectIDs || got.ScanFreeText != want.ScanFreeText ||
		!slices.Equal(got.PIIKeys, want.PIIKeys) {
		t.Fatalf("MostRestrictive = %+v, want %+v", got, want)
	}
	if !slices.Equal(a.PIIKeys, []string{"email", "phone"}) {
		t.Fatal("MostRestrictive mutated its input")
	}
}

func TestLessRestrictiveContentFields(t *testing.T) {
	base := LogContentPolicy{IPAddress: IPAddressTruncated, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDHashed, PIIKeys: []string{"email", "iban"}, ScanFreeText: true}
	same := base
	if got := LessRestrictiveContentFields(same, base); len(got) != 0 {
		t.Fatalf("identical policy reported %v", got)
	}
	looser := LogContentPolicy{IPAddress: IPAddressFull, UserAgent: UserAgentFull, SubjectIDs: SubjectIDUUID, PIIKeys: []string{"email"}, ScanFreeText: false}
	got := LessRestrictiveContentFields(looser, base)
	want := []string{FieldIPAddress, FieldUserAgent, FieldSubjectIDs, FieldPIIKeys, FieldScanFreeText}
	if !slices.Equal(got, want) {
		t.Fatalf("LessRestrictiveContentFields = %v, want %v", got, want)
	}
	stricter := LogContentPolicy{IPAddress: IPAddressOmitted, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDOmitted, PIIKeys: []string{"email", "iban", "phone"}, ScanFreeText: true}
	if got := LessRestrictiveContentFields(stricter, base); len(got) != 0 {
		t.Fatalf("stricter policy reported %v", got)
	}
}
```

- [ ] **Step 2: Eseguire il test e verificare che fallisca**

Run: `cd backend && go test ./pkg/sdk/iface/ -run 'TestModesValid|TestDefaultLogContentPolicy|TestMostRestrictive|TestLessRestrictiveContentFields'`
Expected: FAIL di compilazione, `undefined: IPAddressHashed`.

- [ ] **Step 3: Scrivere l'implementazione**

```go
package iface

import "slices"

// IPAddressMode is how a compliance policy treats client IP addresses in
// operational logs (spec §1.1).
type IPAddressMode string

const (
	IPAddressFull      IPAddressMode = "full"
	IPAddressTruncated IPAddressMode = "truncated"
	IPAddressHashed    IPAddressMode = "hashed"
	IPAddressOmitted   IPAddressMode = "omitted"
)

// UserAgentMode is how a compliance policy treats user agents in logs.
type UserAgentMode string

const (
	UserAgentFull    UserAgentMode = "full"
	UserAgentOmitted UserAgentMode = "omitted"
)

// SubjectIDMode is how a compliance policy treats user identifiers in logs.
type SubjectIDMode string

const (
	SubjectIDUUID    SubjectIDMode = "uuid"
	SubjectIDHashed  SubjectIDMode = "hashed"
	SubjectIDOmitted SubjectIDMode = "omitted"
)

// LogContentPolicy decides what reaches the operational logs. PIIKeys are
// normalized attribute keys (lowercase letters and digits only), sorted.
type LogContentPolicy struct {
	IPAddress    IPAddressMode `bson:"ipAddress" json:"ipAddress"`
	UserAgent    UserAgentMode `bson:"userAgent" json:"userAgent"`
	SubjectIDs   SubjectIDMode `bson:"subjectIds" json:"subjectIds"`
	PIIKeys      []string      `bson:"piiKeys" json:"piiKeys"`
	ScanFreeText bool          `bson:"scanFreeText" json:"scanFreeText"`
}

// Field names shared by LessRestrictiveContentFields and the validation issues.
const (
	FieldIPAddress          = "logContent.ipAddress"
	FieldUserAgent          = "logContent.userAgent"
	FieldSubjectIDs         = "logContent.subjectIds"
	FieldPIIKeys            = "logContent.piiKeys"
	FieldScanFreeText       = "logContent.scanFreeText"
)

// DefaultPIIKeys is the platform default list. Matching is exact on the
// normalized key, so the list spells out variants instead of relying on a
// "contains" match that would also hit filename, hostname and similar.
func DefaultPIIKeys() []string {
	return []string{
		"address", "birthdate", "codicefiscale", "dateofbirth", "displayname",
		"email", "emailaddress", "firstname", "fiscalcode", "fullname", "iban",
		"lastname", "mobile", "phone", "phonenumber", "streetaddress", "taxcode",
		"username",
	}
}

// DefaultLogContentPolicy is the platform default and the boot-time policy
// used before the compliance module is up.
func DefaultLogContentPolicy() LogContentPolicy {
	return LogContentPolicy{
		IPAddress:    IPAddressTruncated,
		UserAgent:    UserAgentFull,
		SubjectIDs:   SubjectIDUUID,
		PIIKeys:      DefaultPIIKeys(),
		ScanFreeText: true,
	}
}

var (
	ipAddressRank = map[IPAddressMode]int{IPAddressFull: 0, IPAddressTruncated: 1, IPAddressHashed: 2, IPAddressOmitted: 3}
	userAgentRank = map[UserAgentMode]int{UserAgentFull: 0, UserAgentOmitted: 1}
	subjectIDRank = map[SubjectIDMode]int{SubjectIDUUID: 0, SubjectIDHashed: 1, SubjectIDOmitted: 2}
)

func (m IPAddressMode) Valid() bool { _, ok := ipAddressRank[m]; return ok }
func (m UserAgentMode) Valid() bool { _, ok := userAgentRank[m]; return ok }
func (m SubjectIDMode) Valid() bool { _, ok := subjectIDRank[m]; return ok }

// MostRestrictive merges two policies field by field, keeping the more
// restrictive value of each (spec §2.2). It never mutates its inputs.
func MostRestrictive(a, b LogContentPolicy) LogContentPolicy {
	out := a
	if ipAddressRank[b.IPAddress] > ipAddressRank[a.IPAddress] {
		out.IPAddress = b.IPAddress
	}
	if userAgentRank[b.UserAgent] > userAgentRank[a.UserAgent] {
		out.UserAgent = b.UserAgent
	}
	if subjectIDRank[b.SubjectIDs] > subjectIDRank[a.SubjectIDs] {
		out.SubjectIDs = b.SubjectIDs
	}
	keys := make([]string, 0, len(a.PIIKeys)+len(b.PIIKeys))
	keys = append(keys, a.PIIKeys...)
	keys = append(keys, b.PIIKeys...)
	slices.Sort(keys)
	out.PIIKeys = slices.Compact(keys)
	out.ScanFreeText = a.ScanFreeText || b.ScanFreeText
	return out
}

// LessRestrictiveContentFields lists, in a fixed order, the content fields
// on which candidate protects less than baseline (spec §2.2).
func LessRestrictiveContentFields(candidate, baseline LogContentPolicy) []string {
	var out []string
	if ipAddressRank[candidate.IPAddress] < ipAddressRank[baseline.IPAddress] {
		out = append(out, FieldIPAddress)
	}
	if userAgentRank[candidate.UserAgent] < userAgentRank[baseline.UserAgent] {
		out = append(out, FieldUserAgent)
	}
	if subjectIDRank[candidate.SubjectIDs] < subjectIDRank[baseline.SubjectIDs] {
		out = append(out, FieldSubjectIDs)
	}
	for _, k := range baseline.PIIKeys {
		if !slices.Contains(candidate.PIIKeys, k) {
			out = append(out, FieldPIIKeys)
			break
		}
	}
	if baseline.ScanFreeText && !candidate.ScanFreeText {
		out = append(out, FieldScanFreeText)
	}
	return out
}
```

- [ ] **Step 4: Eseguire il test e verificare che passi**

Run: `cd backend && go test ./pkg/sdk/iface/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/sdk/iface/compliance_policy.go backend/pkg/sdk/iface/compliance_policy_test.go
git commit -F - <<'EOF'
feat(compliance): tipi della policy di contenuto dei log e ordine di restrittività

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---

### Task 2: Pacchetto `redact` condiviso

**Files:**
- Create: `backend/internal/shared/redact/redact.go`
- Test: `backend/internal/shared/redact/redact_test.go`
- Modify: `backend/internal/core/logging/logquery/redact.go`
- Test: `backend/internal/core/logging/logquery/redact_test.go`

**Interfaces:**
- Consumes: niente.
- Produces:
  - `const redact.Redacted = "[REDACTED]"`
  - `func redact.NormalizeKey(key string) string` — solo lettere e cifre, minuscole
  - `func redact.IsSecretKey(key string) bool`
  - `func redact.IsSecretNormalized(normalized string) bool`

- [ ] **Step 1: Scrivere il test che fallisce**

`backend/internal/shared/redact/redact_test.go`:

```go
package redact

import "testing"

func TestNormalizeKey(t *testing.T) {
	cases := map[string]string{
		"user_id":        "userid",
		"Client-IP":      "clientip",
		"codice.fiscale": "codicefiscale",
		"E-Mail":         "email",
		"":               "",
	}
	for in, want := range cases {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSecretKey(t *testing.T) {
	for _, k := range []string{"password", "new_passwd", "client_secret", "refreshToken", "Authorization", "cookie", "api_key", "private-key", "credentials"} {
		if !IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"module", "trace_id", "email", "ip", "filename"} {
		if IsSecretKey(k) {
			t.Errorf("IsSecretKey(%q) = true, want false", k)
		}
	}
}
```

Aggiungere in fondo a `backend/internal/core/logging/logquery/redact_test.go`:

```go
func TestRedactMasksSharedSecretFragments(t *testing.T) {
	got := Redact(map[string]any{"api_key": "k", "private_key": "p", "module": "auth"}).(map[string]any)
	if got["api_key"] != redactedValue || got["private_key"] != redactedValue {
		t.Fatalf("shared secret fragments not masked: %v", got)
	}
	if got["module"] != "auth" {
		t.Fatalf("non-sensitive key masked: %v", got)
	}
}
```

- [ ] **Step 2: Eseguire i test e verificare che falliscano**

Run: `cd backend && go test ./internal/shared/redact/ ./internal/core/logging/logquery/`
Expected: FAIL. `redact` non compila (`undefined: NormalizeKey`); in `logquery`, `api_key` non è mascherato.

- [ ] **Step 3: Scrivere l'implementazione**

`backend/internal/shared/redact/redact.go`:

```go
// Package redact holds the key rules shared by everything that masks log
// attributes: the operational log PolicyHandler and the Loki preview.
package redact

import (
	"strings"
	"unicode"
)

// Redacted replaces a secret value.
const Redacted = "[REDACTED]"

// secretFragments are always masked, whatever the compliance policy says
// (spec D11). Matching is "contains" on the normalized key: over-masking a
// secret-looking key is safe.
var secretFragments = [...]string{
	"password", "passwd", "secret", "token", "authorization",
	"cookie", "apikey", "privatekey", "credential",
}

// NormalizeKey keeps letters and digits only, lowercased, so user_id,
// userId and user-id compare equal.
func NormalizeKey(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
}

// IsSecretKey reports whether key names a secret.
func IsSecretKey(key string) bool { return IsSecretNormalized(NormalizeKey(key)) }

// IsSecretNormalized is IsSecretKey for a key already passed through
// NormalizeKey.
func IsSecretNormalized(normalized string) bool {
	for _, f := range secretFragments {
		if strings.Contains(normalized, f) {
			return true
		}
	}
	return false
}
```

`backend/internal/core/logging/logquery/redact.go`: sostituire le righe da `const redactedValue` a `var sensitiveKeyFragments = ...` (comprese) e la funzione `sensitiveKey` con:

```go
const redactedValue = redact.Redacted

// previewOnlyFragments are masked in the preview on top of the shared
// secret list: the preview shows raw Loki lines to an operator, so it keeps
// its historic stricter set.
var previewOnlyFragments = [...]string{
	"email",
	"phone",
	"address",
	"userid",
}
```

```go
func sensitiveKey(key string) bool {
	normalized := redact.NormalizeKey(key)
	if redact.IsSecretNormalized(normalized) {
		return true
	}
	for _, fragment := range previewOnlyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
```

Nell'import togliere `"unicode"` e aggiungere `"github.com/orkestra/backend/internal/shared/redact"`.

- [ ] **Step 4: Eseguire i test e verificare che passino**

Run: `cd backend && go test ./internal/shared/redact/ ./internal/core/logging/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/shared/redact backend/internal/core/logging/logquery
git commit -F - <<'EOF'
refactor(logging): elenco dei segreti condiviso in internal/shared/redact

L'anteprima Loki e il futuro handler di mascheramento usano la stessa
normalizzazione delle chiavi e lo stesso elenco dei segreti; l'anteprima
aggiunge passwd, apikey, privatekey e credential.

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---
### Task 3: Annotazioni di richiesta e correzione del request logger

**Files:**
- Create: `backend/pkg/sdk/ctxauth/annotations.go`
- Test: `backend/pkg/sdk/ctxauth/annotations_test.go`
- Create: `backend/internal/shared/middleware/request_annotations.go`
- Modify: `backend/internal/shared/middleware/request_logger.go:131-183`
- Modify: `backend/internal/shared/middleware/auth.go` (`setUserContext` prima della riga 552; `OptionalAuth` prima di `r = r.WithContext(ctx)`)
- Modify: `backend/internal/shared/middleware/jwt_validator.go` (prima di `next.ServeHTTP(w, r.WithContext(ctx))`, riga ~177)
- Modify: `backend/internal/shared/middleware/audience.go:96`
- Test: `backend/internal/shared/middleware/request_logger_test.go`
- Test: `backend/internal/shared/middleware/request_annotations_test.go`

**Interfaces:**
- Consumes: niente.
- Produces:
  - `type ctxauth.RequestAnnotations` con `SetPrincipal(tenantID, tenantKind, userID, userRole string)`, `SetAudience(aud string)`, `Snapshot() ctxauth.AnnotationSnapshot`; tutti i metodi accettano un ricevitore nil.
  - `type ctxauth.AnnotationSnapshot struct{ TenantID, TenantKind, UserID, UserRole, Audience string }`
  - `func ctxauth.WithRequestAnnotations(ctx context.Context) (context.Context, *RequestAnnotations)`
  - `func ctxauth.RequestAnnotationsFrom(ctx context.Context) *RequestAnnotations` (nil se assenti)
  - `func annotatePrincipal(ctx context.Context)` (non esportata, package `middleware`)

- [ ] **Step 1: Scrivere il test delle annotazioni**

`backend/pkg/sdk/ctxauth/annotations_test.go`:

```go
package ctxauth

import (
	"context"
	"sync"
	"testing"
)

func TestRequestAnnotations_RoundTrip(t *testing.T) {
	ctx, a := WithRequestAnnotations(context.Background())
	if RequestAnnotationsFrom(ctx) != a {
		t.Fatal("RequestAnnotationsFrom did not return the installed holder")
	}
	a.SetPrincipal("t-1", "external", "u-1", "administrator")
	a.SetAudience("operator")
	want := AnnotationSnapshot{TenantID: "t-1", TenantKind: "external", UserID: "u-1", UserRole: "administrator", Audience: "operator"}
	if got := a.Snapshot(); got != want {
		t.Fatalf("Snapshot = %+v, want %+v", got, want)
	}
	// An empty value never erases one already recorded.
	a.SetPrincipal("", "", "", "")
	if got := a.Snapshot(); got != want {
		t.Fatalf("empty SetPrincipal erased values: %+v", got)
	}
}

func TestRequestAnnotations_NilSafe(t *testing.T) {
	var a *RequestAnnotations
	a.SetPrincipal("t", "k", "u", "r")
	a.SetAudience("x")
	if got := a.Snapshot(); got != (AnnotationSnapshot{}) {
		t.Fatalf("nil Snapshot = %+v", got)
	}
	if RequestAnnotationsFrom(context.Background()) != nil {
		t.Fatal("RequestAnnotationsFrom on a bare context must be nil")
	}
}

func TestRequestAnnotations_ConcurrentAccess(t *testing.T) {
	_, a := WithRequestAnnotations(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); a.SetPrincipal("t", "k", "u", "r") }()
		go func() { defer wg.Done(); _ = a.Snapshot() }()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Eseguire il test e verificare che fallisca**

Run: `cd backend && go test -race ./pkg/sdk/ctxauth/`
Expected: FAIL di compilazione, `undefined: WithRequestAnnotations`.

- [ ] **Step 3: Implementare le annotazioni**

`backend/pkg/sdk/ctxauth/annotations.go`:

```go
package ctxauth

import (
	"context"
	"sync"
)

// RequestAnnotations carries the principal that authentication resolves
// back up to middleware that ran before it. RequireAuth hands the next
// handler a derived context, so an outer middleware such as the request
// logger never sees the values it stamps; a pointer installed in the outer
// context is shared with every derived one and closes that gap.
type RequestAnnotations struct {
	mu   sync.RWMutex
	snap AnnotationSnapshot
}

// AnnotationSnapshot is a copy of the annotations at one point in time.
type AnnotationSnapshot struct {
	TenantID   string
	TenantKind string
	UserID     string
	UserRole   string
	Audience   string
}

type annotationsKey struct{}

// WithRequestAnnotations installs an empty holder in ctx.
func WithRequestAnnotations(ctx context.Context) (context.Context, *RequestAnnotations) {
	a := &RequestAnnotations{}
	return context.WithValue(ctx, annotationsKey{}, a), a
}

// RequestAnnotationsFrom returns the holder installed in ctx, or nil.
func RequestAnnotationsFrom(ctx context.Context) *RequestAnnotations {
	if ctx == nil {
		return nil
	}
	a, _ := ctx.Value(annotationsKey{}).(*RequestAnnotations)
	return a
}

// SetPrincipal records the resolved principal. Empty values are ignored so a
// later, less informed stamp cannot erase an earlier one.
func (a *RequestAnnotations) SetPrincipal(tenantID, tenantKind, userID, userRole string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	setIfNotEmpty(&a.snap.TenantID, tenantID)
	setIfNotEmpty(&a.snap.TenantKind, tenantKind)
	setIfNotEmpty(&a.snap.UserID, userID)
	setIfNotEmpty(&a.snap.UserRole, userRole)
}

// SetAudience records the audience RequireAudience matched.
func (a *RequestAnnotations) SetAudience(aud string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	setIfNotEmpty(&a.snap.Audience, aud)
}

// Snapshot returns a copy of the recorded values.
func (a *RequestAnnotations) Snapshot() AnnotationSnapshot {
	if a == nil {
		return AnnotationSnapshot{}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.snap
}

func setIfNotEmpty(dst *string, v string) {
	if v != "" {
		*dst = v
	}
}
```

- [ ] **Step 4: Eseguire il test e verificare che passi**

Run: `cd backend && go test -race ./pkg/sdk/ctxauth/`
Expected: PASS.

- [ ] **Step 5: Scrivere i test del request logger che falliscono**

Aggiungere in fondo a `backend/internal/shared/middleware/request_logger_test.go`:

```go
// fakeAuth mimics RequireAuth: it stamps the principal on a DERIVED context,
// which the outer RequestLogger cannot see without the annotations.
func fakeAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ctx = context.WithValue(ctx, ctxauth.KeyUserUUID, "user-downstream")
		ctx = context.WithValue(ctx, ctxauth.KeyTenantID, "tenant-downstream")
		ctx = context.WithValue(ctx, ctxauth.KeyTenantKind, "internal")
		ctx = context.WithValue(ctx, ctxauth.KeySystemRole, "developer")
		annotatePrincipal(ctx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func TestRequestLogger_PrincipalFromDownstreamAuth(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{})(fakeAuth(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/x", nil))

	line := parseLine(t, buf.Bytes())
	for k, want := range map[string]string{"tenant_id": "tenant-downstream", "tenant_kind": "internal", "user_id": "user-downstream", "user_role": "developer"} {
		if line[k] != want {
			t.Errorf("%s = %v, want %s", k, line[k], want)
		}
	}
}

func TestRequestLogger_RealRequireAuthChain(t *testing.T) {
	f := newRequireAuthFixture(t)
	logger, buf := newCapturingLogger(t)
	handler := RequestLogger(logger, RequestLoggerOptions{})(f.mw.RequireAuth(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	))
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+f.issueTokenForUser("user-chain-1", "administrator"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	line := parseLine(t, buf.Bytes())
	if line["user_id"] != "user-chain-1" {
		t.Fatalf("user_id = %v, want user-chain-1 (RequireAuth principal must reach the outer log line)", line["user_id"])
	}
}
```

Creare `backend/internal/shared/middleware/request_annotations_test.go`, la guardia della Review Focus 3:

```go
package middleware

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryPrincipalStampAnnotates fails when a function in this package
// stamps ctxauth.KeyUserUUID (a new auth path) without calling
// annotatePrincipal, or stamps AudienceContextKey without SetAudience: the
// http_request line would silently lose tenant and user again.
func TestEveryPrincipalStampAnnotates(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			var body *ast.BlockStmt
			var label string
			switch fn := n.(type) {
			case *ast.FuncDecl:
				body, label = fn.Body, fn.Name.Name
			case *ast.FuncLit:
				body, label = fn.Body, "func literal"
			default:
				return true
			}
			if body == nil {
				return true
			}
			text := string(src[fset.Position(body.Pos()).Offset:fset.Position(body.End()).Offset])
			if strings.Contains(text, "ctxauth.KeyUserUUID, claims") && !strings.Contains(text, "annotatePrincipal(") {
				t.Errorf("%s: %s stamps ctxauth.KeyUserUUID without annotatePrincipal", name, label)
			}
			if strings.Contains(text, "AudienceContextKey, aud") && !strings.Contains(text, "SetAudience(") {
				t.Errorf("%s: %s stamps AudienceContextKey without SetAudience", name, label)
			}
			return true
		})
	}
}
```

- [ ] **Step 6: Eseguire i test e verificare che falliscano**

Run: `cd backend && go test ./internal/shared/middleware/ -run 'TestRequestLogger_PrincipalFromDownstreamAuth|TestRequestLogger_RealRequireAuthChain|TestEveryPrincipalStampAnnotates'`
Expected: FAIL di compilazione, `undefined: annotatePrincipal`.

- [ ] **Step 7: Implementare `annotatePrincipal` e collegarla**

`backend/internal/shared/middleware/request_annotations.go`:

```go
package middleware

import (
	"context"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
)

// annotatePrincipal copies the principal an auth middleware just stamped on
// ctx into the request annotations RequestLogger installed, so the outer
// http_request line and the log PolicyHandler see tenant and user. Call it
// on the final ctx, right before handing it to the next handler.
func annotatePrincipal(ctx context.Context) {
	a := ctxauth.RequestAnnotationsFrom(ctx)
	if a == nil {
		return
	}
	tenantID, _ := ctxauth.GetTenantID(ctx)
	userID, _ := ctxauth.GetUserUUID(ctx)
	role, _ := ctxauth.GetSystemRole(ctx)
	a.SetPrincipal(tenantID, ctxauth.TenantKindFromContext(ctx), userID, role)
}
```

In `auth.go`, `setUserContext`: subito prima della riga finale `next.ServeHTTP(w, r.WithContext(ctx))` (riga 552), dopo il blocco dell'impersonazione, aggiungere:

```go
	annotatePrincipal(ctx)
```

In `auth.go`, `OptionalAuth`: subito prima di `r = r.WithContext(ctx)` aggiungere `annotatePrincipal(ctx)`.

In `jwt_validator.go`: subito prima di `next.ServeHTTP(w, r.WithContext(ctx))` (dopo il controllo `TenantIDHeader`) aggiungere `annotatePrincipal(ctx)`.

In `audience.go`, riga 96, il blocco diventa:

```go
					ctx := context.WithValue(r.Context(), AudienceContextKey, aud)
					ctxauth.RequestAnnotationsFrom(ctx).SetAudience(aud)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
```

Aggiungere l'import `"github.com/orkestra/backend/pkg/sdk/ctxauth"` in `audience.go` se manca.

In `request_logger.go`, dentro `http.HandlerFunc`, sostituire le righe da `start := time.Now()` fino a `logger.LogAttrs(...)` (righe 138–183) con:

```go
			ctx, ann := ctxauth.WithRequestAnnotations(r.Context())
			r = r.WithContext(ctx)

			start := time.Now()
			ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			duration := time.Since(start)

			attrs := make([]slog.Attr, 0, 16)
			attrs = append(attrs,
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int64("duration_ms", duration.Milliseconds()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.String("remote", r.RemoteAddr),
				slog.String("ua", r.UserAgent()),
			)
			if reqID := chiMiddleware.GetReqID(r.Context()); reqID != "" {
				attrs = append(attrs, slog.String("request_id", reqID))
			}
			if duration >= opts.SlowThreshold {
				attrs = append(attrs, slog.Bool("slow", true))
			}

			// The auth middlewares run downstream on a derived context; they
			// report the principal through the annotations installed above.
			// Values already on this context (tests, outer middleware) are the
			// fallback. Empty values are omitted from the line.
			snap := ann.Snapshot()
			tenantID := snap.TenantID
			if tenantID == "" {
				tenantID, _ = ctxauth.GetTenantID(r.Context())
			}
			tenantKind := snap.TenantKind
			if tenantKind == "" {
				tenantKind = ctxauth.TenantKindFromContext(r.Context())
			}
			userID := snap.UserID
			if userID == "" {
				userID, _ = ctxauth.GetUserUUID(r.Context())
			}
			userRole := snap.UserRole
			if userRole == "" {
				userRole, _ = ctxauth.GetSystemRole(r.Context())
			}
			for _, kv := range [...]struct{ k, v string }{
				{"tenant_id", tenantID}, {"tenant_kind", tenantKind},
				{"user_id", userID}, {"user_role", userRole},
			} {
				if kv.v != "" {
					attrs = append(attrs, slog.String(kv.k, kv.v))
				}
			}
			audience := snap.Audience
			if audience == "" {
				audience = AudienceFromContext(r.Context())
			}
			if audience == "" {
				audience = opts.Audience
			}
			if audience != "" {
				attrs = append(attrs, slog.String("audience", audience))
			}

			logger.LogAttrs(r.Context(), levelForStatus(ww.Status()), "http_request", attrs...)
```

Aggiornare anche il commento sopra `RequestLogger` (righe 119–123) così:

```go
// tenant_id / tenant_kind / user_id / user_role / audience come from the
// request annotations the auth and audience middlewares fill downstream
// (ctxauth.RequestAnnotations); without them this outer middleware could
// not see the principal at all. Empty values are dropped rather than logged
// as "" to keep collector-side filtering predictable.
```

- [ ] **Step 8: Eseguire tutti i test del package e verificare che passino**

Run: `cd backend && go test -race ./internal/shared/middleware/ ./pkg/sdk/ctxauth/`
Expected: PASS, compresi `TestRequestLogger_TenantUserAudienceFromContext` (che usa il fallback) e i tre test nuovi.

- [ ] **Step 9: Commit**

```bash
git add backend/pkg/sdk/ctxauth backend/internal/shared/middleware
git commit -F - <<'EOF'
fix(middleware): tenant e utente sulla riga http_request

RequestLogger gira prima dell'autenticazione e non vedeva il contesto
derivato di RequireAuth: tenant_id, user_id, user_role e tenant_kind non
comparivano mai. Ora installa annotazioni di richiesta in ctxauth, che
RequireAuth, OptionalAuth, il validatore JWT e RequireAudience riempiono.
Un test AST impedisce a un nuovo percorso di autenticazione di saltarle.

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---
### Task 4: Route template sulla riga `http_request`

**Files:**
- Modify: `backend/internal/shared/middleware/request_logger.go` (attributo `path`)
- Test: `backend/internal/shared/middleware/request_logger_test.go`

**Interfaces:**
- Consumes: `chiRoutePattern(r *http.Request) string` (esistente nello stesso file).
- Produces: la riga `http_request` porta `route` (template chi) quando esiste, altrimenti `path`; mai entrambi.

- [ ] **Step 1: Scrivere il test che fallisce**

In fondo a `request_logger_test.go` (aggiungere l'import `"github.com/go-chi/chi/v5"` se manca):

```go
func TestRequestLogger_LogsRouteTemplateNotRawPath(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	r := chi.NewRouter()
	r.Use(RequestLogger(logger, RequestLoggerOptions{}))
	r.Get("/v1/admin/users/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/admin/users/3f2b-anna@example.com", nil))

	line := parseLine(t, buf.Bytes())
	if line["route"] != "/v1/admin/users/{id}" {
		t.Fatalf("route = %v, want the template", line["route"])
	}
	if _, ok := line["path"]; ok {
		t.Fatalf("raw path must not be logged when a template exists: %v", line["path"])
	}
}

func TestRequestLogger_FallsBackToPathWithoutTemplate(t *testing.T) {
	logger, buf := newCapturingLogger(t)
	r := chi.NewRouter()
	r.Use(RequestLogger(logger, RequestLoggerOptions{}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))

	line := parseLine(t, buf.Bytes())
	if line["path"] != "/nope" {
		t.Fatalf("404 without template must log the path: %v", line)
	}
	if _, ok := line["route"]; ok {
		t.Fatalf("no template → no route attribute: %v", line["route"])
	}
}
```

- [ ] **Step 2: Eseguire il test e verificare che fallisca**

Run: `cd backend && go test ./internal/shared/middleware/ -run 'TestRequestLogger_LogsRouteTemplateNotRawPath|TestRequestLogger_FallsBackToPathWithoutTemplate'`
Expected: FAIL: `route = <nil>, want the template`.

- [ ] **Step 3: Implementare**

In `request_logger.go`, nella costruzione di `attrs`, togliere `slog.String("path", r.URL.Path),` dall'elenco iniziale e aggiungere subito dopo l'`append` iniziale:

```go
			// Spec §2.4: the route template identifies the endpoint without
			// the identifiers a raw path carries (user ids, e-mails). The
			// raw path is logged only when chi matched no template (404s,
			// requests outside the router); the PolicyHandler still masks it.
			if route := chiRoutePattern(r); route != "" {
				attrs = append(attrs, slog.String("route", route))
			} else {
				attrs = append(attrs, slog.String("path", r.URL.Path))
			}
```

Aggiornare il commento di `RequestLogger` sugli attributi (riga ~106): «ADR-0005 §1.2 — only allowlisted attributes are written; no bodies, no headers, no raw query strings, and the route template instead of the raw path.»

- [ ] **Step 4: Eseguire tutti i test del package**

Run: `cd backend && go test ./internal/shared/middleware/`
Expected: PASS, compreso `TestRequestLogger_EmitsAllowlistedAttributes` (senza router chi usa il ripiego su `path`).

- [ ] **Step 5: Aggiornare la descrizione della dashboard**

Nessuna query LogQL delle dashboard legge `path` (verificato con `grep -n '\bpath\b' docker/grafana/provisioning/dashboards/*.json`); l'unico riferimento è il testo del pannello introduttivo di `logs-explorer.json` (riga ~55). Sostituire lì `` `method`/`path`/`status` `` con `` `method`/`route`/`status` ``.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/shared/middleware docker/grafana/provisioning/dashboards/logs-explorer.json
git commit -F - <<'EOF'
fix(middleware): route template invece del path reale su http_request

Il path reale può contenere identificativi e indirizzi email; il template
chi identifica l'endpoint senza. Il path resta solo quando non c'è un
template, e passa comunque per il mascheramento.

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---
### Task 5: Regole di mascheramento (`log_masker.go`)

**Files:**
- Create: `backend/internal/shared/utils/log_masker.go`
- Test: `backend/internal/shared/utils/log_masker_test.go`

**Interfaces:**
- Consumes: `iface.LogContentPolicy` e le modalità (Task 1); `redact.NormalizeKey`, `redact.IsSecretNormalized`, `redact.Redacted` (Task 2).
- Produces (non esportato, package `utils`):
  - `type logMasker struct{ p *iface.LogContentPolicy; key []byte }`
  - `func (m logMasker) maskAttr(a slog.Attr) (slog.Attr, bool)` — `false` significa «togli l'attributo»; recupera i panic producendo `[REDACTED:error]`
  - `func (m logMasker) safeText(s string) string` — `maskText` con recupero dai panic
  - costanti `piiMask="[PII]"`, `maskErrValue="[REDACTED:error]"`
  - `func utils.SetMaskingPanicHook(f func())` — chiamata dopo ogni panic recuperato (il Task 6 la collega alla metrica)

- [ ] **Step 1: Scrivere il test che fallisce**

`backend/internal/shared/utils/log_masker_test.go`:

```go
package utils

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
)

var testHashKey = []byte("0123456789abcdef0123456789abcdef")

func policy(mut func(*iface.LogContentPolicy)) *iface.LogContentPolicy {
	p := iface.DefaultLogContentPolicy()
	if mut != nil {
		mut(&p)
	}
	return &p
}

func maskOne(t *testing.T, m logMasker, a slog.Attr) (any, bool) {
	t.Helper()
	out, keep := m.maskAttr(a)
	if !keep {
		return nil, false
	}
	return out.Value.Any(), true
}

func TestMaskAttr_SecretsAlwaysMasked(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.PIIKeys = nil; p.ScanFreeText = false }), key: testHashKey}
	for _, k := range []string{"password", "refresh_token", "Authorization", "api_key"} {
		if v, _ := maskOne(t, m, slog.String(k, "s3cr3t")); v != "[REDACTED]" {
			t.Errorf("%s = %v, want [REDACTED]", k, v)
		}
	}
}

func TestMaskAttr_IPModes(t *testing.T) {
	cases := []struct {
		mode iface.IPAddressMode
		in   string
		want string // "" with drop=true means the attribute is removed
		drop bool
	}{
		{iface.IPAddressFull, "203.0.113.7:51234", "203.0.113.7:51234", false},
		{iface.IPAddressTruncated, "203.0.113.7:51234", "203.0.113.0/24", false},
		{iface.IPAddressTruncated, "[2001:db8:1:2::1]:443", "2001:db8:1::/48", false},
		{iface.IPAddressTruncated, "not-an-ip", "[REDACTED]", false},
		{iface.IPAddressOmitted, "203.0.113.7", "", true},
		{iface.IPAddressTruncated, "", "", false},
	}
	for _, c := range cases {
		m := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = c.mode }), key: testHashKey}
		v, keep := maskOne(t, m, slog.String("remote", c.in))
		if c.drop {
			if keep {
				t.Errorf("%s %q: kept %v, want dropped", c.mode, c.in, v)
			}
			continue
		}
		if v != c.want {
			t.Errorf("%s %q = %v, want %q", c.mode, c.in, v, c.want)
		}
	}
}

func TestMaskAttr_IPHashedStripsPortAndIsStable(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressHashed }), key: testHashKey}
	a, _ := maskOne(t, m, slog.String("client_ip", "203.0.113.7:1111"))
	b, _ := maskOne(t, m, slog.String("ip", "203.0.113.7"))
	if a != b || !strings.HasPrefix(a.(string), "h:") || len(a.(string)) != 18 {
		t.Fatalf("hashed IP not stable or malformed: %v vs %v", a, b)
	}
	noKey := logMasker{p: m.p}
	if _, keep := maskOne(t, noKey, slog.String("ip", "203.0.113.7")); keep {
		t.Fatal("hashed mode without a key must drop the value")
	}
}

func TestMaskAttr_UserAgentAndSubjects(t *testing.T) {
	m := logMasker{p: policy(func(p *iface.LogContentPolicy) {
		p.UserAgent = iface.UserAgentOmitted
		p.SubjectIDs = iface.SubjectIDHashed
	}), key: testHashKey}
	if _, keep := maskOne(t, m, slog.String("ua", "Mozilla/5.0")); keep {
		t.Error("ua kept with userAgent=omitted")
	}
	v, _ := maskOne(t, m, slog.String("user_id", "3f2b"))
	if !strings.HasPrefix(v.(string), "h:") {
		t.Errorf("user_id = %v, want hashed", v)
	}
	ip, _ := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressHashed }), key: testHashKey}.maskIP("3f2b")
	if ip == v {
		t.Error("ip and subject hashes of the same value must differ (domain separation)")
	}
	m.p.SubjectIDs = iface.SubjectIDOmitted
	if _, keep := maskOne(t, m, slog.String("actorUserId", "3f2b")); keep {
		t.Error("actorUserId kept with subjectIds=omitted")
	}
}

func TestMaskAttr_PIIKeysExactMatch(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	if v, _ := maskOne(t, m, slog.String("first_name", "Anna")); v != "[PII]" {
		t.Errorf("first_name = %v, want [PII]", v)
	}
	if v, _ := maskOne(t, m, slog.String("filename", "report.pdf")); v != "report.pdf" {
		t.Errorf("filename = %v, must not be masked (exact match only)", v)
	}
}

func TestMaskAttr_FreeText(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	in := "sent to anna.rossi@example.com from 203.0.113.7, IBAN IT60X0542811101000000123456, CF RSSMRA85T10A562S"
	v, _ := maskOne(t, m, slog.String("detail", in))
	got := v.(string)
	for _, leak := range []string{"anna.rossi", "203.0.113.7", "IT60X0542811101000000123456", "RSSMRA85T10A562S"} {
		if strings.Contains(got, leak) {
			t.Errorf("free text leaked %q: %s", leak, got)
		}
	}
	for _, mark := range []string{"[EMAIL]", "203.0.113.0/24", "[IBAN]", "[CF]"} {
		if !strings.Contains(got, mark) {
			t.Errorf("free text missing %q: %s", mark, got)
		}
	}
	off := logMasker{p: policy(func(p *iface.LogContentPolicy) { p.ScanFreeText = false })}
	if v, _ := maskOne(t, off, slog.String("detail", in)); v != in {
		t.Errorf("scanFreeText=false changed the value: %v", v)
	}
}

func TestMaskAttr_IPv6InFreeTextTruncatedOnce(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	v, _ := maskOne(t, m, slog.String("detail", "peer 2001:db8:1:2::1 closed"))
	if v != "peer 2001:db8:1::/48 closed" {
		t.Fatalf("got %v", v)
	}
}

func TestMaskAttr_GroupsMapsErrors(t *testing.T) {
	m := logMasker{p: policy(nil), key: testHashKey}
	out, _ := m.maskAttr(slog.Group("req", slog.String("email", "a@b.it"), slog.String("path", "/v1")))
	g := out.Value.Group()
	if g[0].Value.String() != "[PII]" || g[1].Value.String() != "/v1" {
		t.Errorf("group not masked: %v", g)
	}
	mv, _ := maskOne(t, m, slog.Any("payload", map[string]any{"token": "x", "nested": map[string]any{"phone": "333"}, "list": []any{"a@b.it"}}))
	mm := mv.(map[string]any)
	if mm["token"] != "[REDACTED]" || mm["nested"].(map[string]any)["phone"] != "[PII]" || mm["list"].([]any)[0] != "[EMAIL]" {
		t.Errorf("map not masked: %v", mm)
	}
	ev, _ := maskOne(t, m, slog.Any("error", errors.New("user a@b.it not found")))
	if ev != "user [EMAIL] not found" {
		t.Errorf("error = %v", ev)
	}
}

func TestMaskAttr_RecoversFromPanic(t *testing.T) {
	var panics int
	SetMaskingPanicHook(func() { panics++ })
	t.Cleanup(func() { SetMaskingPanicHook(nil) })
	m := logMasker{} // nil policy: any policy read panics
	out, keep := m.maskAttr(slog.String("email", "a@b.it"))
	if !keep || out.Value.String() != maskErrValue {
		t.Fatalf("panic not recovered into %s: %v %v", maskErrValue, out, keep)
	}
	if got := m.safeText("x"); got != maskErrValue {
		t.Fatalf("safeText = %q", got)
	}
	if panics != 2 {
		t.Fatalf("panic hook called %d times, want 2", panics)
	}
}
```

- [ ] **Step 2: Eseguire il test e verificare che fallisca**

Run: `cd backend && go test ./internal/shared/utils/ -run TestMaskAttr`
Expected: FAIL di compilazione, `undefined: logMasker`.

- [ ] **Step 3: Scrivere l'implementazione**

`backend/internal/shared/utils/log_masker.go`:

```go
package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/orkestra/backend/internal/shared/redact"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

const (
	piiMask      = "[PII]"
	maskErrValue = "[REDACTED:error]"
	textEmail    = "[EMAIL]"
	textIBAN     = "[IBAN]"
	textCF       = "[CF]"
	textIP       = "[IP]"
)

// Normalized attribute keys with a dedicated rule (spec §2.3).
var (
	ipKeys      = map[string]struct{}{"remote": {}, "ip": {}, "ipaddress": {}, "clientip": {}, "remoteaddr": {}, "remoteip": {}}
	uaKeys      = map[string]struct{}{"ua": {}, "useragent": {}}
	subjectKeys = map[string]struct{}{"userid": {}, "useruuid": {}, "actoruserid": {}, "subjectid": {}}
)

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	ibanRe  = regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}[A-Z0-9]{11,30}\b`)
	cfRe    = regexp.MustCompile(`(?i)\b[A-Z]{6}[0-9]{2}[A-Z][0-9]{2}[A-Z][0-9]{3}[A-Z]\b`)
	ipv4Re  = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	// Any run with at least two colons; net.ParseIP decides whether it is
	// really an IPv6 address (times like 10:20:30 are rejected there).
	ipv6CandidateRe = regexp.MustCompile(`[0-9A-Fa-f:]*:[0-9A-Fa-f:]*:[0-9A-Fa-f:]*`)
)

// maskingPanicHook is called after every recovered masking panic; main.go
// points it at the Prometheus counter (spec §9). nil = no-op.
var maskingPanicHook atomic.Pointer[func()]

// SetMaskingPanicHook installs (or, with nil, removes) the panic hook.
func SetMaskingPanicHook(f func()) {
	if f == nil {
		maskingPanicHook.Store(nil)
		return
	}
	maskingPanicHook.Store(&f)
}

func reportMaskingPanic() {
	if f := maskingPanicHook.Load(); f != nil {
		(*f)()
	}
}

// logMasker applies one LogContentPolicy to log attributes. A nil key makes
// the "hashed" modes drop the value.
type logMasker struct {
	p   *iface.LogContentPolicy
	key []byte
}

// maskAttr returns the masked attribute and whether to keep it. A panic
// while masking becomes a [REDACTED:error] value: a log line must never
// crash the process nor leak the unmasked value.
func (m logMasker) maskAttr(a slog.Attr) (out slog.Attr, keep bool) {
	defer func() {
		if recover() != nil {
			reportMaskingPanic()
			out, keep = slog.String(a.Key, maskErrValue), true
		}
	}()
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		children := v.Group()
		masked := make([]slog.Attr, 0, len(children))
		for _, c := range children {
			if mc, ok := m.maskAttr(c); ok {
				masked = append(masked, mc)
			}
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(masked...)}, true
	}
	nv, ok := m.maskKV(a.Key, v.Any())
	if !ok {
		return slog.Attr{}, false
	}
	return slog.Any(a.Key, nv), true
}

// maskKV applies the key rules in spec order, then the value rules.
func (m logMasker) maskKV(rawKey string, val any) (any, bool) {
	key := redact.NormalizeKey(rawKey)
	if redact.IsSecretNormalized(key) {
		return redact.Redacted, true
	}
	if _, ok := ipKeys[key]; ok {
		return m.maskIP(fmt.Sprint(val))
	}
	if _, ok := uaKeys[key]; ok {
		if m.p.UserAgent == iface.UserAgentFull {
			return val, true
		}
		return nil, false
	}
	if _, ok := subjectKeys[key]; ok {
		return m.maskSubject(fmt.Sprint(val))
	}
	if slices.Contains(m.p.PIIKeys, key) {
		return piiMask, true
	}
	return m.maskValue(val), true
}

func (m logMasker) maskValue(val any) any {
	switch x := val.(type) {
	case string:
		return m.maskText(x)
	case error:
		return m.maskText(x.Error())
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			if nv, ok := m.maskKV(k, v); ok {
				out[k] = nv
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = m.maskValue(v)
		}
		return out
	default:
		return val
	}
}

func (m logMasker) maskIP(raw string) (any, bool) {
	if raw == "" {
		return raw, true
	}
	switch m.p.IPAddress {
	case iface.IPAddressFull:
		return raw, true
	case iface.IPAddressTruncated:
		ip := parseHostIP(raw)
		if ip == nil {
			return redact.Redacted, true
		}
		return truncateIP(ip), true
	case iface.IPAddressHashed:
		if len(m.key) == 0 {
			return nil, false
		}
		host := raw
		if ip := parseHostIP(raw); ip != nil {
			host = ip.String()
		}
		return m.hash("ip:", host), true
	default: // omitted, or an unknown mode: fail closed
		return nil, false
	}
}

func (m logMasker) maskSubject(raw string) (any, bool) {
	if raw == "" {
		return raw, true
	}
	switch m.p.SubjectIDs {
	case iface.SubjectIDUUID:
		return raw, true
	case iface.SubjectIDHashed:
		if len(m.key) == 0 {
			return nil, false
		}
		return m.hash("sub:", raw), true
	default:
		return nil, false
	}
}

// maskText scans free text. IPv6 runs first so that the "/48" a truncated
// IPv6 leaves behind is not re-matched; the IPv4 output has no colons.
func (m logMasker) maskText(s string) string {
	if !m.p.ScanFreeText || s == "" {
		return s
	}
	s = emailRe.ReplaceAllString(s, textEmail)
	s = ibanRe.ReplaceAllString(s, textIBAN)
	s = cfRe.ReplaceAllString(s, textCF)
	if m.p.IPAddress == iface.IPAddressFull {
		return s
	}
	s = ipv6CandidateRe.ReplaceAllStringFunc(s, m.textIP)
	return ipv4Re.ReplaceAllStringFunc(s, m.textIP)
}

func (m logMasker) safeText(s string) (out string) {
	defer func() {
		if recover() != nil {
			reportMaskingPanic()
			out = maskErrValue
		}
	}()
	return m.maskText(s)
}

func (m logMasker) textIP(candidate string) string {
	ip := net.ParseIP(candidate)
	if ip == nil {
		return candidate
	}
	switch m.p.IPAddress {
	case iface.IPAddressTruncated:
		return truncateIP(ip)
	case iface.IPAddressHashed:
		if len(m.key) > 0 {
			return m.hash("ip:", ip.String())
		}
	}
	return textIP
}

// hash is a keyed, domain-separated pseudonym: the same value hashes
// differently as an IP and as a subject.
func (m logMasker) hash(domain, value string) string {
	mac := hmac.New(sha256.New, m.key)
	mac.Write([]byte(domain + value))
	return "h:" + hex.EncodeToString(mac.Sum(nil))[:16]
}

// parseHostIP accepts "ip", "ip:port" and "[ipv6]:port" (http.Request's
// RemoteAddr carries the port).
func parseHostIP(raw string) net.IP {
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	return net.ParseIP(strings.Trim(raw, "[]"))
}

func truncateIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String() + "/24"
	}
	return ip.Mask(net.CIDRMask(48, 128)).String() + "/48"
}
```

- [ ] **Step 4: Eseguire il test e verificare che passi**

Run: `cd backend && go test ./internal/shared/utils/ -run TestMaskAttr -v`
Expected: PASS di tutti i `TestMaskAttr_*`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/shared/utils/log_masker.go backend/internal/shared/utils/log_masker_test.go
git commit -F - <<'EOF'
feat(logging): regole di mascheramento dei log secondo la policy

Segreti sempre mascherati; IP interi, troncati, pseudonimizzati con HMAC o
rimossi, con la porta di RemoteAddr tolta prima; user agent e
identificativi secondo la policy; chiavi di dati personali per confronto
esatto; email, IBAN, codici fiscali e IP anche nel testo libero.

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---
### Task 6: `PolicyHandler` nella catena slog

**Files:**
- Create: `backend/internal/shared/utils/policy_handler.go`
- Test: `backend/internal/shared/utils/policy_handler_test.go`
- Modify: `backend/internal/shared/utils/logger.go:90-103` (inserimento nella catena)
- Modify: `backend/pkg/sdk/metrics/metrics.go` (contatore dei panic di mascheramento)
- Test: `backend/pkg/sdk/metrics/metrics_test.go`
- Modify: `backend/cmd/server/main.go` (collegamento del contatore, subito dopo il primo `SetupLogger`)

**Interfaces:**
- Consumes: `logMasker`, `SetMaskingPanicHook` (Task 5); `iface.LogContentPolicy`, `iface.DefaultLogContentPolicy` (Task 1); `ctxauth.RequestAnnotationsFrom` (Task 3).
- Produces:
  - `type utils.LogPolicyResolver interface { LogContentFor(tenantID string) *iface.LogContentPolicy }`
  - `func utils.NewStaticLogPolicyResolver(p iface.LogContentPolicy) utils.StaticLogPolicyResolver`
  - `func utils.NewPolicyHandler(base slog.Handler, r utils.LogPolicyResolver, hashKey []byte) *utils.PolicyHandler`
  - `func utils.SwapLogPolicyResolver(r utils.LogPolicyResolver)`
  - `func utils.LogHashKeyFromEnv() []byte`
  - `func (*metrics.Collector) RecordLogMaskingPanic()` e la metrica `orkestra_compliance_log_masking_panics_total`
  - Nota: la sostituzione del risolutore in `main.go` con la policy viva arriva nella T2, quando esiste il servizio di compliance.

- [ ] **Step 1: Scrivere il test che fallisce**

`backend/internal/shared/utils/policy_handler_test.go`:

```go
package utils

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// mapResolver returns byTenant[tenant], falling back to byTenant[""].
type mapResolver map[string]*iface.LogContentPolicy

func (m mapResolver) LogContentFor(tenant string) *iface.LogContentPolicy {
	if p, ok := m[tenant]; ok {
		return p
	}
	return m[""]
}

func newPolicyLogger(t *testing.T, r LogPolicyResolver) (*slog.Logger, *PolicyHandler, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	h := NewPolicyHandler(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}), r, testHashKey)
	return slog.New(h), h, buf
}

func TestPolicyHandler_TenantFromContext(t *testing.T) {
	strict := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	loose := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	logger, _, buf := newPolicyLogger(t, mapResolver{"": strict, "t-loose": loose})

	ctx := context.WithValue(context.Background(), ctxauth.KeyTenantID, "t-loose")
	logger.InfoContext(ctx, "x", slog.String("ip", "203.0.113.7"))
	if line := parseLines(t, buf.Bytes())[0]; line["ip"] != "203.0.113.7" {
		t.Fatalf("tenant policy not applied: %v", line)
	}
}

func TestPolicyHandler_TenantFromAnnotations(t *testing.T) {
	strict := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	loose := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	logger, _, buf := newPolicyLogger(t, mapResolver{"": strict, "t-loose": loose})

	ctx, ann := ctxauth.WithRequestAnnotations(context.Background())
	ann.SetPrincipal("t-loose", "", "", "")
	logger.InfoContext(ctx, "http_request", slog.String("remote", "203.0.113.7:1234"))
	if line := parseLines(t, buf.Bytes())[0]; line["remote"] != "203.0.113.7:1234" {
		t.Fatalf("annotated tenant policy not applied: %v", line)
	}
}

// Review Focus 2: a context-less line while a stricter tenant policy is
// active must get the strictest policy, not the platform one.
func TestPolicyHandler_NoTenantUsesStrictest(t *testing.T) {
	strictest := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	platform := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	logger, _, buf := newPolicyLogger(t, mapResolver{"": strictest, "t-platform": platform})

	logger.Info("no ctx", slog.String("ip", "203.0.113.7"))
	if _, ok := parseLines(t, buf.Bytes())[0]["ip"]; ok {
		t.Fatal("context-less line kept the IP: it must use the strictest policy")
	}
}

func TestPolicyHandler_WithAttrsMaskedAtWriteTimeAfterSwap(t *testing.T) {
	full := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressFull })
	omitted := policy(func(p *iface.LogContentPolicy) { p.IPAddress = iface.IPAddressOmitted })
	logger, h, buf := newPolicyLogger(t, mapResolver{"": full})
	child := logger.With(slog.String("ip", "203.0.113.7")).WithGroup("g").With(slog.String("email", "a@b.it"))

	child.Info("before")
	h.SetResolver(mapResolver{"": omitted})
	child.Info("after")

	lines := parseLines(t, buf.Bytes())
	if lines[0]["ip"] != "203.0.113.7" {
		t.Fatalf("before swap: %v", lines[0])
	}
	if _, ok := lines[1]["ip"]; ok {
		t.Fatalf("after swap the With attr must follow the new policy: %v", lines[1])
	}
	if g := lines[1]["g"].(map[string]any); g["email"] != "[PII]" {
		t.Fatalf("grouped With attr not masked: %v", lines[1])
	}
}

func TestPolicyHandler_MessageScanned(t *testing.T) {
	logger, _, buf := newPolicyLogger(t, NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy()))
	logger.Info("login failed for anna@example.com")
	if msg := parseLines(t, buf.Bytes())[0]["msg"]; msg != "login failed for [EMAIL]" {
		t.Fatalf("msg = %v", msg)
	}
}

func TestSetupLogger_InstallsPolicyHandler(t *testing.T) {
	t.Setenv("ENV", "production")
	logger := SetupLogger()
	h := globalPolicyBox.Load()
	if h == nil {
		t.Fatal("SetupLogger did not register the policy handler box")
	}
	_ = logger
}

func TestLogHashKeyFromEnv(t *testing.T) {
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", "")
	if LogHashKeyFromEnv() != nil {
		t.Fatal("missing key must yield nil")
	}
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", "zz")
	if LogHashKeyFromEnv() != nil {
		t.Fatal("non-hex key must yield nil")
	}
	t.Setenv("OAUTH_TOKEN_ENCRYPTION_KEY", hex.EncodeToString(testHashKey))
	k1 := LogHashKeyFromEnv()
	if len(k1) != 32 || bytes.Equal(k1, testHashKey) {
		t.Fatal("key must be a 32-byte HKDF derivation, not the raw secret")
	}
	if !bytes.Equal(k1, LogHashKeyFromEnv()) {
		t.Fatal("derivation must be deterministic")
	}
}

func BenchmarkPolicyHandler_TenAttrs(b *testing.B) {
	p := iface.DefaultLogContentPolicy()
	p.ScanFreeText = false
	h := NewPolicyHandler(slog.NewJSONHandler(io.Discard, nil), NewStaticLogPolicyResolver(p), testHashKey)
	logger := slog.New(h).With(slog.String("service", "orkestra-backend"), slog.String("version", "1"), slog.String("environment", "production"))
	attrs := []any{
		slog.String("method", "GET"), slog.String("path", "/v1/x"), slog.Int("status", 200),
		slog.Int64("duration_ms", 3), slog.Int("bytes", 120), slog.String("remote", "203.0.113.7:1"),
		slog.String("ua", "Mozilla/5.0"), slog.String("request_id", "r-1"), slog.String("tenant_id", "t"),
		slog.String("user_id", "u"),
	}
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("http_request", attrs...)
	}
}
```

- [ ] **Step 2: Eseguire il test e verificare che fallisca**

Run: `cd backend && go test ./internal/shared/utils/ -run 'TestPolicyHandler|TestSetupLogger_InstallsPolicyHandler|TestLogHashKeyFromEnv'`
Expected: FAIL di compilazione, `undefined: NewPolicyHandler`.

- [ ] **Step 3: Implementare l'handler**

`backend/internal/shared/utils/policy_handler.go`:

```go
package utils

import (
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// LogPolicyResolver hands the PolicyHandler the log-content policy for a
// tenant; tenantID == "" asks for the strictest policy in force (spec D6).
// The returned pointer is immutable and must stay the same while the policy
// is unchanged: the handler caches derived handlers by pointer.
type LogPolicyResolver interface {
	LogContentFor(tenantID string) *iface.LogContentPolicy
}

// StaticLogPolicyResolver serves one policy to everyone. SetupLogger boots
// with the platform defaults until the compliance module swaps in the live
// resolver.
type StaticLogPolicyResolver struct{ policy *iface.LogContentPolicy }

func NewStaticLogPolicyResolver(p iface.LogContentPolicy) StaticLogPolicyResolver {
	return StaticLogPolicyResolver{policy: &p}
}

func (r StaticLogPolicyResolver) LogContentFor(string) *iface.LogContentPolicy { return r.policy }

type logPolicyBox struct{ p atomic.Pointer[LogPolicyResolver] }

// globalPolicyBox is the resolver box of the handler built by the most
// recent SetupLogger; SwapLogPolicyResolver targets it (mirrors
// globalPerModule / SwapLevelResolver).
var globalPolicyBox atomic.Pointer[logPolicyBox]

// SwapLogPolicyResolver replaces the resolver behind every logger derived
// from the most recent SetupLogger. Called from main.go once the compliance
// module is up. No-op before SetupLogger or with a nil resolver.
func SwapLogPolicyResolver(r LogPolicyResolver) {
	if b := globalPolicyBox.Load(); b != nil && r != nil {
		b.p.Store(&r)
	}
}

// fallbackPolicy is used when no resolver answers. Package-level so its
// address is stable for the derived-handler cache.
var fallbackPolicy = iface.DefaultLogContentPolicy()

const maxDerivedHandlers = 32

// withStep is one WithAttrs or WithGroup call, kept raw: attributes are
// masked at write time because the policy depends on the record's tenant
// and can change at runtime (spec §2.3).
type withStep struct {
	group string
	attrs []slog.Attr
}

type derivedHandlers struct {
	mu sync.RWMutex
	m  map[*iface.LogContentPolicy]slog.Handler
}

// PolicyHandler masks every record according to the compliance policy of
// the record's tenant before it reaches the fan-out (stdout and OTLP).
type PolicyHandler struct {
	base    slog.Handler
	box     *logPolicyBox
	hashKey []byte
	steps   []withStep
	cache   *derivedHandlers
}

func NewPolicyHandler(base slog.Handler, r LogPolicyResolver, hashKey []byte) *PolicyHandler {
	box := &logPolicyBox{}
	if r != nil {
		box.p.Store(&r)
	}
	return &PolicyHandler{base: base, box: box, hashKey: hashKey, cache: newDerivedHandlers()}
}

func newDerivedHandlers() *derivedHandlers {
	return &derivedHandlers{m: map[*iface.LogContentPolicy]slog.Handler{}}
}

// SetResolver swaps the resolver for this handler and all its clones.
func (h *PolicyHandler) SetResolver(r LogPolicyResolver) {
	if r != nil {
		h.box.p.Store(&r)
	}
}

func (h *PolicyHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.base.Enabled(ctx, level)
}

func (h *PolicyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return h.with(withStep{attrs: slices.Clone(attrs)})
}

func (h *PolicyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return h.with(withStep{group: name})
}

func (h *PolicyHandler) with(s withStep) *PolicyHandler {
	return &PolicyHandler{
		base:    h.base,
		box:     h.box,
		hashKey: h.hashKey,
		steps:   append(slices.Clip(h.steps), s),
		cache:   newDerivedHandlers(),
	}
}

func (h *PolicyHandler) Handle(ctx context.Context, r slog.Record) error {
	p := h.policyFor(ctx)
	m := logMasker{p: p, key: h.hashKey}
	out := slog.NewRecord(r.Time, r.Level, m.safeText(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		if ma, keep := m.maskAttr(a); keep {
			out.AddAttrs(ma)
		}
		return true
	})
	return h.derived(p).Handle(ctx, out)
}

func (h *PolicyHandler) policyFor(ctx context.Context) *iface.LogContentPolicy {
	tenant := ""
	if ctx != nil {
		if t, ok := ctxauth.GetTenantID(ctx); ok {
			tenant = t
		} else {
			tenant = ctxauth.RequestAnnotationsFrom(ctx).Snapshot().TenantID
		}
	}
	if rp := h.box.p.Load(); rp != nil {
		if p := (*rp).LogContentFor(tenant); p != nil {
			return p
		}
	}
	return &fallbackPolicy
}

// derived returns base with this handler's With steps replayed, masked
// under p. Cached per policy pointer; the cache is reset past a small bound
// so replaced policies cannot accumulate.
func (h *PolicyHandler) derived(p *iface.LogContentPolicy) slog.Handler {
	if len(h.steps) == 0 {
		return h.base
	}
	h.cache.mu.RLock()
	d, ok := h.cache.m[p]
	h.cache.mu.RUnlock()
	if ok {
		return d
	}
	m := logMasker{p: p, key: h.hashKey}
	d = h.base
	for _, s := range h.steps {
		if s.group != "" {
			d = d.WithGroup(s.group)
			continue
		}
		masked := make([]slog.Attr, 0, len(s.attrs))
		for _, a := range s.attrs {
			if ma, keep := m.maskAttr(a); keep {
				masked = append(masked, ma)
			}
		}
		d = d.WithAttrs(masked)
	}
	h.cache.mu.Lock()
	if len(h.cache.m) >= maxDerivedHandlers {
		clear(h.cache.m)
	}
	h.cache.m[p] = d
	h.cache.mu.Unlock()
	return d
}

// LogHashKeyFromEnv derives the HMAC key for "hashed" log values from
// OAUTH_TOKEN_ENCRYPTION_KEY (32-byte hex) with HKDF-SHA256. Nothing is
// stored and every replica derives the same key. Returns nil when the
// variable is missing or malformed: hashed modes then drop the value.
func LogHashKeyFromEnv() []byte {
	secret, err := hex.DecodeString(os.Getenv("OAUTH_TOKEN_ENCRYPTION_KEY"))
	if err != nil || len(secret) != 32 {
		return nil
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "orkestra/log-hash/v1", 32)
	if err != nil {
		return nil
	}
	return key
}
```

- [ ] **Step 4: Inserire l'handler in `SetupLogger`**

In `logger.go`, subito **prima** di `resolver := NewStaticLevelResolver(level, loadPerModuleLevels())` (riga 100), aggiungere:

```go
	// Spec §2.3 — compliance masking sits after the level gate (records
	// dropped for level cost nothing) and before the fan-out (stdout and
	// OTLP receive the same masked record). Boot uses the platform
	// defaults; main.go swaps in the compliance module's live resolver.
	policyHandler := NewPolicyHandler(handler, NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy()), LogHashKeyFromEnv())
	globalPolicyBox.Store(policyHandler.box)
	handler = policyHandler
```

Aggiungere l'import `"github.com/orkestra/backend/pkg/sdk/iface"`.

- [ ] **Step 5: Eseguire i test e verificare che passino**

Run: `cd backend && go test -race ./internal/shared/utils/`
Expected: PASS, compresi i test esistenti di `per_module_level_handler_test.go`.

- [ ] **Step 6: Misurare il costo**

Run: `cd backend && go test ./internal/shared/utils/ -run '^$' -bench BenchmarkPolicyHandler_TenAttrs -benchtime 2s`
Expected: meno di `2000 ns/op` (spec §2.3). Se il valore è più alto, fermarsi e riportarlo prima di proseguire, con l'output del profilo `-cpuprofile`.

- [ ] **Step 7: Contatore dei panic di mascheramento**

Test in fondo a `backend/pkg/sdk/metrics/metrics_test.go`:

```go
func TestRecordLogMaskingPanic(t *testing.T) {
	c := NewCollector()
	c.RecordLogMaskingPanic()
	c.RecordLogMaskingPanic()
	if got := testutil.ToFloat64(c.logMaskingPanics); got != 2 {
		t.Fatalf("log_masking_panics_total = %v, want 2", got)
	}
}
```

(aggiungere l'import `"github.com/prometheus/client_golang/prometheus/testutil"` se il file non lo ha).

Run: `cd backend && go test ./pkg/sdk/metrics/ -run TestRecordLogMaskingPanic`
Expected: FAIL, `c.logMaskingPanics undefined`.

In `metrics.go`:
- nella struct `Collector` aggiungere il campo `logMaskingPanics prometheus.Counter`;
- in `buildMetrics()`, dopo `c.authzCacheInvalidationRefusals = ...`:

```go
	c.logMaskingPanics = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "orkestra",
			Subsystem: "compliance",
			Name:      "log_masking_panics_total",
			Help:      "Recovered panics while masking a log or span attribute; the value was written as [REDACTED:error]. Unlabelled by design (ADR-0002).",
		},
	)
```

- nell'elenco di `Register()` aggiungere `c.logMaskingPanics`;
- in fondo al file:

```go
// RecordLogMaskingPanic counts one recovered panic in the compliance log /
// span masker (compliance spec §9).
func (c *Collector) RecordLogMaskingPanic() { c.logMaskingPanics.Inc() }
```

In `backend/cmd/server/main.go`, subito dopo `slog.SetDefault(logger)` della riga 56:

```go
	// Compliance spec §9 — count recovered masking panics.
	utils.SetMaskingPanicHook(metrics.Default().RecordLogMaskingPanic)
```

(aggiungere l'import `"github.com/orkestra/backend/pkg/sdk/metrics"` se `main.go` non lo ha).

Run: `cd backend && go test ./pkg/sdk/metrics/`
Expected: PASS.

- [ ] **Step 8: Verificare la compilazione di tutto il backend**

Run: `cd backend && go build ./... && go vet ./internal/shared/utils/ ./pkg/sdk/metrics/ ./cmd/server/`
Expected: nessun errore.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/shared/utils backend/pkg/sdk/metrics backend/cmd/server/main.go
git commit -F - <<'EOF'
feat(logging): PolicyHandler di mascheramento nella catena slog

Maschera ogni record con la policy del tenant della richiesta (contesto o
annotazioni) e, senza tenant, con la più restrittiva attiva. Gli attributi
di With sono mascherati al momento della scrittura, con cache per policy.
La chiave HMAC è derivata con HKDF da OAUTH_TOKEN_ENCRYPTION_KEY; i
panic recuperati sono contati in una metrica.

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---
### Task 7: Mascheramento delle trace in uscita

**Files:**
- Modify: `backend/internal/shared/utils/log_masker.go` (funzione esportata `MaskKV`)
- Create: `backend/internal/shared/telemetry/masking_exporter.go`
- Test: `backend/internal/shared/telemetry/masking_exporter_test.go`
- Modify: `backend/internal/shared/telemetry/tracer.go` (wrapper attorno all'exporter OTLP)

**Interfaces:**
- Consumes: `logMasker`, `reportMaskingPanic`, `maskErrValue` (Task 5); `utils.LogPolicyResolver`, `utils.NewStaticLogPolicyResolver`, `utils.LogHashKeyFromEnv` (Task 6).
- Produces:
  - `func utils.MaskKV(p *iface.LogContentPolicy, hashKey []byte, key string, val any) (any, bool)`
  - `func telemetry.NewMaskingExporter(next sdktrace.SpanExporter, r utils.LogPolicyResolver, hashKey []byte) *telemetry.MaskingExporter`
  - `func telemetry.SwapSpanPolicyResolver(r utils.LogPolicyResolver)` (la T2 lo chiama da `main.go`)

- [ ] **Step 1: Scrivere il test che fallisce**

`backend/internal/shared/telemetry/masking_exporter_test.go`:

```go
package telemetry

import (
	"context"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type mapResolver map[string]*iface.LogContentPolicy

func (m mapResolver) LogContentFor(tenant string) *iface.LogContentPolicy {
	if p, ok := m[tenant]; ok {
		return p
	}
	return m[""]
}

func spanAttrs(s tracetest.SpanStub) map[string]string {
	out := map[string]string{}
	for _, kv := range s.Attributes {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

func TestMaskingExporter(t *testing.T) {
	strict := iface.DefaultLogContentPolicy()
	strict.IPAddress = iface.IPAddressOmitted
	loose := iface.DefaultLogContentPolicy()
	loose.IPAddress = iface.IPAddressFull
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(NewMaskingExporter(mem, mapResolver{"": &strict, "t-loose": &loose}, nil)))
	tr := tp.Tracer("test")

	_, s1 := tr.Start(context.Background(), "no-tenant")
	s1.SetAttributes(
		attribute.String("client.address", "203.0.113.7"),
		attribute.String("http.route", "/v1/users/{id}"),
		attribute.String("url.path", "/v1/users/anna@example.com"),
		attribute.String("url.full", "https://api.example/v1/users/anna@example.com?x=1"),
		attribute.String("url.query", "email=anna@example.com"),
		attribute.String("password", "s3cr3t"),
		attribute.Int("http.response.status_code", 200),
	)
	s1.End()
	_, s2 := tr.Start(context.Background(), "tenant")
	s2.SetAttributes(attribute.String("tenant.id", "t-loose"), attribute.String("client.address", "203.0.113.7"))
	s2.End()

	spans := mem.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d", len(spans))
	}
	a := spanAttrs(spans[0])
	if _, ok := a["client.address"]; ok {
		t.Error("no-tenant span kept the IP: the strictest policy must apply")
	}
	if a["url.path"] != "/v1/users/{id}" {
		t.Errorf("url.path = %q, want the route template", a["url.path"])
	}
	for _, k := range []string{"url.full", "url.query"} {
		if _, ok := a[k]; ok {
			t.Errorf("%s must be dropped", k)
		}
	}
	if a["password"] != "[REDACTED]" {
		t.Errorf("password = %q", a["password"])
	}
	if a["http.response.status_code"] != "200" {
		t.Errorf("non-string attribute changed: %q", a["http.response.status_code"])
	}
	if b := spanAttrs(spans[1]); b["client.address"] != "203.0.113.7" {
		t.Errorf("tenant policy not applied to its span: %v", b)
	}
}

func TestSwapSpanPolicyResolver(t *testing.T) {
	strict := iface.DefaultLogContentPolicy()
	strict.UserAgent = iface.UserAgentOmitted
	mem := tracetest.NewInMemoryExporter()
	exp := NewMaskingExporter(mem, mapResolver{"": func() *iface.LogContentPolicy { p := iface.DefaultLogContentPolicy(); return &p }()}, nil)
	globalSpanBox.Store(exp.box)
	SwapSpanPolicyResolver(mapResolver{"": &strict})
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	_, s := tp.Tracer("t").Start(context.Background(), "x")
	s.SetAttributes(attribute.String("user_agent.original", "Mozilla/5.0"))
	s.End()
	if _, ok := spanAttrs(mem.GetSpans()[0])["user_agent.original"]; ok {
		t.Fatal("swapped resolver not used")
	}
}
```

- [ ] **Step 2: Eseguire il test e verificare che fallisca**

Run: `cd backend && go test ./internal/shared/telemetry/ -run 'TestMaskingExporter|TestSwapSpanPolicyResolver'`
Expected: FAIL di compilazione, `undefined: NewMaskingExporter`.

- [ ] **Step 3: Esportare `MaskKV` dal mascheratore**

In fondo a `log_masker.go`:

```go
// MaskKV applies policy p to one key/value pair with the same rules as the
// slog PolicyHandler (spec §2.3), for callers outside the slog chain such as
// the span exporter. keep=false means "drop the pair". Never panics.
func MaskKV(p *iface.LogContentPolicy, hashKey []byte, key string, val any) (out any, keep bool) {
	defer func() {
		if recover() != nil {
			reportMaskingPanic()
			out, keep = maskErrValue, true
		}
	}()
	return logMasker{p: p, key: hashKey}.maskKV(key, val)
}
```

- [ ] **Step 4: Scrivere il wrapper dell'exporter**

`backend/internal/shared/telemetry/masking_exporter.go`:

```go
package telemetry

import (
	"context"
	"sync/atomic"

	"github.com/orkestra/backend/internal/shared/utils"
	"github.com/orkestra/backend/pkg/sdk/iface"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// spanKeyAliases maps OTel semantic-convention keys onto the masker's
// normalized key vocabulary (spec §2.5), so client.address is treated as an
// IP and user.id as a subject identifier.
var spanKeyAliases = map[string]string{
	"client.address":      "ip",
	"net.peer.ip":         "ip",
	"net.sock.peer.addr":  "ip",
	"http.client_ip":      "ip",
	"user_agent.original": "useragent",
	"http.user_agent":     "useragent",
	"user.id":             "userid",
	"enduser.id":          "userid",
}

// droppedSpanKeys carry the raw URL (path + query) and never leave the
// process; http.route identifies the endpoint instead.
var droppedSpanKeys = map[string]bool{"url.full": true, "http.url": true, "url.query": true}

// pathSpanKeys are replaced by http.route when the span has one.
var pathSpanKeys = map[string]bool{"url.path": true, "http.target": true}

type spanPolicyBox struct{ p atomic.Pointer[utils.LogPolicyResolver] }

var globalSpanBox atomic.Pointer[spanPolicyBox]

// SwapSpanPolicyResolver replaces the resolver of the exporter built by the
// most recent Init. main.go calls it once the compliance module is up.
func SwapSpanPolicyResolver(r utils.LogPolicyResolver) {
	if b := globalSpanBox.Load(); b != nil && r != nil {
		b.p.Store(&r)
	}
}

// MaskingExporter masks span and event attributes with the compliance
// policy of the span's tenant (attribute tenant.id; none → strictest)
// before handing them to the real exporter.
type MaskingExporter struct {
	next    sdktrace.SpanExporter
	box     *spanPolicyBox
	hashKey []byte
}

func NewMaskingExporter(next sdktrace.SpanExporter, r utils.LogPolicyResolver, hashKey []byte) *MaskingExporter {
	box := &spanPolicyBox{}
	if r != nil {
		box.p.Store(&r)
	}
	return &MaskingExporter{next: next, box: box, hashKey: hashKey}
}

func (e *MaskingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	out := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, s := range spans {
		out[i] = e.mask(s)
	}
	return e.next.ExportSpans(ctx, out)
}

func (e *MaskingExporter) Shutdown(ctx context.Context) error { return e.next.Shutdown(ctx) }

func (e *MaskingExporter) mask(s sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	stub := tracetest.SpanStubFromReadOnlySpan(s)
	p := e.policyFor(stub.Attributes)
	stub.Attributes = e.maskAttributes(p, stub.Attributes)
	for i := range stub.Events {
		stub.Events[i].Attributes = e.maskAttributes(p, stub.Events[i].Attributes)
	}
	return stub.Snapshot()
}

func (e *MaskingExporter) policyFor(attrs []attribute.KeyValue) *iface.LogContentPolicy {
	tenant := ""
	for _, kv := range attrs {
		if kv.Key == "tenant.id" {
			tenant = kv.Value.AsString()
		}
	}
	if rp := e.box.p.Load(); rp != nil {
		if p := (*rp).LogContentFor(tenant); p != nil {
			return p
		}
	}
	fallback := iface.DefaultLogContentPolicy()
	return &fallback
}

func (e *MaskingExporter) maskAttributes(p *iface.LogContentPolicy, attrs []attribute.KeyValue) []attribute.KeyValue {
	route := ""
	for _, kv := range attrs {
		if kv.Key == "http.route" {
			route = kv.Value.AsString()
		}
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		k := string(kv.Key)
		if droppedSpanKeys[k] {
			continue
		}
		if pathSpanKeys[k] && route != "" {
			out = append(out, attribute.String(k, route))
			continue
		}
		maskKey := k
		if alias, ok := spanKeyAliases[k]; ok {
			maskKey = alias
		}
		v, keep := utils.MaskKV(p, e.hashKey, maskKey, kv.Value.AsInterface())
		if !keep {
			continue
		}
		if s, ok := v.(string); ok {
			out = append(out, attribute.String(k, s))
		} else {
			out = append(out, kv) // non-string values are never rewritten
		}
	}
	return out
}
```

- [ ] **Step 5: Collegare il wrapper in `Init`**

In `tracer.go`, nel ramo con l'exporter OTLP valido, sostituire `sdktrace.WithBatcher(exp),` con:

```go
				// Compliance spec §2.5: spans leave the process masked with
				// the tenant's policy; main.go swaps in the live resolver.
				sdktrace.WithBatcher(newGlobalMaskingExporter(exp)),
```

e in fondo a `masking_exporter.go`:

```go
// newGlobalMaskingExporter wraps exp with the boot defaults and registers
// its resolver box for SwapSpanPolicyResolver.
func newGlobalMaskingExporter(exp sdktrace.SpanExporter) *MaskingExporter {
	m := NewMaskingExporter(exp, utils.NewStaticLogPolicyResolver(iface.DefaultLogContentPolicy()), utils.LogHashKeyFromEnv())
	globalSpanBox.Store(m.box)
	return m
}
```

- [ ] **Step 6: Eseguire i test e la compilazione**

Run: `cd backend && go build ./... && go test -race ./internal/shared/telemetry/ ./internal/shared/utils/`
Expected: PASS. Se `go mod tidy` segnala `go.opentelemetry.io/otel/sdk/trace/tracetest` come dipendenza nuova: fa parte del modulo `go.opentelemetry.io/otel/sdk` già richiesto, quindi `go.mod` non cambia; se cambia, fermarsi e riportarlo (vincolo «nessuna nuova dipendenza»).

- [ ] **Step 7: Commit**

```bash
git add backend/internal/shared/telemetry backend/internal/shared/utils/log_masker.go
git commit -F - <<'EOF'
feat(telemetry): trace in uscita mascherate con la policy del tenant

Un wrapper dell'exporter OTLP applica agli attributi degli span e degli
eventi le stesse regole dei log: IP, user agent e identificativi secondo la
policy del tenant dello span (senza tenant la più restrittiva), segreti
sempre mascherati, URL completo e query mai esportati, path sostituito dal
route template.

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---
### Task 8: Linter `logscope` come gate di CI

**Files:**
- Create: `backend/tools/logscope/scanner.go`
- Test: `backend/tools/logscope/scanner_test.go`
- Create: `backend/tools/logscope/cmd/logscope/main.go`
- Create: `backend/tools/logscope/baseline.txt` (generato)
- Modify: `Makefile` (target `backend-logscope`, `.PHONY`, `ci-backend`)

**Interfaces:**
- Consumes: `redact.IsSecretKey` (Task 2); `piiscan.LoadBaseline(path string) (map[string]bool, error)` (esistente, formato `categoria:chiave` per riga).
- Produces:
  - `type logscope.Finding struct{ Category, File, Func, Key string; Line int }` con `func (Finding) BaselineKey() string` = `Category + ":" + File + ":" + Func + ":" + Key`
  - `func logscope.ScanFiles(fset *token.FileSet, files []*ast.File, info *types.Info, relFile func(string) string) []Finding`
  - `func logscope.Scan(dir string, patterns []string) ([]Finding, error)`
  - categorie `logscope.any_opaque_value` e `logscope.secret_key_dynamic_value`

- [ ] **Step 1: Scrivere il test che fallisce**

`backend/tools/logscope/scanner_test.go`:

```go
package logscope

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"testing"
)

const sample = `package sample

import (
	"errors"
	"log/slog"
)

type user struct{ Email string }

func f(u user, p *user, v any, tok string) {
	slog.Info("x",
		slog.Any("user", u),                 // struct: flagged
		slog.Any("ptr", p),                  // pointer to struct: flagged
		slog.Any("anything", v),             // interface other than error: flagged
		slog.Any("err", errors.New("e")),    // error: ok
		slog.Any("tags", []string{"a"}),     // slice: ok
		slog.Any("meta", map[string]any{}),  // map: ok (masked recursively)
		slog.String("token", tok),           // secret key, dynamic value: flagged
		slog.String("token_type", "bearer"), // secret key, constant value: ok
		slog.String("module", tok),          // not a secret key: ok
	)
}
`

func TestScanFiles(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", sample, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	if _, err := conf.Check("sample", fset, []*ast.File{file}, info); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, f := range ScanFiles(fset, []*ast.File{file}, info, func(p string) string { return p }) {
		got = append(got, f.BaselineKey())
	}
	sort.Strings(got)
	want := []string{
		"logscope.any_opaque_value:sample.go:f:anything",
		"logscope.any_opaque_value:sample.go:f:ptr",
		"logscope.any_opaque_value:sample.go:f:user",
		"logscope.secret_key_dynamic_value:sample.go:f:token",
	}
	if len(got) != len(want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("findings = %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 2: Eseguire il test e verificare che fallisca**

Run: `cd backend && go test ./tools/logscope/`
Expected: FAIL di compilazione, `undefined: ScanFiles`.

- [ ] **Step 3: Scrivere l'analizzatore**

`backend/tools/logscope/scanner.go`:

```go
// Package logscope flags slog calls the compliance PolicyHandler cannot
// mask reliably (compliance spec §2.6): slog.Any with an opaque value (a
// struct, a pointer to one, or an interface other than error), and
// secret-looking keys carrying a non-constant value. Existing calls live in
// baseline.txt; a new one fails CI.
package logscope

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"

	"github.com/orkestra/backend/internal/shared/redact"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	CategoryAnyOpaque        = "logscope.any_opaque_value"
	CategorySecretKeyDynamic = "logscope.secret_key_dynamic_value"
)

type Finding struct {
	Category string
	File     string
	Line     int
	Func     string
	Key      string
}

// BaselineKey omits the line number so unrelated edits do not churn the
// baseline.
func (f Finding) BaselineKey() string {
	return f.Category + ":" + f.File + ":" + f.Func + ":" + f.Key
}

var errorType = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

// ScanFiles inspects already type-checked files. relFile maps an absolute
// file name to the path written in findings.
func ScanFiles(fset *token.FileSet, files []*ast.File, info *types.Info, relFile func(string) string) []Finding {
	var out []Finding
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				callee, ok := typeutil.Callee(info, call).(*types.Func)
				if !ok || callee.Pkg() == nil || callee.Pkg().Path() != "log/slog" {
					return true
				}
				keyTV, ok := info.Types[call.Args[0]]
				if !ok || keyTV.Value == nil || keyTV.Value.Kind() != constant.String {
					return true
				}
				key := constant.StringVal(keyTV.Value)
				pos := fset.Position(call.Pos())
				mk := func(cat string) Finding {
					return Finding{Category: cat, File: relFile(pos.Filename), Line: pos.Line, Func: fn.Name.Name, Key: key}
				}
				valTV := info.Types[call.Args[1]]
				if callee.Name() == "Any" && opaque(valTV.Type) {
					out = append(out, mk(CategoryAnyOpaque))
				}
				if redact.IsSecretKey(key) && valTV.Value == nil {
					out = append(out, mk(CategorySecretKeyDynamic))
				}
				return true
			})
		}
	}
	return out
}

// opaque reports values the masker passes through unchanged: structs,
// pointers to structs, and interfaces other than error.
func opaque(t types.Type) bool {
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Struct:
		return true
	case *types.Pointer:
		_, isStruct := u.Elem().Underlying().(*types.Struct)
		return isStruct
	case *types.Interface:
		return !types.Implements(t, errorType) || u.NumMethods() == 0
	default:
		return false
	}
}

// Scan loads the packages matching patterns (relative to dir) and scans
// their non-test files.
func Scan(dir string, patterns []string) ([]Finding, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  dir,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	abs, _ := filepath.Abs(dir)
	rel := func(p string) string {
		if r, err := filepath.Rel(abs, p); err == nil {
			return filepath.ToSlash(r)
		}
		return p
	}
	var out []Finding
	for _, p := range pkgs {
		out = append(out, ScanFiles(p.Fset, p.Syntax, p.TypesInfo, rel)...)
	}
	return out, nil
}
```

Nota su `opaque` per le interfacce: `error` implementa `error` e ha un metodo, quindi non è opaca; `any` (zero metodi) è opaca; un'altra interfaccia che non implementa `error` è opaca.

- [ ] **Step 4: Eseguire il test e verificare che passi**

Run: `cd backend && go test ./tools/logscope/ -v`
Expected: PASS.

- [ ] **Step 5: Scrivere il comando e generare la baseline**

`backend/tools/logscope/cmd/logscope/main.go`:

```go
// Command logscope fails when an slog call the compliance masker cannot
// handle is not in the baseline (compliance spec §2.6).
//
//	go run ./tools/logscope/cmd/logscope -baseline=tools/logscope/baseline.txt ./internal/... ./pkg/... ./cmd/...
//	go run ./tools/logscope/cmd/logscope -write-baseline=tools/logscope/baseline.txt ./internal/... ./pkg/... ./cmd/...
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/orkestra/backend/tools/logscope"
	"github.com/orkestra/backend/tools/piiscan"
)

func main() {
	baseline := flag.String("baseline", "", "baseline file (category:file:func:key per line)")
	write := flag.String("write-baseline", "", "write every current finding to this file and exit 0")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./internal/...", "./pkg/...", "./cmd/..."}
	}
	findings, err := logscope.Scan(".", patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *write != "" {
		keys := map[string]bool{}
		for _, f := range findings {
			keys[f.BaselineKey()] = true
		}
		lines := make([]string, 0, len(keys))
		for k := range keys {
			lines = append(lines, k)
		}
		sort.Strings(lines)
		header := "# logscope baseline — existing slog calls the compliance masker cannot mask reliably.\n# Remove a line when the call is fixed; never add one by hand for new code.\n"
		if err := os.WriteFile(*write, []byte(header+strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	base, err := piiscan.LoadBaseline(*baseline)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	failed := 0
	for _, f := range findings {
		if base[f.BaselineKey()] {
			continue
		}
		failed++
		fmt.Printf("%s:%d: %s key %q in %s\n", f.File, f.Line, f.Category, f.Key, f.Func)
	}
	if failed > 0 {
		fmt.Printf("\nlogscope: %d new finding(s). Log a map, a slice, basic values or an error instead of an opaque value, and keep secrets out of logs.\n", failed)
		os.Exit(1)
	}
}
```

Run:

```bash
cd backend && go run ./tools/logscope/cmd/logscope -write-baseline=tools/logscope/baseline.txt ./internal/... ./pkg/... ./cmd/...
wc -l tools/logscope/baseline.txt
go run ./tools/logscope/cmd/logscope -baseline=tools/logscope/baseline.txt ./internal/... ./pkg/... ./cmd/...
```

Expected: la baseline contiene le chiamate esistenti (l'analisi iniziale ne contava circa 81 di `slog.Any`, non tutte opache); il secondo comando esce con 0.

- [ ] **Step 6: Aggiungere il target al Makefile**

In `Makefile`, dopo il target `backend-piiscan`:

```make
# backend-logscope flags slog calls the compliance PolicyHandler cannot mask
# reliably: slog.Any with an opaque value, secret-looking keys with a dynamic
# value (compliance spec §2.6). Baseline carries the pre-existing calls.
backend-logscope:
	@cd backend && go test ./tools/logscope/...
	@cd backend && go run ./tools/logscope/cmd/logscope \
	  -baseline=tools/logscope/baseline.txt ./internal/... ./pkg/... ./cmd/...
```

Aggiungere `backend-logscope` alla riga `.PHONY` dei target backend e alla lista di `ci-backend`, subito dopo `backend-piiscan`.

Run: `make backend-logscope`
Expected: esce con 0.

- [ ] **Step 7: Commit**

```bash
git add backend/tools/logscope Makefile
git commit -F - <<'EOF'
build(ci): gate logscope sulle chiamate slog non mascherabili

Segnala slog.Any con valori opachi (struct, puntatori a struct, interfacce
diverse da error) e chiavi da segreto con valore dinamico; le chiamate
esistenti stanno nella baseline.

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---
### Task 9: Documentazione della T1

**Files:**
- Modify: `backend/internal/core/logging/AGENTS.md:98`
- Modify: `docs/site/modules/core/logging.mdx:43` e nuova sezione «Masking»
- Modify: `AGENTS.md:180` (elenco dei gate di `ci-backend`)

**Interfaces:**
- Consumes: il comportamento dei Task 1–8.
- Produces: documentazione; nessun codice.

- [ ] **Step 1: Correggere l'affermazione sul `tenant_id`**

In `backend/internal/core/logging/AGENTS.md`, sostituire la riga 98 con:

```markdown
- Per-tenant log levels — the threshold is global per module. Tenant-scoped filtering happens at query time in Loki via `tenant_id`, which is present on the `http_request` line (through `ctxauth.RequestAnnotations`) and on records logged with a context that carries the tenant; other module lines have it only when logged with a `*Context` call.
```

e aggiungere in fondo al file:

```markdown
## Compliance masking (compliance spec §2)

- Every record passes through `utils.PolicyHandler` (after the level gate,
  before the fan-out to stdout and OTLP). Secrets are always masked; IP,
  user agent, user ids, personal-data keys and free text follow the
  compliance policy of the record's tenant. A record without a tenant gets
  the strictest policy in force. Until the compliance module provides the
  live policy (T2), the platform defaults apply.
- `http_request` logs the chi route template (`route`), not the raw path;
  `path` appears only when no template matched.
- Outgoing spans are masked the same way by `telemetry.MaskingExporter`.
- `make backend-logscope` rejects new `slog.Any` calls with opaque values
  and secret-looking keys with dynamic values; pre-existing ones are in
  `tools/logscope/baseline.txt`.
```

- [ ] **Step 2: Aggiornare la pagina del sito**

In `docs/site/modules/core/logging.mdx`, riga 43, sostituire «via the `tenant_id` field already stamped on every line» con «via the `tenant_id` field of the `http_request` line and of records logged with a tenant-scoped context». In fondo alla pagina aggiungere:

```mdx
## Masking

Every log record and every exported span is masked before it leaves the
backend. Secrets (passwords, tokens, cookies, keys) are always replaced by
`[REDACTED]`. IP addresses, user agents, user identifiers, personal-data keys
and personal data found inside free text are handled according to the
compliance policy of the tenant the record belongs to: kept, truncated,
pseudonymised with a keyed hash, or removed. Records that belong to no
tenant get the strictest policy in force.

The `http_request` line identifies the endpoint by its route template
(`/v1/admin/users/{id}`), not by the raw path.
```

- [ ] **Step 3: Aggiornare l'elenco dei gate**

In `AGENTS.md`, riga 180: `(lint, tenantscope, policycoverage, piiscan, logscope, vuln, tests, build, openapi-check)`.

- [ ] **Step 4: Controlli e commit**

Run: `make agents-check && git diff --check`
Expected: nessun errore.

```bash
git add backend/internal/core/logging/AGENTS.md docs/site/modules/core/logging.mdx AGENTS.md
git commit -F - <<'EOF'
docs(logging): mascheramento dei log e delle trace, route template, logscope

Prop: upstream
Claude-Session: https://claude.ai/code/session_01Cu7Ufssk6RTyo9AMsj5WsU
EOF
```

---

### Task 10: Verifica finale della T1

**Files:** nessuno (solo verifica).

**Interfaces:**
- Consumes: l'intero branch.
- Produces: esito dei gate e della prova sullo stack di sviluppo.

- [ ] **Step 1: Gate completi**

Run: `make ci-backend`
Expected: `Backend CI: OK`. Non esportare `docker/.env` nell'ambiente di `make ci-backend`; servono `MONGO_TEST_URI` e `REDIS_URL` dello stack di sviluppo.

- [ ] **Step 2: Prova sullo stack di sviluppo**

Dopo la ricompilazione AIR, fare una richiesta autenticata (es. `GET /v1/me` con un token di `scripts/devtoken.sh administrator`) e leggere la riga di log:

```bash
STACK="$(sed -n 's/^APP_NAME=//p' docker/.env)-$(sed -n 's/^ENV=//p' docker/.env)"
docker logs --since 2m "$(docker ps -q --filter label=orkestra.stack=$STACK --filter label=com.docker.compose.service=backend)" 2>&1 | grep http_request | tail -3
```

Expected sulla riga della richiesta autenticata:
- `route=/v1/me` e nessun `path`;
- `user_id` presente (e `tenant_id` se il token ha un tenant);
- `remote` troncato a `/24` (default di piattaforma), nessun indirizzo intero.

- [ ] **Step 3: Revisione**

Usare `superpowers:requesting-code-review` sull'intero branch, poi `superpowers:finishing-a-development-branch`. La T2 (motore di policy) parte da questo branch dopo il merge.
