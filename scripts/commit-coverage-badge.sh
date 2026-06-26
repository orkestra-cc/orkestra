#!/usr/bin/env bash
# Commit + push a freshly-refreshed coverage badge to the current branch,
# resilient to the badge-push race between sibling CI workflows.
#
# The backend and frontend-admin workflows each have a `coverage-badge` job that
# pushes its own badge SVG to dev/main. A push that touches BOTH trees runs both
# workflows; their badge jobs race to push to the same branch and one is rejected
# `! [rejected] (non-fast-forward)`, failing an otherwise-green run. Per-workflow
# `concurrency` groups do not serialise across the two workflows.
#
# Strategy: keep the generated badge aside, then on each attempt reset hard to the
# latest remote tip, re-apply the badge, commit and push. Because every attempt
# rebuilds on top of the current remote, a concurrent push can never produce a
# non-fast-forward — a transient race just costs one retry. The two badge files
# are disjoint, so this never loses the sibling workflow's badge.
#
# Usage:
#   scripts/commit-coverage-badge.sh <project>
#
# Where <project> is one of: backend | frontend-admin | mobile.
# Must run inside a GitHub Actions checkout (reads GITHUB_REF_NAME).

set -euo pipefail

project="${1:?usage: $0 <backend|frontend-admin|mobile>}"
ref="${GITHUB_REF_NAME:?GITHUB_REF_NAME is required (run under GitHub Actions)}"
repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"
badge=".github/badges/${project}-coverage.svg"

if [[ ! -f "$badge" ]]; then
  echo "Badge $badge not found — did 'Refresh badge SVG' run first?" >&2
  exit 1
fi

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"

# Preserve the freshly-generated badge across the hard resets below.
tmp_badge="$(mktemp)"
cp "$badge" "$tmp_badge"

for attempt in 1 2 3 4 5; do
  git fetch --quiet origin "$ref"
  git reset --quiet --hard "origin/${ref}"
  cp "$tmp_badge" "$badge"

  if git diff --quiet -- "$badge"; then
    echo "Coverage badge already current on origin/${ref} — nothing to push."
    exit 0
  fi

  git add "$badge"
  git commit --quiet -m "chore(badges): refresh ${project} coverage [skip ci]"

  if git push origin "HEAD:${ref}"; then
    echo "Pushed refreshed ${project} coverage badge (attempt ${attempt})."
    exit 0
  fi

  echo "Push rejected on attempt ${attempt} (concurrent badge push) — retrying."
  sleep $(( (RANDOM % 4) + 1 ))
done

echo "Failed to push ${project} coverage badge after 5 attempts." >&2
exit 1
