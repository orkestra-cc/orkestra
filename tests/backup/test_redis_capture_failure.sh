#!/usr/bin/env bash
# Fix round 3, Finding 2: backup_redis's SAVE-reply check (added in fix
# round 2) had no automated regression coverage. This one matters most of
# the three: redis-cli exits 0 even on a WRONGPASS/NOAUTH reply (confirmed
# empirically against the live container during round 2 — only the reply
# TEXT signals the failure, not the process exit status), and SAVE (not
# BGSAVE) is what makes checking for the literal string "OK" valid — a
# future maintainer "simplifying" this back to an `if ! redis-cli SAVE;
# then` exit-status check, or swapping in BGSAVE without updating the
# check, would silently reintroduce the exact swallowed-failure shape this
# whole task exists to eliminate, with no signal except this test.
#
# Uses a deliberately fast, cheap path: the real redis container (so this
# is a genuine capture-failure test, not a container-absence one) with a
# wrong REDIS_PASSWORD. The auth check rejects before SAVE ever runs, so
# no snapshot is written and no real data moves — this is not the "full
# blocking SAVE against live data" cost the round-1 test was flagged for.
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# Drives a real (blocking) Redis SAVE against the live stack. docker/.env
# is gitignored so it's never present in CI: without this explicit opt-in,
# wiring tests/backup/ into `make ci` would silently run nothing (see
# tests/backup/README.md).
[ "${ORKESTRA_LIVE_STACK_TESTS:-}" = "1" ] || { echo "SKIP: set ORKESTRA_LIVE_STACK_TESTS=1 to run this against a live stack"; exit 0; }

if [ ! -f docker/.env ]; then
  echo "SKIP: docker/.env not found — this test needs a live stack's env file"
  exit 0
fi

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

# Same APP_NAME/ENV as the real stack (so the redis container still
# resolves and is running — this must reach `SAVE`, not the
# container-absence path), but with a deliberately wrong password so the
# in-band Redis reply is WRONGPASS/NOAUTH rather than "OK".
FAKE_ENV="$TMP/.env"
sed 's/^REDIS_PASSWORD=.*/REDIS_PASSWORD=deliberately-wrong-password-xyz/' docker/.env > "$FAKE_ENV"

if ! grep -q '^REDIS_PASSWORD=deliberately-wrong-password-xyz$' "$FAKE_ENV"; then
  echo "FAIL: could not stage a corrupted REDIS_PASSWORD (no such key in docker/.env?)"
  exit 1
fi

out=$(ORKESTRA_ENV_FILE="$FAKE_ENV" ./backup.sh --yes --components redis --output "$TMP/out.tar.gz" 2>&1)
rc=$?

if [ "$rc" -eq 0 ]; then
  echo "FAIL: exited 0 despite SAVE being rejected on bad credentials"
  echo "$out" | tail -10
  exit 1
fi

# Must NOT have taken the "container not running" skip path — that would
# mean this run never actually reached SAVE.
if echo "$out" | grep -q "container not running"; then
  echo "FAIL: hit the container-absence skip instead of a real SAVE failure"
  echo "      (redis container likely wasn't running — this test requires a live stack)"
  echo "$out" | tail -10
  exit 1
fi

if ! echo "$out" | grep -q "SAVE did not return OK"; then
  echo "FAIL: exit was non-zero but no 'SAVE did not return OK' message was printed"
  echo "      (if this regressed to an exit-status check, a WRONGPASS reply with rc=0 would pass silently)"
  echo "$out" | tail -10
  exit 1
fi

if [ -e "$TMP/out.tar.gz" ]; then
  echo "FAIL: a tarball was written despite the redis SAVE failure"
  exit 1
fi

echo "PASS: a rejected SAVE (rc=0, non-OK reply) is now a hard failure (rc=$rc)"
