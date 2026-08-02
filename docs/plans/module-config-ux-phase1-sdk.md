# Module Config UX — Phase 1 (SDK contract) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the SDK config contract with presentation groups, conditional
visibility, and declarative validation metadata, and serve the groups from the admin
API — with no user-visible change.

**Architecture:** `ConfigGroup` is a new type carried by a new optional sub-interface
`HasConfigGroups`, read through a `ConfigGroupsOf(m)` type-assertion accessor — the
pattern `pkg/sdk/CLAUDE.md` mandates ("Never widen the `Module` interface"). Groups are
**never persisted**: `toConfigResponse` already resolves the live module out of
`h.registry.AllModules()` to fill service declarations, and groups are filled in the same
loop. `ConfigField` gains optional metadata fields, all `omitempty`, all additive.

**Tech Stack:** Go 1.25.12, Huma v2, MongoDB driver (bson tags on the persisted
`ConfigField` only), stdlib `testing`.

**Parent spec:** [`module-config-ux.md`](module-config-ux.md) §2, §5, §8 (phase 1).

## Global Constraints

- `pkg/sdk/` must not import anything from `backend/internal/`. Verify with
  `grep -rn "internal/" backend/pkg/sdk/ --include="*.go"` — only doc-comment hits allowed.
- The `Module` interface stays frozen at `Name` / `Category` / `Init`. New capability =
  new `HasFoo` sub-interface + `FooOf(m)` accessor.
- `ConfigField` is persisted (`bson:"configSchema"` on `ModuleConfig`), so **every new
  `ConfigField` field needs both a `json` and a `bson` tag**, both `omitempty`.
- `ConfigGroup` is **not** persisted, so it carries `json` tags only — no `bson` tags.
- Route/response shape changes require `make openapi-dump` from `backend/`, with
  `openapi/enterprise.json` committed, or `make backend-openapi-check` fails CI.
- Go test files in this package use plain `testing` with table-driven `t.Errorf`, package
  `module` (internal tests) — see `module_minimal_test.go`, `registry_navitem_test.go`.

---

### Task 1: `ConfigGroup`, `Condition`, and the `ConfigGroupsOf` accessor

**Files:**
- Modify: `backend/pkg/sdk/module/types.go:32-43` (the `ConfigField` struct)
- Modify: `backend/pkg/sdk/module/types.go` (add `ConfigGroup` + `Condition` above `ConfigField`)
- Modify: `backend/pkg/sdk/module/module.go:56-61` (add `HasConfigGroups` next to `HasConfigSchema`)
- Modify: `backend/pkg/sdk/module/module.go:356-363` (add `ConfigGroupsOf` next to `RequiredServicesOf`)
- Test: `backend/pkg/sdk/module/config_groups_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type ConfigGroup struct { Key, Label, Description, Icon, Parent string; Order int }`
  - `type Condition struct { Key string; In []string }`
  - `type HasConfigGroups interface { ConfigGroups() []ConfigGroup }`
  - `func ConfigGroupsOf(m Module) []ConfigGroup`
  - `ConfigField` gains `Advanced bool`, `DependsOn []Condition`, `Min *int`, `Max *int`,
    `Pattern string`, `Placeholder string`, `HelpURL string`

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/sdk/module/config_groups_test.go`:

```go
package module

import "testing"

// groupedModule implements Module + HasConfigGroups. It mirrors the shape
// core auth will take: a parent group with nested provider children.
type groupedModule struct{}

func (groupedModule) Name() string               { return "grouped" }
func (groupedModule) Category() ModuleCategory   { return CategoryToggleable }
func (groupedModule) Init(_ *Dependencies) error { return nil }
func (groupedModule) ConfigGroups() []ConfigGroup {
	return []ConfigGroup{
		{Key: "oauth", Label: "OAuth Providers", Order: 1},
		{Key: "oauth.google", Label: "Google", Parent: "oauth", Order: 2},
	}
}

var _ HasConfigGroups = groupedModule{}

func TestConfigGroupsOf_Declared(t *testing.T) {
	got := ConfigGroupsOf(groupedModule{})
	if len(got) != 2 {
		t.Fatalf("ConfigGroupsOf returned %d groups, want 2", len(got))
	}
	if got[0].Key != "oauth" {
		t.Errorf("first group Key = %q, want %q", got[0].Key, "oauth")
	}
	if got[1].Parent != "oauth" {
		t.Errorf("nested group Parent = %q, want %q", got[1].Parent, "oauth")
	}
}

func TestConfigGroupsOf_NotDeclared(t *testing.T) {
	// A module that does not implement the sub-interface must degrade to nil,
	// not panic — this is the path every un-migrated fork addon takes.
	if got := ConfigGroupsOf(minimalModule{name: "minimal"}); got != nil {
		t.Errorf("ConfigGroupsOf = %v, want nil for a module without the sub-interface", got)
	}
}

func TestConfigField_CarriesVisibilityAndValidationMetadata(t *testing.T) {
	min, max := 8, 128
	f := ConfigField{
		Key:         "googleClientId",
		Label:       "Client ID",
		Group:       "oauth.google",
		Type:        FieldString,
		Advanced:    true,
		DependsOn:   []Condition{{Key: "googleEnabledAdmin", In: []string{"true"}}},
		Min:         &min,
		Max:         &max,
		Pattern:     `^[A-Za-z0-9._-]+$`,
		Placeholder: "1234-abc.apps.googleusercontent.com",
		HelpURL:     "https://docs.orkestra.cc/auth/oauth",
	}
	if !f.Advanced {
		t.Error("Advanced did not round-trip")
	}
	if len(f.DependsOn) != 1 || f.DependsOn[0].Key != "googleEnabledAdmin" {
		t.Errorf("DependsOn = %+v, want one condition on googleEnabledAdmin", f.DependsOn)
	}
	if f.Min == nil || *f.Min != 8 || f.Max == nil || *f.Max != 128 {
		t.Errorf("Min/Max = %v/%v, want 8/128", f.Min, f.Max)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd backend && go test ./pkg/sdk/module/ -run 'ConfigGroups|CarriesVisibility' -v
```

Expected: FAIL to **compile** — `undefined: ConfigGroup`, `undefined: HasConfigGroups`,
`undefined: ConfigGroupsOf`, and `unknown field Advanced in struct literal`.

- [ ] **Step 3: Add the types to `types.go`**

Insert immediately above the existing `ConfigField` declaration:

```go
// ConfigGroup describes one section of the admin settings rail. Groups form
// a tree through Parent, so a module can nest (e.g. "oauth" → "oauth.google")
// instead of flattening every section into one row of tabs.
//
// Deliberately NOT persisted: unlike ConfigSchema, which is snapshotted into
// module_configs and refreshed by RefreshMetadata on every boot, groups are
// pure presentation and fully code-derived. The admin handler resolves them
// from the live registry on each request, so there is nothing to keep in sync
// and no bson tags here.
//
// Label is the literal EN fallback. The admin UI prefers the derived i18n key
// config.groups.<Key>.label and falls back to this string.
type ConfigGroup struct {
	Key         string `json:"key"`                   // stable identifier, never translated
	Label       string `json:"label"`                 // literal EN fallback
	Description string `json:"description,omitempty"` // panel subtitle
	Icon        string `json:"icon,omitempty"`        // FontAwesome name
	Parent      string `json:"parent,omitempty"`      // Key of the parent group
	Order       int    `json:"order,omitempty"`
}

// Condition gates a field's visibility on the value of another field of the
// SAME module. Semantics: AND across a field's DependsOn slice, OR within a
// single condition's In list.
//
// A struct rather than an expression string on purpose — there is no parser to
// write, ship, and keep behaviourally identical between Go and TypeScript.
type Condition struct {
	Key string   `json:"key"` // another field key of the same module
	In  []string `json:"in"`  // values that satisfy the condition
}
```

Then extend `ConfigField` with the new fields, keeping the existing ones untouched:

```go
	Options     []string        `json:"options,omitempty" bson:"options,omitempty"` // valid values for FieldEnum (ignored for other types)
	Advanced    bool            `json:"advanced,omitempty" bson:"advanced,omitempty"`
	DependsOn   []Condition     `json:"dependsOn,omitempty" bson:"dependsOn,omitempty"`
	Min         *int            `json:"min,omitempty" bson:"min,omitempty"`
	Max         *int            `json:"max,omitempty" bson:"max,omitempty"`
	Pattern     string          `json:"pattern,omitempty" bson:"pattern,omitempty"`
	Placeholder string          `json:"placeholder,omitempty" bson:"placeholder,omitempty"`
	HelpURL     string          `json:"helpUrl,omitempty" bson:"helpUrl,omitempty"`
```

Also update the `ConfigField.Group` comment — it now carries a `ConfigGroup.Key`, not a
display label:

```go
	Group       string          `json:"group,omitempty" bson:"group,omitempty"` // ConfigGroup.Key when the module declares ConfigGroups(); a legacy display label when it does not
```

- [ ] **Step 4: Add the sub-interface and accessor to `module.go`**

Immediately after the `HasConfigSchema` block:

```go
// HasConfigGroups lets a module declare presentation groups for the fields
// in its ConfigSchema. Purely cosmetic: a module that omits it renders a
// flat form, which is the degradation path every un-migrated addon takes.
type HasConfigGroups interface {
	ConfigGroups() []ConfigGroup
}
```

Immediately after `RequiredServicesOf`:

```go
// ConfigGroupsOf returns the presentation groups m declares, or nil.
func ConfigGroupsOf(m Module) []ConfigGroup {
	if g, ok := m.(HasConfigGroups); ok {
		return g.ConfigGroups()
	}
	return nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

```bash
cd backend && go test ./pkg/sdk/module/ -run 'ConfigGroups|CarriesVisibility' -v
```

Expected: PASS (3 tests).

- [ ] **Step 6: Verify BaseModule was not accidentally widened**

`TestBaseModule_SatisfiesEverySubInterface` lists sub-interfaces explicitly and
`TestMinimalModule_ImplementsModuleOnly` asserts the negative. `HasConfigGroups` is
intentionally **not** added to either list: `BaseModule` must NOT implement it, so that
embedding `BaseModule` keeps a module on the flat-form path until it opts in.

```bash
cd backend && go test ./pkg/sdk/module/ -run 'MinimalModule|BaseModule' -v
```

Expected: PASS — unchanged.

- [ ] **Step 7: Commit**

```bash
git add backend/pkg/sdk/module/types.go backend/pkg/sdk/module/module.go \
        backend/pkg/sdk/module/config_groups_test.go
git commit -m "feat(sdk): add ConfigGroup, Condition, and per-field config metadata

ConfigField could only be grouped by a bare string with no hierarchy,
description, icon, or ordering, so a module with 60+ fields had no way to
present them beyond one flat row of tabs.

ConfigGroup arrives behind the HasConfigGroups sub-interface read through
ConfigGroupsOf, per the SDK rule that Module itself stays frozen. It is not
persisted: unlike ConfigSchema it is pure presentation, so the handler
resolves it from the live registry instead of a snapshot that would need
refreshing.

BaseModule deliberately does not implement HasConfigGroups — embedding it
must keep a module on the flat-form path until it opts in."
```

---

### Task 2: `ValidateConfigDeclarations`

A bad `Group` value is silent at runtime — it renders a rail entry pointing at nothing.
This function turns that class of defect into a test failure.

**Files:**
- Create: `backend/pkg/sdk/module/config_validate.go`
- Test: `backend/pkg/sdk/module/config_validate_test.go`

**Interfaces:**
- Consumes: `ConfigField`, `ConfigGroup`, `Condition` from Task 1.
- Produces: `func ValidateConfigDeclarations(schema []ConfigField, groups []ConfigGroup) error`

**The backward-compatibility rule that makes this safe:** when a module declares **no**
groups, its `Field.Group` values are legacy display labels and group references are
**not** checked. Without this, every module in the tree — `auth` declares
`Group: "Password Policy"` today and no `ConfigGroups()` — fails validation the moment
Task 4 lands. `DependsOn` is checked regardless.

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/sdk/module/config_validate_test.go`:

```go
package module

import (
	"strings"
	"testing"
)

func TestValidateConfigDeclarations_Valid(t *testing.T) {
	groups := []ConfigGroup{
		{Key: "oauth", Label: "OAuth Providers"},
		{Key: "oauth.google", Label: "Google", Parent: "oauth"},
	}
	schema := []ConfigField{
		{Key: "googleEnabled", Label: "Enable Google", Group: "oauth", Type: FieldBool},
		{Key: "googleClientId", Label: "Client ID", Group: "oauth.google", Type: FieldString,
			DependsOn: []Condition{{Key: "googleEnabled", In: []string{"true"}}}},
	}
	if err := ValidateConfigDeclarations(schema, groups); err != nil {
		t.Errorf("ValidateConfigDeclarations = %v, want nil", err)
	}
}

func TestValidateConfigDeclarations_LegacyUngroupedModule(t *testing.T) {
	// No ConfigGroups() declared: Group values are legacy display labels and
	// must not be validated as references. This is the state of every module
	// in the tree before its migration, and of every un-migrated fork addon.
	schema := []ConfigField{
		{Key: "passwordMinLength", Label: "Minimum length", Group: "Password Policy", Type: FieldInt},
	}
	if err := ValidateConfigDeclarations(schema, nil); err != nil {
		t.Errorf("ValidateConfigDeclarations with no groups = %v, want nil", err)
	}
}

func TestValidateConfigDeclarations_UndeclaredGroup(t *testing.T) {
	groups := []ConfigGroup{{Key: "oauth", Label: "OAuth Providers"}}
	schema := []ConfigField{
		{Key: "googleClientId", Label: "Client ID", Group: "oauth.googel", Type: FieldString},
	}
	err := ValidateConfigDeclarations(schema, groups)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for the typo'd group")
	}
	if !strings.Contains(err.Error(), "oauth.googel") {
		t.Errorf("error %q does not name the offending group", err)
	}
}

func TestValidateConfigDeclarations_UndeclaredDependsOnKey(t *testing.T) {
	schema := []ConfigField{
		{Key: "googleClientId", Label: "Client ID", Type: FieldString,
			DependsOn: []Condition{{Key: "googleEnabld", In: []string{"true"}}}},
	}
	err := ValidateConfigDeclarations(schema, nil)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for the typo'd DependsOn key")
	}
	if !strings.Contains(err.Error(), "googleEnabld") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

func TestValidateConfigDeclarations_EmptyInList(t *testing.T) {
	// An empty In can never be satisfied, so the field would be permanently
	// invisible — always a mistake, never an intent.
	schema := []ConfigField{
		{Key: "a", Label: "A", Type: FieldBool},
		{Key: "b", Label: "B", Type: FieldString, DependsOn: []Condition{{Key: "a"}}},
	}
	if err := ValidateConfigDeclarations(schema, nil); err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for an empty In list")
	}
}

func TestValidateConfigDeclarations_ParentCycle(t *testing.T) {
	groups := []ConfigGroup{
		{Key: "a", Label: "A", Parent: "b"},
		{Key: "b", Label: "B", Parent: "a"},
	}
	err := ValidateConfigDeclarations(nil, groups)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want an error for a Parent cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention a cycle", err)
	}
}

func TestValidateConfigDeclarations_UndeclaredParentAndDuplicateKey(t *testing.T) {
	groups := []ConfigGroup{
		{Key: "child", Label: "Child", Parent: "ghost"},
		{Key: "child", Label: "Child again"},
	}
	err := ValidateConfigDeclarations(nil, groups)
	if err == nil {
		t.Fatal("ValidateConfigDeclarations = nil, want errors")
	}
	for _, want := range []string{"ghost", "duplicate"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd backend && go test ./pkg/sdk/module/ -run ValidateConfigDeclarations -v
```

Expected: FAIL to compile — `undefined: ValidateConfigDeclarations`.

- [ ] **Step 3: Write the implementation**

Create `backend/pkg/sdk/module/config_validate.go`:

```go
package module

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateConfigDeclarations reports every structural defect in a module's
// config declarations: a field pointing at an undeclared group, a DependsOn
// condition naming a field the module does not have, a duplicate or
// orphaned group key, and cycles in the Parent chain.
//
// These defects are silent at runtime — a mistyped Group renders a rail
// entry pointing at nothing rather than failing — so this function exists to
// turn them into a test failure instead.
//
// Backward compatibility: a module that declares NO groups is using Group as
// a legacy display label, so group references are not checked. Every module
// predates ConfigGroups(), and un-migrated fork addons keep that shape
// indefinitely. DependsOn is checked either way.
//
// All problems are collected and reported together — fixing declarations one
// error per run is needless churn.
func ValidateConfigDeclarations(schema []ConfigField, groups []ConfigGroup) error {
	var problems []string

	groupByKey := make(map[string]ConfigGroup, len(groups))
	for _, g := range groups {
		switch {
		case g.Key == "":
			problems = append(problems, fmt.Sprintf("config group %q has an empty Key", g.Label))
		default:
			if _, dup := groupByKey[g.Key]; dup {
				problems = append(problems, fmt.Sprintf("duplicate config group %q", g.Key))
				continue
			}
			groupByKey[g.Key] = g
		}
	}

	for _, g := range groups {
		if g.Parent == "" {
			continue
		}
		if _, ok := groupByKey[g.Parent]; !ok {
			problems = append(problems, fmt.Sprintf("group %q has undeclared Parent %q", g.Key, g.Parent))
			continue
		}
		// Walk the ancestry; revisiting a key means the chain loops.
		seen := map[string]bool{g.Key: true}
		for cur := g.Parent; cur != ""; {
			if seen[cur] {
				problems = append(problems, fmt.Sprintf("group %q is part of a Parent cycle", g.Key))
				break
			}
			seen[cur] = true
			next, ok := groupByKey[cur]
			if !ok {
				break // reported as an undeclared Parent on its own iteration
			}
			cur = next.Parent
		}
	}

	fieldKeys := make(map[string]bool, len(schema))
	for _, f := range schema {
		fieldKeys[f.Key] = true
	}

	for _, f := range schema {
		if len(groups) > 0 && f.Group != "" {
			if _, ok := groupByKey[f.Group]; !ok {
				problems = append(problems,
					fmt.Sprintf("field %q references undeclared group %q", f.Key, f.Group))
			}
		}
		for _, c := range f.DependsOn {
			if !fieldKeys[c.Key] {
				problems = append(problems,
					fmt.Sprintf("field %q depends on undeclared field %q", f.Key, c.Key))
			}
			if len(c.In) == 0 {
				problems = append(problems,
					fmt.Sprintf("field %q has a DependsOn on %q with an empty In list", f.Key, c.Key))
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("invalid config declarations: %s", strings.Join(problems, "; "))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd backend && go test ./pkg/sdk/module/ -run ValidateConfigDeclarations -v
```

Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/sdk/module/config_validate.go backend/pkg/sdk/module/config_validate_test.go
git commit -m "feat(sdk): validate module config group and dependency declarations

A mistyped Group or DependsOn key is silent at runtime — it renders a rail
entry pointing at nothing, or a field that never becomes visible. This
collects every structural defect in one pass so a typo fails a test.

A module declaring no groups keeps using Group as a legacy display label and
is not checked, which is the shape of every module in the tree today and of
every un-migrated fork addon."
```

---

### Task 3: Serve `configGroups` from the admin API

**Files:**
- Modify: `backend/pkg/sdk/module/handler.go:71-93` (`ModuleConfigResponse`)
- Modify: `backend/pkg/sdk/module/handler.go:490-503` (the `h.registry.AllModules()` loop in `toConfigResponse`)
- Modify: `backend/openapi/enterprise.json` (regenerated, not hand-edited)
- Test: `backend/pkg/sdk/module/config_groups_test.go` (extend)

**Interfaces:**
- Consumes: `ConfigGroupsOf` from Task 1.
- Produces: `ModuleConfigResponse.ConfigGroups []ConfigGroup` serialised as
  `configGroups`, omitted when empty.

- [ ] **Step 1: Write the failing test**

Append to `backend/pkg/sdk/module/config_groups_test.go`:

```go
func TestModuleConfigResponse_SerialisesConfigGroups(t *testing.T) {
	resp := ModuleConfigResponse{
		ModuleName:   "auth",
		ConfigGroups: []ConfigGroup{{Key: "oauth", Label: "OAuth Providers", Order: 5}},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	got := string(raw)
	for _, want := range []string{`"configGroups"`, `"key":"oauth"`, `"order":5`} {
		if !strings.Contains(got, want) {
			t.Errorf("payload %s missing %s", got, want)
		}
	}
}

func TestModuleConfigResponse_OmitsEmptyConfigGroups(t *testing.T) {
	// A module without groups must not ship an empty key — the frontend
	// treats "absent" as the flat-form degradation path.
	raw, err := json.Marshal(ModuleConfigResponse{ModuleName: "compliance"})
	if err != nil {
		t.Fatalf("Marshal = %v", err)
	}
	if strings.Contains(string(raw), "configGroups") {
		t.Errorf("payload %s should omit configGroups when empty", raw)
	}
}
```

Add the imports this needs to the top of the file:

```go
import (
	"encoding/json"
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd backend && go test ./pkg/sdk/module/ -run ModuleConfigResponse -v
```

Expected: FAIL to compile — `unknown field ConfigGroups in struct literal`.

- [ ] **Step 3: Add the response field**

In `ModuleConfigResponse`, directly below the existing `ConfigSchema` line:

```go
	ConfigSchema          []ConfigField          `json:"configSchema"`
	ConfigGroups          []ConfigGroup          `json:"configGroups,omitempty"`
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd backend && go test ./pkg/sdk/module/ -run ModuleConfigResponse -v
```

Expected: PASS (2 tests).

- [ ] **Step 5: Fill the field from the live registry**

In `toConfigResponse`, inside the existing `for _, m := range h.registry.AllModules()`
loop that already fills `ProvidedServices` / `RequiredServices` / `OptionalServices` /
`InfraContainers`, add one line before `break`:

```go
		resp.InfraContainers = h.collectInfraStatus(m)
		// Groups are resolved live rather than read from the persisted doc:
		// they are presentation-only and never written to module_configs.
		resp.ConfigGroups = ConfigGroupsOf(m)
		break
```

- [ ] **Step 6: Build and run the full package suite**

```bash
cd backend && go build ./... && go test ./pkg/sdk/module/
```

Expected: build clean, all tests PASS.

- [ ] **Step 7: Regenerate the OpenAPI spec**

The dump needs infra running; it uses an isolated Mongo namespace and Redis DB 15, so
dev data is untouched.

```bash
cd docker && docker compose -f docker-compose.infra.yml up -d
cd ../backend && make openapi-dump
git --no-pager diff --stat -- openapi/
```

Expected: `openapi/enterprise.json` changes, showing the new `ConfigGroup` schema and the
`configGroups` property plus the six new `ConfigField` properties.

- [ ] **Step 8: Verify the CI gate is satisfied**

```bash
cd backend && make openapi-check
```

Expected: `✓ openapi/*.json matches the current source.`

- [ ] **Step 9: Commit**

```bash
git add backend/pkg/sdk/module/handler.go backend/pkg/sdk/module/config_groups_test.go \
        backend/openapi/enterprise.json
git commit -m "feat(sdk): serve module config groups from the admin API

toConfigResponse already resolves the live module out of the registry to fill
service declarations; groups are filled in the same loop rather than read
from the persisted document, because they are presentation-only and are
never written to module_configs.

configGroups is omitempty on purpose: the frontend reads an absent key as the
flat-form degradation path, which is what every module without ConfigGroups()
must keep rendering."
```

---

### Task 4: Enforce declaration integrity across the real catalog

Task 2 gives a checker; nothing calls it yet. This wires it to the modules that actually
ship, so a bad declaration in any core module — or in a fork's addon, since the same test
runs there — fails CI.

**Files:**
- Create: `backend/cmd/server/config_declarations_test.go`
- Test: same file (this task *is* the test)

**Interfaces:**
- Consumes: `module.ValidateConfigDeclarations`, `module.ConfigSchemaOf`,
  `module.ConfigGroupsOf` from Tasks 1–2; the `coreModules` and `optionalModules`
  catalogs from `cmd/server/catalog.go`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the test**

`cmd/server/catalog.go` exposes `coreModules(cfg *config.Config) []func() module.Module`
and the package-level `optionalModules map[string]func() module.Module`. Only
`auth.NewModule(cfg)` takes the config, and it just stores the pointer
(`func NewModule(cfg *config.Config) *AuthModule { return &AuthModule{cfg: cfg} }`) —
declaration methods never read it, so a zero value is enough and the test needs no DB,
Redis, or environment.

Create `backend/cmd/server/config_declarations_test.go`:

```go
package main

import (
	"testing"

	"github.com/orkestra/backend/internal/shared/config"
	"github.com/orkestra/backend/pkg/sdk/module"
)

// coreModuleCount guards against the gate silently going partial: if a core
// module is added to the catalog without this number moving, the new module
// is being checked, but a *removal* would otherwise shrink the gate unnoticed.
const coreModuleCount = 8

// buildAllModules instantiates every module compiled into this binary. The
// instances are used for reading declarations only — Init is never called —
// so no infrastructure is needed.
func buildAllModules() []module.Module {
	factories := coreModules(&config.Config{})
	if len(factories) != coreModuleCount {
		panic("coreModules changed size — update coreModuleCount so the gate stays honest")
	}
	for _, f := range optionalModules {
		factories = append(factories, f)
	}
	mods := make([]module.Module, 0, len(factories))
	for _, f := range factories {
		mods = append(mods, f())
	}
	return mods
}

// TestConfigDeclarationsAreValid runs the SDK's declaration checker over every
// module compiled into this binary. The SDK package cannot do this itself —
// pkg/sdk must never import internal/ — so the gate lives here, where the
// catalog is in scope. A fork gets the same coverage for its addons for free,
// because its catalog_<name>.go registers into the same optionalModules map.
func TestConfigDeclarationsAreValid(t *testing.T) {
	for _, m := range buildAllModules() {
		t.Run(m.Name(), func(t *testing.T) {
			if err := module.ValidateConfigDeclarations(
				module.ConfigSchemaOf(m),
				module.ConfigGroupsOf(m),
			); err != nil {
				t.Errorf("%s: %v", m.Name(), err)
			}
		})
	}
}

// TestEveryGroupHasFields catches the inverse defect from
// ValidateConfigDeclarations: a declared group that nothing points at renders
// an empty panel in the admin rail.
func TestEveryGroupHasFields(t *testing.T) {
	for _, m := range buildAllModules() {
		groups := module.ConfigGroupsOf(m)
		if len(groups) == 0 {
			continue // flat-form module, nothing to check
		}
		t.Run(m.Name(), func(t *testing.T) {
			used := make(map[string]bool)
			for _, f := range module.ConfigSchemaOf(m) {
				used[f.Group] = true
			}
			// A parent group legitimately holds no fields of its own when it
			// exists only to nest children, so count children as usage.
			for _, g := range groups {
				if g.Parent != "" {
					used[g.Parent] = true
				}
			}
			for _, g := range groups {
				if !used[g.Key] {
					t.Errorf("group %q has no fields and no child groups — it renders an empty panel", g.Key)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run the test**

```bash
cd backend && go test ./cmd/server/ -run 'ConfigDeclarations|EveryGroupHasFields' -v
```

Expected: PASS. Today no module declares groups, so every subtest either skips or passes
trivially — that is the correct result for phase 1, and the gate goes live the moment
phase 4 migrates `auth`.

- [ ] **Step 3: Prove the gate actually bites**

Temporarily break one declaration to confirm the test is not vacuous. In
`backend/internal/core/compliance/module.go`, add to the `ConfigSchema()` return:

```go
		{Key: "canary", Label: "Canary", Type: module.FieldBool,
			DependsOn: []module.Condition{{Key: "does_not_exist", In: []string{"true"}}}},
```

```bash
cd backend && go test ./cmd/server/ -run ConfigDeclarations -v
```

Expected: FAIL naming `compliance` and `does_not_exist`. **Revert the canary** before
continuing:

```bash
git checkout -- backend/internal/core/compliance/module.go
```

- [ ] **Step 4: Run the full backend CI surface**

```bash
cd /home/tore/orkestra && make ci-backend
```

Expected: all gates pass — lint, tenantscope, policycoverage, piiscan, vuln, tests,
build, openapi-check.

- [ ] **Step 5: Update the SDK documentation**

`backend/pkg/sdk/CLAUDE.md` lists what lives in `module/`. Add `ConfigGroup` and
`HasConfigGroups` to the `module/` row of the package map, and add a bullet to **Rules**:

```markdown
- **Config groups are presentation-only and never persisted.** `ConfigSchema`
  is snapshotted into `module_configs` and refreshed by `RefreshMetadata` on
  every boot; `ConfigGroups()` is resolved live from the registry by the admin
  handler. Do not add `bson` tags to `ConfigGroup`.
```

`backend/CLAUDE.md`'s "Adding a New Module" list gains a step after step 5
(`Declare ConfigSchema()`):

```markdown
5b. Optionally declare `ConfigGroups()` to give the admin settings page a
    sectioned rail instead of one flat form. Omit it and the form stays flat —
    that is a supported end state, not a gap.
```

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/server/config_declarations_test.go backend/pkg/sdk/CLAUDE.md backend/CLAUDE.md
git commit -m "test(server): gate module config declarations across the real catalog

ValidateConfigDeclarations had no caller. pkg/sdk cannot run it over the
shipping modules itself — it must never import internal/ — so the gate lives
in cmd/server where the catalog is in scope. A fork's addons register into the
same maps, so they get the same coverage without writing anything.

The companion check catches the inverse defect: a declared group no field
points at, which renders an empty panel."
```

---

## Phase exit criteria

- `make ci-backend` green.
- `GET /v1/admin/modules/{name}` returns no `configGroups` key for any module (nothing
  declares groups yet) — confirming the degradation path is the default.
- No behaviour change visible in `/admin/modules`.
- `grep -rn "internal/" backend/pkg/sdk/ --include="*.go"` shows only doc-comment hits.

Update [`module-config-ux.md`](module-config-ux.md) §8: set phase 1 to ✅ and the plan's
`**Status:**` line to 🟡 In progress.
