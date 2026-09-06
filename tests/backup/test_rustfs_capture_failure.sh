#!/usr/bin/env bash
# Fix round 2: backup_rustfs previously fell through to `ok` regardless of
# whether `aws s3 sync` actually succeeded, so a sync failure (bad
# credentials, unreachable endpoint, ...) was reported as a captured
# component. This asserts the fix — a rejected sync now fails the
# component — using a deliberately fast path: `--components rustfs` alone
# (no mongodb dump / redis SAVE needed) against real, bad credentials, which
# aws-cli rejects before transferring any real object data. This is
# intentionally cheaper than test_capture_failure_required.sh, which must
# run the full `all` set — see that test's comment about not adding to the
# live-stack cost of these checks.
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# Drives a real aws-cli sync against the live rustfs container. docker/.env
# is gitignored so it's never present in CI: without this explicit opt-in,
# wiring tests/backup/ into `make ci` would silently run nothing (see
# tests/backup/README.md).
[ "${ORKESTRA_LIVE_STACK_TESTS:-}" = "1" ] || { echo "SKIP: set ORKESTRA_LIVE_STACK_TESTS=1 to run this against a live stack"; exit 0; }

if [ ! -f docker/.env ]; then
  echo "SKIP: docker/.env not found — this test needs a live stack's env file"
  exit 0
fi

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

# Same APP_NAME/ENV as the real stack (so the rustfs container still
# resolves and is running — this must reach `aws s3 sync`, not the
# container-absence path), but with a deliberately wrong secret key so the
# sync itself is rejected. Rejection happens on the signature check, before
# any object bytes move, which is what keeps this fast.
FAKE_ENV="$TMP/.env"
sed 's/^STORAGE_SECRET_KEY=.*/STORAGE_SECRET_KEY=deliberately-wrong-secret-xyz/' docker/.env > "$FAKE_ENV"

if ! grep -q '^STORAGE_SECRET_KEY=deliberately-wrong-secret-xyz$' "$FAKE_ENV"; then
  echo "FAIL: could not stage a corrupted STORAGE_SECRET_KEY (no such key in docker/.env?)"
  exit 1
fi

out=$(ORKESTRA_ENV_FILE="$FAKE_ENV" ./backup.sh --yes --components rustfs --output "$TMP/out.tar.gz" 2>&1)
rc=$?

if [ "$rc" -eq 0 ]; then
  echo "FAIL: exited 0 despite the rustfs sync being rejected on bad credentials"
  echo "$out" | tail -10
  exit 1
fi

# Must NOT have taken the "container not running" skip path — that would
# mean this run never actually reached `aws s3 sync`.
if echo "$out" | grep -q "container not running"; then
  echo "FAIL: hit the container-absence skip instead of a real sync failure"
  echo "      (rustfs container likely wasn't running — this test requires a live stack)"
  echo "$out" | tail -10
  exit 1
fi

if ! echo "$out" | grep -q "failed to sync bucket"; then
  echo "FAIL: exit was non-zero but no per-bucket sync-failure message was printed"
  echo "$out" | tail -10
  exit 1
fi

if [ -e "$TMP/out.tar.gz" ]; then
  echo "FAIL: a tarball was written despite the rustfs sync failure"
  exit 1
fi

echo "PASS: rustfs sync failure with container up is now a hard failure (rc=$rc)"
