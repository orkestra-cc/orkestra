#!/usr/bin/env bash
# Asserts that `backup.sh --yes all` exits NON-ZERO when a required data
# component is undetectable, instead of silently producing a secrets-only
# tarball (the 2026-07-12 failure).
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# Drives backup.sh against docker/.env and `docker ps` — needs a live
# install, not just a live stack (see tests/backup/README.md). docker/.env
# is gitignored, so this is never present in CI: without this explicit
# opt-in, wiring tests/backup/ into `make ci` would silently run nothing.
[ "${ORKESTRA_LIVE_STACK_TESTS:-}" = "1" ] || { echo "SKIP: set ORKESTRA_LIVE_STACK_TESTS=1 to run this against a live stack"; exit 0; }

if [ ! -f docker/.env ]; then
  echo "SKIP: docker/.env not found — this test needs a live install's env file"
  exit 0
fi

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

# Point the script at a stack name that cannot match any running container.
FAKE_ENV="$TMP/.env"
sed 's/^APP_NAME=.*/APP_NAME=orkestra-doesnotexist/' docker/.env > "$FAKE_ENV"

out=$(ORKESTRA_ENV_FILE="$FAKE_ENV" ./backup.sh --yes all --output "$TMP/out.tar.gz" 2>&1)
rc=$?

if [ "$rc" -eq 0 ]; then
  echo "FAIL: exited 0 despite mongodb/redis/rustfs being undetectable"
  echo "$out" | tail -5
  exit 1
fi
if ! echo "$out" | grep -q "required component"; then
  echo "FAIL: exit was non-zero but no 'required component' message was printed"
  exit 1
fi
echo "PASS: silent degradation is now a hard failure (rc=$rc)"
