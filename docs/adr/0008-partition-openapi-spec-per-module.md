---
title: ADR-0008 — Partition the OpenAPI spec per module so a fork's addons never collide with core
status: proposed
public: true
---

# ADR-0008 — Partition the OpenAPI spec per module

| Field | Value |
|---|---|
| **Status** | 🟡 Proposed |
| **Date** | 2026-06-14 (proposed) |
| **Authors** | @salvatore.balestrino |
| **Extends** | [ADR-0006](0006-collapse-to-core-only-base.md) — the core-only base + single in-tree Go module is the premise. [ADR-0007](0007-per-addon-i18n-namespaces.md) — applies the *same* ownership model to the **i18n** artifact; this ADR is its OpenAPI twin. |
| **Related** | [ADR-0003](0003-three-audience-host-split.md) (operator vs client surface — one in-memory doc today; see follow-ups). |

## Context

[ADR-0006](0006-collapse-to-core-only-base.md) collapsed Orkestra to a **core-only base** that is a single in-tree Go module — no satellite `go.mod`, no `replace`. A fork adds its verticals as in-tree `internal/addons/<name>/` modules and re-syncs from upstream periodically. [ADR-0007](0007-per-addon-i18n-namespaces.md) then closed the **i18n** half of a shared-artifact defect: addon translations now live in per-module namespaces and never touch the core `src/locales/*.json`.

**One shared-ownership generated artifact remains: the OpenAPI spec.** `backend/openapi/enterprise.json` is a single committed monolith (144 paths today), serialized from `operatorAPI.OpenAPI()` in `OPENAPI_DUMP` mode (`cmd/server/main.go:463`, driven by `scripts/openapi-dump.sh`). Every registered module's routes — core and addon — land in that one file, and the CI gate `make openapi-check` regenerates it and fails on any `git diff` under `openapi/`.

For the base this is fine: only core routes exist. **For a fork it is a structural defect identical to the one ADR-0007 fixed for i18n** — a mutable artifact with shared ownership:

- Every upstream sync is **guaranteed to conflict** on `enterprise.json`, because upstream edits the core regions of the same file the fork has filled with addon paths.
- The committed monolith **drifts permanently**: a fork running the dump produces (core + its addons) paths, while the committed file is upstream's core-only snapshot — so `openapi-check` is **red on the fork independent of any change**. A fork that added two billing DTO fields observed a 16,000-insertion phantom diff for this reason: the committed spec was simply the wrong (core-only) baseline.

The structural fact that makes a clean fix possible — mirroring ADR-0006's "core → addon coupling is zero" and ADR-0007's namespace separation — is that **OpenAPI content is fully attributable**: every operation is registered by exactly one module. The content is already partitioned *logically*; only the *file* is monolithic.

The base sees little pain today (it has no addons), but — exactly as ADR-0006 kept the module system "by design" for forks — shipping the partitioning upstream means **every fork inherits a conflict-free artifact contract** instead of rediscovering this defect and patching it locally (as one fork already has, in a proposal this ADR generalizes).

We remain pre-GA with permission to make breaking changes.

## Decision

**Generated specs follow the same ownership model as code and i18n (ADR-0007): the core owns the core spec; each addon owns a self-contained fragment co-located with its code; the assembled monolith is a *derived* dump output, never hand-edited, never committed, never merged.** Acceptance test: *deleting an addon's directory removes its spec fragment with it, touching nothing the core owns.*

### D1 — Committed per-owner fragments; the assembled monolith is derived

The dump partitions operations by owning module (D2) and writes:

- **`backend/openapi/core.json`** — all core modules' operations. **Upstream-owned.** A fork edits it only when it patches core, so upstream syncs touch it cleanly or not at all.
- **`backend/internal/addons/<name>/openapi.json`** — one fragment per addon, **co-located with the addon code** (not in a central `openapi/` dir). Fork-owned; upstream never has these paths, so they can never conflict.
- **`backend/openapi/enterprise.json`** — the assembled core + all addon fragments. **Derived → `.gitignore`d**, regenerated on demand (`make openapi-assemble` / the dump), consumed by docs.orkestra.cc and client codegen.

Committed fragments keep reviewable per-module API diffs in PRs; the derived whole still exists for everything that needs a single spec — just never in git.

### D2 — Route→module attribution via an auto-stamped `x-orkestra-module` extension

Every operation carries `x-orkestra-module: <name>`. Attribution must **not** depend on tag-string convention — today's tags are not uniformly parseable (`Users` / `Users Admin` / `Tenants` / `Tenants Admin` / `Auth - Device Trust` / `MFA` / `WebAuthn`: one module spans several tags, core tags carry no module prefix) — nor on per-`huma.Register` discipline (forgettable).

**The module registry stamps it automatically.** When the registry invokes each module's `RegisterRoutes`, it snapshots `api.OpenAPI().Paths` before and after the call; every operation that appeared during that module's registration gets `Extensions["x-orkestra-module"] = module.Name()`. Forgery-proof, zero authoring discipline. The partitioner (D1) groups purely on this extension: core module → `core.json`, otherwise → that addon's fragment.

### D3 — CI gates per-fragment; the monolith is never diffed

`openapi-check` stops diffing the committed monolith. It regenerates and diffs **each committed fragment** (`core.json` + every `internal/addons/*/openapi.json`) against a fresh partitioned dump. The assembled `enterprise.json` is gitignored and not diffed. Optional later hardening: an `oasdiff` breaking-change gate per fragment, recovering the regression safety the git-diff gave.

## Consequences

### What we enable

- **Conflict-free upstream syncs** on the spec — the point. Core-owned `core.json` matches upstream; addon-owned fragments don't exist upstream.
- **Self-contained addons.** A vertical is code + `openapi.json` in one directory; lifting it in or out (ADR-0006's archived-addon workflow) carries its spec with it.
- **The stale-spec bug class disappears.** No committed monolith to drift; the effective spec is always a fresh assembly that matches reality.
- **Honest CI.** `openapi-check` is green and *meaningful* on a fork instead of permanently red against a core-only baseline.
- **Reviewable API diffs survive** — per-fragment committed specs still show "what changed in this module's API" in a PR.

### What we give up

- **A single committed source-of-truth spec.** Tooling/people that grabbed `enterprise.json` from git now run `make openapi-assemble` or read fragments. Mitigated: the docs build assembles it on demand.
- **Some net-new machinery** — registry stamping, a partitioning dump, generalized CI gates — small but non-zero to maintain.
- **Discipline:** a fork must not edit `core.json` except for genuine core patches; violating it reintroduces conflicts (a forbidden pattern below).

### Forbidden patterns (post-cutover)

- **Putting addon paths in `core.json`,** or committing the assembled `enterprise.json` — re-pollutes core-owned files / resurrects the drift+merge hazard this ADR removes.
- **Attributing routes by tag-string parsing or per-call extension setting** — attribution is the registry's automatic `x-orkestra-module` stamp; don't build a parallel fragile path.

## Implementation

Single branch, each phase gated by CI green before the next. Reversible until merge. (The i18n phases of the originating fork proposal are **already shipped** as [ADR-0007](0007-per-addon-i18n-namespaces.md) and are not repeated here.)

| Phase | Scope | Gate |
|---|---|---|
| **1** | Registry auto-stamps `x-orkestra-module` on every operation during each module's `RegisterRoutes` (snapshot-diff `api.OpenAPI().Paths`). Unit test: every operation in a dump carries the extension. | `make ci-backend` |
| **2** | Rework `OPENAPI_DUMP` in `cmd/server/main.go` to partition by `x-orkestra-module` → write `openapi/core.json` + `internal/addons/<name>/openapi.json` + assembled `openapi/enterprise.json`. Update `scripts/openapi-dump.sh`; add `make openapi-assemble`. | dump produces fragments + whole |
| **3** | `.gitignore backend/openapi/enterprise.json`; `git rm --cached` it; commit fresh `core.json`. Rewrite `openapi-check` to diff fragments, not the monolith. | `make openapi-check` green |
| **4** | Docs/CI: docs.orkestra.cc build assembles `enterprise.json` from fragments; refresh `backend/CLAUDE.md` (spec section) + the docs-site addon-authoring pages to describe the per-module spec contract. | `make ci-all` |

### Open follow-ups (not blocking)

- **Operator vs client spec split ([ADR-0003](0003-three-audience-host-split.md)).** Both surfaces share one in-memory doc today; the partitioner already has per-operation metadata to also bucket by audience — fold in then, not now.
- **`oasdiff` breaking-change gate** on fragments — recovers the regression safety the old git-diff provided; add once fragments are stable.

## Alternatives considered

1. **Gitignore `enterprise.json` entirely, no committed fragments.** Fresh-generate in CI, validate with `oasdiff` vs a baseline. Rejected as the *primary* model — loses the reviewable per-PR API diff — but adopted *partially*: the assembled monolith is exactly this (derived/gitignored). D1 keeps committed fragments for review and uses the derived whole for consumption.
2. **Per-addon dump = a minimal server per addon.** Rejected — incompatible with ADR-0006's single in-tree module; there is no per-addon binary, and faking one is heavy machinery for no gain over registry attribution.
3. **Attribute routes by normalizing the `Tags` string** (`"Auth - Device Trust".split(" - ")[0]`). Rejected (D2) — fragile: core tags carry no uniform module prefix, sub-tags vary per module, a renamed tag silently misfiles operations. The registry stamp is deterministic and convention-free.
4. **Keep the monolith but auto-merge via a custom git merge driver.** Rejected — papers over the textual symptom without giving ownership separation, self-containment, or honest CI; strictly more fragile than partitioning the content.
