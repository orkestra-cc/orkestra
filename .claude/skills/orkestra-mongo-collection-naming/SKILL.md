---
name: orkestra-mongo-collection-naming
description: "Use when adding, reviewing, or renaming MongoDB collections in the Orkestra Go backend — any change to Collections() declarations, db.Collection(...) calls, collection-name constants, or Mongo index setup under backend/internal/."
---

# Orkestra MongoDB Collection Naming

Use this skill whenever you are touching `Collections()` declarations, repository files that call `db.Collection(...)`, or MongoDB index setup anywhere under `backend/internal/`. Applies equally to core modules and fork addons (`internal/addons/<name>/` — see the `orkestra-addon` skill for the full addon contract).

## The Rules

There are **two sanctioned prefixes**. Every collection carries exactly one of them.

> **Rule 1 — module prefix (default).** If a module owns 2 or more MongoDB collections, every one of them begins with `<module_dir_name>_` — the module's directory name under `backend/internal/core/` or `backend/internal/addons/`, lowercase, singular, exactly as it appears on disk.
>
> **Rule 2 — tier prefix (overrides Rule 1).** A collection that is split per audience tier (ADR-0003: separate operator-side and client-side rows) is declared as a **pair** named `operator_<thing>` / `client_<thing>`. The tier prefix *replaces* the module prefix — it is `operator_sessions`, not `auth_operator_sessions`. The two names must be identical apart from the prefix.
>
> **Exemption.** Single-collection modules may keep any name (e.g. `log_levels` in `core/logging`).

Which prefix? Ask one question: *does this collection hold rows for exactly one audience tier, with a sibling collection for the other tier?* Yes → tier prefix pair. No → module prefix.

## Why

- **Grep-ability.** `rg 'authz_'` reveals every collection owned by the authz module; `rg '"client_'` reveals the entire client-tier PII surface in one shot.
- **Ops clarity.** `show collections` in mongosh groups related collections together, which makes debugging and manual surgery tractable.
- **Tier isolation is enforceable.** The tier prefix is what lets the ADR-0003 tier-guard tests assert that `operator_*` collections only ever hold `Tier="operator"` rows (see `auth/models/collections.go`).
- **Ownership.** The registry treats each module as the owner of its collections; the prefix makes ownership visible at the storage layer. A fork's addon collections (`<addon>_*`) are instantly distinguishable from core.

## Canonical Examples (current core modules)

| Module (dir) | Collections | Pattern |
|---|---|---|
| `core/user` | `operator_users`, `client_users` | tier pair |
| `core/auth` | `auth_security_events`, `auth_device_trust` + tier pairs: `{operator,client}_oauth_providers`, `_refresh_tokens`, `_sessions`, `_mfa_factors`, `_email_tokens` | mixed — security events and device trust are deliberately not tier-split (audit log keyed on userUUID) |
| `core/authz` | `authz_permissions`, `authz_roles`, `authz_bindings` + tier pair `operator_roles` / `client_roles` | mixed |
| `core/tenant` | `tenants`†, `tenant_memberships`, `tenant_invites`, `tenant_ancestors`, `tenant_entitlements` | module prefix |
| `core/notification` | `notification_messages`, `_templates`, `_preferences`, `_suppressions`, `_unsubscribe_tokens` + tier pair `{operator,client}_unsubscribe_tokens` | mixed |
| `core/navigation` | `navigation_overrides` | single, prefixed anyway |
| `core/logging` | `log_levels` | single — exempt |
| `core/compliance` | `compliance_audit_events`, `_kms_keys`, `_legal_holds`, `_erasure_requests` | module prefix |

† `tenants` is grandfathered (primary aggregate, pre-dates the rule). Do not rename it, and do not cite it as precedent for new unprefixed names.

### Bad — do not merge

```go
// in a fork's internal/addons/crm/module.go
func (m *CRMModule) Collections() []module.CollectionSpec {
    return []module.CollectionSpec{
        {Name: "leads"},              // BAD: crm owns >1 collection → crm_leads
        {Name: "crm_contacts"},
        {Name: "auth_crm_notes"},     // BAD: prefix is the OWNING module, never another module's
    }
}
```

```go
collection := db.Collection("operator_sessions") // BAD: hardcoded literal — use models.OperatorSessionsCollection
```

### Good

```go
// declaration uses the constant (models/ or repository/ package)
{Name: models.OperatorSessionsCollection, Indexes: ...}

// repository resolves the same constant
collection: db.Collection(models.OperatorSessionsCollection),
```

## Audit Checklist

Run through this whenever you edit a `Collections()` method or a repository file:

1. **Count collections.** 2+ entries in `Collections()` → the rules apply.
2. **Classify each name.** Module-prefixed, or an `operator_`/`client_` tier pair. Anything else must be renamed as part of the current change.
3. **Tier pairs are pairs.** If you add `operator_<thing>`, the matching `client_<thing>` constant must exist (even if the cutover lands later — see notification's unsubscribe tokens).
4. **Follow the name to its usage.** Every `db.Collection(...)` call must reference the named constant, never a literal.
5. **Growing from 1 to 2 collections.** The pre-existing collection gains the prefix in the same commit. Flag the breaking rename to the user — existing deployments need `db.collection.renameCollection(...)` in mongosh; there is no automated migration.
6. **New modules/addons.** Default to prefixed names from day one, even for a single collection. Cheaper than renaming later.
7. **Docs sync.** Update the module's `CLAUDE.md` "MongoDB collections" table in the same change.

## Where Collection Names Live

| Layer | Path pattern | What lives here |
|---|---|---|
| Declaration | `backend/internal/<core\|addons>/<module>/module.go` | `Collections()` returning `[]module.CollectionSpec` — the registry reads this and ensures collections + indexes at boot |
| Constants | `.../<module>/models/*.go` or `.../<module>/repository/*.go` | `const FooCollection = "module_foo"` / `const CollFoo = "module_foo"` — the single source of truth |
| Usage | `.../<module>/repository/*.go`, occasionally `services/*.go` | `db.Collection(models.FooCollection)` — must reference the constant |

A literal `db.Collection("foo")` in a repository is a code smell even when the name is correct — replace it with the constant in the same change.

## Audit Grep Commands

Run from the repo root. Both verified against the current tree:

```bash
# Every collection-name constant (catches Coll*, *Collection, and truncated *Collect styles)
rg -n '[Cc]oll\w*\s*=\s*"[^"]+"' backend/internal/ backend/pkg/sdk/

# Collection-spec literals inside module.go (lowercase snake names only — NavItem Names are capitalized, so they don't match)
rg -n 'Name:\s*"[a-z0-9_]+"' backend/internal/ -g 'module.go'

# Every db.Collection(...) call — eyeball for literals
rg -n 'db\.Collection\(' backend/internal/
```

## When NOT to Apply

- **Single-collection modules** (`log_levels`). Do not invent a prefix just for symmetry — but new modules should prefix anyway (checklist #6).
- **Shared infrastructure collections**, owned by no module: `module_configs` (`pkg/sdk/module/config_repository.go`) and `system_init` (`internal/shared/systeminit/`).
- **`tenants`** — grandfathered, see above.
- **Non-MongoDB stores.** Redis keys have their own conventions. This skill covers MongoDB only.

## Quick Summary for Claude

1. Editing a `Collections()` method or a `db.Collection(...)` call? → rules may apply.
2. Module owns 2+ collections? → every name is either `<module>_*` or an `operator_*`/`client_*` tier pair. Nothing else.
3. Name coming from a literal? → swap for the constant in `models/` or `repository/`.
4. Renamed a collection? → flag the breaking change; deployments need a mongosh `renameCollection`.
