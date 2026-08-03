# Module Config UX — Phase 4 (migrate `auth`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `auth` a declared group tree so its 62 configuration fields render as 7 rail
sections with the four OAuth providers nested under one parent, and hide the 19 provider
credentials until that provider is switched on.

**Architecture:** Almost entirely a Go data change — `auth` gains `ConfigGroups()` and its
`ConfigField.Group` values move from display labels to group keys. One small SDK contract
extension is needed first (Task 1), because the current `DependsOn` cannot express the
condition every OAuth provider actually needs.

**Tech Stack:** Go 1.25.12, Huma v2; React 19 + TypeScript for the evaluator half; i18n
via react-i18next.

**Parent spec:** [`module-config-ux.md`](module-config-ux.md) §2.3, §3.
**Builds on:** phases 1–3, all merged to `dev`.

## Why this phase changes what operators see

`auth` is the only module with more than one group bucket today, so it is the only page
already showing the phase-3 card rail. The moment it declares `ConfigGroups()`, it also
clears `hasPageRail` — **this phase is the first time anyone sees the full-page rail**
(Overview → configuration tree → Dependencies → Environments). Everything phase 3 built
for that surface has, until now, been exercised only by test fixtures.

Two consequences:

- The rail path's latent defects surface here, all at once. Budget review attention for
  the *page*, not just the Go declarations.
- The other three core modules are untouched and keep the stacked page. Nothing about this
  phase should alter them; if it does, that is a regression on the modules an operator
  reaches most often.

## Global Constraints

- The `Module` interface stays frozen. `ConfigGroups()` is the optional `HasConfigGroups`
  sub-interface added in phase 1 — implement it on `*AuthModule`, do not widen anything.
- `pkg/sdk/` must not import from `backend/internal/`.
- Any new `ConfigField` member needs **both** `json` and `bson` tags, both `omitempty` —
  `ConfigField` is persisted in `module_configs` and rewritten by `RefreshMetadata` on
  every boot.
- Response-shape changes require `make openapi-dump` and a committed
  `openapi/enterprise.json`, or `make backend-openapi-check` fails CI.
- Frontend: bare path aliases, TypeScript strict, `npm run lint --max-warnings 0`.
- Every new i18n key must exist in **both** `src/locales/en.json` and `it.json`.
- **SCSS is not compiled by `vite build`.** If any SCSS changes, `public/css/theme.css`
  and `theme.rtl.css` must be recompiled and committed. This phase should need none.
- `cmd/server/config_declarations_test.go` validates every module's declarations against
  the real catalog. Once `auth` declares groups, that gate starts checking **every**
  `Group` reference — a typo fails the build rather than rendering a phantom rail entry.

---

### Task 1: `DependsOnMatch` — let a field depend on *any* of its conditions

**Files:**
- Modify: `backend/pkg/sdk/module/types.go` (`ConfigField`)
- Modify: `backend/pkg/sdk/module/config_validate.go`
- Modify: `backend/pkg/sdk/module/config_validate_test.go`
- Modify: `backend/pkg/sdk/module/config_groups_test.go`
- Modify: `backend/openapi/enterprise.json` (regenerated)
- Modify: `frontend-admin/src/store/api/moduleApi.ts` (`ConfigField`)
- Modify: `frontend-admin/src/pages/admin/modules/configModel.ts` (`isFieldVisible`)
- Modify: `frontend-admin/src/pages/admin/modules/configModel.test.ts`

**Interfaces:**
- Produces: `ConfigField.DependsOnMatch string` — `""`/`"all"` (default, unchanged AND
  semantics) or `"any"`. Mirrored as `dependsOnMatch?: 'all' | 'any'` on the TS type.

**Why this is needed, concretely.** Every OAuth provider in `auth` has **two** independent
enable toggles — `googleEnabledAdmin` (operator console) and `googleEnabledClient` (client
app). Its five credential fields must appear when the provider is on for **either**
surface. `DependsOn` is AND across entries and OR only *within* one entry's `In` list, so:

- both conditions → credentials appear only when **both** surfaces are enabled, so an
  operator who enables Google only on the client app can never reach the Client ID field;
- one condition → wrong for the other surface.

The parent spec wrote this as "`dependsOn googleEnabled{Admin,Client}`" and assumed an OR
that the contract does not have.

- [ ] **Step 1: Write the failing Go test**

Append to `backend/pkg/sdk/module/config_groups_test.go`:

```go
func TestConfigField_DependsOnMatchTag(t *testing.T) {
	f := ConfigField{
		Key:            "googleClientId",
		Label:          "Client ID",
		Type:           FieldString,
		DependsOnMatch: "any",
		DependsOn: []FieldCondition{
			{Key: "googleEnabledAdmin", In: []string{"true"}},
			{Key: "googleEnabledClient", In: []string{"true"}},
		},
	}

	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal = %v", err)
	}
	if !strings.Contains(string(raw), `"dependsOnMatch":"any"`) {
		t.Errorf("json payload %s missing dependsOnMatch", raw)
	}

	// omitempty: a field using the default AND semantics must not carry the key.
	bare, err := json.Marshal(ConfigField{Key: "k", Type: FieldString})
	if err != nil {
		t.Fatalf("json.Marshal bare = %v", err)
	}
	if strings.Contains(string(bare), "dependsOnMatch") {
		t.Errorf("bare payload %s should omit dependsOnMatch", bare)
	}

	// bson: ConfigSchema is persisted and rewritten from the binary on every
	// boot. A missing bson tag serves correctly then vanishes across a restart.
	encoded, err := bson.Marshal(f)
	if err != nil {
		t.Fatalf("bson.Marshal = %v", err)
	}
	var back ConfigField
	if err := bson.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("bson.Unmarshal = %v", err)
	}
	if back.DependsOnMatch != "any" {
		t.Errorf("DependsOnMatch after bson round-trip = %q, want %q", back.DependsOnMatch, "any")
	}
}
```

Append to `backend/pkg/sdk/module/config_validate_test.go`:

```go
func TestValidateConfigDeclarations_DependsOnMatch(t *testing.T) {
	base := []ConfigField{
		{Key: "on", Label: "On", Type: FieldBool},
		{Key: "dep", Label: "Dep", Type: FieldString,
			DependsOn: []FieldCondition{{Key: "on", In: []string{"true"}}}},
	}

	for _, ok := range []string{"", "all", "any"} {
		schema := append([]ConfigField(nil), base...)
		schema[1].DependsOnMatch = ok
		if err := ValidateConfigDeclarations(schema, nil); err != nil {
			t.Errorf("DependsOnMatch %q = %v, want nil", ok, err)
		}
	}

	schema := append([]ConfigField(nil), base...)
	schema[1].DependsOnMatch = "either"
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for an unknown match mode")
	}
	if !strings.Contains(err.Error(), "either") {
		t.Errorf("error %q does not name the offending value", err)
	}

	// A match mode with no conditions to match is a declaration mistake.
	lone := []ConfigField{{Key: "a", Label: "A", Type: FieldString, DependsOnMatch: "any"}}
	if err := ValidateConfigDeclarations(lone, nil); err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for a match mode without DependsOn")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd backend && go test ./pkg/sdk/module/ -run 'DependsOnMatch' -v
```

Expected: FAIL to compile — `unknown field DependsOnMatch`.

- [ ] **Step 3: Add the field**

In `ConfigField`, directly below `DependsOn`:

```go
	// DependsOnMatch selects how the DependsOn conditions combine.
	// "" and "all" (the default) require every condition to hold; "any"
	// requires at least one.
	//
	// "any" exists because a capability can legitimately be enabled from more
	// than one independent switch. Every OAuth provider in auth has two —
	// one per audience surface — and its credentials are needed as soon as
	// either is on. Without this, the only expressible rules are "both
	// surfaces" (which strands a client-only deployment) or a single surface
	// (which is simply wrong for the other).
	DependsOnMatch string `json:"dependsOnMatch,omitempty" bson:"dependsOnMatch,omitempty"`
```

- [ ] **Step 4: Validate it**

In `ValidateConfigDeclarations`, inside the per-field loop:

```go
		switch f.DependsOnMatch {
		case "", "all", "any":
		default:
			problems = append(problems,
				fmt.Sprintf("field %q has unknown DependsOnMatch %q (want \"all\" or \"any\")",
					f.Key, f.DependsOnMatch))
		}
		if f.DependsOnMatch != "" && len(f.DependsOn) == 0 {
			problems = append(problems,
				fmt.Sprintf("field %q sets DependsOnMatch %q but declares no DependsOn",
					f.Key, f.DependsOnMatch))
		}
```

- [ ] **Step 5: Run the Go tests**

```bash
cd backend && go test ./pkg/sdk/module/ ./cmd/server/
```

Expected: PASS.

- [ ] **Step 6: Regenerate the OpenAPI spec**

Infra must be up; the dump uses an isolated Mongo namespace and Redis index, so dev data is
untouched. **Do not start, stop, or rebuild any container** — if it is not already running,
stop and report.

```bash
cd backend && make openapi-dump && make openapi-check
```

Expected: `✓ openapi/*.json matches the current source.`

- [ ] **Step 7: Mirror the type on the frontend**

In `frontend-admin/src/store/api/moduleApi.ts`, add to `ConfigField`:

```ts
  dependsOnMatch?: 'all' | 'any';
```

and extend the `FieldCondition` doc comment to say the combination rule is chosen by
`dependsOnMatch`.

- [ ] **Step 8: Write the failing frontend test**

Append to `frontend-admin/src/pages/admin/modules/configModel.test.ts`, inside the
`isFieldVisible` describe:

```ts
  it("ORs the conditions when dependsOnMatch is 'any'", () => {
    // Each OAuth provider has one toggle per audience surface, and its
    // credentials are needed as soon as either is on.
    const s = [
      field({ key: 'admin', type: 'bool', default: 'false' }),
      field({ key: 'client', type: 'bool', default: 'false' }),
      field({
        key: 'cred',
        dependsOnMatch: 'any',
        dependsOn: [
          { key: 'admin', in: ['true'] },
          { key: 'client', in: ['true'] }
        ]
      })
    ];
    expect(isFieldVisible(s[2], {}, s)).toBe(false);
    expect(isFieldVisible(s[2], { admin: 'true' }, s)).toBe(true);
    expect(isFieldVisible(s[2], { client: 'true' }, s)).toBe(true);
    expect(isFieldVisible(s[2], { admin: 'true', client: 'true' }, s)).toBe(true);
  });

  it("keeps AND semantics when dependsOnMatch is absent or 'all'", () => {
    const build = (match?: 'all' | 'any') => [
      field({ key: 'a', type: 'bool', default: 'false' }),
      field({ key: 'b', type: 'bool', default: 'false' }),
      field({
        key: 'dep',
        dependsOnMatch: match,
        dependsOn: [
          { key: 'a', in: ['true'] },
          { key: 'b', in: ['true'] }
        ]
      })
    ];
    for (const match of [undefined, 'all' as const]) {
      const s = build(match);
      expect(isFieldVisible(s[2], { a: 'true' }, s)).toBe(false);
      expect(isFieldVisible(s[2], { a: 'true', b: 'true' }, s)).toBe(true);
    }
  });
```

- [ ] **Step 9: Implement the evaluator**

In `configModel.ts`'s `isFieldVisible`, extract the per-condition predicate and choose the
combinator:

```ts
  const satisfied = (cond: FieldCondition): boolean => {
    /* …existing body of the current .every() callback, unchanged… */
  };
  return field.dependsOnMatch === 'any'
    ? field.dependsOn.some(satisfied)
    : field.dependsOn.every(satisfied);
```

- [ ] **Step 10: Run the full gate on both halves**

```bash
cd backend && go test ./... && cd ../frontend-admin && npm run typecheck && npm run lint && npm run test
```

- [ ] **Step 11: Commit**

```bash
git add backend/pkg/sdk/module/ backend/openapi/enterprise.json \
        frontend-admin/src/store/api/moduleApi.ts \
        frontend-admin/src/pages/admin/modules/configModel.ts \
        frontend-admin/src/pages/admin/modules/configModel.test.ts
git commit -m "feat(sdk): let a config field depend on any of its conditions

DependsOn combines with AND, and OR only within one condition's value list,
so a capability enabled from two independent switches could not be expressed.
Every OAuth provider in auth is exactly that shape: one toggle per audience
surface, credentials needed as soon as either is on. The only expressible
rules were \"both surfaces\" — which strands a client-only deployment — or a
single surface, which is wrong for the other.

DependsOnMatch defaults to the existing AND behaviour, so no declaration
changes meaning."
```

---

### Task 2: give `auth` its group tree

**Files:**
- Modify: `backend/internal/core/auth/module.go`
- Test: `backend/internal/core/auth/config_groups_test.go` (create)

**Interfaces:**
- Consumes: `module.ConfigGroup`, `module.FieldCondition`, `DependsOnMatch` from Task 1.
- Produces: `func (m *AuthModule) ConfigGroups() []module.ConfigGroup`.

**The tree** — 7 top-level nodes, 4 nested under `oauth`, 62 fields total:

| Key | Label | Parent | Order | Fields |
| --- | --- | --- | ---: | ---: |
| `registration` | Registration | | 1 | 5 |
| `login` | Login & Sessions | | 2 | 6 |
| `password` | Password Policy | | 3 | 7 |
| `mfa` | MFA | | 4 | 5 |
| `oauth` | OAuth Providers | | 5 | 11 |
| `oauth.google` | Google | `oauth` | 6 | 5 |
| `oauth.apple` | Apple | `oauth` | 7 | 8 |
| `oauth.github` | GitHub | `oauth` | 8 | 3 |
| `oauth.discord` | Discord | `oauth` | 9 | 3 |
| `antiabuse` | Anti-abuse & Notifications | | 10 | 7 |
| `sessions` | Sessions & Account | | 11 | 2 |

Every current `Group:` display label maps 1:1 onto one of these keys, so no field changes
bucket — only the identifier does.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/core/auth/config_groups_test.go`:

```go
package auth

import (
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

func schemaOf(t *testing.T) []module.ConfigField {
	t.Helper()
	return (&AuthModule{}).ConfigSchema()
}

func TestConfigGroups_TreeShape(t *testing.T) {
	groups := (&AuthModule{}).ConfigGroups()
	byKey := make(map[string]module.ConfigGroup, len(groups))
	for _, g := range groups {
		byKey[g.Key] = g
	}

	for _, key := range []string{
		"registration", "login", "password", "mfa", "oauth",
		"oauth.google", "oauth.apple", "oauth.github", "oauth.discord",
		"antiabuse", "sessions",
	} {
		if _, ok := byKey[key]; !ok {
			t.Errorf("group %q not declared", key)
		}
	}

	for _, key := range []string{"oauth.google", "oauth.apple", "oauth.github", "oauth.discord"} {
		if got := byKey[key].Parent; got != "oauth" {
			t.Errorf("group %q Parent = %q, want %q", key, got, "oauth")
		}
	}
	if byKey["oauth"].Parent != "" {
		t.Errorf("oauth must be top-level, got Parent %q", byKey["oauth"].Parent)
	}
}

func TestConfigGroups_EveryFieldIsGrouped(t *testing.T) {
	groups := (&AuthModule{}).ConfigGroups()
	declared := make(map[string]bool, len(groups))
	for _, g := range groups {
		declared[g.Key] = true
	}
	for _, f := range schemaOf(t) {
		if f.Group == "" {
			t.Errorf("field %q has no group — it would be unreachable in the settings rail", f.Key)
			continue
		}
		if !declared[f.Group] {
			t.Errorf("field %q references undeclared group %q", f.Key, f.Group)
		}
	}
}

func TestConfigGroups_DeclarationsValidate(t *testing.T) {
	// The same checker cmd/server runs over the whole catalog, applied here so
	// a mistake fails this module's own package first.
	if err := module.ValidateConfigDeclarations(
		schemaOf(t), (&AuthModule{}).ConfigGroups(),
	); err != nil {
		t.Errorf("ValidateConfigDeclarations: %v", err)
	}
}

func TestProviderCredentials_HiddenUntilEitherSurfaceEnabled(t *testing.T) {
	// The whole point of the migration: 19 of 62 fields are provider
	// credentials that are dead weight on an install not using that provider.
	cases := map[string][]string{
		"oauth.google":  {"googleEnabledAdmin", "googleEnabledClient"},
		"oauth.apple":   {"appleEnabledAdmin", "appleEnabledClient"},
		"oauth.github":  {"githubEnabledAdmin", "githubEnabledClient"},
		"oauth.discord": {"discordEnabledAdmin", "discordEnabledClient"},
	}
	counted := 0
	for _, f := range schemaOf(t) {
		want, ok := cases[f.Group]
		if !ok {
			continue
		}
		counted++
		if f.DependsOnMatch != "any" {
			t.Errorf("field %q DependsOnMatch = %q, want \"any\" — a provider enabled on only one surface still needs its credentials",
				f.Key, f.DependsOnMatch)
		}
		got := make(map[string]bool, len(f.DependsOn))
		for _, c := range f.DependsOn {
			got[c.Key] = true
		}
		for _, key := range want {
			if !got[key] {
				t.Errorf("field %q does not depend on %q", f.Key, key)
			}
		}
	}
	if counted != 19 {
		t.Errorf("gated %d provider credential fields, want 19", counted)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd backend && go test ./internal/core/auth/ -run 'ConfigGroups|ProviderCredentials' -v
```

Expected: FAIL — `ConfigGroups` undefined.

- [ ] **Step 3: Declare the groups**

Add to `backend/internal/core/auth/module.go`, next to `ConfigSchema`:

```go
// ConfigGroups gives the admin settings page a sectioned rail instead of one
// flat list. auth is by far the largest configuration surface in the base —
// 62 fields — and the four OAuth providers are declared as children of the
// single "OAuth Providers" node rather than as siblings of it, which is what
// the old flat Group labels made them look like.
func (m *AuthModule) ConfigGroups() []module.ConfigGroup {
	return []module.ConfigGroup{
		{Key: "registration", Label: "Registration", Order: 1,
			Description: "Who may create an account, and on which surface."},
		{Key: "login", Label: "Login & Sessions", Order: 2,
			Description: "Sign-in availability, lockout, and token lifetimes."},
		{Key: "password", Label: "Password Policy", Order: 3,
			Description: "Rules enforced on every password change, on both the operator console and the client app."},
		{Key: "mfa", Label: "MFA", Order: 4,
			Description: "Second-factor requirements and enrollment."},
		{Key: "oauth", Label: "OAuth Providers", Order: 5,
			Description: "Which providers are offered, and on which surface. Each provider's credentials appear once it is switched on."},
		{Key: "oauth.google", Label: "Google", Parent: "oauth", Order: 6},
		{Key: "oauth.apple", Label: "Apple", Parent: "oauth", Order: 7},
		{Key: "oauth.github", Label: "GitHub", Parent: "oauth", Order: 8},
		{Key: "oauth.discord", Label: "Discord", Parent: "oauth", Order: 9},
		{Key: "antiabuse", Label: "Anti-abuse & Notifications", Order: 10,
			Description: "Rate limiting, IP and country rules, and the alerts they raise."},
		{Key: "sessions", Label: "Sessions & Account", Order: 11},
	}
}
```

- [ ] **Step 4: Move `Group` values from labels to keys**

In `ConfigSchema()`, rewrite every `Group:` value:

| Was | Becomes |
| --- | --- |
| `"Google"` | `"oauth.google"` |
| `"Apple"` | `"oauth.apple"` |
| `"GitHub"` | `"oauth.github"` |
| `"Discord"` | `"oauth.discord"` |
| `"Registration"` | `"registration"` |
| `"Login & Sessions"` | `"login"` |
| `"MFA"` | `"mfa"` |
| `"Password Policy"` | `"password"` |
| `"OAuth Providers"` | `"oauth"` |
| `"Anti-abuse & Notifications"` | `"antiabuse"` |
| `"Sessions & Account"` | `"sessions"` |

Mechanical and total — after this, no `Group:` in the file holds a human-readable label.

- [ ] **Step 5: Gate the 19 provider credentials**

Add to every field in `oauth.google` / `oauth.apple` / `oauth.github` / `oauth.discord`:

```go
			DependsOnMatch: "any",
			DependsOn: []module.FieldCondition{
				{Key: "googleEnabledAdmin", In: []string{"true"}},
				{Key: "googleEnabledClient", In: []string{"true"}},
			},
```

substituting the provider's own two toggles. The eight toggles themselves live in `oauth`
and are **never** gated — they are how the operator turns a provider on.

- [ ] **Step 6: Collapse the rarely-touched fields**

Set `Advanced: true` on exactly these eight, all verified present in the file:

`googleAndroidClientId`, `googleIOSClientId`, `appleIOSClientId`, `appleAndroidClientId`
(mobile-only client IDs), `applePrivateKeyPath` (filesystem fallback for the inline PEM),
`recoveryCodesCount`, `inactiveAccountAutoDisableDays`, `oauthAutoLinkByEmail`.

- [ ] **Step 7: Run the tests**

```bash
cd backend && go test ./internal/core/auth/ ./cmd/server/ ./pkg/sdk/module/ -v -run 'ConfigGroups|ProviderCredentials|ConfigDeclarations|EveryGroupHasFields'
```

Expected: PASS. `cmd/server`'s catalog gate now genuinely exercises the group half of the
validator for the first time — before this, every module declared zero groups.

- [ ] **Step 8: Regenerate the OpenAPI spec**

`configSchema` is embedded in the response, so the dumped spec changes.

```bash
cd backend && make openapi-dump && make openapi-check
```

- [ ] **Step 9: Full backend CI**

```bash
cd /home/tore/orkestra && make ci-backend
```

- [ ] **Step 10: Commit**

```bash
git add backend/internal/core/auth/ backend/openapi/enterprise.json
git commit -m "feat(auth): declare the configuration group tree

auth carries 62 of the base's 79 config fields and rendered them as 11 flat
buckets, in which Google, Apple, GitHub and Discord appeared as siblings of
\"OAuth Providers\" rather than as its children.

They are now nested under it, and each provider's credentials are hidden
until that provider is switched on for either audience surface — 19 of the 62
fields, dead weight on an install that does not use them. A fresh install
shows 43.

Eight rarely-touched fields (mobile client IDs, the .p8 path fallback,
recovery-code count, inactive-account auto-disable, OAuth auto-link) collapse
under Advanced."
```

---

### Task 3: Italian and English labels for `auth`'s configuration

Until now every config label came straight from the Go schema as an English literal, so an
Italian operator saw a page with Italian chrome and 62 English field labels. The resolver
built in phase 2 reads `moduleConfig.auth.fields.<key>.{label,desc}` and
`moduleConfig.auth.groups.<key>.label` from the core bundle before falling back to the
literal.

**Files:**
- Modify: `frontend-admin/src/locales/en.json`, `frontend-admin/src/locales/it.json`
- Test: `frontend-admin/src/locales/parity.test.ts` (already covers the new keys)

- [ ] **Step 1: Add the group labels**

Add a `moduleConfig.auth.groups` object to **both** locale files, one entry per group key
declared in Task 2 (11 of them), with `desc` for the six that carry a `Description` — take the count from the Go source, not from this sentence. The
resolver looks up `moduleConfig.auth.groups.<groupKey>.label` through `t()`. `src/i18n.ts`
sets no `keySeparator`, so i18next splits on `.` — but its lookup **also** rejoins split
segments and resolves a literal flat property, so *both* shapes work. (Verified with a
bare `i18next.init()` carrying one of each: both resolve. An earlier draft of this plan
claimed the flat form would silently fall back to the backend literal; that was wrong.)

Author the nested shape anyway — it keeps the four providers visibly grouped under their
parent in the file. The `oauth` object carries the parent's own `label`/`desc` alongside
its four children: `…groups.oauth.label` resolves the parent, `…groups.oauth.google.label`
the child.

```jsonc
// en.json
"moduleConfig": {
  "auth": {
    "groups": {
      "registration": { "label": "Registration", "desc": "Who may create an account, and on which surface." },
      "oauth": {
        "label": "OAuth Providers",
        "desc": "Which providers are offered, and on which surface. Each provider's credentials appear once it is switched on.",
        "google": { "label": "Google" },
        "apple": { "label": "Apple" },
        "github": { "label": "GitHub" },
        "discord": { "label": "Discord" }
      }
      // …the remaining 5 top-level groups
    }
  }
}
```

```jsonc
// it.json — same shape
"moduleConfig": {
  "auth": {
    "groups": {
      "registration": { "label": "Registrazione", "desc": "Chi può creare un account, e su quale superficie." },
      "oauth": {
        "label": "Provider OAuth",
        "desc": "Quali provider sono offerti, e su quale superficie. Le credenziali di ciascuno compaiono quando lo si attiva.",
        "google": { "label": "Google" },
        "apple": { "label": "Apple" },
        "github": { "label": "GitHub" },
        "discord": { "label": "Discord" }
      }
      // …the remaining 5 top-level groups
    }
  }
}
```

Confirm the resolution actually works before writing all 11 — render one nested group in a
test and assert the child label resolves, so a wrong assumption fails loudly rather than
silently falling back to the English literal (which is exactly what a missing key looks
like).

> **Note for phase 5 — dotted field keys are two separate problems, and only one of them
> is cosmetic.** `notification`'s *field* keys contain dots (`email.provider`,
> `email.smtp.host`).
>
> **i18n: cosmetic.** Per the check above, i18next resolves either shape, so whether the
> bundle spells the key flat (`"email.smtp.host": {…}`) or nested
> (`email: { smtp: { host: {…} } }`) is a readability choice — but pick one and hold it,
> because a file mixing both reads as a merge accident later.
>
> **React Hook Form: a correctness bug, and phase 5 must fix it before migrating
> `notification`.** RHF treats `.` in a field `name` as a **path separator**, and
> `ModuleConfigFields` registers by `field.key` verbatim (`register(key)` /
> `Controller name={key}`). Verified against the installed `react-hook-form@7.76.1`:
> seeding `defaultValues: { 'email.provider': 'smtp' }` and then typing into
> `register('email.provider')` leaves `getValues()` as
> `{"email.provider":"smtp","email":{"provider":"ses"}}` — the flat property keeps the
> *stale seeded* value while the edit lands at the nested path. `collectDiff`
> (`useModuleConfigForm.ts`) reads `values[field.key]` flat, so it compares the untouched
> seed against the defaults, finds no change, and **silently drops the operator's edit on
> save**. The same flat-lookup assumption is baked into `isFieldVisible`/`visibleFields`
> (a `dependsOn` naming a dotted key would never see the live value) and into the yup
> shape, which is built as `shape[field.key]` — a literal property name, not a path, so
> RHF's dot-path error keys would not line up with it either.
>
> auth is unaffected: all 62 of its keys are dot-free. Phase 5 has to decide how to
> reconcile the two — escape the name at registration and unescape in the diff, flatten
> RHF's values before `collectDiff` reads them, or rename `notification`'s keys — and that
> decision is independent of, and not settled by, the i18n question above.

- [ ] **Step 2: Add the field labels**

Add `moduleConfig.auth.fields`, one entry per field key in `ConfigSchema()` (62), with
`label` always and `desc` only where the Go schema carries a `Description`:

```jsonc
// en.json — mirrors the Go Label/Description verbatim
"fields": {
  "passwordMinLength": { "label": "Minimum length" },
  "breachedPasswordCheck": {
    "label": "Reject breached passwords (HIBP)",
    "desc": "Check new passwords against the Have I Been Pwned range API."
  }
}
```

```jsonc
// it.json — real Italian, not the English pasted through
"fields": {
  "passwordMinLength": { "label": "Lunghezza minima" },
  "breachedPasswordCheck": {
    "label": "Rifiuta password compromesse (HIBP)",
    "desc": "Verifica le nuove password sulla range API di Have I Been Pwned."
  }
}
```

Take the EN text from the Go `Label` / `Description` verbatim, so the core bundle is a
faithful mirror and a later change to either is easy to spot.

Leave untranslated in IT — product vocabulary and industry terms, consistent with the 358
intentional EN==IT entries the repo already documents: `OAuth`, `MFA`, `client ID`,
`client secret`, `redirect URL`, `token`, `HIBP`, `SMTP`, `.p8`, and every provider name.

- [ ] **Step 3: Verify parity and no dead keys**

```bash
cd frontend-admin && npx vitest run src/locales/parity.test.ts
```

Expected: PASS — every key present in both files.

Then confirm each declared group key has a matching label entry:

```bash
cd frontend-admin && node -e "
const en = require('./src/locales/en.json');
const groups = Object.keys(en.moduleConfig.auth.groups);
const fields = Object.keys(en.moduleConfig.auth.fields);
console.log('groups:', groups.length, 'fields:', fields.length);
"
```

Expected: 11 groups, 62 fields.

- [ ] **Step 4: Full frontend gate**

```bash
cd frontend-admin && npm run typecheck && npm run lint && npm run test
```

- [ ] **Step 5: Commit**

```bash
git add frontend-admin/src/locales/
git commit -m "feat(i18n): translate the auth module's configuration labels

Config labels came straight from the Go schema as English literals, so an
Italian operator saw Italian chrome around 62 English field labels — auth is
78% of the base's configuration surface, so this was most of what they read.

Keys are derived from the backend's own stable field keys, so the schema
carries no redundant i18n field and the English literal remains the final
fallback for anything left untranslated."
```

---

### Task 4: documentation

**Files:**
- Modify: `backend/internal/core/auth/CLAUDE.md`
- Modify: `backend/pkg/sdk/CLAUDE.md`
- Modify: `docs/plans/module-config-ux.md` (§8 phase 4 → ✅)

- [ ] **Step 1: `auth`'s own CLAUDE.md**

Document the group tree as the shape a contributor must keep valid when adding a field:
every field needs a `Group` naming a declared key, a provider credential needs the
`DependsOnMatch: "any"` pair for that provider, and the catalog gate fails the build on a
mistake.

- [ ] **Step 2: `backend/pkg/sdk/CLAUDE.md`**

Add `DependsOnMatch` to the config-contract notes: default `all`, `any` for a capability
reachable from more than one switch, with `auth`'s OAuth providers as the worked example.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/core/auth/CLAUDE.md backend/pkg/sdk/CLAUDE.md docs/plans/module-config-ux.md
git commit -m "docs: record auth's config group tree and the any-match rule"
```

---

## Phase exit criteria

- `make ci-backend` green; `npm run typecheck && npm run lint && npm run test` green.
- `/admin/modules/auth` renders the **full-page rail** — the first module ever to. Verify
  against the running stack, not only in tests: this surface has had no real exercise.
- A fresh install shows **43** of `auth`'s 62 fields; enabling a provider reveals its
  credentials; disabling it hides them again **without discarding the stored secret**.
- `notification`, `tenant` and `compliance` are untouched and still render the stacked page.
- No `Group:` value in `auth/module.go` is a human-readable label.

Update [`module-config-ux.md`](module-config-ux.md) §8: set phase 4 to ✅.
