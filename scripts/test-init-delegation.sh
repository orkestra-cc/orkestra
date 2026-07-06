#!/usr/bin/env bash
# `./orkestra.sh init --help` must delegate to scripts/init.sh (whose help text
# contains its own banner). Proves the flag path still reaches init.sh.
set -uo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
out="$("$DIR/../orkestra.sh" init --help 2>&1 || true)"
if printf '%s' "$out" | grep -q "scripts/init.sh — bootstrap"; then
    echo "ok   init --help delegates to init.sh"
else
    echo "FAIL init --help did not delegate"; printf '%s\n' "$out"; exit 1
fi
