# Rootless Development Pilot Implementation Plan

> **Do not execute — superseded on 2026-09-07.** The operator selected concurrent
> worktree apps sharing the same dev infra **and dataset** by default. Separate
> data or dedicated infra are exceptions. The task list below preserves an
> unexecuted historical proposal; it is not implementation authority. Read the
> [current blueprint](../../onboarding/development-vm-blueprint.md#shared-development-environment).

Before a replacement plan is executable, specify persistent infra ownership,
cross-user rootless connectivity, shared data-key delivery, attachment-only
app lifecycle, compatibility/background-job policy and revised acceptance
tests. Reconcile guest tasks too; do not assume Tasks 1–4 can be executed
unchanged independently of these decisions.

> **For agentic workers:** Only after a replacement plan is reviewed, use
> superpowers:subagent-driven-development or superpowers:executing-plans for
> its tasks. The checkboxes below remain historical and unexecuted.

**Goal:** Produce a reproducible multiuser development guest, then determine whether four isolated Orkestra task stacks meet the agreed pilot criteria.

**Architecture:** OpenTofu owns the VM and generated inventory; Ansible owns accounts, disks, tooling and per-user rootless Docker. Personal clones and worktrees consume the application's existing lifecycle contract. Guest acceptance and Orkestra runtime acceptance are separate gates.

**Tech Stack:** Proxmox, OpenTofu, Ansible, the approved Debian development image, systemd/cgroup v2, Docker rootless, Git, Worktrunk, mise and Orkestra's Compose/Bash tooling.

**Spec:** [Shared development VM blueprint](../../onboarding/development-vm-blueprint.md) and [development workflow](../../onboarding/development-stack.md).

**Status:** Superseded; not executed. Historical goals, assumptions and task instructions below are not the current runtime contract.

## Global Constraints

- "The VM still has a target of four active tasks total, regardless of how many accounts, clones or retained worktrees exist."
- "One writer owns each task workspace at a time."
- "Do not silently grant privileged Docker access as a fix."
- "Stopping a stack retains its source and data by default."
- "Agents use task-account privileges, not administrator privileges."
- Start with one VM. The 8-vCPU, 32-GiB RAM, 40/80/120-GiB disk proposal is an experiment, not authorization to provision six VMs.
- No staging/production changes, existing-VM conversion, repository migration, CodeOps service or automatic cleanup in this plan.
- Infrastructure code and real identities belong in the private infrastructure repository. Its own instructions apply; keep its documentation in Italian. This public plan contains no real inventory or credentials.
- Before execution, create or verify an isolated implementation workspace using the worktree skill. Preserve the existing documentation changes; never copy `docker/.env` or signing keys into a new worktree.

---

## Scope and repository boundaries

Deliverable A is a guest that two distinct users can access and use with their
own rootless daemons. It is independently testable without Orkestra running.
Deliverable B is the application compatibility/capacity report on that guest.

Do not preselect an untested fix for the current application's UID mapping.
If the runtime probe fails, its captured reproduction becomes the input to a
small, separately reviewed application change. The four-stack gate stays open
until that change passes; a healthy Docker daemon alone does not close it.

The infrastructure sources were re-read on 2026-09-07. Record their actual
commit in the private work record at execution time and review intervening
changes. The file map below identifies integration points, not copies of the
private configuration.

| Repository     | Files                                                                                                                                                  | Responsibility                                                                                     |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- |
| Infrastructure | `config/development.yml` (new), `bin/development_policy.py` (new), `bin/development-validate` (new), `tests/test_development.py` (new)                 | Developer identities, VM/codebase assignments and offline validation                               |
| Infrastructure | `config/profiles.yml`, `config/services.yml`, `config/sizes.yml`, `config/vms.yml`, `config/backups.yml`, `Makefile`                                   | Explicit rootless developer capability, admitted pilot resources and policy integration            |
| Infrastructure | `tofu/locals.tf`, `tofu/inventory.tf`                                                                                                                  | Derive validated guest inputs without handwritten inventory                                        |
| Infrastructure | `ansible/site.yml`, `ansible/roles/developer_storage/tasks/main.yml` (new)                                                                             | Fresh developer disks; exclude the existing data-migration path                                    |
| Infrastructure | `ansible/roles/developer_accounts/tasks/main.yml` (new), `ansible/roles/ssh_hardening/templates/10-hardening.conf.j2`                                  | Personal accounts, key access and SSH allowlist                                                    |
| Infrastructure | `ansible/roles/docker/tasks/main.yml`, `ansible/roles/docker/vars/main.yml`, `ansible/roles/developer_runtime/tasks/main.yml` (new)                    | Package setup and separate rootful/rootless service configuration                                  |
| Infrastructure | `ansible/roles/developer_tooling/tasks/main.yml` (new), `ansible/developer-verify.yml` (new), `tests/test_developer_roles.py` (new)                    | Repeatable tooling and guest verification                                                          |
| Infrastructure | `docs/20-infrastructure/development-vm.md` (new), `docs/20-infrastructure/index.md`, `mkdocs.yml`                                                      | Private operator procedure and navigation                                                          |
| Orkestra       | `docker/docker-compose.dev.yml`, `docker/docker-compose.infra.yml`, `docker/.env.example`, `orkestra.sh`, `scripts/init.sh`, `scripts/health-check.sh` | Read during the compatibility probe; change only through the evidence-driven application follow-up |
| Orkestra       | This plan and the two linked onboarding documents                                                                                                      | Public contract and dated acceptance status, not private test output                               |

## Task 1: Validate the development inventory contract offline

**Files:** Infrastructure development policy files, `Makefile`, profile/service
catalogues, `tofu/locals.tf`, `tofu/inventory.tf`, and the private runbook above.

**Interfaces:**

- Consume the existing VM, profile and service catalogues.
- Produce `validate_development(development, vms, profiles) -> list[str]` in
  `bin/development_policy.py`; an empty list means valid. The CLI loads the
  three YAML documents, prints only validation errors, and exits nonzero on
  malformed input or validation failure. Arguments are the full development
  document, `config/vms.yml`'s `vms` map and `config/profiles.yml`'s `profiles`
  map, respectively.
- Produce `vm_development` in generated inventory, `null` for non-development
  hosts; never infer development privileges from the hostname or environment.
  Assigned hosts receive `codebase_id`, `repository_url`,
  `active_worktree_target` and `users`: a list of resolved records, each with
  `name`, `uid`, `gid`, `subid_start`, `subid_count` and `authorized_keys`.

- [ ] Define `config/development.yml` with `schema_version: 1`, a `users` map
      and an `assignments` map keyed by existing canonical VM identity. User records
      contain `uid`, `gid`, `subid_start`, `subid_count` and `authorized_keys`.
      Assignments contain `codebase_id`, `repository_url`, `users` and
      `active_worktree_target: 4`. Store no IP addresses, passwords or tokens here.
- [ ] Add red tests for missing users/VMs, duplicate IDs, overlapping sub-ID
      ranges, fewer than 65,536 sub-IDs, missing keys, embedded URL credentials,
      duplicate codebase assignments, a target other than four and a rootless
      assignment on a non-developer profile. Include a valid two-user fixture with
      synthetic identities; generate its SSH public keys in a test temporary
      directory rather than copying a real developer's keys.

  The overlap rule's minimal test is:

  ```python
  import copy
  import sys
  import unittest
  from pathlib import Path

  sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "bin"))
  from development_policy import subid_ranges_overlap

  class SubordinateRanges(unittest.TestCase):
      def test_overlap_and_adjacent_ranges(self):
          users = [
              {"subid_start": 231072, "subid_count": 65536},
              {"subid_start": 296608, "subid_count": 65536},
          ]
          self.assertFalse(subid_ranges_overlap(users))
          overlapping = copy.deepcopy(users)
          overlapping[1]["subid_start"] -= 1
          self.assertTrue(subid_ranges_overlap(overlapping))
  ```

- [ ] Run `python3 -m unittest discover -s tests -p test_development.py -v`;
      confirm the new test fails for the missing implementation, not an unrelated
      import or toolchain error. Implement the pure range helper and invoke it from
      the validator after validating integer types and positive lengths:

  ```python
  def subid_ranges_overlap(users):
      spans = sorted(
          (user["subid_start"], user["subid_start"] + user["subid_count"])
          for user in users
      )
      return any(right[0] < left[1] for left, right in zip(spans, spans[1:]))
  ```

- [ ] Implement the remaining declared validation cases using allowlisted
      fields and explicit type checks, rejecting booleans as numeric IDs. Reserve
      subordinate ranges separately from login/system IDs. On the guest, reject
      conflicts with pre-existing accounts and mappings rather than overwriting
      them. Re-run the negative fixtures and require specific diagnostic messages.
- [ ] Add `docker.mode: rootless` to the developer profile and `rootful` to
      the other profiles. Preserve the developer ingress restriction. Set the
      developer automatic reboot policy to false; record the maintenance window
      in the private pilot work record. Do not copy the administrator's sudo policy
      onto developer accounts.
- [ ] Allow `stack: none` for the owning service's developer guest in the
      service catalogue; keep its normal deployed application stack available.
      Validate that assigned developer VMs use `stack: none`. This prevents the
      single-stack application role and its secrets from becoming the bootstrap.
- [ ] Derive `vm_development` from the assignment and referenced users in
      `tofu/locals.tf` and emit it in `tofu/inventory.tf`. Keep the original remote
      URL only in private configuration and reject credential-bearing URLs.
      Add equivalent OpenTofu preconditions for the new contract; a caller using
      OpenTofu directly must not bypass the essential assignment checks.
- [ ] Add `make development-validate` to the existing lint dependency chain.
      Run the new tests, `make services-validate`, `make development-validate`,
      `make security-test` and initialized OpenTofu validation. Commit the coherent
      schema/validation change in the infrastructure implementation branch.

## Task 2: Add safe, executable developer storage

**Files:** New `developer_storage` role, `ansible/site.yml`, role tests and
private runbook. Keep the existing deployed-stack disk behavior unchanged.

**Interfaces:** Consume admitted `disk_docker_dev`, `disk_data_dev` and
`vm_development`; produce `/srv/docker-users` and `/srv/workspaces` mounts.

- [ ] Write role tests asserting that a rootless developer host selects
      `developer_storage`, skips the existing `disks` role, and never enters its
      Docker migration/copy/delete steps. Check that non-developer hosts retain
      their existing selection. Run the tests before changing role dispatch.
- [ ] Require the operator's exact new-VM identity and verified disk mapping
      before first formatting. Refuse disks with existing signatures unless their
      UUID, filesystem and expected mount already match this managed layout.
      Missing devices or a failed inspection are errors, not blank disks.
- [ ] Use the new role to mount the Docker disk at `/srv/docker-users` and
      the workspace disk at `/srv/workspaces`; use UUID-backed persistent mounts,
      `force: false` when creating filesystems, and no source-data migration.
      Both filesystems must permit execution:

  ```yaml
  # Mount contract for both developer filesystems, after disk admission.
  fstype: xfs
  opts: defaults,noatime,nodev,nosuid
  state: mounted
  passno: 2
  ```

- [ ] Test refusal of a mismatched existing filesystem, missing inspection
      result and unexpected mount. Test idempotency on the approved fresh guest:
      the second run must neither format nor move any data. Verify XFS supports
      the selected Docker storage driver.
- [ ] Run the role tests, Ansible syntax check and relevant infrastructure
      checks. Commit this storage boundary separately so it can be reviewed without
      accepting any user-account or application-runtime changes.

The existing application-data mount's `noexec` policy is not appropriate for
build outputs. Do not globally remove that policy from staging/production.

## Task 3: Bootstrap personal accounts and rootless Docker

**Files:** New `developer_accounts`/`developer_runtime` roles, Docker package
role, SSH template, `ansible/site.yml`, role tests and private runbook.

**Interfaces:** Consume validated `vm_development` users and the Task 2 mounts;
produce each user's private workspace, rootless service and explicit Docker
context. Use account names and UIDs from inventory, never fixed UID 1000.

- [ ] Add failing dispatch/template tests: rootless hosts must not start the
      system Docker service, enable its prune timer, install an application stack
      or run provisioning as a daily developer. Non-developer hosts retain the
      existing rootful path. SSH must allow the provisioning administrator plus
      exactly the assigned developers.
- [ ] Create assigned accounts with stable UID/GID, private home and `.ssh`
      directories, declared public keys and private workspace/data-root directories.
      Do not add them to `sudo`/`docker`, share credentials or recursively change
      ownership of an existing worktree. Refuse conflicting existing identities.
- [ ] Order account creation before installing the expanded SSH allowlist;
      validate sshd configuration before reload. Keep the administrator's recovery
      session open while testing a new developer login. Do not enable SSH agent
      forwarding or widen the private network's allowed sources.
- [ ] Separate Docker package installation from rootful service setup. Install
      the approved package versions and rootless prerequisites, including `uidmap`,
      `dbus-user-session` and matching `docker-ce-rootless-extras`. Do not substitute
      an unpinned downloaded shell installer. On the admitted fresh developer guest,
      prevent system Docker/socket activation and the global prune timer.
- [ ] Install non-overlapping subordinate mappings while preserving unrelated
      entries. Enable lingering for assigned accounts and establish their user
      systemd managers before user-scoped service operations. Do not assume that
      `become_user` alone creates a working D-Bus session.
- [ ] Configure each user's daemon before its first start. For an illustrative
      account, the generated `~/.config/docker/daemon.json` payload is:

  ```json
  { "data-root": "/srv/docker-users/alice" }
  ```

  Generate the path from the validated account name. Refuse an unexpected
  existing daemon configuration; do not silently redirect a live data-root.
  Run the packaged `dockerd-rootless-setuptool.sh install` in the user's proper
  session only when installation is absent; no `--force` workaround.

- [ ] Use user-scoped systemd, with explicit `XDG_RUNTIME_DIR` and a working
      user bus. Enable the user's `rootless` context and verify its socket resolves
      to `/run/user/<that user's UID>/docker.sock`. No TCP daemon endpoint is needed.
      Keep secret-bearing task `.env` files out of systemd environment directives.
- [ ] Delegate CPU, memory, I/O and process controllers to the assigned user
      managers. Verify cgroup v2/systemd and effective controllers; do not count
      accepted CLI resource flags as enforcement evidence.
- [ ] Re-run role tests and infrastructure validation; commit the account and
      daemon integration. Record package versions and guest test prerequisites in
      the private runbook, without recording private keys or access tokens.

Use Docker's packaged rootless installation and Ansible's documented user
service support, including the user-bus prerequisite.
[Docker rootless installation](https://docs.docker.com/engine/security/rootless/),
[Ansible systemd service scope](https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/systemd_service_module.html).

## Task 4: Provide tooling without creating task state

**Files:** New `developer_tooling` role, role tests, private runbook.

**Interfaces:** Consume assigned accounts; produce tools and workspace
directories, not repositories, credentials or running application services.

- [ ] Test that repeated guest configuration does not invoke Git checkout,
      pull/reset/clean, `orkestra.sh init --force`, stack deployment or worktree
      deletion. Include these forbidden command checks in the role test suite.
- [ ] Install Git, a pinned/tested Worktrunk release and the approved mise
      installation mechanism. Respect each checkout's `.mise.toml`; the OS image's
      language packages do not replace project toolchain pins. Install only the
      prerequisites needed for the selected editor and agent clients.
- [ ] Document personal first-clone authentication and Worktrunk configuration
      for the blueprint's workspace layout. Require review before trusting repository
      hooks or mise configuration. Leave agent-provider login to each developer;
      do not bake provider credentials or a shared agent account into the guest.
- [ ] Disable automatic workload eviction/pruning in the pilot. Describe how
      owners review disk pressure, retain work and request a maintenance window.
      Task count remains an operating agreement, not a new scheduler/service.
- [ ] Run role tests and syntax checks, update the private operator instructions
      and commit. A rerun must leave existing worktree branches and files unchanged.

## Task 5: Admit and provision exactly one guest

**Files:** Infrastructure size/VM/network/backup configuration and private
pilot record. No real inventory edits belong in this public plan.

**Interfaces:** Consume reviewed Tasks 1–4; produce one admitted guest and
generated inventory. This task requires explicit provisioning authority.

- [ ] Record the owning codebase, two consenting developers and keys, canonical
      VM identity, reserved VMID/IP, guest image/template revision, actual host
      CPU/storage capacity, maintenance window and backup disposition. An absent
      value blocks provisioning; do not invent identities or treat planned hardware
      as running capacity. The initial guest may contain only disposable test work
      until backup/recovery is verified.
- [ ] Review a dedicated 8-vCPU/32-GiB, 40/80/120-GiB size entry against the
      whole host budget. Add only the admitted pilot VM and corresponding
      development/backup assignments. Do not declare the other five VMs for apply.
- [ ] Run repository validation from the infrastructure checkout:

  ```bash
  make services-validate
  make development-validate
  make lint
  make pbs-validate
  make security-test
  make docs-check
  ```

  These checks are not provisioning approval or operational evidence. Some
  need initialized local tools and build documentation images. Review commands
  before running them on a shared control system.

- [ ] From the authorized infrastructure control checkout, initialize OpenTofu
      and produce a saved private plan. Verify every proposed resource change:
      one new pilot and expected derived artifacts only, no unrelated mutation,
      replacement or deletion. `LIMIT` scopes Ansible, **not** OpenTofu. Never use
      `-target` to hide an unexplained whole-plan change.
- [ ] After approval of that exact plan and host capacity, apply the saved plan.
      Use the resulting generated inventory; do not manually write an IP into it.
      Store plan/state artifacts privately, not with these public documents.
- [ ] Set `PILOT_VM` to the approved canonical inventory identity in the
      operator shell. Confirm scope before guest configuration:

  ```bash
  : "${PILOT_VM:?Set the approved pilot inventory identity first}"
  ansible-playbook -i ansible/inventory/generated.yml ansible/site.yml \
    --limit "$PILOT_VM" --list-hosts
  ```

  Require exactly the intended one host. Only then run
  `make config LIMIT="$PILOT_VM"` from the infrastructure checkout. Do not rely
  on `--check` to make arbitrary playbooks harmless or print secrets with
  unrestricted `--diff`.

## Task 6: Verify the guest independently of Orkestra

**Files:** New `ansible/developer-verify.yml`, role tests and private evidence.

**Interfaces:** Consume the admitted guest; produce pass/fail evidence for
accounts, storage, daemon isolation, persistence and effective resource control.

- [ ] Implement a read-only verification playbook with failed assertions for
      wrong mount/owner, missing rootless service, rootful socket access, missing
      cgroup controllers and undeclared login access. It must not install packages,
      start services or repair failures while reporting verification success.
- [ ] Each developer logs in using their own identity and runs:

  ```bash
  set -euo pipefail
  test "$(id -u)" -ne 0
  test "${XDG_RUNTIME_DIR:?A real user session is required}" = "/run/user/$(id -u)"
  systemctl --user is-active --quiet docker
  docker --context rootless info --format '{{json .}}' |
    jq -e '(.SecurityOptions | any(. == "name=rootless")) and
           .CgroupVersion == "2" and .CgroupDriver == "systemd"' >/dev/null
  if docker --host unix:///var/run/docker.sock info >/dev/null 2>&1; then
    printf '%s\n' 'FAIL: developer can access the rootful socket' >&2
    exit 1
  fi
  findmnt --target /srv/workspaces --output TARGET,FSTYPE,OPTIONS
  findmnt --target /srv/docker-users --output TARGET,FSTYPE,OPTIONS
  ```

  The playbook must also confirm each data-root and socket path, private
  directories, no `sudo`/`docker` membership and no sudoers grant. A negative
  socket probe alone cannot prove all these properties.

- [ ] Exercise a bounded disposable container workload in each daemon to
      verify effective cgroup memory/CPU/process limits and volume persistence.
      Use a reviewed image digest and uniquely identified test resources; never
      stress the whole host or inspect a colleague's application environment.
- [ ] Re-run bootstrap with a retained test file/worktree and compare its
      content hash, ownership and branch before/after. Reboot during the approved
      test window; verify the documented service policy and preserved work.
- [ ] Record both positive and negative results. Commit the verification
      playbook and runbook only after their checks distinguish real failure from
      missing tools or unreachable hosts. Do not mark guest acceptance from static
      tests alone.

User service state and cgroup availability must be checked in the actual user
context. Rootless resource flags are ineffective without the prerequisites.
[Docker rootless service and resource guidance](https://docs.docker.com/engine/security/rootless/tips/).

## Task 7: Probe Orkestra compatibility before expanding to four stacks

**Files:** Read the application integration points in the file map plus
`docker/CLAUDE.md` and `scripts/CLAUDE.md`. Save probe evidence privately.

**Interfaces:** Consume the accepted guest and two fresh personal clones;
produce a verified runtime contract or a reproducible compatibility failure.

- [ ] Create one disposable task in each personal clone using reviewed
      Worktrunk configuration. Allocate distinct stack names, ports and origins
      explicitly in the private work records before starting them. The current
      free-port scan does not reserve ports for another concurrently initialized,
      stopped task; serialize pilot allocation and verify host-wide uniqueness.
- [ ] Initialize each fresh task through `./orkestra.sh init --yes`, without
      `--force`. Before deploy, set `ENV=development`, task-specific `APP_NAME`,
      distinct ports/URLs and `HOST_BIND_ADDRESS=127.0.0.1` in its local config.
      Verify every service's binding and data isolation. Use only disposable seeds.
- [ ] Verify the same rootless context used in Task 6 is effective for the
      lifecycle command. From each task root, run:

  ```bash
  DOCKER_CONTEXT=rootless ./orkestra.sh deploy --scope all --yes
  DOCKER_CONTEXT=rootless ./orkestra.sh status
  ```

  Record failed health checks as failures even if a container started successfully.

- [ ] Test source edits, Go and frontend caches, signing-key access, MongoDB,
      Redis and object storage for both host UIDs. Require the private signing key
      to remain mode `0600`. Confirm bind mounts resolve to the intended worktree,
      not its primary clone or a different user's path.
- [ ] If UID mapping fails, preserve the reproduction and stop this gate.
      Rootless maps container UID 0 to the owning host user; container UID 1000
      does not automatically mean host UID 1000. Do not use world-writable paths,
      readable private keys or host-root Docker as a workaround. Plan the smallest
      development-only adjustment with regression tests before changing Compose.
- [ ] A follow-up application change must test the observed failure plus
      rootful-dev, staging and production non-regression, private-key mode and
      explicit context selection. Keep lifecycle integration in `orkestra.sh`;
      do not introduce a second deployment script. Run the affected Bash/Compose
      tests and repository `make` checks, then repeat the live two-user probe.

The UID behavior is documented by Docker, but the correct adjustment for this
application still requires evidence.
[Docker UID/GID mapping](https://docs.docker.com/engine/security/rootless/uid-gid-mapping/).

## Task 8: Validate four-task operation and close the pilot

**Files:** Private pilot report and the public blueprint's acceptance status.

**Interfaces:** Consume the passing runtime probe; produce a capacity decision
and a safe operating/handoff procedure, not automatic fleet rollout.

- [ ] Agree workload and acceptable slowdown before testing. Run four task
      stacks across at least two users with the relevant addons/frontends, agents
      and overlapping `make` checks. Compare memory/CPU pressure, build times and
      disk growth against one-task baseline. No OOM or sustained swap thrashing.
- [ ] Confirm four distinguishable code versions and separate data. Test image
      tag/cache conflicts between two worktrees owned by the same user as well as
      cross-user isolation. Stop one task with `orkestra.sh`; verify the other
      three continue and the stopped task resumes with its retained data.
- [ ] Reconcile each owner's Git/worktree and Docker state. A missing user
      session or unreachable VM is unchecked, not clean. Record retained work and
      data explicitly; do not add a new service to aggregate business task status.
- [ ] Test recovery of unfinished work and selected task data from the approved
      backup in an isolated recovery target. Never restore over a live workspace.
      Git branches alone do not retain ignored config, signing keys or local data.
- [ ] Exercise offboarding with a disposable pilot account: preserve and hand
      off its work first, revoke keys/provider access, end sessions and stop its
      lingering services. Verify denial using a new session. Retain files until
      their exact disposition is separately approved.
- [ ] Record the outcome: accepted size, revised size needing retest, or
      compatibility failure. Keep preview ingress and staging/production release
      design as separate work. Do not provision the other five VMs until the whole
      host/storage budget and general team access/backup requirements are approved.
- [ ] Update public documents with dated, sanitized acceptance facts and commit
      that documentation separately. Leave unexecuted tasks unchecked.

## Review and execution handoff

No execution handoff is active for this superseded plan. None of its tasks
was executed. Replace its isolated-per-task assumptions with the approved
shared-data baseline, resolve the integration points listed at the top and
review the replacement before selecting an execution method or provisioning.
