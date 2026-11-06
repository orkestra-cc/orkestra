#!/usr/bin/env bash
# Serves a fake build with the REAL nginx config of the console
# (docker/frontend-admin/nginx.conf, mounted by the prod compose), then checks
# the response headers.
#
# Why live and not grep: nginx drops every inherited add_header as soon as a
# location declares one of its own, and which location answers a request
# (index directive, try_files fallback, named locations, ^~ vs regex) is only
# certain once nginx has resolved it.
#
# The cache contract it pins:
#   - every response that is index.html: Cache-Control: no-cache. Without it
#     browsers kept the previous build's index.html after a deploy, whose
#     lazy chunks the new image no longer had.
#   - hashed /assets/*: one year, immutable; a missing one is a plain 404.
#   - unhashed static files (TinyMCE, favicons): revalidated, never immutable.
#   - /config.js: no-store.
# plus the security headers each location must re-state — the whole set on
# documents, since nothing in front of nginx adds any behind a tunnel.
#
# The fallback configs baked into frontend-admin/Dockerfile and
# frontend-client/Dockerfile are checked statically at the end.
set -euo pipefail
cd "$(dirname "$0")/../.."

IMAGE=${NGINX_TEST_IMAGE:-nginx:alpine}
fail=0
containers=()
work=$(mktemp -d)
cleanup() {
  for c in "${containers[@]}"; do docker rm -f "$c" >/dev/null 2>&1 || true; done
  rm -rf "$work"
}
trap cleanup EXIT

# A docroot shaped like a Vite build. World-readable: the nginx workers run as
# the `nginx` user, and mktemp creates 0700 directories.
root="$work/html"
mkdir -p "$root/assets" "$root/tinymce"
echo '<!doctype html><title>spa</title>' >"$root/index.html"
echo 'window.__ORKESTRA_CONFIG__ = {};' >"$root/config.js"
echo 'export {};' >"$root/assets/UserDetail-17cCus6X.js"
echo 'body{}' >"$root/assets/index-Bq3x9Z1a.css"
echo '/* tinymce */' >"$root/tinymce/tinymce.min.js"
printf '\0' >"$root/favicon.ico"
chmod -R a+rX "$work"

docker image inspect "$IMAGE" >/dev/null 2>&1 || docker pull -q "$IMAGE" >/dev/null

# start NAME CONFIG_MOUNT [docker run args...] — prints the published host port.
start() {
  local name=$1 mount=$2; shift 2
  local cid
  cid=$(docker run -d --rm -p 127.0.0.1::80 -v "$root:/usr/share/nginx/html:ro" -v "$mount" "$@" "$IMAGE")
  containers+=("$cid")
  local port
  port=$(docker port "$cid" 80/tcp | head -n1 | sed 's/.*://')
  for _ in $(seq 1 50); do
    curl -fsS -o /dev/null "http://127.0.0.1:$port/config.js" 2>/dev/null && { echo "$port"; return; }
    sleep 0.2
  done
  echo "FAIL: $name nginx did not come up" >&2
  docker logs "$cid" >&2 || true
  exit 1
}

# headers PORT PATH — status line + headers, without CR. Matching below is
# case-insensitive (header names are).
headers() { curl -sS -D - -o /dev/null "http://127.0.0.1:$1$2" | tr -d '\r'; }

# expect LABEL PORT PATH [has:HEADER-LINE | lacks:HEADER-PREFIX | status:CODE]...
expect() {
  local label=$1 port=$2 path=$3; shift 3
  local h
  h=$(headers "$port" "$path")
  for rule in "$@"; do
    case "$rule" in
      status:*) head -n1 <<<"$h" | grep -q " ${rule#status:}" ||
        { echo "FAIL: $label $path: status $(head -n1 <<<"$h"), want ${rule#status:}"; fail=1; } ;;
      has:*) grep -qixF -- "${rule#has:}" <<<"$h" ||
        { echo "FAIL: $label $path: missing header '${rule#has:}'"; fail=1; } ;;
      lacks:*) ! grep -qi -- "^${rule#lacks:}" <<<"$h" ||
        { echo "FAIL: $label $path: unexpected '$(grep -i -- "^${rule#lacks:}" <<<"$h" | head -n1)'"; fail=1; } ;;
    esac
  done
}

# Behind the Cloudflare Tunnel no reverse proxy adds headers: every document
# carries the whole set from nginx itself (docs/runbooks/cloudflare-tunnel.md).
origin_headers=(
  'has:referrer-policy: strict-origin-when-cross-origin'
  'has:permissions-policy: camera=(), microphone=(), geolocation=()'
  'has:strict-transport-security: max-age=31536000'
)
doc_headers=(
  'has:cache-control: no-cache'
  'has:x-frame-options: SAMEORIGIN'
  'has:x-content-type-options: nosniff'
  'has:x-xss-protection: 0'
  "${origin_headers[@]}"
)
immutable=(
  'status:200'
  'has:cache-control: public, max-age=31536000, immutable'
  'has:x-content-type-options: nosniff'
)

# ---- frontend-admin (the console) -----------------------------------------
admin=$(start admin "$PWD/docker/frontend-admin/nginx.conf:/etc/nginx/conf.d/default.conf:ro")
for p in / /index.html /admin/users /admin/users/abc; do
  expect admin "$admin" "$p" status:200 "${doc_headers[@]}"
done
expect admin "$admin" /assets/UserDetail-17cCus6X.js "${immutable[@]}"
expect admin "$admin" /assets/index-Bq3x9Z1a.css "${immutable[@]}"
# A chunk of a previous build: 404, and not cached for a year.
expect admin "$admin" /assets/UserDetail-Cb8watoq.js status:404 lacks:cache-control
for p in /tinymce/tinymce.min.js /favicon.ico; do
  expect admin "$admin" "$p" status:200 'has:cache-control: no-cache' 'has:x-content-type-options: nosniff'
done
expect admin "$admin" /tinymce/missing.min.js status:404
expect admin "$admin" /config.js status:200 'has:cache-control: no-store' 'has:x-content-type-options: nosniff'

# ---- fallback configs baked into the images (static) ----------------------
for f in frontend-admin/Dockerfile frontend-client/Dockerfile; do
  grep -qF 'location ^~ /assets/ {' "$f" || { echo "FAIL: $f: no /assets/ location"; fail=1; }
  grep -qF 'add_header Cache-Control "public, max-age=31536000, immutable";' "$f" ||
    { echo "FAIL: $f: /assets/ not immutable"; fail=1; }
  grep -qF 'add_header Cache-Control "no-cache" always;' "$f" ||
    { echo "FAIL: $f: SPA fallback (index.html) not no-cache"; fail=1; }
done

[ "$fail" = 0 ] && echo "frontend nginx cache headers: OK"
exit "$fail"
