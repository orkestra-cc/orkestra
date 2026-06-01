#!/usr/bin/env bash
# scripts/tenantscope-baseline-audit.sh
#
# Summarizes the current state of backend/tools/tenantscope/baseline.txt
# so operators can track how the Phase 5.4-introduced backlog shrinks
# over time. Two views:
#
#   1. Per-package histogram — which modules carry the most drift.
#   2. Method-family breakdown — what operations dominate the baseline
#      (Find reads vs UpdateOne writes vs Aggregate pipelines).
#
# Run from the repo root:
#
#   ./scripts/tenantscope-baseline-audit.sh

set -euo pipefail

BASELINE="backend/tools/tenantscope/baseline.txt"

if [[ ! -f "$BASELINE" ]]; then
    echo "error: $BASELINE not found — run from the repo root." >&2
    exit 1
fi

# Strip comments + blank lines, then count.
total=$(grep -cv '^\s*#\|^\s*$' "$BASELINE" || true)
echo "tenantscope baseline audit"
echo "=========================="
echo "File:   $BASELINE"
echo "Total:  $total entries"
echo

echo "Top 15 packages by entry count:"
echo "-------------------------------"
grep -v '^\s*#\|^\s*$' "$BASELINE" \
    | awk -F: '{
        split($1, a, "/");
        # path is e.g. internal/core/auth/repository/auth_session_repository.go
        # → group by first 3-4 segments to keep module-level granularity.
        pkg = a[1] "/" a[2] "/" a[3];
        if (a[4] != "") pkg = pkg "/" a[4];
        print pkg;
    }' \
    | sort | uniq -c | sort -rn | head -15

echo
echo "Method family distribution:"
echo "---------------------------"
grep -v '^\s*#\|^\s*$' "$BASELINE" \
    | awk -F: '{print $NF}' \
    | sort | uniq -c | sort -rn

echo
echo "Read vs write balance:"
echo "----------------------"
reads=$(grep -v '^\s*#\|^\s*$' "$BASELINE" \
    | awk -F: '{print $NF}' \
    | grep -cE '^(Find|FindOne|CountDocuments|Aggregate|Distinct)$' || true)
writes=$(grep -v '^\s*#\|^\s*$' "$BASELINE" \
    | awk -F: '{print $NF}' \
    | grep -cvE '^(Find|FindOne|CountDocuments|Aggregate|Distinct)$' || true)
echo "  reads:  $reads"
echo "  writes: $writes"
