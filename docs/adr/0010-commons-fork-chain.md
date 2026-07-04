---
title: ADR-0010 — Two-level fork chain for multi-product operators (the commons pattern)
status: accepted
public: true
---

# ADR-0010 — Two-level fork chain for multi-product operators (the commons pattern)

| Field | Value |
|---|---|
| **Status** | ✅ Accepted — adopted 2026-07-04 (reference instance created at v0.3.12) |
| **Date** | 2026-07-04 |
| **Authors** | @salvatore.balestrino |
| **Related** | [ADR-0006](0006-collapse-to-core-only-base.md) (core-only base; addons are per-fork responsibility — this ADR builds directly on it) |

## Context

[ADR-0006](0006-collapse-to-core-only-base.md) settled *where an addon lives*: in-tree, inside a fork, against the in-tree SDK — one Go module, no `replace`, no `go.work`, nothing published. That answer is complete for an operator running **one** product.

It leaves a follow-up question open for an operator running **several** products (an agency serving multiple clients, each on its own Orkestra fork): when an addon built for product A turns out to be useful for product B, how does it get there — and how do its future bugfixes reach every product that adopted it?

The constraints that shape the answer are all inherited:

1. **Sharing via a published Go module is off the table.** It would re-create exactly the multi-repo machinery ADR-0006 removed (version skew, `replace` chains, publish friction) — and it would only cover half an addon anyway, because an addon spans backend *and* frontend-admin (manifest, pages, RTK Query slice, i18n bundle). The frontend half would need a parallel npm package, doubling the publishing surface per addon.
2. **Git merge is already the distribution mechanism.** Every fork syncs from upstream by merging; the conflict surface is small and well understood (`go.mod`, the generated OpenAPI spec, `baseApi.ts` tag registration).
3. **Addon changes are naturally portable.** The module-name conventions (everything under `internal/addons/<name>/`, `catalog_<name>.go`, `pages/<name>/`, per-addon i18n namespace) mean a well-formed addon touches almost no shared files — it moves between forks as a clean cherry-pick.
4. **Private forks are mirror-clones anyway.** GitHub forks of a public repository cannot be made private, so a product fork is already a standalone private repo wired to upstream via a git remote — there is no platform-level fork relationship to preserve.

## Decision

**Insert one private intermediate fork — the *commons* — between upstream and the product forks. Reusable addons live in commons; product-specific addons live in each product fork; git merge distributes everything downstream.**

```
orkestra (upstream, public, core-only)
   │  sync: merge upstream/dev → commons/dev, once per release
   ▼
<your-org>/orkestra-commons (private: core + reusable addons)
   │  sync: merge commons/dev → product/dev, same recipe
   ├──▶ product fork A (+ addons specific to A)
   ├──▶ product fork B (+ addons specific to B)
   └──▶ product fork C (…)
```

### D1 — Commons is a private mirror-clone fork

Created with the standard private-fork recipe (new private repo, full history + tags pushed, `upstream` remote pointing at `orkestra-cc/orkestra`). It is a fork like any other as far as ADR-0006 is concerned: single Go module, addons in-tree, zero core diff beyond registration points.

### D2 — Product forks chain off commons, not upstream

A product fork's `upstream` remote points at commons. Upstream releases reach products *through* commons: one upstream→commons merge per release, then one commons→product merge per product. The per-product sync count is unchanged from the flat topology — only the source moves — and each sync now also carries shared-addon updates.

### D3 — Addons are born in a product fork and promoted on second use

An addon starts where its paying use case is: in the product fork. When a second product needs it, it is **promoted** — cherry-picked to commons — and from then on maintained there; bugfixes reach every adopter through ordinary syncs. Promotion stays cheap only under a commit discipline: **addon commits separate from product-customization commits**, so the cherry-pick is clean.

Nothing is promoted speculatively. A commons with zero adopters per addon is inventory, not leverage.

### D4 — The flow toward upstream carries no addon code

Merges go downstream only. A generic core fix discovered in commons or a product fork is cherry-picked onto a clean branch based on upstream and submitted as an ordinary PR — never merged back wholesale, so addon code cannot leak into the public base.

### Reference instance

The pattern's reference instance is **`orkestra-cc/orkestra-commons`** (baseline v0.3.12). The repository is **private**: this ADR and the [docs-site guide](https://docs.orkestra.cc/getting-started/private-forks) document the mechanism fully, but access to the code is granted at the maintainer's discretion.

## Consequences

### What this buys

- **Reuse without publishing.** No Go module, no npm package, no version negotiation — an addon is shared by the same merge that already ships upstream releases.
- **ADR-0006 holds everywhere.** Every repo in the chain is individually a compliant core-only-plus-in-tree-addons monorepo. No forbidden pattern is reintroduced at any level.
- **Isolation where it matters.** Product forks never see each other's code; commons contains only what was deliberately promoted.

### What it costs

- **One more repo to keep green.** Commons needs its own CI run per sync; a broken commons blocks every downstream sync.
- **Promotion is manual.** Cherry-picking and the scoped-commit discipline are human obligations; sloppy mixed commits make promotion a surgery.
- **Merge conflicts at known hotspots.** The generated OpenAPI spec must be regenerated (never hand-merged), `baseApi.ts` tag arrays are unioned, and **every sync ends with `go build ./...`** — an auto-merge that reports no conflicts can still drop fork-only SDK symbols silently.

### Forbidden patterns (inherited and extended)

- Everything ADR-0006 forbids, in every repo of the chain.
- Merging commons or a product branch into an upstream PR branch (D4).
- Promoting an addon by re-implementing it in commons instead of cherry-picking — the histories must stay connectable.

## Alternatives considered

1. **Private Go modules for shared addons.** Rejected: re-creates the coordination layers ADR-0006 removed, and covers only the backend half of an addon (see Context §1).
2. **One shared fork for all products, addons toggled per deployment.** Runtime toggling makes this *feasible* (all addons compile into the one binary; each deployment enables its subset), but it mixes every client's code in one repo, couples unrelated release cadences, and one broken addon blocks every product's deploy. Acceptable for products owned by one team; wrong for client work.
3. **Per-addon repos consumed via `git subtree`.** An addon spans multiple directory prefixes (backend dir, catalog file, frontend pages, manifest) plus edits to shared registration points — no single subtree prefix contains it. Workable with mirror-layout merge tricks, but strictly more fragile than the commons chain for the same outcome.
4. **Do nothing (cherry-pick between product forks ad hoc).** Fine for a one-off; every shared addon then needs N-way manual bugfix propagation with no authoritative copy. This is the degenerate case the commons chain replaces once the second adopter appears.
