---
name: mongo-collection-naming
description: "Enforce Orkestra MongoDB collection naming — modules with 2+ collections must prefix every collection with their module directory name (singular). Use when adding, reviewing, or renaming MongoDB collections in the Go backend."
---

# MongoDB Collection Naming Convention

Use this skill whenever you are touching `Collections()` declarations, repository files that call `db.Collection(...)`, or MongoDB index setup anywhere under `backend/internal/`.

## The Rule

> **If a module owns 2 or more MongoDB collections, every one of them must begin with `<module_dir_name>_`.**
>
> The prefix is the module's directory name under `backend/internal/core/` or `backend/internal/addons/`, lowercase, singular, exactly as it appears on disk.
>
> Single-collection modules may keep any name.

## Why

- **Grep-ability.** `rg 'auth_'` reveals every collection owned by the auth module in one shot.
- **Ops clarity.** `show collections` in mongosh groups related collections next to each other, which makes debugging and manual surgery tractable.
- **Ownership.** The module registry already treats each module as the owner of a set of collections. Prefixing makes that ownership visible at the storage layer.
- **Consistency with the rest of the backend.** Most modules already follow this (`billing_*`, `rag_*`, `sales_*`, `agent_*`, `notification_*`, `authz_*`, `tenant_*`, `auth_*`). New collections that break the pattern stand out for the wrong reasons.

## Canonical Examples

| Module (dir) | Collections | Compliant? |
|---|---|---|
| `core/auth` | `auth_oauth_providers`, `auth_refresh_tokens`, `auth_sessions`, `auth_security_events`, `auth_email_tokens` | yes |
| `core/authz` | `authz_permissions`, `authz_roles`, `authz_bindings` | yes |
| `core/notification` | `notification_messages`, `notification_templates`, `notification_preferences`, `notification_suppressions`, `notification_unsubscribe_tokens` | yes |
| `core/tenant` | `tenant_orgs`, `tenant_memberships`, `tenant_org_invites` | yes |
| `addons/billing` | `billing_invoices`, `billing_customers`, `billing_suppliers`, `billing_companies`, `billing_notifications`, `billing_polling_state` | yes |
| `addons/rag` | `rag_documents`, `rag_models`, `rag_relationship_types` | yes |
| `addons/sales` | `sales_jobs`, `sales_reports`, `sales_prompts`, `sales_settings`, `sales_batches` | yes |
| `core/user` | `users` | yes — single-collection module, rule does not apply |
| `addons/company` | `company_lookups` | yes — single collection, prefix optional but already present |
| `addons/aimodels` | `ai_models` | yes — single collection |

### Bad — do not merge

```go
// in internal/addons/billing/module.go
func (m *BillingModule) Collections() []module.CollectionSpec {
    return []module.CollectionSpec{
        {Name: "invoices"},                 // BAD: billing has >1 collection, must be billing_invoices
        {Name: "billing_customers"},
        {Name: "suppliers"},                // BAD: must be billing_suppliers
    }
}
```

```go
// in internal/core/auth/services/security_event_service.go
collection := db.Collection("security_events")  // BAD: hardcoded literal, and wrong name
```

### Good

```go
// declaration uses the constant from models/
{Name: models.OAuthProvidersCollection, ...}

// repository resolves the same constant
collection: db.Collection(models.OAuthProvidersCollection),
```

## Audit Checklist

Run through this whenever you edit a `module.go` `Collections()` method or a repository file:

1. **Count collections.** Read the module's `Collections()` return value. If there are two or more entries, the rule applies.
2. **Check every `Name:`.** Each one must start with `<module>_`, where `<module>` is the module directory name. If any does not, rename it as part of the current change.
3. **Follow the name to its usage.** Grep the module for the collection name. Every `db.Collection("...")` call must reference the same name, and should prefer a named constant over a literal.
4. **Growing from 1 to 2 collections.** When you add a second collection to a module that previously had one, the pre-existing collection must be renamed to gain the prefix in the same commit. Flag the breaking rename to the user — there is no automated migration, existing deployments will need a `db.collection.renameCollection(...)` in mongosh.
5. **New modules.** Default to prefixed names from day one, even for a single collection. Cheaper than renaming later.
6. **Docs sync.** If the module has a `CLAUDE.md` with a "MongoDB Collections" table, update it in the same change.

## Where Collection Names Live

When hunting for every place a rename has to land, look at exactly these three layers per module:

| Layer | Path pattern | What lives here |
|---|---|---|
| Declaration | `backend/internal/<core\|addons>/<module>/module.go` | `Collections()` method returning `[]module.CollectionSpec` — the registry reads this and calls `ensureCollections()` at boot |
| Constants | `backend/internal/<core\|addons>/<module>/models/*.go` or `.../repository/*.go` | `const FooCollection = "module_foo"` or `const CollFoo = "module_foo"` — the single source of truth |
| Usage | `backend/internal/<core\|addons>/<module>/repository/*.go` and occasionally `services/*.go` | `db.Collection(models.FooCollection)` — must reference the constant, not a literal |

If a repository file contains a literal `db.Collection("foo")`, that is a code smell even if the name is already correct. Replace it with a named constant at the same time.

## Audit Grep Commands

Run these from the repo root. They should return zero suspicious hits when the codebase is clean:

```bash
# Every db.Collection(...) call in the backend — eyeball for literals that don't match their module's prefix
rg -n 'db\.Collection\(' backend/internal/

# Every Name: "..." literal inside a module.go — confirm prefixes
rg -n 'Name:\s*"[^"]+"' backend/internal/ -g '*/module.go'

# Reverse check: any collection-name constants NOT prefixed with a plausible module name
rg -n 'Collection\s*=\s*"[^"]+"' backend/internal/
```

## When NOT to Apply

- **Single-collection modules.** The rule explicitly exempts them. Do not invent a prefix just to be symmetric.
- **Shared infrastructure collections.** `module_configs` (runtime module config store in `backend/internal/shared/module/config_repository.go`) is owned by the shared registry, not by any single module, and is outside the rule.
- **The `users` collection.** The `user` module owns exactly one collection, so the rule does not apply. The `auth` module shares it via the `user` module's repository — do not rename it.
- **Non-MongoDB stores.** Memgraph node labels, Redis keys, and NATS subjects have their own conventions. This skill covers MongoDB only.

## Quick Summary for Claude

When editing backend module code:

1. Is this a `Collections()` method or a `db.Collection(...)` call? → rule may apply.
2. Does the owning module have 2+ collections? → every name must start with `<module>_`.
3. Is the name coming from a literal? → swap for a constant from the module's `models/` or `repository/` package.
4. Did I rename a collection? → flag the breaking change to the user and remind them that existing deployments need a mongosh rename.
