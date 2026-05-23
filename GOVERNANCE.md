# Governance

This document describes how decisions get made in Orkestra, who can make them, and how to become one of those people.

## TL;DR

Orkestra is a young open-source project. Today it's a **single-maintainer model** ([@salvatore-balestrino](https://github.com/salvatore-balestrino), aka [Salvatore Balestrino](mailto:salvatore.balestrino@gmail.com)) who carries the BDFL pro-tem role: writes specs, reviews PRs, cuts releases, makes architectural calls. This is honest about where the project is — single-maintainer is fine for the current scale.

The path forward (described below) opens that role up to contributors who show sustained, quality involvement. There is no foundation, board, technical steering committee, or vendor neutrality structure today.

## Roles

### Contributor

Anyone who has opened a PR, reported a bug, written a doc, answered a question in Discussions, or otherwise improved the project. No formal grant — being listed in `git log` as a contributor is the role.

Contributors can:

- Open issues and PRs
- Comment on any issue / PR / Discussion
- Propose changes via the [RFC process](#rfc-process)
- Use the [`good first issue`](https://github.com/orkestra-cc/orkestra/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22) and [`help wanted`](https://github.com/orkestra-cc/orkestra/issues?q=is%3Aopen+is%3Aissue+label%3A%22help+wanted%22) labels as on-ramps

### Maintainer

A contributor who has demonstrated sustained, quality involvement over multiple releases, AND has explicit write access to the repo from the BDFL.

Maintainers can:

- Merge PRs (their own and others') after review
- Triage issues (label, close, assign)
- Cut releases (tag, ship)
- Approve architectural ADRs

Today the only maintainer is the BDFL pro-tem. The path to add maintainers is described in [Becoming a maintainer](#becoming-a-maintainer) below.

### BDFL pro-tem

[@salvatore-balestrino](https://github.com/salvatore-balestrino). "Pro-tem" means *temporary* — the role exists until the project grows enough to warrant a more distributed structure (technical steering committee or similar). The BDFL:

- Has final say on architectural decisions when consensus is needed
- Owns release cadence policy
- Promotes contributors to maintainer status
- Manages repo-level settings (Actions, branch protection, Discussions, secrets, etc.)

The BDFL's day-to-day role is the same as any maintainer's; the title only matters when a tie needs breaking.

## Decision-making

### Most decisions: lazy consensus

Open a PR. If no maintainer objects within a reasonable review window (~3 business days for non-trivial PRs, faster for obvious fixes), it can land. Objections are addressed in the PR or escalated to an ADR.

This is the default. Tactical decisions about implementation details, dependencies, naming, refactors — all lazy consensus.

### Architectural decisions: ADRs

A decision is "architectural" if it:

- Changes a load-bearing assumption (tenancy model, auth flow, module boundary)
- Adds or removes a contract that other code depends on (a `shared/iface` interface, an OpenAPI surface, an event-bus shape)
- Affects how operators deploy or upgrade the system
- Is hard to reverse later

For these, write an ADR. The format and existing examples are in [`docs/adr/`](docs/adr/). See [RFC process](#rfc-process) below.

### Disputes: BDFL breaks ties

Disagreement that can't be resolved on a PR or ADR escalates to the BDFL, who makes a call. The call is binding for the current cycle but can be revisited via a new ADR.

## RFC process

For ADR-worthy decisions (see above), the process is:

1. **Open a draft PR** under `docs/adr/NNNN-<slug>.md`, following the style of existing ADRs (`docs/adr/0001-...` through `docs/adr/0005-...`).
2. **Set the frontmatter** with `status: proposed` and `public: false`. The `public` flag controls whether the ADR appears on [docs.orkestra.cc](https://docs.orkestra.cc/adrs) once the decision lands — proposed ADRs stay private to the monorepo.
3. **Iterate in PR comments** until rough consensus emerges. Maintainers + the BDFL are the deciders, but every contributor is welcome to comment.
4. **When accepted**: change `status: proposed` → `status: accepted`, flip `public: true` if the decision is OK to share publicly, merge the PR.
5. **When superseded** by a later ADR: change `status: accepted` → `status: superseded`, link to the superseding ADR in the frontmatter `Supersedes:` / `Superseded by:` field.

There is no fixed review window for ADRs — they take however long they take. Don't merge until the discussion has actually converged.

## Becoming a maintainer

There's no fixed formula. The signal is **sustained, quality involvement**. Concretely:

- Multiple merged non-trivial PRs across more than one release cycle
- Constructive PR reviews (your reviews catch real bugs / improve real code)
- Active in Discussions / issue triage
- Domain ownership — you're the de-facto go-to person for some part of the codebase

When a contributor matches that pattern, the BDFL invites them privately. The invite is not a vote — it's the BDFL's call. The invitee can decline (no obligation).

There is no formal "step down" process today. If a maintainer becomes inactive for 6+ months, the BDFL may move them to an "alumni" status (no write access, recognized in `MAINTAINERS.md` or similar). Activity-driven, not punitive.

## Releases

See [CHANGELOG.md](CHANGELOG.md) for the per-release history. The release cadence today is **opportunistic**: a `vX.Y.Z` tag goes out when:

- A meaningful feature/fix bundle has accumulated on `main`
- All CI gates are green on `main` (see [the pre-flight checklist](https://github.com/orkestra-cc/orkestra/blob/main/CONTRIBUTING.md#pre-release-pre-flight))
- The BDFL or a maintainer has time to author the release notes

Tagging is **manual** — push an annotated tag like `v0.2.0`, the [release workflow](.github/workflows/release.yml) takes over: regenerates `CHANGELOG.md` from `cliff.toml`, opens a GitHub Release with the changelog body. No release-please bot, no automatic version bumps.

[Semantic Versioning](https://semver.org) for the public API:

- `MAJOR.MINOR.PATCH`
- MAJOR for backward-incompatible API changes (HTTP surface, OpenAPI shape, env-var contract)
- MINOR for additive features
- PATCH for bug fixes

The SDK (`github.com/orkestra-cc/orkestra-sdk`) follows its own SemVer — see that repo's policy.

## Code of conduct

We use the [Contributor Covenant 2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/) — see [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md). Reports go to the BDFL at <salvatore.balestrino@gmail.com>. Confidential, no retaliation, responded to within 5 business days.

## Project changes

Changes to this governance doc itself follow the ADR process. The BDFL drafts the proposal; existing contributors are invited to comment via the PR; the BDFL merges when discussion converges.

If/when the project transitions away from a single-maintainer model (technical steering committee, foundation, etc.), this document is the canonical home for that change.

## See also

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — practical contributor guide (setup, PR checklist, conventions)
- [`CODE_OF_CONDUCT.md`](CODE_OF_CONDUCT.md) — Contributor Covenant 2.1
- [`ROADMAP.md`](ROADMAP.md) — what we're working on next
- [`docs/adr/`](docs/adr/) — Architecture Decision Records
- [`SECURITY.md`](SECURITY.md) — security disclosure policy
