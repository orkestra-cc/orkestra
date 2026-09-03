#!/usr/bin/env bash
#
# scripts/env-file.sh — pure read/write helpers for KEY=value env files.
#
# Sourced by orkestra.sh and by scripts/test-env-file.sh. Defines functions
# only — no side effects on source. Every function takes the target file as its
# first argument so it is testable against a throwaway temp file.
#
#   env_get <file> <key>          -> echoes current value ("" if absent)
#   env_set <file> <key> <value>  -> upsert (uncomment / replace / append)
#   secret_is_placeholder <value> -> 0 when VALUE is empty or a shipped literal
#   secret_is_weak <value> [min]  -> 0 when placeholder OR shorter than MIN (16)
#
# env_set is awk-based (values routinely contain '/' and ':' — e.g. URLs — so a
# sed delimiter would be fragile), writes atomically (temp + mv), and resets the
# file to mode 600 (every env file we manage holds secrets).

# env_get FILE KEY — active assignment wins; falls back to a commented stub so
# reconfigure can pre-fill from a still-commented optional seed.
env_get() {
    local file=$1 key=$2 line=""
    [ -f "$file" ] || return 0
    line=$(grep -E "^${key}=" "$file" 2>/dev/null | head -1 || true)
    [ -n "$line" ] || line=$(grep -E "^#[[:space:]]*${key}=" "$file" 2>/dev/null | head -1 || true)
    [ -n "$line" ] || return 0
    line=${line#*"${key}="}     # strip through the first KEY=
    # Strip one matched surrounding quote pair (double or single) only.
    if [ "${line#\"}" != "$line" ] && [ "${line%\"}" != "$line" ]; then
        line=${line#\"}; line=${line%\"}
    elif [ "${line#\'}" != "$line" ] && [ "${line%\'}" != "$line" ]; then
        line=${line#\'}; line=${line%\'}
    fi
    printf '%s' "$line"
}

# env_set FILE KEY VALUE — replace the first active-or-commented KEY= line in
# place (uncommenting a stub), else append. Value is passed through ENVIRON so
# awk does not reinterpret backslashes/escapes.
env_set() {
    local file=$1 key=$2 value=$3 tmp
    [ -f "$file" ] || : > "$file"
    tmp="${file}.tmp.$$"
    val="$value" awk -v key="$key" '
        BEGIN { done = 0 }
        {
            if (!done && match($0, "^#?[ \t]*" key "=")) {
                print key "=" ENVIRON["val"]; done = 1; next
            }
            print
        }
        END { if (!done) print key "=" ENVIRON["val"] }
    ' "$file" > "$tmp"
    chmod 600 "$tmp"
    mv "$tmp" "$file"
}

# --- Secret hygiene ----------------------------------------------------------
#
# Shared by scripts/env-validate.sh (refuses these in staging/production and
# warns in development), orkestra.sh's setup wizard (never keeps a placeholder
# on Enter, generates a real value instead) and the tests. The list is every
# literal that docker/.env.example, the compose files or the bundled images
# have ever shipped or defaulted to — the bundled RustFS root was exactly one
# of them, on a browser-facing S3 API. Case-insensitive. Keep it in step with
# weakSecretReason in backend/internal/shared/config/config.go.

# secret_is_placeholder VALUE — 0 when VALUE is empty or a known literal.
secret_is_placeholder() {
    local v
    v=$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')
    case "$v" in
        ''|changeme*|replace_with_*|generate*|your_*|placeholder*|example*|dev-*|dev_*) return 0 ;;
        rustfsadmin|minioadmin|password|secret|admin) return 0 ;;
    esac
    return 1
}

# secret_is_weak VALUE [MIN] — 0 when VALUE is a placeholder or shorter than
# MIN characters (default 16: `openssl rand -hex 16` yields 32).
secret_is_weak() {
    local v=${1:-} min=${2:-16}
    secret_is_placeholder "$v" && return 0
    [ "${#v}" -lt "$min" ]
}
