# Implementation Plans

Phased implementation plans for non-trivial features — the architecture decisions,
phase gates, and verification steps for work that spans more than a single commit.
Plans complement [ADRs](../adr/README.md): an ADR records *what we decided and why*; a
plan records *how we roll it out*.

## When to write a plan

- The work has 2+ phases or touches multiple modules / both backend and frontend.
- There are non-obvious sequencing constraints, migrations, or verification gates.
- One-off, single-commit changes do **not** need a plan.

## Status labels

Each plan opens with a `**Status:**` line using one of:

- `🔴 Not started` — drafted, work not begun.
- `🟡 In progress` — actively being built; keep the per-phase status current.
- `✅ Done` — every phase shipped, no outstanding in-scope items. Archive it.

## Lifecycle

1. Draft the plan here when the work is scoped.
2. Update the `**Status:**` line and per-phase markers as phases ship.
3. When a plan is **fully shipped**, move it to [`../archive/`](../archive/) with
   `git mv` (preserves history) so the active folder only shows live work.

Recently archived plans (now in `../archive/`): `developer-nav-section.md`,
`user-security-center.md`, `frontend-admin-i18n.md`.
