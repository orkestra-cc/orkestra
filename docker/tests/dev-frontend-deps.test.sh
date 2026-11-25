#!/usr/bin/env bash
# The dev stack's frontends install their Alpine (musl) dependencies with
# docker/frontend-deps.sh into <app>/.docker/node_modules, never into the
# host's node_modules, and never with a plain `npm install`, which rewrites a
# lockfile that is behind package.json.
set -euo pipefail
root=$(cd "$(dirname "$0")/../.." && pwd -P)
fixture=$(mktemp -d)
trap 'rm -r "$fixture"' EXIT
cat > "$fixture/dev.env" <<'EOF'
APP_NAME=orkestra
ENV=development
MONGO_ROOT_PASSWORD=x
REDIS_PASSWORD=x
STORAGE_ACCESS_KEY=x
STORAGE_SECRET_KEY=x
PDF_RENDERER_PASSWORD=x
EOF
docker compose --env-file "$fixture/dev.env" -f "$root/docker/docker-compose.dev.yml" config --format json |
  ROOT="$root" python3 -c '
import json,os,sys
root=os.environ["ROOT"]
c=json.load(sys.stdin)["services"]
for svc,app in (("frontend-admin","frontend-admin"),("client-frontend","frontend-client")):
    s=c[svc]
    mounts={v["target"]:v for v in s["volumes"]}
    assert mounts["/app"]["source"]==f"{root}/{app}", (svc,mounts["/app"])
    assert mounts["/app/node_modules"]["source"]==f"{root}/{app}/.docker/node_modules", (svc,mounts["/app/node_modules"])
    script=mounts["/opt/orkestra/frontend-deps.sh"]
    assert script["source"]==f"{root}/docker/frontend-deps.sh" and script["read_only"], (svc,script)
    command=s["command"][2]
    assert "sh /opt/orkestra/frontend-deps.sh && npm run dev -- --host 0.0.0.0" in command, (svc,command)
    assert "npm install" not in command and "npm ci" not in command, (svc,command)
print("dev frontend Compose configuration passed")'

# The install itself: `npm ci`, only when package.json or the lockfile changed
# since the last install or the install is incomplete, so a restart skips it.
# ci refusing an out-of-sync lockfile falls back to an install that neither
# reads nor writes the lockfile, and drops the stamp; any other failure exits.
app=$fixture/app
mkdir -p "$app/node_modules" "$fixture/fakebin" "$fixture/tmp"
printf '{"name":"x"}\n' > "$app/package.json"; printf '{"lockfileVersion":3}\n' > "$app/package-lock.json"
cat > "$fixture/fakebin/npm" <<'SH'
#!/bin/sh
echo "$*" >> "$NPM_LOG"
if [ "$1" = ci ]; then
  echo 'npm warn deprecated example@1.0.0' >&2
  [ -e "$NPM_FAIL.sync" ] && { echo 'npm error `npm ci` can only install packages when your package.json and package-lock.json or npm-shrinkwrap.json are in sync.' >&2; exit 1; }
  [ -e "$NPM_FAIL.network" ] && { echo 'npm error network ECONNRESET' >&2; exit 1; }
fi
[ "$1" = install ] && [ -e "$NPM_FAIL.install" ] && exit 1
if [ "$1" = ci ] || [ "$1" = install ]; then mkdir -p node_modules/.bin && : > node_modules/.bin/vite && chmod +x node_modules/.bin/vite; fi
exit 0
SH
chmod +x "$fixture/fakebin/npm"
deps() { : > "$fixture/npm.log"; (cd "$app" && PATH="$fixture/fakebin:$PATH" TMPDIR="$fixture/tmp" NPM_LOG="$fixture/npm.log" NPM_FAIL="$fixture/npm-fail" sh "$root/docker/frontend-deps.sh"); }
deps 2> "$fixture/deps.err"
test "$(cat "$fixture/npm.log")" = ci
test -s "$app/node_modules/.orkestra-install"
grep -Fq 'npm warn deprecated' "$fixture/deps.err"
stamp=$(cat "$app/node_modules/.orkestra-install")
deps
test ! -s "$fixture/npm.log"
# A stamp over an incomplete install (no vite) does not skip ci.
rm "$app/node_modules/.bin/vite"
deps
test "$(cat "$fixture/npm.log")" = ci
# A network (or any other) ci failure exits: no fallback.
rm "$app/node_modules/.orkestra-install"; touch "$fixture/npm-fail.network"
if deps 2> "$fixture/deps.err"; then echo "frontend-deps succeeded after a failed npm ci" >&2; exit 1; fi
test "$(cat "$fixture/npm.log")" = ci
grep -Fq 'ECONNRESET' "$fixture/deps.err"
rm "$fixture/npm-fail.network"
deps
# Out of sync: the fallback, which drops the stamp, succeeds and says so.
printf '{"name":"x","dependencies":{"left-pad":"1.3.0"}}\n' > "$app/package.json"
touch "$fixture/npm-fail.sync"
deps 2> "$fixture/deps.err"
test "$(cat "$fixture/npm.log")" = "$(printf '%s\n' ci 'install --no-package-lock')"
grep -Fq 'orkestra dev: package.json and package-lock.json are out of sync' "$fixture/deps.err"
test ! -e "$app/node_modules/.orkestra-install"
# Both installs failing: exits.
touch "$fixture/npm-fail.install"
if deps 2>/dev/null; then echo "frontend-deps succeeded after both installs failed" >&2; exit 1; fi
test "$(cat "$fixture/npm.log")" = "$(printf '%s\n' ci 'install --no-package-lock')"
rm "$fixture/npm-fail.sync" "$fixture/npm-fail.install"
# Back in sync with the old stamp's files: the fallback's install is not
# trusted, ci runs again.
printf '{"name":"x"}\n' > "$app/package.json"
deps
test "$(cat "$app/node_modules/.orkestra-install")" = "$stamp"
test "$(cat "$fixture/npm.log")" = ci
test -z "$(ls -A "$fixture/tmp")"
echo "dev frontend install passed"
