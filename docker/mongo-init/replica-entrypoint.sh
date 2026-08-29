#!/usr/bin/env bash
set -euo pipefail

key_file=/data/configdb/orkestra-replica.key

if [[ ! -s "$key_file" ]]; then
  : "${MONGO_INITDB_ROOT_PASSWORD:?MONGO_INITDB_ROOT_PASSWORD is required}"
  tmp_key="$(mktemp)"
  trap 'rm -f "$tmp_key"' EXIT
  printf '%s' "$MONGO_INITDB_ROOT_PASSWORD" | sha512sum | awk '{print $1}' >"$tmp_key"
  install -o mongodb -g mongodb -m 0400 "$tmp_key" "$key_file"
fi

exec /usr/local/bin/docker-entrypoint.sh mongod \
  --replSet rs0 \
  --keyFile "$key_file" \
  --bind_ip_all
