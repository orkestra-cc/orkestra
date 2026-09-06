#!/usr/bin/env bash
# Fix round 3, Finding 1: a genuinely bucket-less install (per-domain
# buckets are provisioned lazily on first use — see
# backend/internal/shared/config/config.go:346 and docker/.env.example:
# 298-299) must be a legitimate empty capture, not a hard failure. Before
# this fix, `list-buckets` succeeding with zero buckets fell through to a
# sync attempt against the configured default bucket, which doesn't exist
# yet, got NoSuchBucket, and (after fix round 2) failed the whole rustfs
# component and dropped the entire `--yes all` run to exit 3 — losing an
# otherwise-good mongodb/redis/secrets backup because nothing had ever
# uploaded an avatar.
#
# This can't be reproduced against the real staging stack: it already has
# three real buckets, and emptying them to test this would be destructive
# to live data. Instead this stubs `docker` on PATH so backup.sh's rustfs
# path runs against a simulated bucket-less endpoint — deterministic, fast,
# and it touches no live infrastructure at all. Any docker invocation this
# scenario doesn't expect (a sync call, in particular — the whole point is
# that one must NOT be attempted) fails the stub loudly rather than being
# silently permissive, so the test can't degenerate into a false pass.
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

FAKE_APP_NAME="orkestra-emptybuckettest"
FAKE_ENV_NAME="staging"
FAKE_RUSTFS_CONTAINER="${FAKE_APP_NAME}-rustfs-${FAKE_ENV_NAME}"

mkdir -p "$TMP/bin"
cat > "$TMP/bin/docker" <<STUB
#!/usr/bin/env bash
# Fake docker: intercepts exactly the calls a bucket-less rustfs capture
# should make (container check + a list-buckets call returning nothing),
# and fails loudly on anything else so this test can't silently take an
# unintended path.
case "\$1" in
  ps)
    echo "$FAKE_RUSTFS_CONTAINER"
    ;;
  run)
    args="\$*"
    case "\$args" in
      *list-buckets*)
        # Simulate: endpoint reachable, credentials valid, zero buckets
        # provisioned yet.
        exit 0
        ;;
      *"s3 sync"*)
        echo "FAKE DOCKER: unexpected s3 sync call — a bucket-less install must not attempt a sync" >&2
        exit 9
        ;;
      *)
        echo "FAKE DOCKER: unexpected docker run: \$args" >&2
        exit 9
        ;;
    esac
    ;;
  *)
    echo "FAKE DOCKER: unexpected docker subcommand: \$*" >&2
    exit 9
    ;;
esac
STUB
chmod +x "$TMP/bin/docker"

# Self-contained: prefer the real docker/.env when present, fall back to the
# tracked example so this test also runs on a fresh checkout and in CI. Every
# docker call is stubbed, so no real credential is ever used — depending on a
# gitignored file was an accident of where this test was written, not a need.
SRC_ENV="docker/.env"; [ -f "$SRC_ENV" ] || SRC_ENV="docker/.env.example"
FAKE_ENV="$TMP/.env"
sed -e "s/^APP_NAME=.*/APP_NAME=${FAKE_APP_NAME}/" \
    -e "s/^ENV=.*/ENV=${FAKE_ENV_NAME}/" \
    "$SRC_ENV" > "$FAKE_ENV"

out=$(PATH="$TMP/bin:$PATH" ORKESTRA_ENV_FILE="$FAKE_ENV" ./backup.sh --yes --components rustfs --output "$TMP/out.tar.gz" 2>&1)
rc=$?

if echo "$out" | grep -q "FAKE DOCKER: unexpected"; then
  echo "FAIL: backup.sh took an unexpected path for a bucket-less install"
  echo "$out" | tail -15
  exit 1
fi

if [ "$rc" -ne 0 ]; then
  echo "FAIL: exited $rc for a genuinely bucket-less install (should be a legitimate empty success)"
  echo "$out" | tail -15
  exit 1
fi

if ! echo "$out" | grep -q "no buckets exist yet on this install"; then
  echo "FAIL: exited 0 but didn't print the expected empty-install message"
  echo "$out" | tail -15
  exit 1
fi

if [ ! -e "$TMP/out.tar.gz" ]; then
  echo "FAIL: no tarball was written despite a successful (empty) capture"
  exit 1
fi

shred -u "$TMP/out.tar.gz"

echo "PASS: a bucket-less install is a legitimate empty capture, not a hard failure (rc=$rc)"
