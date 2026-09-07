# Shared development data: blocking compatibility checklist

**Date:** 2026-09-07  
**Status:** Compatibility policy and blocking checklist approved; launcher/reloader enforcement and pilot evidence pending. No environment is certified by this document.  
**Parent:** [Development VM blueprint](development-vm-blueprint.md).

Four worktree applications may concurrently read and write one codebase's
development dataset. Admission requires compatible data contracts, not merely
four healthy containers or four independently passing CI runs. Upstream,
commons and each product have separate environments and separate evidence;
passing the public core's checks does not certify private addons.

## Blocking rule and responsibility

There are two gates: **A enables the shared-data profile for a codebase**;
**B admits an individual worktree runtime against that profile**. Both must
pass before the candidate application can initialize against common data.

Each item needs a recorded result, evidence, responsible person and timestamp.
An unchecked, failed, unknown or stale item blocks admission. An inapplicable
item needs a recorded reason accepted by the environment maintainer; absence
of a test or an uninspected addon is not a reason to mark it inapplicable.
Never convert a failed automated check into a pass with a manual checkbox.

- The **environment maintainer or substitute** approves gate A, its compatible
  contract/profile revision and any coordinated change to shared state.
- The **work owner** records gate B and the change's impact. Routine edits
  within a validated profile use automated revalidation, not production-style
  human release approval on every save. Unknown compatibility requires review
  or an explicitly isolated environment, not optimistic admission.
- Keep evidence with the private environment/work records. The tracker links
  the result; it is not the enforcement mechanism. Record key references and
  versions, never key material, tokens, connection strings or dataset contents.

Failure blocks only the candidate's shared-data start/resume/update. It must
not stop other applications, reset data or prevent safe app-only stop/closure.
An environment-wide contract change is a separate coordinated operation with
affected owners; it cannot invalidate running consumers without a transition.

## Gate A — codebase profile qualification

Run these tests first on a disposable dev dataset with synthetic fixtures and
isolated side effects, never staging/production or the team's working data.
Exercise four representative versions across at least two Linux users, with
every relevant product addon. Bind the results to the tested revisions and
the compatibility range they actually establish.

- [ ] **A1 — Binding and ownership.** Distinct source mounts, app identities,
  ports and rootless daemons resolve to the intended common dev services and
  dataset. Missing services fail without creating replacements. App accounts
  cannot administer the infra daemon. The binding rejects staging/prod.
- [ ] **A2 — Data keys.** All consumers can read the same encrypted fixtures
  with the dataset-owned ConfigService/OAuth, MFA and KMS key references.
  Restart/recreation does not generate replacement keys. Wrong/missing keys
  fail admission without replacing ciphertext or silently reseeding secrets.
- [ ] **A3 — Sessions and browser boundaries.** The dev JWT signing/issuer,
  claim and session policies are compatible across admitted versions; keys
  are separate from staging/prod. Test login, refresh rotation, replay refusal,
  restart and revocation on both tiers. Each WT has distinct browser hosts
  with host-scoped cookies, preserving operator/client separation. Merely
  changing ports is insufficient. Verify OAuth start/callback/relay routing
  and passkey RP/origin settings for all supported WT origins.
- [ ] **A4 — Cache and shared security state.** Inventory Redis keys/channels,
  in-process caches and addon equivalents. Version or isolate derived cache
  values where readers differ, while retaining common invalidation generations,
  session revocations, security counters, leases and idempotency scopes where
  they govern the same state. Verify cross-version permission changes and
  revocations, repopulation races and store errors. No broad Redis flush.
- [ ] **A5 — Global jobs and local workers.** Inventory all scheduled jobs,
  queue consumers and startup goroutines. Prove safe coordination for each
  global job, including lease loss, retries and termination during a run.
  A lease alone is not proof of exactly-once effects: verify atomic claims or
  idempotent effects as appropriate. For jobs not ready for coordinated
  execution, allow only one designated WT among the four to execute them;
  the others must be demonstrably prevented from running those jobs. Record
  explicit role handover, with no replacement until the old worker is stopped
  or safely fenced. Local request-serving workers remain available.
- [ ] **A6 — Data writes and indexes.** All admitted readers/writers preserve
  the contract, including older writers preserving fields introduced by newer
  versions. Inspect actual MongoDB index keys/options, unique constraints,
  partial filters and TTLs; a healthy backend is not proof of index agreement.
  Migration/seed/index changes are coordinated once, not independently applied
  by four boot sequences. Destructive or incompatible changes use an isolated
  dataset. Test that startup cannot alter shared definitions outside the
  approved profile, and that incompatible specs fail admission.
- [ ] **A7 — Persistent configuration and bootstrap.** Verify module metadata,
  defaults/backfill, permission catalogs, role seeds and runtime enable/disable
  across versions. Distinguish persisted shared configuration from local
  process state. One app's boot or admin action must not silently overwrite
  another version's required contract or claim its workers restarted.
- [ ] **A8 — Objects and outbound effects.** Shared buckets, key conventions,
  browser-reachable signed URLs and cache scope are compatible. Test uploads,
  reads and permitted deletion from each origin after peer restarts. Bucket
  CORS is coordinated, never replaced with just the last WT's origins. Mail,
  webhooks, payments and other addon effects use verified dev sinks/sandboxes;
  background and request-triggered paths are both covered.
- [ ] **A9 — Enforcement and closure.** An invalid binding, missing result,
  changed contract and incompatible hot reload are refused before application
  initialization or affected code execution. Stopping/removing one WT stops
  its workers and leaves the other three and common infra/data available.
  Closing all four preserves infra/data. Validate the failed-stop removal
  block and a separately selected isolation exception.

Shared accounts have shared consequences. Use personal test accounts: password
changes, MFA removal, lockouts, account deletion and revoke-all operations can
affect their sessions across WTs. Separate cookies prevent accidental browser
interference, not token portability or access to the deliberately shared data.
Keep destructive retention testing isolated; disabling application jobs does
not disable MongoDB's own TTL deletions.

## Gate B — worktree admission and revalidation

This is a short per-task check that reuses gate A evidence while it remains
applicable. Do not repeat the full pilot for each UI edit, and do not claim an
old pilot proves a changed data/security contract.

- [ ] **B1 — Qualified target.** Select a passed, current codebase profile and
  verify its dev dataset/infra identity, contract revision and authorized owner.
- [ ] **B2 — Exact runtime inputs.** Record Work-ID, source path, base/HEAD,
  relevant dirty and untracked source changes, effective non-secret config,
  image/tooling references and the actual app binding. HEAD alone is not the
  code AIR/Vite runs. Check unique app ports/origins and key-reference versions.
- [ ] **B3 — Compatibility impact.** Classify changes against A2–A8, including
  dependencies and addons. Reuse applicable evidence for unchanged contracts;
  attach fresh checks for changed behavior and verify coexistence with the
  currently admitted versions. Unknown impact or an unsupported old version
  blocks shared use. A path filter may trigger review, not certify semantics.
- [ ] **B4 — Execution role and side effects.** Resolve each global worker's
  coordination/designated owner and verify this WT's permitted role. Confirm
  local workers, sandbox endpoints, browser callbacks and shared bucket policy.
- [ ] **B5 — Preflight result.** All applicable items and read-only live checks
  pass against the actual inputs. No implicit seeding, index reconciliation,
  data reset or service upgrade is permitted to make preflight pass. Link the
  result and any maintainer decision before enabling the runtime.

The future tooling must bind admission to effective inputs and the environment
contract revision. Re-evaluate when source/config/dependencies, dataset/key
references, worker role or compatibility profile changes, and on resume/rebind.
Ordinary changes inside the validated profile can pass automatically; changed
contracts need fresh compatibility evidence. The gate does not require a
clean worktree, a commit per save or a human approval per frontend edit.

**Hot reload is an enforcement boundary.** A launcher-only check is insufficient
when AIR/Vite can execute changed bind-mounted source later. Integrate the check
before application `Init`, rebuild/restart or affected hot-reloaded execution,
not only before HTTP traffic or PR merge: startup itself can write data.
Stop the task's app before editing contract-sensitive code unless the validated
reloader prevents unapproved changes from executing. A post-change watcher that
detects a violation after execution is not a blocking gate. If the selected
runtime cannot enforce this, keep that work stopped or use an isolated target.

Required controls belong in the sanctioned `orkestra.sh`/reloader path, with
Worktrunk calling that path where appropriate. CI uses the same repository
checks via `make`; host-side checks still verify the actual runtime and target.
No command names, flags or hooks for this gate are implemented here. Mutable
project hooks, CI status and a green issue checkbox alone do not enforce it.
This remains a trusted-team workflow, not a sandbox for malicious developers
who already have application-level write access to the common dataset.

## Evidence record and blocked outcome

Record once in the existing private work/environment record and link it from
the [coordination issue](development-stack.md#coordination-issue-template):

- Codebase, Work-ID/runtime binding, dataset and compatibility-profile revision.
- Tested source/dirty-content identity and effective configuration references.
- For each A/B item: result, evidence link, responsible person and check time;
  for an inapplicable item, the maintainer's accepted reason.
- Supported coexistence set, worker assignment and any changed-contract decision.
- Overall admission result and invalidation reason if evidence becomes stale.

Do not duplicate the issue's workflow-state label: admission is a check result,
not a second task status. A blocked shared-data admission may still allow the
task to progress on an explicitly isolated target.

On failure: report the failed item and leave the candidate stopped/unadmitted
to common data. Choose an approved correction, a coordinated environment change
or an explicit separate dataset/dedicated infra binding. Isolation needs its
own namespace/credential/worker and lifecycle checks; changing only a database
name is not proof. Never silently create an exception, reset the common dataset,
or offer a routine force/skip-checks route to shared admission. Preserve source
and retained data, and rerun the affected checks before resuming.

## Current implementation evidence and limits

Static inspection on 2026-09-07 identified useful mechanisms and remaining
qualification work; none of these observations is a passed gate A:

- [Auth maintenance](../../backend/internal/core/auth/maintenance.go) already
  elects a refresh-token sweep leader through a Redis lease. This does not
  certify every scheduled job or mixed-version behavior.
- [Compliance retention](../../backend/internal/core/compliance/services/retention.go)
  is off by default but has a per-process loop when enabled, without that lease.
- [Permission caches](../../backend/internal/core/authz/services/service.go)
  already use shared invalidation generations; they must not be lost by
  indiscriminately separating Redis per WT.
- [Module configuration seeding](../../backend/pkg/sdk/module/config_service.go),
  [role seeding](../../backend/internal/core/authz/repository/repository.go) and
  [index creation](../../backend/pkg/sdk/module/registry.go) have boot-time writes.
  Index conflicts are reported without automatically reconciling the old index.
- [OAuth configuration](../../backend/internal/core/auth/services/oauth_config_resolver.go)
  resolves persisted callback URLs; per-WT environment variables alone cannot
  establish distinct callback behavior. [Storage setup](../../backend/internal/shared/blob/s3.go)
  can reapply shared bucket CORS, and signed-URL caches also need scope checks.

The attachment-only lifecycle, automated admission/reloader checks, controlled
bootstrap and complete worker policy must be implemented and tested before
general shared-data adoption. Until then the checklist is a blocking rollout
requirement, not a claim of protection for existing stacks. This document
changes no current runtime, credentials, hooks, pipelines or VM configuration.

Technical references: [cookie scope across ports](https://www.rfc-editor.org/info/rfc6265/),
[MongoDB TTL processing](https://www.mongodb.com/docs/manual/core/index-ttl/),
[Redis lease validity and ownership](https://redis.io/docs/latest/develop/clients/patterns/distributed-locks/).
