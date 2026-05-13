#!/usr/bin/env bash
# Refresh a coverage badge SVG under .github/badges/ from the project's
# raw coverage artifact. Called by GitHub Actions on push to dev/main.
#
# Usage:
#   scripts/refresh-coverage-badge.sh <project>
#
# Where <project> is one of:
#   backend         — reads backend/coverage.out via `go tool cover`
#   frontend-admin  — reads frontend-admin/coverage/coverage-summary.json via jq
#   mobile          — reads mobile/coverage/lcov.info via awk
#
# Output: .github/badges/<project>-coverage.svg (fetched from shields.io).
# Exits non-zero if the coverage artifact is missing or unparseable.

set -euo pipefail

project="${1:?usage: $0 <backend|frontend-admin|mobile>}"
repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

case "$project" in
  backend)
    file="backend/coverage.out"
    [ -f "$file" ] || { echo "::error::missing $file"; exit 1; }
    # `go tool cover -func` resolves package paths, so it must run inside the Go module.
    pct=$(cd backend && go tool cover -func=coverage.out | awk '/^total:/ {gsub("%",""); print $NF}')
    logo="go"
    label="backend"
    ;;
  frontend-admin)
    file="frontend-admin/coverage/coverage-summary.json"
    [ -f "$file" ] || { echo "::error::missing $file"; exit 1; }
    pct=$(jq -r '.total.lines.pct' "$file")
    logo="react"
    label="frontend-admin"
    ;;
  mobile)
    file="mobile/coverage/lcov.info"
    [ -f "$file" ] || { echo "::error::missing $file"; exit 1; }
    pct=$(awk -F: 'BEGIN{lh=0;lf=0} /^LH:/{lh+=$2} /^LF:/{lf+=$2} END{ if(lf>0) printf "%.1f", lh/lf*100; else print "0.0"}' "$file")
    logo="flutter"
    label="mobile"
    ;;
  *)
    echo "::error::unknown project: $project (expected: backend|frontend-admin|mobile)"
    exit 1
    ;;
esac

int=$(printf '%.0f' "$pct")
if   [ "$int" -ge 80 ]; then color=brightgreen
elif [ "$int" -ge 70 ]; then color=green
elif [ "$int" -ge 60 ]; then color=yellowgreen
elif [ "$int" -ge 50 ]; then color=yellow
elif [ "$int" -ge 40 ]; then color=orange
else                         color=red
fi

mkdir -p .github/badges
out=".github/badges/${project}-coverage.svg"
curl -fsSL "https://img.shields.io/badge/coverage-${pct}%25-${color}?style=flat-square&logo=${logo}&logoColor=white&label=${label}" -o "$out"
echo "Refreshed $out (${pct}%, ${color})"
