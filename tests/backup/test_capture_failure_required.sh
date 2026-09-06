#!/usr/bin/env bash
# Fix round 1, Critical finding: --require's pre-execution check only proves
# a container was reachable at selection time — it can't see a capture that
# starts and then fails (bad credentials, disk full, ...). This asserts the
# *post-execution* check: with the real stack running (so mongodb passes
# the pre-execution --require gate) but a wrong MONGO_ROOT_PASSWORD (so
# mongodump itself fails), `backup.sh --yes all` must still exit non-zero,
# and the error must be distinguishable from the "container down" case
# covered by test_all_requires_components.sh.
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# Drives a real mongodump against the live stack. docker/.env is gitignored
# so it's never present in CI: without this explicit opt-in, wiring
# tests/backup/ into `make ci` would silently run nothing (see
# tests/backup/README.md).
[ "${ORKESTRA_LIVE_STACK_TESTS:-}" = "1" ] || { echo "SKIP: set ORKESTRA_LIVE_STACK_TESTS=1 to run this against a live stack"; exit 0; }

if [ ! -f docker/.env ]; then
  echo "SKIP: docker/.env not found — this test needs a live stack's env file"
  exit 0
fi

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

# Same APP_NAME/ENV as the real stack (so mongodb/redis/rustfs containers
# still resolve and are running — this must reach mongodump, not the
# container-absence path already covered by the sibling test), but with a
# deliberately wrong Mongo password so the capture itself fails.
FAKE_ENV="$TMP/.env"
sed 's/^MONGO_ROOT_PASSWORD=.*/MONGO_ROOT_PASSWORD=deliberately-wrong-password-xyz/' docker/.env > "$FAKE_ENV"

if ! grep -q '^MONGO_ROOT_PASSWORD=deliberately-wrong-password-xyz$' "$FAKE_ENV"; then
  echo "FAIL: could not stage a corrupted MONGO_ROOT_PASSWORD (no such key in docker/.env?)"
  exit 1
fi

out=$(ORKESTRA_ENV_FILE="$FAKE_ENV" ./backup.sh --yes all --output "$TMP/out.tar.gz" 2>&1)
rc=$?

if [ "$rc" -eq 0 ]; then
  echo "FAIL: exited 0 despite mongodump failing on bad credentials"
  echo "$out" | tail -10
  exit 1
fi

# Must NOT have taken the pre-execution "container unavailable" shortcut —
# that would mean this run never actually reached mongodump, and the test
# isn't exercising the code path it claims to.
if echo "$out" | grep -q "required component(s) unavailable"; then
  echo "FAIL: hit the pre-execution 'unavailable' check instead of a real capture failure"
  echo "      (mongodb container likely wasn't running — this test requires a live stack)"
  echo "$out" | tail -10
  exit 1
fi

if ! echo "$out" | grep -q "required component(s) failed to capture"; then
  echo "FAIL: exit was non-zero but no post-execution 'failed to capture' message was printed"
  echo "$out" | tail -10
  exit 1
fi

if [ -e "$TMP/out.tar.gz" ]; then
  echo "FAIL: a tarball was written despite the required capture failure"
  exit 1
fi

echo "PASS: mongodump capture failure with container up is now a hard failure (rc=$rc)"
