#!/usr/bin/env bash
# Plain-bash tests for scripts/env-validate.sh's secret-hygiene rules (no
# framework). The validator derives its ENV_FILE from its own location, so a
# copy in a scratch PROJECT_ROOT is run against a scratch docker/.env — the
# developer's real one is never read (same trick as test-orkestra-helpers.sh).
set -uo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$DIR")"
# shellcheck source=/dev/null
source "$DIR/env-file.sh"

pass=0; fail=0
check() {  # check <desc> <expected> <actual>
    if [ "$2" = "$3" ]; then
        pass=$((pass + 1)); printf '  ok   %s\n' "$1"
    else
        fail=$((fail + 1)); printf '  FAIL %s\n       expected: [%s]\n       actual:   [%s]\n' "$1" "$2" "$3"
    fi
}

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/scripts" "$tmp/docker"
cp "$DIR/env-validate.sh" "$DIR/env-file.sh" "$tmp/scripts/"
env_file="$tmp/docker/.env"
out="$tmp/out"

# run KEY=VALUE... — shipped .env.example plus the overrides, validated; echoes
# the exit status and leaves the output in $out for assertions.
run() {
    local kv
    cp "$PROJECT_ROOT/docker/.env.example" "$env_file"
    for kv in "$@"; do env_set "$env_file" "${kv%%=*}" "${kv#*=}"; done
    bash "$tmp/scripts/env-validate.sh" > "$out" 2>&1
    printf '%s' "$?"
}
saw() { grep -q "$1" "$out" && printf yes || printf no; }

hex32=0123456789abcdef0123456789abcdef
# What `make init` plus the wizard leave behind for a production stack.
prod=(
    ENV=production
    COOKIE_SECURE=true
    COOKIE_SAME_SITE=strict
    OAUTH_GOOGLE_CLIENT_ID=client-id-for-tests
    OAUTH_GOOGLE_CLIENT_SECRET=client-secret-for-tests
    "COOKIE_SECRET=${hex32}${hex32}"
    "OAUTH_TOKEN_ENCRYPTION_KEY=${hex32}${hex32}"
    "ORKESTRA_KMS_MASTER_KEY=${hex32}${hex32}"
    "LLM_SECRET_ENCRYPTION_KEY=${hex32}${hex32}"
    "MONGO_ROOT_PASSWORD=${hex32}"
    "REDIS_PASSWORD=${hex32}"
    "STORAGE_SECRET_KEY=${hex32}"
    "PDF_RENDERER_PASSWORD=${hex32}"
    "QDRANT_API_KEY=${hex32}${hex32}"
    "RETRIEVAL_INFERENCE_API_KEY=${hex32}${hex32}"
)

# --- development: the shipped placeholders are tolerated, but named ---
check "development: shipped placeholders only warn"        "0"   "$(run)"
check "development: the warning names STORAGE_SECRET_KEY"  "yes" "$(saw 'STORAGE_SECRET_KEY is empty or a placeholder')"

# --- production: generated secrets pass, anything weaker is an error ---
check "production: generated secrets pass"                 "0"   "$(run "${prod[@]}")"
check "production: the shipped RustFS literal is refused"  "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=changeme-rustfs)"
check "production: the refusal names the key"              "yes" "$(saw 'STORAGE_SECRET_KEY is empty or a placeholder')"
check "production: the refusal says how to fix it"         "yes" "$(saw 'openssl rand -hex 16')"
check "production: an unfilled placeholder is refused"     "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=REPLACE_WITH_RANDOM_HEX_32_STORAGE_SECRET)"
check "production: the image default is refused"           "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=rustfsadmin)"
check "production: a short datastore password is refused"  "1"   "$(run "${prod[@]}" REDIS_PASSWORD=short1234)"
check "production: the refusal explains the length rule"   "yes" "$(saw 'REDIS_PASSWORD is shorter than 16 characters')"
check "production: a set RustFS root password is checked"  "1"   "$(run "${prod[@]}" RUSTFS_ROOT_PASSWORD=changeme-rustfs)"
check "production: an unset RustFS root is not an error"   "0"   "$(run "${prod[@]}")"

# --- LLM_SECRET_ENCRYPTION_KEY must be exactly 64 hex chars: the backend
# drops anything else with a WARN and disables secret writes, so this is the
# only deploy-time gate. ---
llm_nonhex=zzzzzzzzzzzzzzzzzzzz
check "production: a 64-hex LLM key passes"                "0"   "$(run "${prod[@]}" "LLM_SECRET_ENCRYPTION_KEY=${hex32}${hex32}")"
check "production: a non-hex LLM key is refused"           "1"   "$(run "${prod[@]}" "LLM_SECRET_ENCRYPTION_KEY=${llm_nonhex}")"
check "production: the refusal names the 64-hex rule"      "yes" "$(saw 'LLM_SECRET_ENCRYPTION_KEY must be 64 hex characters')"
check "production: the refusal gives the right hint"       "yes" "$(saw 'openssl rand -hex 32')"
check "production: a 32-hex (16 byte) LLM key is refused"  "1"   "$(run "${prod[@]}" "LLM_SECRET_ENCRYPTION_KEY=${hex32}")"
check "production: a 64-char non-hex LLM key is refused"   "1"   "$(run "${prod[@]}" "LLM_SECRET_ENCRYPTION_KEY=${llm_nonhex}${llm_nonhex}${llm_nonhex}zzzz")"
check "production: an LLM key placeholder is refused"      "1"   "$(run "${prod[@]}" LLM_SECRET_ENCRYPTION_KEY=REPLACE_WITH_RANDOM_HEX_64_LLM_SECRET)"
check "production: the placeholder refusal gives the 64-hex hint" "yes" "$(run "${prod[@]}" LLM_SECRET_ENCRYPTION_KEY=REPLACE_WITH_RANDOM_HEX_64_LLM_SECRET >/dev/null; saw 'LLM_SECRET_ENCRYPTION_KEY is empty or a placeholder — generate one with: openssl rand -hex 32')"
check "production: a missing LLM key is refused"            "1"   "$(run "${prod[@]}" LLM_SECRET_ENCRYPTION_KEY=)"
check "production: the missing-key refusal gives the 64-hex hint" "yes" "$(run "${prod[@]}" LLM_SECRET_ENCRYPTION_KEY= >/dev/null; saw 'LLM_SECRET_ENCRYPTION_KEY is empty or a placeholder — generate one with: openssl rand -hex 32')"
check "staging: a non-hex LLM key is refused"              "1"   "$(run "${prod[@]}" ENV=staging "LLM_SECRET_ENCRYPTION_KEY=${llm_nonhex}")"
check "development: a non-hex LLM key only warns"          "0"   "$(run "LLM_SECRET_ENCRYPTION_KEY=${llm_nonhex}")"
check "development: the warning names the 64-hex rule"     "yes" "$(saw 'LLM_SECRET_ENCRYPTION_KEY must be 64 hex characters')"
check "development: a 32-hex LLM key only warns"           "0"   "$(run "LLM_SECRET_ENCRYPTION_KEY=${hex32}")"
check "development: a 64-hex LLM key passes silently"      "no"  "$(run "LLM_SECRET_ENCRYPTION_KEY=${hex32}${hex32}" >/dev/null; saw 'LLM_SECRET_ENCRYPTION_KEY must be')"

# --- object storage may be disabled outright: both keys empty — but the bundled
# rustfs container still starts with the infra stack and needs a root of its own ---
rustfs_root=(RUSTFS_ROOT_USER=rustfs-root "RUSTFS_ROOT_PASSWORD=${hex32}")
check "production: storage disabled with a RustFS root passes" "0" "$(run "${prod[@]}" STORAGE_ACCESS_KEY= STORAGE_SECRET_KEY= "${rustfs_root[@]}")"
check "production: storage disabled is reported"           "yes" "$(saw 'object storage disabled')"
check "production: storage disabled without a RustFS root is refused" "1" "$(run "${prod[@]}" STORAGE_ACCESS_KEY= STORAGE_SECRET_KEY=)"
check "production: the refusal names the RustFS root"      "yes" "$(saw 'set RUSTFS_ROOT_USER and RUSTFS_ROOT_PASSWORD')"
check "development: storage disabled without a RustFS root is refused too" "1" "$(run STORAGE_ACCESS_KEY= STORAGE_SECRET_KEY=)"
check "production: a key id with no secret is an error"    "1"   "$(run "${prod[@]}" STORAGE_SECRET_KEY=)"

# --- PDF_RENDERER_PASSWORD gates the gotenberg sidecar, which always starts
# with the infra stack — docker-compose.infra.yml's
# `${PDF_RENDERER_PASSWORD:?...}` is unconditional on the gotenberg service,
# even when PDF_RENDERER_URL is empty (that only disables the *backend's*
# use of PDF features — orkestra.sh starts gotenberg with every
# infra deploy regardless). A URL-gated skip here let a "PDF disabled"
# stack pass validation and then fail `docker compose up -d` for ALL of
# infra, not just gotenberg. Checked unconditionally now, same as every
# other datastore secret. ---
check "production: PDF_RENDERER_PASSWORD is required even with PDF disabled" "1" \
    "$(run "${prod[@]}" PDF_RENDERER_URL= PDF_RENDERER_PASSWORD=)"
check "production: the refusal names PDF_RENDERER_PASSWORD"                  "yes" \
    "$(saw 'PDF_RENDERER_PASSWORD is empty or a placeholder')"
check "development: an empty PDF_RENDERER_URL still warns about the password" "yes" \
    "$(run PDF_RENDERER_URL= PDF_RENDERER_PASSWORD= > /dev/null; saw 'PDF_RENDERER_PASSWORD is empty or a placeholder')"

# --- the RustFS S3 API must stay reachable by the reverse proxy ----------
# STORAGE_PUBLIC_ENDPOINT means the BROWSER PUTs presigned uploads to rustfs
# through a proxy, but docker-compose.infra.yml publishes that port on
# ${HOST_BIND_ADDRESS:-127.0.0.1}. A loopback (or unset) value leaves the S3
# API reachable only from the docker host: presigned uploads 503 at the proxy
# while every other service keeps working.
pub=(STORAGE_PUBLIC_ENDPOINT=https://storage.example.com)
# run_without KEY KV... — like run(), with KEY deleted from the .env after
# the overrides, to cover the "variable absent, compose default applies" arm.
run_without() {
    local key=$1; shift
    local kv
    cp "$PROJECT_ROOT/docker/.env.example" "$env_file"
    for kv in "$@"; do env_set "$env_file" "${kv%%=*}" "${kv#*=}"; done
    sed -i "/^${key}=/d" "$env_file"
    bash "$tmp/scripts/env-validate.sh" > "$out" 2>&1
    printf '%s' "$?"
}
check "storage: a loopback bind with a public endpoint warns"  "0"   "$(run "${pub[@]}" HOST_BIND_ADDRESS=127.0.0.1)"
check "storage: the warning names HOST_BIND_ADDRESS"           "yes" "$(saw 'HOST_BIND_ADDRESS')"
check "storage: the warning names the presigned upload"        "yes" "$(saw 'presigned')"
check "storage: an absent bind address warns the same way"     "yes" "$(run_without HOST_BIND_ADDRESS "${pub[@]}" > /dev/null; saw 'presigned')"
check "storage: localhost counts as loopback"                  "yes" "$(run "${pub[@]}" HOST_BIND_ADDRESS=localhost > /dev/null; saw 'presigned')"
check "storage: a routable bind address is silent"             "no"  "$(run "${pub[@]}" HOST_BIND_ADDRESS=0.0.0.0 > /dev/null; saw 'presigned')"
check "storage: no public endpoint, loopback is fine"          "no"  "$(run HOST_BIND_ADDRESS=127.0.0.1 > /dev/null; saw 'presigned')"

# --- staging is as strict as production ---
check "staging: the shipped RustFS literal is refused"     "1"   "$(run "${prod[@]}" ENV=staging COOKIE_SAME_SITE=lax STORAGE_SECRET_KEY=changeme-rustfs)"

# --- the storage public endpoint must be https in staging/production -----
# Browsers — and, for public cover images, anonymous visitors — fetch
# presigned URLs from STORAGE_PUBLIC_ENDPOINT. Plain HTTP there is a
# cleartext signed URL and mixed content on every HTTPS site embedding it.
check "production: a plain-http public endpoint is refused"    "1"   "$(run "${prod[@]}" STORAGE_PUBLIC_ENDPOINT=http://storage.example.com)"
check "production: the error names STORAGE_PUBLIC_ENDPOINT"    "yes" "$(saw 'STORAGE_PUBLIC_ENDPOINT must be an https')"
check "production: an https public endpoint passes"            "0"   "$(run "${prod[@]}" STORAGE_PUBLIC_ENDPOINT=https://storage.example.com)"
check "production: an unset public endpoint passes"            "0"   "$(run_without STORAGE_PUBLIC_ENDPOINT "${prod[@]}")"
check "staging: a plain-http public endpoint is refused"       "1"   "$(run "${prod[@]}" ENV=staging STORAGE_PUBLIC_ENDPOINT=http://storage.example.com)"
check "development: a plain-http public endpoint is tolerated" "0"   "$(run STORAGE_PUBLIC_ENDPOINT=http://localhost:9100)"

# --- retrieval: Qdrant + inference endpoints (ADR-0023 D3/D4) ---
ep_ok='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"abc","pooling":"cls","normalize":true,"truncate":true}]'
ep_norev='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3"}]'
ep_dup='[{"name":"e","url":"http://a","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"},{"name":"e","url":"http://b","kind":"rerank","boundary":"local","model":"m","revision":"r"}]'
ep_unk='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls","foo":1}]'
ep_rr_pool='[{"name":"rr","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_noprefix_pool='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"r"}]'
ep_badname='[{"name":"Embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_query='[{"name":"embed","url":"http://tei-embed:80/?a=b","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_userinfo='[{"name":"embed","url":"http://u:p@tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_url_space='[{"name":"embed","url":"http://tei embed:80","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_url_port='[{"name":"embed","url":"http://h:abc","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_url_pct='[{"name":"embed","url":"http://h/%zz","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_url_upper='[{"name":"embed","url":"HTTP://tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls"}]'
ep_badmodel='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"-m","revision":"r","pooling":"cls"}]'
ep_badrev='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"_r","pooling":"cls"}]'
ep_norm_rr='[{"name":"rr","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"m","revision":"r","normalize":true}]'
ep_trunc_chat='[{"name":"c","url":"http://llm:80","kind":"chat","boundary":"local","model":"m","revision":"r","truncate":true}]'
ep_prefix_rr='[{"name":"rr","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"m","revision":"r","queryPrefix":"q: "}]'
ep_akenv_bad='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls","apiKeyEnv":"PATH"}]'
ep_akenv='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"m","revision":"r","pooling":"cls","apiKeyEnv":"RETRIEVAL_INFERENCE_EXTRA_KEY"}]'
ep_hosted_http='[{"name":"h","url":"http://api.example.com","kind":"chat","boundary":"hosted","model":"m","revision":"r"}]'
ep_hosted_https='[{"name":"h","url":"https://api.example.com/","kind":"chat","boundary":"hosted","model":"m","revision":"r"}]'
check "production: the shipped Qdrant placeholder is refused"   "1"   "$(run "${prod[@]}" QDRANT_API_KEY=REPLACE_WITH_RANDOM_HEX_64_QDRANT_API_KEY)"
check "production: the refusal names QDRANT_API_KEY"            "yes" "$(saw 'QDRANT_API_KEY is empty or a placeholder — generate one with: openssl rand -hex 32')"
check "production: a short inference key is refused"            "1"   "$(run "${prod[@]}" RETRIEVAL_INFERENCE_API_KEY=short1234)"
check "production: retrieval not configured passes"             "0"   "$(run "${prod[@]}" QDRANT_API_KEY= RETRIEVAL_INFERENCE_API_KEY=)"
check "production: not configured is reported"                  "yes" "$(saw 'Retrieval not configured')"
check "production: QDRANT_URL without a key is refused"         "1"   "$(run "${prod[@]}" QDRANT_URL=http://qdrant.internal:6333 QDRANT_API_KEY=)"
check "production: the refusal names the API-key rule"          "yes" "$(saw 'QDRANT_URL is set but QDRANT_API_KEY is empty')"
check "production: a valid endpoint list passes"                "0"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_ok}")"
check "production: a single-quoted endpoint list passes"        "0"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS='${ep_ok}'")"
check "production: an empty endpoint list passes (Go: no endpoints)" "0" "$(run "${prod[@]}" 'RETRIEVAL_INFERENCE_ENDPOINTS=[]')"
check "production: JSON null is refused like Go's parser"       "1"   "$(run "${prod[@]}" 'RETRIEVAL_INFERENCE_ENDPOINTS=null')"
check "production: malformed endpoint JSON is refused"          "1"   "$(run "${prod[@]}" 'RETRIEVAL_INFERENCE_ENDPOINTS=[{"name":')"
check "production: the refusal names the variable"              "yes" "$(saw 'RETRIEVAL_INFERENCE_ENDPOINTS is not a valid endpoint list')"
check "production: an endpoint without revision is refused"     "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_norev}")"
check "production: the refusal names the missing revision"      "yes" "$(saw 'entry 0: missing or invalid revision')"
check "production: duplicate endpoint names are refused"        "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_dup}")"
check "production: the refusal names the duplicate"             "yes" "$(saw 'duplicate name e')"
check "production: endpoints without any key are refused"       "1"   "$(run "${prod[@]}" RETRIEVAL_INFERENCE_API_KEY= "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_ok}")"
check "development: malformed endpoint JSON only warns"         "0"   "$(run 'RETRIEVAL_INFERENCE_ENDPOINTS=[{"name":')"
check "staging: the Qdrant placeholder is refused"              "1"   "$(run "${prod[@]}" ENV=staging COOKIE_SAME_SITE=lax QDRANT_API_KEY=REPLACE_WITH_RANDOM_HEX_64_QDRANT_API_KEY)"
# Parity with llm/installation parseElement (P11): refuse what the backend drops.
check "production: an unknown endpoint field is refused"        "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_unk}")"
check "production: the refusal names the unknown field"         "yes" "$(saw 'entry 0: unknown field foo')"
check "production: pooling on a rerank endpoint is refused"     "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_rr_pool}")"
check "production: an embed endpoint without pooling is refused" "1"  "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_noprefix_pool}")"
check "production: an invalid endpoint name is refused"         "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_badname}")"
check "production: a URL query string is refused"               "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_query}")"
check "production: URL userinfo is refused"                     "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_userinfo}")"
check "production: a space in the URL host is refused"          "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_url_space}")"
check "production: a non-numeric URL port is refused"           "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_url_port}")"
check "production: an invalid percent escape is refused"        "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_url_pct}")"
check "production: an uppercase URL scheme passes"              "0"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_url_upper}")"
check "production: an invalid model is refused"                 "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_badmodel}")"
check "production: an invalid revision is refused"              "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_badrev}")"
check "production: normalize on a rerank endpoint is refused"   "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_norm_rr}")"
check "production: truncate on a chat endpoint is refused"      "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_trunc_chat}")"
check "production: a prefix on a rerank endpoint is refused"    "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_prefix_rr}")"
check "production: an apiKeyEnv outside the prefix is refused"  "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_akenv_bad}")"
check "production: an empty apiKeyEnv variable is refused"      "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_akenv}")"
check "production: the refusal says to pass it to the backend"  "yes" "$(saw 'RETRIEVAL_INFERENCE_EXTRA_KEY is empty.*backend container')"
check "production: a set apiKeyEnv variable passes"             "0"   "$(run "${prod[@]}" RETRIEVAL_INFERENCE_API_KEY= RETRIEVAL_INFERENCE_EXTRA_KEY=abc "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_akenv}")"
check "production: a hosted http endpoint is refused"           "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_hosted_http}")"
check "production: a hosted https endpoint passes"              "0"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_hosted_https}")"
check "development: a hosted http endpoint only warns"          "0"   "$(run "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_hosted_http}")"
check "production: only one retrieval key warns"                "yes" "$(run "${prod[@]}" RETRIEVAL_INFERENCE_API_KEY= > /dev/null; saw 'needs both QDRANT_API_KEY and RETRIEVAL_INFERENCE_API_KEY')"
check "production: a bad collection prefix only warns"          "0"   "$(run "${prod[@]}" QDRANT_COLLECTION_PREFIX=Bad_Prefix)"
check "production: the bad prefix warning is shown"             "yes" "$(saw 'QDRANT_COLLECTION_PREFIX')"
check "production: a valid collection prefix is silent"         "no"  "$(run "${prod[@]}" QDRANT_COLLECTION_PREFIX=acme-1 > /dev/null; saw 'QDRANT_COLLECTION_PREFIX')"
check "a commented endpoint stub is not read as active"         "no"  "$(run "${prod[@]}" > /dev/null; saw 'not a valid endpoint list')"

# Without jq the endpoint list cannot be checked: refused in staging and
# production, a warning in development. PATH is rebuilt from the real one
# with every command but jq, so the rest of the validator still runs.
nojq="$tmp/nojq-bin"
mkdir -p "$nojq"
IFS=: read -r -a path_dirs <<< "$PATH"
for d in "${path_dirs[@]}"; do
    [ -d "$d" ] || continue
    for c in "$d"/*; do
        name="${c##*/}"
        [ "$name" = jq ] || [ -e "$nojq/$name" ] || ln -s "$c" "$nojq/$name" 2> /dev/null
    done
done
run_nojq() { PATH="$nojq" run "$@"; }
check "the jq-less PATH really hides jq"                        "no"  "$(PATH="$nojq" command -v jq > /dev/null && printf yes || printf no)"
check "production: an endpoint list without jq is refused"      "1"   "$(run_nojq "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_ok}")"
check "production: the refusal names jq and the variable"       "yes" "$(saw 'jq is required to validate RETRIEVAL_INFERENCE_ENDPOINTS')"
check "development: an endpoint list without jq only warns"     "0"   "$(run_nojq "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_ok}")"
check "development: the jq warning is shown"                    "yes" "$(saw 'jq is required to validate RETRIEVAL_INFERENCE_ENDPOINTS.*tolerated in development')"
check "production: no endpoint list needs no jq"                "0"   "$(run_nojq "${prod[@]}")"

echo
printf 'env-validate: %d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
