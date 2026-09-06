#!/usr/bin/env bash
# A `docker cp` that fails while pulling Redis persistence out of the container
# must fail the redis component — it must not be covered for by a sibling
# artifact that happened to land.
#
# Before the fix, the three `docker cp` calls in backup_redis() were unchecked.
# That is not caught by `set -e` either: the dispatcher calls the function as
# `backup_redis && INCLUDED+=(...) || true`, and that `&&`/`||` context disables
# errexit for the whole body. So a failed RDB copy fell through to the final
# existence check, which passed on the strength of the AOF directory alone, and
# the component was reported captured with today's RDB missing.
#
# Stubbed rather than run against the real stack: reproducing this for real
# means breaking a running Redis, and the interesting case (RDB copy fails,
# AOF copy succeeds) cannot be provoked on demand at all.
set -uo pipefail
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT

FAKE_APP_NAME="orkestra-rediscptest"
FAKE_ENV_NAME="staging"
FAKE_REDIS_CONTAINER="${FAKE_APP_NAME}-redis-${FAKE_ENV_NAME}"

mkdir -p "$TMP/bin"
cat > "$TMP/bin/docker" <<STUB
#!/usr/bin/env bash
# Fake docker: a healthy Redis that answers every probe truthfully — SAVE
# returns OK, the RDB and the AOF directory both exist — and then fails the
# RDB copy, which is the one thing under test. The AOF copy is allowed to
# succeed so that a regression would have something to be covered by.
case "\$1" in
  ps)
    echo "$FAKE_REDIS_CONTAINER"
    ;;
  exec)
    args="\$*"
    case "\$args" in
      *SAVE*)              echo "OK" ;;
      *"CONFIG GET dir"*)             printf 'dir\n/data\n' ;;
      *"CONFIG GET dbfilename"*)      printf 'dbfilename\ndump.rdb\n' ;;
      *"CONFIG GET appenddirname"*)   printf 'appenddirname\nappendonlydir\n' ;;
      *"CONFIG GET appendfilename"*)  printf 'appendfilename\nappendonly.aof\n' ;;
      *"test -f /data/dump.rdb"*)     exit 0 ;;
      *"test -d /data/appendonlydir"*) exit 0 ;;
      *"ls -A /data/appendonlydir"*)  echo "appendonly.aof.1.base.rdb" ;;
      *)
        echo "FAKE DOCKER: unexpected docker exec: \$args" >&2
        exit 9 ;;
    esac
    ;;
  cp)
    case "\$*" in
      *dump.rdb*)
        echo "Error response from daemon: simulated copy failure" >&2
        exit 1 ;;
      *appendonlydir*)
        # Land a plausible AOF directory, so a regression has a sibling
        # artifact to pass the final existence check on.
        dest="\${@: -1}"
        mkdir -p "\$dest" && echo stub > "\$dest/appendonly.aof.1.base.rdb" ;;
      *)
        echo "FAKE DOCKER: unexpected docker cp: \$*" >&2
        exit 9 ;;
    esac
    ;;
  *)
    echo "FAKE DOCKER: unexpected docker subcommand: \$*" >&2
    exit 9 ;;
esac
STUB
chmod +x "$TMP/bin/docker"

# Self-contained: prefer the real docker/.env when present, fall back to the
# tracked example so this test also runs on a fresh checkout and in CI. Every
# docker call is stubbed, so no real credential is ever used.
SRC_ENV="docker/.env"; [ -f "$SRC_ENV" ] || SRC_ENV="docker/.env.example"
FAKE_ENV="$TMP/.env"
sed -e "s/^APP_NAME=.*/APP_NAME=${FAKE_APP_NAME}/" \
    -e "s/^ENV=.*/ENV=${FAKE_ENV_NAME}/" \
    "$SRC_ENV" > "$FAKE_ENV"

out=$(PATH="$TMP/bin:$PATH" ORKESTRA_ENV_FILE="$FAKE_ENV" \
      ./backup.sh --yes --components redis --output "$TMP/out.tar.gz" 2>&1)
rc=$?

if echo "$out" | grep -q "FAKE DOCKER: unexpected"; then
  echo "FAIL: backup.sh took an unexpected path"
  echo "$out" | tail -15
  exit 1
fi

if [ "$rc" -eq 0 ]; then
  echo "FAIL: exited 0 despite the RDB copy failing — the component was reported as captured"
  echo "$out" | tail -15
  exit 1
fi

if ! echo "$out" | grep -q "failed to copy /data/dump.rdb"; then
  echo "FAIL: exited $rc but never said the RDB copy failed"
  echo "$out" | tail -15
  exit 1
fi

if [ -e "$TMP/out.tar.gz" ]; then
  echo "FAIL: wrote a tarball despite the redis capture failing"
  exit 1
fi

echo "PASS: a failed Redis copy fails the component instead of riding on a sibling artifact (rc=$rc)"
