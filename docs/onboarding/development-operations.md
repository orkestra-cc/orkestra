# Development operations: access, secrets and recovery

**Date:** 2026-09-07  
**Status:** Operational design approved; configuration, numerical recovery targets, retention, capacity admission and operational validation pending.  
**Parent decision:** [Development stack and delivery workflow](development-stack.md).  
**Pilot contract:** [Shared development VM](development-vm-blueprint.md).

This document records point 5 of the operating model: team URLs/access,
secret delivery, backup/restore and measured pilot sizing. It does not install
services, change network policies, distribute credentials, schedule backups,
restore data or provision VMs. It does not introduce a CodeOps service.

Private identities, domains, grants, secret references, backup destinations
and recovery commitments belong in the infrastructure repository's canonical
declarations, not in this public guide or a second editable inventory.

## Approved private preview access

Use one persistent **Caddy reverse proxy per development VM**, independent of
the four worktree applications. SSH tunnels remain an option for a restricted
technical pilot, not the normal team preview interface. This decision selects
the development ingress model; it does not change public production exposure.

- Assign stable task URLs with distinct hostnames for each worktree and its
  operator/client surfaces. Derive mappings from the VM/task bindings; do not
  confuse a task hostname with a new VM identity. Ports alone do not isolate
  browser cookies. Preserve the existing tier and shared-data auth contract.
- Distribute private DNS through NetBird and grant only the intended team
  access to preview endpoints. Browser access, personal SSH access and VM or
  shared-infra administration remain separate permissions, scoped by the
  [product/environment matrix](development-stack.md#approved-product-and-environment-permissions).
- Use HTTPS. DNS validation for a domain under the operator's control can
  obtain certificates without exposing application ports to the Internet.
  The domain, certificate scope, issuer and narrowly scoped DNS credentials
  still need configuration; this document creates none of them.
- Keep proxy configuration and certificate material outside worktrees under
  infrastructure management. Route to verified published app endpoints, not
  rootless container IPs; do not grant the proxy personal Docker sockets.
- Stopping one app makes only its preview unavailable. The proxy, shared
  infra and other apps remain running. Task retirement must not remove
  another task's route; restarting a retained task preserves its URL binding.

NetBird supports private DNS distribution, but DNS resolution is not an
access-control grant. Caddy's ACME DNS challenge does not require the service
to be publicly reachable.
[NetBird DNS](https://docs.netbird.io/manage/dns),
[Caddy HTTPS and DNS validation](https://caddyserver.com/docs/automatic-https).

On the shared staging VM, an IP/port grant to one common HTTPS listener does
not distinguish product hostnames. Choose distinct enforceable endpoints or
per-application authorization at the ingress before claiming product-scoped
preview access. Network admission never replaces application authentication,
RBAC or release approval. The existing administration-only NetBird design
needs explicit preview policies; administrative access is not an implicit
grant to all applications.
[NetBird access policies](https://docs.netbird.io/manage/access-control).

**Dev previews are for trusted, authorized codebase developers.** The current
[dev-token handler](../../backend/internal/shared/devtoken/devtoken.go)
can issue privileged signed tokens to an anonymous caller in development.
Do not treat the ordinary dev login screen as a boundary protecting the
shared dataset from someone allowed to reach that endpoint. Use staging for
testers who require limited application privileges; broader dev preview
audiences need a separately reviewed access design.

Before team use, test DNS and TLS from the actual client devices, API host
routing, WebSocket/HMR forwarding, host-only cookies, OAuth/passkey behavior
and RustFS presigned uploads/downloads. All active origins must keep working
when another app starts or stops; preserve the coordinated bucket CORS policy.
Verify allowed and denied access, including the shared staging product boundary,
without exposing databases, Docker sockets or unauthenticated dev services
publicly. Exact hostnames, port allocations, proxy service ownership and the
restricted route-update mechanism remain implementation details to specify.

## Approved secret management

Follow the infrastructure repository's selected separation:

| Secret class | Selected authority | Boundary |
| --- | --- | --- |
| Bootstrap | SOPS + age | Only authorized provisioning; no plaintext or private decryption keys in Git |
| Runtime | OpenBao | Only the workload's codebase/environment paths and service permissions |
| Recovery | Protected password manager and offline recovery material | Available independently of the infrastructure being recovered |

Do not copy an entire `.env` between tasks. Each runtime receives only its
selected environment's application-scoped service credentials and required
key material. Infra administrator credentials, provisioning state and
production secrets stay out of development workspaces and ordinary build jobs.
Personal Git and agent-provider credentials remain personal.

For rootless apps, use protected runtime files outside the checkout, delivered
to the authorized user and mounted read-only where the consumer supports it.
OpenBao Agent can render secret files; configure restrictive permissions
explicitly, since a newly created destination defaults to `0644` if permissions
are omitted. Do not place values in command arguments, logs, tracker records
or images. Configure the delivery identity, UID mapping, file ownership and
the application's file/env consumption path before calling this operational.
[OpenBao Agent templates](https://openbao.org/docs/agent-and-proxy/agent/template/).

Shared data deliberately require compatible dataset encryption keys across
authorized worktrees, including ConfigService/OAuth, MFA and KMS. Authentication
credentials and data-encryption keys have different rotation consequences:
creating a task must not regenerate keys for an existing dataset. Preserve the
decryption capability required by retained backups. Rootless separation and
read-only mounts do not hide a secret from an authorized process that must
read it; the common dev dataset remains a trusted-codebase boundary.

Specify and test credential renewal/rotation, application refresh or restart,
revocation and behavior when OpenBao is unavailable or credentials expire.
Do not assume that replacing a file reloads a running application's settings,
or that a short-lived OpenBao token makes every downstream credential dynamic.
An unavailable secret source must not cause a fallback to another environment
or newly generated dataset keys. Recovery material, including required unseal
and backup decryption keys, must be recoverable without the failed VM or forge.
[Proxmox Backup key recovery](https://pbs.proxmox.com/docs/backup-client.html).

## Approved backup and restore coverage

Protect three distinct kinds of state:

1. **Unpublished work:** local repositories and Git metadata, branches not
   pushed, uncommitted changes, new untracked files and retained worktrees.
   A remote Git repository alone does not preserve these. Linked worktrees
   also depend on their owning clone's common Git directory; recover that
   relationship, not just a directory of source files.
2. **Application state:** MongoDB records, RustFS objects, required configuration
   and the corresponding recoverable keys. Classify Redis state explicitly:
   sessions and coordination state are not automatically disposable caches.
   Record whether retained exception datasets are covered or reproducible.
3. **Machine reconstruction:** canonical infrastructure declarations and
   encrypted VM backups through Proxmox Backup Server, with an off-site copy.
   Recovery credentials and keys must remain independently accessible.

Backup scope must explicitly include the persistent workspace, Docker/data
and configuration disks selected for the VM. Exclusions for reproducible
images, dependencies or caches must not accidentally exclude unpublished
source, Git metadata, unique test data or required configuration. Protect any
secret-bearing backup; a backup is not a way to publish local `.env` files.

**PBS is selected as the VM backup foundation, not proof of application
consistency or an achieved recovery objective.** A consistent MongoDB dump
does not by itself establish a matching RustFS object state. Define a coherent
capture/recovery point across the required services and keys. An agreed dev
maintenance window is acceptable where necessary; coordinate all writers and
background activity rather than stopping only one of four apps. Exact capture
and quiescence procedures still require design and testing.
[MongoDB consistent dumps](https://www.mongodb.com/docs/database-tools/mongodump/).

The environment maintainer and substitute own the recovery procedure. Work
owners identify unpublished work and retained exception data. For each
codebase/product and environment, record privately:

- covered failure scenarios and state, acceptable data loss (RPO) and target
  restoration time (RTO);
- backup/copy frequency, retention and protected destinations;
- recovery authority, prerequisite keys/configuration/artifacts and procedure;
- last usable recovery point, failures or missing copies, and restore-test
  evidence including measured loss and elapsed time.

**No numerical RPO, RTO, frequency or retention is approved here.** Do not
inherit a generic infrastructure backup target for every product, or give dev
and staging production commitments by default. Choose schedules and capture
mechanisms against the actual requirements; successful job execution alone
does not prove that a sufficiently recent, usable recovery point exists.
See [product-specific recovery requirements](development-stack.md#product-specific-recovery-requirements).

The pilot must restore a copy into an **isolated environment**, with background
jobs and external sending prevented before restored applications can start.
Keep restored network identity away from live consumers. Verify the database,
object references, decryption and recovery of a deliberately incomplete
worktree. Decide how sessions and coordination state are safely resumed or
invalidated; neither blindly replay old workers/leases nor assume a blanket
Redis reset is harmless. Record actual elapsed recovery time and recovered
state, not just a successful file restore.

A restore test does not authorize overwriting the common dataset. Restoring
live data requires a separate explicit operation and a known loss window.
Closing a worktree, a failed deployment or ordinary application rollback
must never trigger automatic database restore or dataset reset.

## Approved measured sizing and rollout gate

Use one representative codebase VM and the existing
[capacity hypothesis](development-vm-blueprint.md#pilot-capacity-hypothesis)
and [pilot acceptance contract](development-vm-blueprint.md#pilot-acceptance-and-failure-handling).
The baseline is four simultaneous app runtimes across at least two users,
sharing one infra/dataset, not four complete infrastructure copies.

Measure a one-task baseline and the four-task workload with real editor/agent
activity, hot reload and overlapping checks/builds. Include the product's
addons and required frontends. Record peak memory, OOM/swap behavior, CPU
pressure, disk I/O waits, image/cache/data growth and build/reload duration.
Measure an explicit data/infra isolation exception separately. Agree workload,
measurement window and acceptable slowdown before declaring the VM sufficient;
do not infer sustained capacity from four idle containers.

The candidate 8 vCPU / 32 GiB RAM and disk sizes are still unmeasured. Add the
persistent proxy and supporting agents to the whole-VM budget. Check the
physical-host budget for all six dev VMs, at least four active codebases,
staging, production, forge, runners and other platform services, including
backup and restore space. Stopped VMs still consume persistent disk and thin
provisioning does not create physical capacity. The current fleet hypothesis
is not admitted by the private host-capacity review; revise capacity or sizes
before rollout rather than silently lowering the four-task target.

Team rollout requires successful private-access and secret-delivery tests,
the [blocking shared-data checklist](development-shared-data-checklist.md),
app-only closure, reboot and isolated restore evidence, and an accepted
whole-host budget. A passing capacity test does not waive the other gates.

## Remaining infrastructure alignment

Before implementation, update the private canonical design to distinguish
preview access from administration-only NetBird access, parameterize recovery
requirements by product/environment, and reconcile the capacity catalogue with
pilot measurements. Select concrete domains, endpoints, service identities,
secret delivery/refresh controls, backup procedures and recovery targets there.
Do not publish private inventory or recovery commitments in this repository.

This approval records the operational model only. The infrastructure repository
has not been modified by this document, no pilot has passed, and the
[superseded implementation plan](../superpowers/plans/2026-09-07-rootless-development-pilot.md)
remains non-executable until replaced and reviewed against all approved decisions.
