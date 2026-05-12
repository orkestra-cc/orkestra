<!--
Thanks for the PR! A few asks before you mark this ready for review:

1. Title follows Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`, `ci:`, `build:`, `perf:`, `style:`, `revert:`). The commit-msg hook enforces this locally.
2. `make ci` passes locally for every surface you touched. The same `make` targets run in CI.
3. Affected `CLAUDE.md` / `README.md` files are updated **in this PR**, not deferred.

Drafts are welcome — set status to "Ready for review" when you're done iterating.
-->

## Summary

<!-- 1-3 sentences. Why does this change exist? What problem does it solve? -->

## Changes

<!-- Bullet list of what changed, organized by surface (backend / frontend-admin / frontend-client / mobile / docs / ci). Reviewers should be able to skim this and know where to look. -->

-

## Tenancy tier

<!-- For changes that touch endpoints, collections, or RBAC: which tier does this serve? -->

- [ ] Tier-1 (internal operator)
- [ ] Tier-2 (external customer)
- [ ] Both / cross-tier (boundary change — flag for extra review)
- [ ] Not applicable (tooling / docs / pure refactor)

## Test plan

<!-- How did you verify this works? Include commands you ran, screenshots for UI changes, before/after metrics for perf work. CI runs `make ci-*` automatically — this section is for things CI cannot check (UX, performance, real-data behavior). -->

-

## Checklist

- [ ] `make ci` passes locally for every surface touched
- [ ] Affected `CLAUDE.md` / `README.md` files updated in this PR
- [ ] New endpoints declare their tenancy tier and enforce org-scoped RBAC
- [ ] New MongoDB collections follow the [module-prefix naming convention](backend/CLAUDE.md)
- [ ] No secrets added to code, logs, env examples, or commit messages
- [ ] If this touches the backend API contract, generated TypeScript clients are regenerated in the same PR
- [ ] Breaking changes are called out below

## Breaking changes

<!-- If none, write "None." If any, describe migration steps. -->

None.

## Related issues

<!-- Closes #N / Refs #N / Related to discussion #N. -->
