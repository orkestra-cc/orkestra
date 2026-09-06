#!/usr/bin/env bash
# Fix round 3, Finding 2: backup_secrets's cp -a failure handling (added in
# fix round 2) had no automated regression coverage — only the manual
# verification transcribed into the report. This closes that gap: it stubs
# `cp` on PATH to fail unconditionally, then runs `--components secrets`
# (no docker/live-stack work at all — this is the cheapest test in the
# suite, pure local filesystem faults) and asserts the mandatory
# docker/.env copy failure is a hard error, not a silently-reported
# success.
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# docker/.env is gitignored so it's never present in CI: without this
# explicit opt-in, wiring tests/backup/ into `make ci` would silently run
# nothing (see tests/backup/README.md). This particular test doesn't touch
# any live container (it stubs `cp` and only exercises the local-filesystem
# `secrets` component), but it still needs a real docker/.env on disk, so it
# is gated the same as its live-stack siblings for consistency.
[ "${ORKESTRA_LIVE_STACK_TESTS:-}" = "1" ] || { echo "SKIP: set ORKESTRA_LIVE_STACK_TESTS=1 to run this test"; exit 0; }

if [ ! -f docker/.env ]; then
  echo "SKIP: docker/.env not found — this test needs a live install's env file"
  exit 0
fi

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

mkdir -p "$TMP/bin"
cat > "$TMP/bin/cp" <<'STUB'
#!/usr/bin/env bash
# Fake cp: simulates a copy failure (disk full, permissions, ...) for
# every invocation. backup_secrets is the only backup_* function that
# calls a bare `cp` (mongodb/redis/rustfs all go through `docker cp` or
# `docker exec`, an entirely different binary, so faking this one is safe
# to scope to --components secrets).
echo "FAKE CP: refusing to copy (simulated failure): $*" >&2
exit 1
STUB
chmod +x "$TMP/bin/cp"

out=$(PATH="$TMP/bin:$PATH" ./backup.sh --yes --components secrets --output "$TMP/out.tar.gz" 2>&1)
rc=$?

if [ "$rc" -eq 0 ]; then
  echo "FAIL: exited 0 despite docker/.env failing to copy"
  echo "$out" | tail -10
  exit 1
fi

if ! echo "$out" | grep -q "secrets: failed to copy"; then
  echo "FAIL: exit was non-zero but no 'secrets: failed to copy' message was printed"
  echo "$out" | tail -10
  exit 1
fi

if [ -e "$TMP/out.tar.gz" ]; then
  echo "FAIL: a tarball was written despite the secrets copy failure"
  exit 1
fi

echo "PASS: a failed docker/.env copy is now a hard failure, not a silent skip (rc=$rc)"
