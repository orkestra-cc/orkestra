#!/bin/sh
# Installs a dev frontend's dependencies in its node:alpine container, from the
# app directory, before `npm run dev`. docker-compose.dev.yml mounts this file
# read-only at /opt/orkestra/frontend-deps.sh and binds <app>/.docker/node_modules
# over /app/node_modules, so these musl packages never replace the host's.
#
# `npm ci`, never a plain `npm install`, which would rewrite a lockfile that is
# behind package.json, and the change could end up in a commit. It runs only
# when package.json or the lockfile changed since the last install (or the
# install is incomplete), so a restart skips it. If ci refuses because the two
# are out of sync, the dev server still starts on an install that neither
# reads nor writes the lockfile, and says so; that install drops the stamp, so
# the next start tries ci again. Any other ci failure exits.
stamp=$(cat package.json package-lock.json | sha256sum | cut -d' ' -f1)
if [ -x node_modules/.bin/vite ] &&
   [ "$(cat node_modules/.orkestra-install 2>/dev/null)" = "$stamp" ]; then
  exit 0
fi
err=$(mktemp) || exit 1
trap 'rm -f "$err"' EXIT
if npm ci 2> "$err"; then
  cat "$err" >&2
  echo "$stamp" > node_modules/.orkestra-install
  exit 0
fi
cat "$err" >&2
grep -q 'are in sync' "$err" || exit 1
echo "orkestra dev: package.json and package-lock.json are out of sync; installing without the lockfile, which stays untouched. Run npm install on the host and commit package-lock.json." >&2
rm -f node_modules/.orkestra-install
npm install --no-package-lock
