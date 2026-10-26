# Retrieval PR 8 — Deployment Overlay and Benchmark Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** ship the opt-in `docker-compose.retrieval.yml` overlay (Qdrant + two TEI servers on an internal network), its secrets, validation and `./orkestra.sh retrieval …` lifecycle, and the D14 benchmark harness with a committed multilingual smoke fixture.

**Architecture:** the overlay joins the stack's single Compose project like the observability overlay: its own services plus a partial `backend` override that sets the four retrieval variables and adds the backend to `retrieval-net` (`internal: true`). Serving containers never get egress; model weights are fetched once by short-lived *seed* containers that join only an egress network nobody else is on. The base app files pass the variables through empty-safe, so without the overlay the backend runs in D1's "not configured" mode. The benchmark is a library under `retrieval/bench` driven by `cmd/retrieval-bench`, run inside the backend container (the only place that reaches `retrieval-net`) through `./orkestra.sh retrieval bench` / `make retrieval-bench`.

**Tech Stack:** Docker Compose v2, bash, jq, Qdrant (`-unprivileged` image, REST), Hugging Face Text Embeddings Inference CPU image, Go 1.26.10.

**Spec:** `docs/adr/0023-core-retrieval-module.md` (D1, D3, D4 "Reference configuration", D9, D14 "Benchmark first", Consequences "Infrastructure") and the roadmap `docs/superpowers/plans/2026-10-26-retrieval-00-roadmap.md` (row 8, "Environment" under the PR 1 contract, "Environment read by `retrieval` Init", "Internal engine contract").

## Global Constraints

Every task includes the roadmap's **Global constraints** verbatim. PR-specific additions:

- Commit trailers on every commit, after a blank line: `Prop: upstream` then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Commit steps below show only the subject.
- Overlay tasks (1–4) depend only on PRs 1 and 3; benchmark tasks (5–8) also need PR 5 merged (the stack must serve real searches for the smoke run) and consume only the roadmap's engine contract.
- Images pinned by digest: `qdrant/qdrant:v1.<N>.<M>-unprivileged@sha256:<64 hex>` with `N ≥ 20`, and `ghcr.io/huggingface/text-embeddings-inference:cpu-<version>@sha256:<64 hex>` (latest `cpu-1.*` release at implementation time). Resolve each digest with `docker buildx imagetools inspect <image>:<tag>` and record tag + digest in the docs model inventory.
- Models pinned by revision: `BAAI/bge-m3` (MIT, dense 1024, `--pooling cls`) and `BAAI/bge-reranker-v2-m3` (Apache-2.0); resolve each 40-hex revision with `curl -s https://huggingface.co/api/models/<id>/revision/main | jq -r .sha` and use the same value in the TEI command and in `RETRIEVAL_INFERENCE_ENDPOINTS`.
- No published host port for `qdrant`, `tei-*` or the seed services in any environment (development included); the backend reaches them only over `retrieval-net`.
- Secrets have no literal fallback in any compose file (`docker/tests/credential-fallbacks.test.sh`); the overlay refuses to render without them (`${X:?…}`).
- The bench writes to stdout/stderr with `fmt`, not `slog` (logscope), creates only its own scratch collection and drops it on every exit path.
- Dev-slot wiring in the octolabs-worktree kit is out of scope (separate repository); say so in `docker/AGENTS.md`.

## Decisions taken by this plan

- **Weights egress → seed step, not the default network.** D9 puts the inference servers "on the stack's internal network only", and they process tenant text, so a serving TEI must never have egress. Putting TEI on `default` as well would also let it reach MongoDB and Redis. Instead the overlay declares `tei-embed-seed` and `tei-rerank-seed` (profile `retrieval-seed`): same image, model and revision, same `tei-weights` volume, joined **only** to `retrieval-egress` (a plain bridge no other service joins). `retrieval up` starts them, waits until healthy (TEI is healthy only after it has loaded the weights), removes them, then starts the serving containers on `retrieval-net` with `HF_HUB_OFFLINE=1`. Offline hosts pre-seed the volume and set `RETRIEVAL_SKIP_SEED=true`.
- **"Overlay configured" for `env-validate.sh` = the key is set.** The validator cannot see which compose files a deploy merges, and with the overlay `QDRANT_URL` is set by the overlay itself, not in `.env` — so a `QDRANT_URL`-in-`.env` condition would skip the check exactly when the overlay runs. Following the `RUSTFS_ROOT_PASSWORD` precedent, `QDRANT_API_KEY` and `RETRIEVAL_INFERENCE_API_KEY` are checked whenever non-empty (`.env.example` ships placeholders that `make init` fills, so this is the default); emptying both is the explicit opt-out, and the overlay then refuses to render. Separately, `QDRANT_URL` set with an empty `QDRANT_API_KEY` is an error (D3: API key required).
- **The bench lives in `backend/internal/core/retrieval/bench` (library, unit-tested) + `backend/cmd/retrieval-bench` (flags, env, exit code)** and talks to TEI's HTTP API directly: it measures the models, the engine arms and fusion, not the `llm` gateway (which needs MongoDB and ledger rows). It runs inside the backend container because nothing else reaches `retrieval-net`.
- **One `stack_compose_files` helper decides the `-f` list** for deploy and for every overlay's apply/restore, so neither overlay's `down` drops the other's backend override, and the production rolling update keeps both.

## Review Focus

1. **The served model drifts from the declared profile** (someone bumps the TEI `--revision` but not `RETRIEVAL_INFERENCE_ENDPOINTS`, or the reverse): the hardening test fails (pinned in Task 3 by `endpoints JSON names the served model and revision`).
2. **Both overlays active, then one is taken down**: the backend keeps the other overlay's environment and networks (pinned in Task 4 by `retrieval down keeps the observability override` and `observability down keeps the retrieval override`).
3. **A production deploy while the overlay runs**: the rolling backend update keeps `QDRANT_URL` and `retrieval-net` (pinned in Task 4 by `production rolling update uses the stack file list`).
4. **An operator who does not want retrieval empties both keys**: production validation passes and says retrieval is not configured, and the overlay refuses to render instead of starting an unauthenticated Qdrant (pinned in Task 2 by `production: retrieval not configured passes` and in Task 3 by `overlay refuses to render without QDRANT_API_KEY`).
5. **A benchmark query whose principals cannot see a relevant item, and an interrupted run**: a returned invisible item is counted as a violation and the command exits 1; the scratch collection is dropped even when the context is cancelled (pinned in Task 7 by `TestRun_CountsVisibilityViolations` and `TestRun_DropsCollectionOnCancel`).

---

## File map

| File | Responsibility | Task |
|---|---|---|
| `docker/.env.example` (modify) | retrieval section with placeholders | 1 |
| `scripts/init.sh` (modify) | generate the two keys | 1 |
| `docker/tests/credential-fallbacks.test.sh` (modify) | no literal fallback for the new keys | 1 |
| `scripts/env-validate.sh`, `scripts/test-env-validate.sh` (modify) | key hygiene, endpoint JSON check | 2 |
| `docker/docker-compose.retrieval.yml` (create) | qdrant, tei-*, seeds, networks, volumes, backend override | 3 |
| `docker/docker-compose.{dev,staging,prod}.yml` (modify) | empty-safe passthrough of five variables | 3 |
| `docker/tests/retrieval-hardening.test.sh` (create), `Makefile` (modify) | static gate in `make ci-backend` | 3 |
| `orkestra.sh`, `scripts/test-orkestra-helpers.sh` (modify) | `retrieval up/down/status/info/logs/bench`, `stack_compose_files`, deploy fix | 4, 8 |
| `backend/internal/core/retrieval/bench/fixture.go`, `metrics.go` (+ tests, `testdata/smoke-it-en.jsonl`) | fixture loading, metrics | 5 |
| `backend/internal/core/retrieval/bench/tei.go` (+ test) | TEI HTTP client | 6 |
| `backend/internal/core/retrieval/bench/runner.go`, `report.go` (+ tests) | index, run four modes, report | 7 |
| `backend/cmd/retrieval-bench/main.go` (+ test), `Makefile` (modify) | command and `make retrieval-bench` | 8 |
| `docker/AGENTS.md`, `scripts/AGENTS.md`, `AGENTS.md`, `backend/AGENTS.md`, `backend/internal/core/retrieval/AGENTS.md`, `docs/site/operating/retrieval-overlay.mdx` (create), `docs/site/operating/index.mdx`, `docs/site/operating/deployment/index.mdx`, `docs/site/modules/core/retrieval.mdx` | docs | 9 |

---

### Task 1: Secrets — `.env.example`, `init.sh`, credential-fallback gate

**Files:**
- Modify: `docker/.env.example` (new section after the PDF renderer block, ~L487)
- Modify: `scripts/init.sh:159-180`
- Modify: `docker/tests/credential-fallbacks.test.sh:31-52`

**Interfaces:**
- Produces: placeholders `REPLACE_WITH_RANDOM_HEX_64_QDRANT_API_KEY`, `REPLACE_WITH_RANDOM_HEX_64_RETRIEVAL_INFERENCE_KEY`; `.env.example` keys `QDRANT_URL=`, `QDRANT_API_KEY=…`, `QDRANT_COLLECTION_PREFIX=`, `RETRIEVAL_INFERENCE_ENDPOINTS=`, `RETRIEVAL_INFERENCE_API_KEY=…`; Tasks 2–4 rely on these names.

- [ ] **Step 1: Extend the gate (failing test)**

In `credential-fallbacks.test.sh`: add `QDRANT_API_KEY|RETRIEVAL_INFERENCE_API_KEY` to `creds`; add `docker/docker-compose.retrieval.yml` to the literal-fallback file loop, guarded by `[ -f "$f" ] || continue` until Task 3 creates it; add `QDRANT_API_KEY RETRIEVAL_INFERENCE_API_KEY` to the placeholder loop (which also checks that `init.sh` fills each placeholder).

- [ ] **Step 2: Run to verify failure**

Run: `bash docker/tests/credential-fallbacks.test.sh`
Expected: FAIL — `docker/.env.example ships a literal for QDRANT_API_KEY (want a REPLACE_WITH_RANDOM_HEX_* placeholder)`.

- [ ] **Step 3: Implement**

`.env.example` section, comment stating: the overlay (`./orkestra.sh retrieval up`) sets `QDRANT_URL` and `RETRIEVAL_INFERENCE_ENDPOINTS` on the backend itself, so leave them empty unless pointing at services outside the stack (GPU host, existing Qdrant; JSON format as in the roadmap, single-quoted); empty keys mean "retrieval not configured" — the overlay refuses to start and search answers `retrieval.not_configured`; `QDRANT_COLLECTION_PREFIX` empty = `orkestra`. `init.sh`: `qdrant_key=$(openssl rand -hex 32)`, `inference_key=$(openssl rand -hex 32)` and two `-e "s|…|…|"` lines with a comment (Qdrant and TEI sit on an internal network, but a shipped literal is still a public secret).

- [ ] **Step 4: Run to verify pass**

Run: `bash docker/tests/credential-fallbacks.test.sh && bash -n scripts/init.sh`
Expected: `credential fallbacks: OK` (its placeholder loop also proves `init.sh` substitutes both placeholders).

- [ ] **Step 5: Commit**

```bash
git add docker/.env.example scripts/init.sh docker/tests/credential-fallbacks.test.sh
git commit -m "feat(docker): generate Qdrant and inference keys for the retrieval overlay"
```

---

### Task 2: `env-validate.sh` — retrieval keys and endpoint list

**Files:**
- Modify: `scripts/env-validate.sh` (`SECRET_VARS` L63-73, the secret loop L233-283, `show_usage` L449-490)
- Modify: `scripts/test-env-validate.sh` (prod baseline L40-54, new cases before the final summary)

**Interfaces:**
- Consumes: `env_value`, `secret_is_placeholder`, `secret_is_weak` (`scripts/env-file.sh:67-84`).
- Produces:

```bash
# env_raw_value KEY — first active assignment, surrounding whitespace and ONE pair of
# surrounding single or double quotes removed; inner quotes kept (JSON values).
env_raw_value() { … }

# inference_endpoints_error VALUE — prints "" when VALUE is a valid endpoint list,
# otherwise one short reason. Needs jq; the caller checks `command -v jq` first.
inference_endpoints_error() { … }
```

Validity (jq): a non-empty array; every entry has non-empty string `name`, `url` starting `http://` or `https://`, `kind` ∈ {embed, rerank, chat}, `boundary` ∈ {local, hosted}, non-empty string `model` and `revision`; names unique. Reasons: `not a JSON array`, `empty list`, `entry <i>: missing or invalid <field>`, `duplicate name <name>`.

Rules:
- `QDRANT_API_KEY`, `RETRIEVAL_INFERENCE_API_KEY` join `SECRET_VARS` with a `case` arm `[ -z "$value" ] && continue` (checked only when set); their hint is `openssl rand -hex 32`.
- After the secret loop, `print_info "Checking retrieval settings..."`: both keys empty and `QDRANT_URL` and `RETRIEVAL_INFERENCE_ENDPOINTS` empty → `print_info "Retrieval not configured (QDRANT_API_KEY and RETRIEVAL_INFERENCE_API_KEY empty) — the retrieval overlay will refuse to start"`.
- `QDRANT_URL` non-empty and `QDRANT_API_KEY` empty → strict error / dev warning `QDRANT_URL is set but QDRANT_API_KEY is empty — Qdrant must require an API key (ADR-0023 D3)`.
- `RETRIEVAL_INFERENCE_ENDPOINTS` non-empty: no `jq` → warning `jq not found — RETRIEVAL_INFERENCE_ENDPOINTS not validated`; invalid → strict error / dev warning `RETRIEVAL_INFERENCE_ENDPOINTS is not a valid endpoint list: <reason>`; valid but `RETRIEVAL_INFERENCE_API_KEY` empty and some entry has no `apiKeyEnv` → strict error / dev warning `RETRIEVAL_INFERENCE_API_KEY is empty and an endpoint names no apiKeyEnv`.
- `show_usage` lists the new checks.

- [ ] **Step 1: Write the failing tests**

Add to `prod`: `"QDRANT_API_KEY=${hex32}${hex32}"`, `"RETRIEVAL_INFERENCE_API_KEY=${hex32}${hex32}"`. Then:

```bash
ep_ok='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"abc","pooling":"cls","normalize":true,"truncate":true}]'
ep_norev='[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3"}]'
ep_dup='[{"name":"e","url":"http://a","kind":"embed","boundary":"local","model":"m","revision":"r"},{"name":"e","url":"http://b","kind":"rerank","boundary":"local","model":"m","revision":"r"}]'
check "production: the shipped Qdrant placeholder is refused"   "1"   "$(run "${prod[@]}" QDRANT_API_KEY=REPLACE_WITH_RANDOM_HEX_64_QDRANT_API_KEY)"
check "production: the refusal names QDRANT_API_KEY"            "yes" "$(saw 'QDRANT_API_KEY is empty or a placeholder — generate one with: openssl rand -hex 32')"
check "production: a short inference key is refused"            "1"   "$(run "${prod[@]}" RETRIEVAL_INFERENCE_API_KEY=short1234)"
check "production: retrieval not configured passes"             "0"   "$(run "${prod[@]}" QDRANT_API_KEY= RETRIEVAL_INFERENCE_API_KEY=)"
check "production: not configured is reported"                  "yes" "$(saw 'Retrieval not configured')"
check "production: QDRANT_URL without a key is refused"         "1"   "$(run "${prod[@]}" QDRANT_URL=http://qdrant.internal:6333 QDRANT_API_KEY=)"
check "production: the refusal names the API-key rule"          "yes" "$(saw 'QDRANT_URL is set but QDRANT_API_KEY is empty')"
check "production: a valid endpoint list passes"                "0"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_ok}")"
check "production: a single-quoted endpoint list passes"        "0"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS='${ep_ok}'")"
check "production: malformed endpoint JSON is refused"          "1"   "$(run "${prod[@]}" 'RETRIEVAL_INFERENCE_ENDPOINTS=[{"name":')"
check "production: the refusal names the variable"              "yes" "$(saw 'RETRIEVAL_INFERENCE_ENDPOINTS is not a valid endpoint list')"
check "production: an endpoint without revision is refused"     "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_norev}")"
check "production: duplicate endpoint names are refused"        "1"   "$(run "${prod[@]}" "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_dup}")"
check "production: endpoints without any key are refused"       "1"   "$(run "${prod[@]}" RETRIEVAL_INFERENCE_API_KEY= "RETRIEVAL_INFERENCE_ENDPOINTS=${ep_ok}")"
check "development: malformed endpoint JSON only warns"         "0"   "$(run 'RETRIEVAL_INFERENCE_ENDPOINTS=[{"name":')"
check "staging: the Qdrant placeholder is refused"              "1"   "$(run "${prod[@]}" ENV=staging COOKIE_SAME_SITE=lax QDRANT_API_KEY=REPLACE_WITH_RANDOM_HEX_64_QDRANT_API_KEY)"
```

- [ ] **Step 2: Run to verify failure**

Run: `./scripts/test-env-validate.sh`
Expected: FAIL on the new checks; the existing checks still pass.

- [ ] **Step 3: Implement the rules**

- [ ] **Step 4: Run to verify pass**

Run: `make backend-script-tests`
Expected: `env-validate: N passed, 0 failed`.

- [ ] **Step 5: Commit**

```bash
git add scripts/env-validate.sh scripts/test-env-validate.sh
git commit -m "feat(scripts): validate retrieval keys and inference endpoints"
```

---

### Task 3: The overlay, base passthrough and the hardening gate

**Files:**
- Create: `docker/docker-compose.retrieval.yml`
- Modify: `docker/docker-compose.dev.yml` (~L203-207), `docker-compose.staging.yml` (~L215-220), `docker-compose.prod.yml` (~L177-181): backend `environment`
- Create: `docker/tests/retrieval-hardening.test.sh`
- Modify: `Makefile` (`.PHONY` L161, `ci-backend` L295, new target after `backend-gotenberg-hardening` L314)

**Interfaces:**
- Consumes: Task 1 variable names; roadmap env format of `RETRIEVAL_INFERENCE_ENDPOINTS`.
- Produces (Task 4 relies on these names): services `qdrant`, `tei-embed`, `tei-rerank`, `tei-embed-seed`, `tei-rerank-seed` (profile `retrieval-seed`); volumes `qdrant-data`, `tei-weights`; networks `retrieval-net` (internal), `retrieval-egress`; backend URLs `http://qdrant:6333`, `http://tei-embed:80`, `http://tei-rerank:80`.

Base app files (all three), next to the PDF renderer block, with a comment that the overlay overrides the first and fourth and that empty values leave retrieval "not configured" (D1):

```yaml
      QDRANT_URL: ${QDRANT_URL:-}
      QDRANT_API_KEY: ${QDRANT_API_KEY:-}
      QDRANT_COLLECTION_PREFIX: ${QDRANT_COLLECTION_PREFIX:-}
      RETRIEVAL_INFERENCE_ENDPOINTS: ${RETRIEVAL_INFERENCE_ENDPOINTS:-}
      RETRIEVAL_INFERENCE_API_KEY: ${RETRIEVAL_INFERENCE_API_KEY:-}
```

Overlay shape (header comment modelled on `docker-compose.observability.yml:1-27`: not standalone, always merged with infra + app, use `./orkestra.sh retrieval up`; the seed/egress decision above; offline pre-seeding; no host ports; one engine per stack):

```yaml
x-tei-common: &tei-common
  image: ghcr.io/huggingface/text-embeddings-inference:cpu-<version>@sha256:<digest>
  labels:
    orkestra.stack: ${APP_NAME}-${ENV}
  security_opt: [no-new-privileges:true]
  cap_drop: [ALL]
  volumes: [tei-weights:/data]
  healthcheck: { <GET /health == 200>, interval: 15s, timeout: 5s, retries: 5, start_period: 300s }

services:
  backend:
    environment:
      QDRANT_URL: http://qdrant:6333
      QDRANT_API_KEY: ${QDRANT_API_KEY:?QDRANT_API_KEY is not set — run make init}
      RETRIEVAL_INFERENCE_ENDPOINTS: '[{"name":"embed","url":"http://tei-embed:80","kind":"embed","boundary":"local","model":"BAAI/bge-m3","revision":"<rev-m3>","pooling":"cls","normalize":true,"truncate":true},{"name":"rerank","url":"http://tei-rerank:80","kind":"rerank","boundary":"local","model":"BAAI/bge-reranker-v2-m3","revision":"<rev-rr>","truncate":true}]'
      RETRIEVAL_INFERENCE_API_KEY: ${RETRIEVAL_INFERENCE_API_KEY:?RETRIEVAL_INFERENCE_API_KEY is not set — run make init}
    networks: [default, pdf-net, retrieval-net]

  qdrant:
    image: qdrant/qdrant:v1.<N>.<M>-unprivileged@sha256:<digest>
    container_name: ${APP_NAME}-qdrant-${ENV}
    labels: { orkestra.stack: ${APP_NAME}-${ENV} }
    restart: unless-stopped
    security_opt: [no-new-privileges:true]
    cap_drop: [ALL]
    mem_limit: ${QDRANT_MEM_LIMIT:-2g}
    environment:
      QDRANT__SERVICE__API_KEY: ${QDRANT_API_KEY:?QDRANT_API_KEY is not set — run make init}
      QDRANT__TELEMETRY_DISABLED: "true"
    volumes: [qdrant-data:/qdrant/storage]
    networks: [retrieval-net]
    healthcheck: { <GET /readyz == 200>, interval: 15s, timeout: 5s, retries: 5, start_period: 30s }

  tei-embed:
    <<: *tei-common
    container_name: ${APP_NAME}-tei-embed-${ENV}
    restart: unless-stopped
    mem_limit: ${TEI_EMBED_MEM_LIMIT:-4g}
    command: [--model-id, BAAI/bge-m3, --revision, <rev-m3>, --pooling, cls, --auto-truncate, --hostname, 0.0.0.0, --port, "80"]
    environment:
      HUGGINGFACE_HUB_CACHE: /data
      HF_HUB_OFFLINE: "1"
      API_KEY: ${RETRIEVAL_INFERENCE_API_KEY:?RETRIEVAL_INFERENCE_API_KEY is not set — run make init}
    networks: [retrieval-net]

  tei-rerank:   # same as tei-embed with BAAI/bge-reranker-v2-m3, <rev-rr>, no --pooling, TEI_RERANK_MEM_LIMIT (default 4g)

  tei-embed-seed:   # profiles: [retrieval-seed]; same image/command/volume as tei-embed; no restart; no HF_HUB_OFFLINE; networks: [retrieval-egress]
  tei-rerank-seed:  # likewise for the reranker

volumes:
  qdrant-data: { driver: local }
  tei-weights: { driver: local }

networks:
  retrieval-net:
    internal: true
  retrieval-egress: {}
```

Healthchecks: probe with the first tool the pinned image ships among `curl`, `wget`, `bash` (`/dev/tcp`); find out with `docker run --rm --entrypoint sh <image> -c 'command -v curl wget bash'`. Qdrant's `/readyz` needs no API key; TEI's `/health` needs none either. Seed services carry the API key too (they serve briefly on a network nobody else joins).

- [ ] **Step 1: Write the failing hardening test**

`docker/tests/retrieval-hardening.test.sh`, structure of `gotenberg-hardening.test.sh` (scratch env file with `APP_NAME`, `ENV`, Mongo/Redis/storage/PDF secrets, `QDRANT_API_KEY=x`, `RETRIEVAL_INFERENCE_API_KEY=y`; renders with `--profile retrieval-seed`, JSON via jq). Assertions, each printing `FAIL: <what>` and setting `fail=1`:

```bash
for app in docker-compose.dev.yml docker-compose.staging.yml docker-compose.prod.yml; do
  m=$(docker compose --env-file "$env_file" -f docker-compose.infra.yml -f "$app" -f docker-compose.retrieval.yml --profile retrieval-seed config --format json)
  # images: qdrant matches ^qdrant/qdrant:v1\.(2[0-9]|[3-9][0-9])\.[0-9]+-unprivileged@sha256:[0-9a-f]{64}$
  #         tei-embed/tei-rerank match ^ghcr\.io/huggingface/text-embeddings-inference:cpu-[0-9.]+@sha256:[0-9a-f]{64}$
  #         each seed's image == its serving service's image
  # ports: (.ports // []) | length == 0 for qdrant, tei-embed, tei-rerank, tei-embed-seed, tei-rerank-seed
  # networks: qdrant, tei-embed, tei-rerank == ["retrieval-net"]; seeds == ["retrieval-egress"];
  #           backend == ["default","pdf-net","retrieval-net"];
  #           .networks."retrieval-net".internal == true; .networks."retrieval-egress".internal != true;
  #           no service other than the two seeds joins retrieval-egress
  # hardening: security_opt contains "no-new-privileges:true" and cap_drop == ["ALL"] on all five
  # healthcheck.test non-empty on all five
  # offline: tei-embed/tei-rerank environment.HF_HUB_OFFLINE == "1"; seeds have no HF_HUB_OFFLINE
  # "endpoints JSON names the served model and revision":
  #   ep=$(jq -r '.services.backend.environment.RETRIEVAL_INFERENCE_ENDPOINTS' <<<"$m")
  #   for name in embed rerank: the entry's url host == the service (tei-embed/tei-rerank), and its
  #   model and revision equal the values after --model-id and --revision in that service's command;
  #   the seed's command equals the serving command
  # backend.environment.QDRANT_URL == "http://qdrant:6333"; QDRANT_API_KEY == "x"
done
# standalone base files: QDRANT_URL and RETRIEVAL_INFERENCE_ENDPOINTS render "" when unset,
#   and the .env value when set (degrade without the overlay, pass an external service through)
# "overlay refuses to render without QDRANT_API_KEY" and without RETRIEVAL_INFERENCE_API_KEY:
#   docker compose … -f docker-compose.retrieval.yml config fails with the key line removed
[ "$fail" = 0 ] && echo "ok: retrieval overlay hardening present"
exit "$fail"
```

- [ ] **Step 2: Run to verify failure**

Run: `bash docker/tests/retrieval-hardening.test.sh`
Expected: FAIL — `docker-compose.retrieval.yml` not found / every assertion fails.

- [ ] **Step 3: Resolve pins and write the overlay and passthrough**

Resolve the two image digests and two model revisions with the commands in Global Constraints; write the overlay and the five passthrough lines per app file. Remove the `[ -f "$f" ] || continue` guard added in Task 1.

- [ ] **Step 4: Wire the gate into CI**

`Makefile`: `backend-retrieval-hardening:` → `@bash docker/tests/retrieval-hardening.test.sh` with a comment (no published port, retrieval-net only, digests, profile ↔ served model); add it to `.PHONY` and to `ci-backend` right after `backend-gotenberg-hardening`.

- [ ] **Step 5: Run to verify pass**

Run: `make backend-retrieval-hardening backend-gotenberg-hardening backend-credential-fallbacks`
Expected: `ok: retrieval overlay hardening present`, `ok: gotenberg hardening present` (the backend networks assertion there still expects `["default","pdf-net"]` on the standalone app files — unchanged), `credential fallbacks: OK`.

- [ ] **Step 6: Commit**

```bash
git add docker/docker-compose.retrieval.yml docker/docker-compose.dev.yml docker/docker-compose.staging.yml \
  docker/docker-compose.prod.yml docker/tests/retrieval-hardening.test.sh docker/tests/credential-fallbacks.test.sh Makefile
git commit -m "feat(docker): retrieval overlay with Qdrant and TEI on an internal network"
```

---

### Task 4: `./orkestra.sh retrieval up|down|status|info|logs` and overlay-aware deploy

**Files:**
- Modify: `orkestra.sh` (constants near L163-167; new section after the observability section ~L1782; `list_all_services` ~L1786; deploy ~L1290-1308; help ~L2095; CLI dispatch ~L2201)
- Modify: `scripts/test-orkestra-helpers.sh` (after the observability block ~L223)

**Interfaces:**
- Consumes: Task 3 names; existing `resolve_stack_identity`, `detect_environment`, `set_env_config`, `with_spinner`, `page_header`, `draw_box`, `die`, `logs_cli`.
- Produces:

```bash
RETRIEVAL_COMPOSE="$DOCKER_DIR/docker-compose.retrieval.yml"
declare -ar RET_SERVICES=(qdrant tei-embed tei-rerank)
declare -ar RET_SEED_SERVICES=(tei-embed-seed tei-rerank-seed)
declare -ar RET_VOLUMES=(qdrant-data tei-weights)

overlay_init_env            # body of today's observability_init_env; observability_init_env and retrieval_init_env call it
retrieval_check_file
retrieval_init_env
retrieval_list_services     # fail-closed: merged infra+app+retrieval with --profile retrieval-seed must list all five services and both volumes
retrieval_project_active    # any RET_SERVICES container exists
# stack_compose_files [--with observability|retrieval] [--without observability|retrieval]
# sets the global array STACK_COMPOSE_FILES=(-f infra -f app [-f observability] [-f retrieval]):
# an overlay is included when its project is active or it is named by --with, unless named by --without.
stack_compose_files
retrieval_seed              # unless RETRIEVAL_SKIP_SEED=true: up -d --wait --wait-timeout ${RETRIEVAL_SEED_TIMEOUT:-1800} the two seeds; ALWAYS rm -sf them; die on failure after removal
retrieval_apply_backend_override    # backend running → stack_compose_files --with retrieval; up -d --no-deps backend
retrieval_restore_backend_config    # backend running → stack_compose_files --without retrieval; up -d --no-deps backend
retrieval_up                # check docker/file, init env, list services, seed, up -d --wait --wait-timeout 900 qdrant tei-embed tei-rerank, apply override, info
retrieval_down              # rm -sf the five services (volumes kept), restore backend
retrieval_status            # ps of the three + docker stats --no-stream (MemUsage) — the sizing source of Task 9
retrieval_info              # internal URLs (no host ports), backend variables, opt-out (empty keys), offline seeding, bench command
```

Every compose call in the retrieval functions uses the merged `-f infra -f app -f retrieval` list and names services explicitly (never a project-wide `down`). `observability_apply_backend_override`/`observability_restore_backend_config` switch to `stack_compose_files --with observability` / `--without observability`. In `fullstack_execute_deploy`, `compose_files` becomes `("${STACK_COMPOSE_FILES[@]}")` after `stack_compose_files`, and the production rolling backend update (L1306-1307) uses `"${compose_files[@]}"`. CLI: `retrieval up|down|status|info|logs <svc>`; `logs` uses `list_all_services retrieval` (the five explicit names). Help gets a `RETRIEVAL` block (no TUI entry in this PR).

- [ ] **Step 1: Write the failing helper tests**

Same stubbing style as the observability blocks (subshell, `docker()` logging `$*` to a temp file, stubbed `check_docker_running`, `page_header`, `p_ok`, `with_spinner() { shift; "$@"; }`, `retrieval_init_env` setting the four paths and `COMPOSE_PROJECT_NAME=orkestra-test-development`, `config --services`/`--volumes` returning app + overlay names, `ps -q backend` returning an id). Checks:

```bash
check "retrieval discovery fails closed on invalid compose" "yes" …   # docker returns 17 → non-zero
check "retrieval discovery validates the merged stack"      "3"   …   # three ' -f ' on the first call
check "retrieval up seeds before serving"                   "yes" …   # line number of 'up -d --wait .* tei-embed-seed tei-rerank-seed' < line of 'up -d --wait .* qdrant tei-embed tei-rerank$'
check "retrieval up removes the seed containers"            "yes" …   # 'rm -sf tei-embed-seed tei-rerank-seed$'
check "retrieval up applies its backend override"           "yes" …   # 'compose .* -f .*docker-compose.retrieval.yml .* up -d --no-deps backend$'
check "RETRIEVAL_SKIP_SEED skips the seed step"             "no"  …   # no 'tei-embed-seed' in an 'up' line
check "a failed seed exits non-zero"                       "yes" …   # docker returns 1 on the seed 'up'
check "a failed seed still removes the seeds"               "yes" …   # 'rm -sf tei-embed-seed tei-rerank-seed$' present
check "a failed seed starts no serving container"           "no"  …   # no 'up -d --wait .* qdrant tei-embed tei-rerank$'
check "retrieval down removes only five explicit services"  "yes" …   # 'rm -sf qdrant tei-embed tei-rerank tei-embed-seed tei-rerank-seed$'
check "retrieval down never uses project-wide down"         "no"  …
check "retrieval down restores the backend without the overlay" "yes" … # 'compose -f .*infra.yml -f .*dev.yml --env-file .* up -d --no-deps backend$'
check "retrieval down keeps the observability override"     "yes" …   # observability_project_active() { return 0; } → restore line contains docker-compose.observability.yml and not docker-compose.retrieval.yml
check "observability down keeps the retrieval override"     "yes" …   # retrieval_project_active() { return 0; } → observability restore line contains docker-compose.retrieval.yml
check "stack files: no overlay active"                      "4"   …   # ${#STACK_COMPOSE_FILES[@]} with both *_project_active returning 1
check "stack files: both overlays active"                   "8"   …
check "stack files: --without retrieval"                    "6"   …   # both active
check "production rolling update uses the stack file list"  "0"   …   # declare -f fullstack_execute_deploy | grep 'up -d --no-deps "$BACKEND_SERVICE"' | grep -vc 'compose_files\[@\]'
check "retrieval log picker uses the explicit service catalog" "5" …
```

- [ ] **Step 2: Run to verify failure**

Run: `bash scripts/test-orkestra-helpers.sh`
Expected: FAIL on every new check (undefined functions); existing checks still pass.

- [ ] **Step 3: Implement the functions, deploy change, help and dispatch**

- [ ] **Step 4: Run to verify pass**

Run: `bash -n orkestra.sh && bash scripts/test-orkestra-helpers.sh && bash scripts/test-init-delegation.sh`
Expected: `orkestra-helpers: N passed, 0 failed`; delegation ok.

- [ ] **Step 5: Smoke on a development stack (PRs 1 and 3 merged)**

Run, from the repo root with the dev stack up:
1. `./orkestra.sh retrieval up` → seeds become healthy, are removed, then `qdrant`, `tei-embed`, `tei-rerank` healthy. A cold volume takes minutes (weights download).
2. `docker inspect -f '{{json .NetworkSettings.Networks}}' <app>-tei-embed-development | jq 'keys'` → `["<project>_retrieval-net"]`; `docker exec <app>-backend-development wget -qO- --header "Authorization: Bearer $RETRIEVAL_INFERENCE_API_KEY" http://tei-embed:80/info | jq '{model_id, model_sha}'` → `BAAI/bge-m3` and the pinned revision (proves the offline start reports the declared profile; if TEI cannot start offline with `HF_HUB_OFFLINE=1`, stop and report — never give serving containers egress).
3. Backend logs show the `llm` installation endpoints loaded and the `retrieval` module configured (no "not configured" WARN).
4. `./orkestra.sh retrieval down` → backend restarted without `QDRANT_URL`; volumes still listed by `docker volume ls`.

- [ ] **Step 6: Commit**

```bash
git add orkestra.sh scripts/test-orkestra-helpers.sh
git commit -m "feat(orkestra): retrieval overlay lifecycle and overlay-aware deploy"
```

---

### Task 5: Bench fixture, smoke fixture and metrics

**Files:**
- Create: `backend/internal/core/retrieval/bench/fixture.go`, `fixture_test.go`
- Create: `backend/internal/core/retrieval/bench/metrics.go`, `metrics_test.go`
- Create: `backend/internal/core/retrieval/bench/testdata/smoke-it-en.jsonl`

**Interfaces:**
- Produces:

```go
// One JSON object per line; "kind" selects the shape.
type FixtureItem struct {
	ID        string   `json:"id"`
	Lang      string   `json:"lang"`
	Title     string   `json:"title"`
	Text      string   `json:"text"`
	VisibleTo []string `json:"visibleTo"`
}
type FixtureQuery struct {
	ID         string   `json:"id"`
	Lang       string   `json:"lang"`
	Text       string   `json:"text"`
	Principals []string `json:"principals"`
	Expected   []string `json:"expected"` // relevant AND visible to Principals
	Hidden     []string `json:"hidden"`   // relevant but NOT visible to Principals
}
type Fixture struct {
	Items   []FixtureItem
	Queries []FixtureQuery
}
func LoadFixture(r io.Reader) (*Fixture, error)
func (f *Fixture) VisibleTo(itemID string, principals []string) bool // visibleTo ∩ principals ≠ ∅

func RecallAtK(ranked, relevant []string, k int) float64 // len(relevant) > 0
func NDCGAtK(ranked, relevant []string, k int) float64   // binary relevance, DCG = Σ rel_i / log2(i+1), i from 1
func P95(d []time.Duration) time.Duration                // nearest rank: sorted[ceil(0.95·n)−1]; 0 when empty
```

`LoadFixture` errors (each names the line): unknown `kind`; duplicate id; empty `text`; query with no principals; expected id unknown or not visible to the query's principals; hidden id unknown or visible to them.

Smoke fixture composition: 40 items — 20 `it`, 20 `en`, short professional-profile texts of a CRM (role, sector, city, skills); visibility 24 `["tenant:*"]`, 8 `["role:r-sales"]`, 8 `["user:u-2"]`; 12 queries — 6 `it`, 6 `en` — over three principal sets `P1 = ["user:u-1","tenant:*"]`, `P2 = ["user:u-1","tenant:*","role:r-sales"]`, `P3 = ["user:u-2","tenant:*"]` (four queries each); at least 4 cross-language queries (an expected item in the other language, e.g. "responsabile commerciale" ↔ "head of sales"); at least 3 queries with a non-empty `hidden`. Example lines:

```json
{"kind":"item","id":"it-001","lang":"it","title":"Responsabile commerciale Nord Italia","text":"Guida un team di dodici venditori B2B nel settore packaging, sede a Milano.","visibleTo":["tenant:*"]}
{"kind":"item","id":"en-004","lang":"en","title":"Head of Sales, EMEA","text":"Leads B2B sales for an industrial packaging group; based in Turin.","visibleTo":["role:r-sales"]}
{"kind":"query","id":"q-it-01","lang":"it","text":"chi guida le vendite nel packaging?","principals":["user:u-1","tenant:*"],"expected":["it-001"],"hidden":["en-004"]}
```

- [ ] **Step 1: Write the failing tests**

```go
func TestRecallAtK(t *testing.T)  // ([a b c d], [a d e], 3) == 1.0/3; k=4 → 2.0/3; k=10 → 2.0/3
func TestNDCGAtK(t *testing.T)    // ([a b c], [a c], 3) ≈ 0.9197 (±1e-4); ([x y], [a], 2) == 0; ([a], [a], 1) == 1
func TestP95(t *testing.T)        // 1..100 ms → 95ms; [5ms] → 5ms; nil → 0
func TestLoadFixture_Errors(t *testing.T) // table: one bad input per rule above; err message contains "line <n>"
func TestSmokeFixture_Composition(t *testing.T)
// opens testdata/smoke-it-en.jsonl; 40 items (20 it, 20 en); visibility counts 24/8/8;
// 12 queries (6 it, 6 en), 4 per principal set; ≥4 cross-language; ≥3 with hidden.
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/bench/`
Expected: FAIL — undefined functions, fixture missing.

- [ ] **Step 3: Implement the loader, metrics and write the fixture**

- [ ] **Step 4: Run to verify pass**

Run: same. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/bench/
git commit -m "feat(retrieval): benchmark fixture format, smoke fixture and metrics"
```

---

### Task 6: TEI client for the bench

**Files:**
- Create: `backend/internal/core/retrieval/bench/tei.go`, `tei_test.go`

**Interfaces:**
- Produces:

```go
type TEIClient struct {
	BaseURL string
	APIKey  string // sent as "Authorization: Bearer <key>" when non-empty
	HTTP    *http.Client
}
type TEIInfo struct {
	ModelID  string `json:"model_id"`
	ModelSHA string `json:"model_sha"`
}
func (c *TEIClient) Info(ctx context.Context) (TEIInfo, error)                                // GET /info
func (c *TEIClient) Embed(ctx context.Context, inputs []string) ([][]float32, error)          // POST /embed {"inputs":[…],"truncate":true}
func (c *TEIClient) Rerank(ctx context.Context, query string, texts []string) ([]iface.RerankScore, error) // POST /rerank {"query","texts","truncate":true}; sorted by Score desc
```

Response bodies are read through `io.LimitReader` (64 MiB); a non-2xx answer becomes `fmt.Errorf("tei %s: status %d", path, code)` (body not echoed).

- [ ] **Step 1: Write the failing tests** (`httptest.Server`)

```go
func TestTEIClient_EmbedSendsInputsAndKey(t *testing.T)   // server asserts path, method, bearer, body {"inputs":["a","b"],"truncate":true}; returns [[1,2],[3,4]] → same
func TestTEIClient_RerankSortsByScore(t *testing.T)       // server returns [{"index":0,"score":0.1},{"index":1,"score":0.9}] → [{1,0.9},{0,0.1}]
func TestTEIClient_Info(t *testing.T)                     // {"model_id":"BAAI/bge-m3","model_sha":"abc"} → TEIInfo
func TestTEIClient_ErrorStatusHidesBody(t *testing.T)     // 503 with body "secret" → error contains "503", not "secret"
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/bench/ -run TestTEIClient`
Expected: FAIL — undefined `TEIClient`.

- [ ] **Step 3: Implement**

- [ ] **Step 4: Run to verify pass**

Run: same. Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/bench/tei*.go
git commit -m "feat(retrieval): TEI client for the benchmark"
```

---

### Task 7: Bench runner and report

**Files:**
- Create: `backend/internal/core/retrieval/bench/runner.go`, `runner_test.go`
- Create: `backend/internal/core/retrieval/bench/report.go`, `report_test.go`

**Interfaces:**
- Consumes: `engine.Engine`, `engine.CollectionSpec`, `engine.Point`, `engine.Runtime`, `engine.PointID`, `engine.Query`, `engine.Weights`, `engine.Candidate`, `engine.ErrNotFound` (roadmap engine contract, PR 3); `iface.RetrievalAnalyzer`; Task 5 and 6 types.
- Produces:

```go
type Config struct {
	Engine        engine.Engine
	Embed         *TEIClient
	Rerank        *TEIClient // nil: the hybrid+rerank mode is skipped and reported as such
	TenantID      string     // default "t-bench"
	K             int        // default 10
	PerArm        int        // default 50
	RerankTopK    int        // default 20
	Repeat        int        // default 3 (latency samples per query and mode)
	BatchSize     int        // default 32
	Analyzer      iface.RetrievalAnalyzer // default {Tokenizer: "multilingual"}
	CollectionTag string     // scratch name suffix; default fmt.Sprint(time.Now().UnixNano())
}
type ModeResult struct {
	Mode                 string // "keyword" | "dense" | "hybrid" | "hybrid+rerank"
	RecallAtK, NDCGAtK   float64 // mean over queries with len(Expected) > 0
	P95                  time.Duration
	Samples              int
	VisibilityViolations int     // returned ids not visible to the query's principals
	Skipped              string  // reason when the mode did not run
}
type Report struct {
	Fixture                       string
	Items, Queries, K, Dimensions int
	EmbedModel, EmbedRevision     string
	RerankModel, RerankRevision   string
	IndexDuration                 time.Duration
	ItemsPerSecond                float64
	PeakRSSBytes                  int64 // VmHWM of /proc/self/status; 0 off Linux
	Modes                         []ModeResult
}
func (r *Report) OK() bool // no mode has VisibilityViolations
func (r *Report) WriteText(w io.Writer) error
func Run(ctx context.Context, cfg Config, fx *Fixture, fixtureName string) (*Report, error)
```

`Run`: `Info` on both TEI clients; scratch collection `bench_scratch<tag>_g1`; embed items in `BatchSize` batches (`Title + "\n" + Text`), `EnsureCollection` with the measured dimension, `Upsert` one point per item (`PointID(TenantID, "scratch<tag>", id, 0)`, `Runtime{TenantID, SourceID: id, SourceRevision: 1, ChunkID: id + "#0", VisibleTo, ContentHash: sha256 hex of the text, IngestionProfile: "bench", CharEnd: rune count}`); `IndexDuration` covers embed + upsert. Modes, each `Repeat` times per query: `keyword` = `Query{Keyword, Weights{Keyword: 1}}`; `dense` = `Query{Dense, Weights{Dense: 1}}`; `hybrid` = both, `Weights{1, 1}`; `hybrid+rerank` = hybrid with `Limit: RerankTopK`, rerank the candidates' `Text`, keep the top `K`. Every query sets `TenantID`, the fixture principals, `PerArm`, `Limit` (`K` or `RerankTopK`). Latency covers query embedding + engine + rerank. Cleanup: `defer` `DropCollection` with `context.WithoutCancel(ctx)` bounded by 30 s, so a cancelled run still drops.

- [ ] **Step 1: Write the failing tests** (in-memory fake `engine.Engine` in `runner_test.go` that stores points, filters by `TenantID` and principals, returns keyword hits by token overlap and dense hits by dot product; `httptest` TEI returning deterministic vectors)

```go
func TestRun_ModesAndCleanup(t *testing.T)            // Modes == [keyword dense hybrid hybrid+rerank]; one EnsureCollection with Dimensions == fake TEI dimension; one DropCollection, same name; every Query: TenantID "t-bench", Principals == fixture query principals
func TestRun_DropsCollectionOnError(t *testing.T)     // rerank server answers 500 → Run errors; DropCollection called
func TestRun_DropsCollectionOnCancel(t *testing.T)    // fake Query blocks until ctx done; cancel → Run returns ctx error; DropCollection called once
func TestRun_CountsVisibilityViolations(t *testing.T) // fake engine ignoring principals returns a hidden item → that mode's VisibilityViolations > 0; Report.OK() == false
func TestRun_RerankReordersHybrid(t *testing.T)       // rerank server scores inversely → hybrid+rerank top-K order is the reverse of hybrid's for that query
func TestRun_NoRerankClientSkipsMode(t *testing.T)    // Rerank nil → hybrid+rerank Skipped != "", OK() still true
func TestReport_WriteText(t *testing.T)               // output contains "recall@10", "ndcg@10", "p95", the four mode names, items/s and the model ids
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./internal/core/retrieval/bench/ -run 'TestRun|TestReport'`
Expected: FAIL — undefined `Run`.

- [ ] **Step 3: Implement `Run`, `Report`, `WriteText`**

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test -race ./internal/core/retrieval/bench/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/core/retrieval/bench/
git commit -m "feat(retrieval): benchmark runner for keyword, dense, hybrid and rerank"
```

---

### Task 8: `cmd/retrieval-bench`, `./orkestra.sh retrieval bench`, `make retrieval-bench`

**Files:**
- Create: `backend/cmd/retrieval-bench/main.go`, `main_test.go`
- Modify: `orkestra.sh` (`retrieval_bench`, dispatch, help), `scripts/test-orkestra-helpers.sh`
- Modify: `Makefile` (new `retrieval-bench` target, `.PHONY`, `help` line)

**Interfaces:**
- Consumes: `bench.LoadFixture`, `bench.Run`, `bench.TEIClient` (Tasks 5–7); the PR 3 Qdrant driver constructor in `retrieval/engine/qdrant` (not named by the roadmap — wrap it in one function here).
- Produces:

```go
func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr)) }
// run parses flags and env, runs the bench, prints the report; 0 ok, 1 run error or !Report.OK(), 2 usage.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int
// resolveEndpoint returns the url and API key of the named endpoint in RETRIEVAL_INFERENCE_ENDPOINTS:
// the key is getenv(entry.apiKeyEnv) when set, else RETRIEVAL_INFERENCE_API_KEY.
func resolveEndpoint(endpointsJSON, name string, getenv func(string) string) (url, apiKey string, err error)
func newEngine(url, apiKey string) (engine.Engine, error) // the only call into the PR 3 driver
```

Flags: `-fixture` (required), `-k 10`, `-per-arm 50`, `-rerank-top 20`, `-repeat 3`, `-batch 32`, `-tenant t-bench`, `-embed-endpoint embed`, `-rerank-endpoint rerank` (empty = skip rerank), `-embed-url`, `-rerank-url` (override the endpoint list; the key then comes from `RETRIEVAL_INFERENCE_API_KEY`), `-json <path>` (also write the report as JSON). Env: `QDRANT_URL`, `QDRANT_API_KEY`, `RETRIEVAL_INFERENCE_ENDPOINTS`, `RETRIEVAL_INFERENCE_API_KEY`. `-embed-url` is how smaller embedding models are compared (D14): start another TEI on `retrieval-net` and point at it.

`orkestra.sh`: `retrieval_bench "$@"` — `retrieval_init_env`; `ENV=production` → die `"The benchmark does not run in production (no Go toolchain in the image)."`; `retrieval_project_active` false → die `"Start the overlay first: ./orkestra.sh retrieval up"`; then `stack_compose_files` and `docker compose "${STACK_COMPOSE_FILES[@]}" --env-file "$ENV_FILE" exec -T backend go run ./cmd/retrieval-bench -fixture internal/core/retrieval/bench/testdata/smoke-it-en.jsonl "$@"` (later flags override the fixture). Root `Makefile`: `retrieval-bench:` → `@./orkestra.sh retrieval bench $(RETRIEVAL_BENCH_ARGS)` with a comment that it needs the overlay and a dev or staging stack.

- [ ] **Step 1: Write the failing tests**

```go
func TestRun_UsageErrors(t *testing.T)         // no -fixture → 2, stderr mentions -fixture; unknown flag → 2
func TestRun_NeedsQdrant(t *testing.T)         // QDRANT_URL "" → 1, stderr "QDRANT_URL is not set — start the overlay (./orkestra.sh retrieval up)"
func TestResolveEndpoint(t *testing.T)         // roadmap JSON: "embed" → http://tei-embed:80 + shared key; entry with apiKeyEnv "K2" → getenv("K2"); unknown name → error naming it; malformed JSON → error
```

Helper suite:

```bash
check "retrieval bench refuses production"            "yes" …  # ENV=production → non-zero, no 'exec' line
check "retrieval bench needs the overlay"             "yes" …  # retrieval_project_active() { return 1; } → non-zero, no 'exec' line
check "retrieval bench runs in the backend container" "yes" …  # 'compose .* -f .*docker-compose.retrieval.yml .* exec -T backend go run ./cmd/retrieval-bench -fixture internal/core/retrieval/bench/testdata/smoke-it-en.jsonl -k 5$' for args "-k 5"
```

- [ ] **Step 2: Run to verify failure**

Run: `cd backend && go test ./cmd/retrieval-bench/ && cd .. && bash scripts/test-orkestra-helpers.sh`
Expected: FAIL — package and function missing.

- [ ] **Step 3: Implement the command, the `orkestra.sh` subcommand and the make target**

- [ ] **Step 4: Run to verify pass**

Run: same as Step 2. Expected: PASS.

- [ ] **Step 5: Run the smoke benchmark (PR 5 merged, overlay up)**

Run: `make retrieval-bench RETRIEVAL_BENCH_ARGS="-json /tmp/retrieval-bench.json"`
Expected: exit 0; four mode rows; `VisibilityViolations` 0 everywhere; models `BAAI/bge-m3` / `BAAI/bge-reranker-v2-m3` with the pinned revisions; `docker compose … exec backend ls /tmp/retrieval-bench.json` exists. Keep the printed table for Task 9.

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/retrieval-bench/ orkestra.sh scripts/test-orkestra-helpers.sh Makefile
git commit -m "feat(retrieval): retrieval-bench command and make target"
```

---

### Task 9: Measure, document, full gate

**Files:**
- Create: `docs/site/operating/retrieval-overlay.mdx` (`sidebar_position: 6`)
- Modify: `docs/site/operating/index.mdx` (Pages list), `docs/site/operating/deployment/index.mdx:29` (compose-file table row), `docs/site/modules/core/retrieval.mdx` (link to the operating page)
- Modify: `docker/AGENTS.md` (new "Retrieval overlay (docker-compose.retrieval.yml)" after the OTEL section ~L805; "`retrieval-net`" under Network Architecture ~L397; env table rows), `scripts/AGENTS.md` (env-validate bullet ~L87), `AGENTS.md:135` and `backend/AGENTS.md:414` (overlay list), `backend/internal/core/retrieval/AGENTS.md` (deployment variables and the bench)

- [ ] **Step 1: Measure on the reference CPU host**

The reference CPU host is the maintainers' development VM (decided on 2026-10-26): 16 vCPU Intel Xeon Gold 6130 with AVX-512, 30 GB RAM, no GPU. Before enabling the overlay there, check `df -h /var/lib/docker`: the TEI images plus the two models' weights need several GB, so make room first and record the free space before and after. With the overlay up on that host (record `lscpu | grep -E 'Model name|^CPU\(s\)|Flags' | grep -o 'Model name.*\|CPU(s).*\|avx512f'` and `free -g` in the page, as the host description):
- idle: `./orkestra.sh retrieval status` → MemUsage of qdrant, tei-embed, tei-rerank;
- under load: run `make retrieval-bench` and, during the query phase, `docker stats --no-stream <app>-qdrant-<env> <app>-tei-embed-<env> <app>-tei-rerank-<env>`;
- disk: `docker system df -v | grep -E 'qdrant-data|tei-weights'` and `docker image ls --digests | grep -E 'qdrant|text-embeddings-inference'`;
- the smoke bench table and `ItemsPerSecond`, `PeakRSSBytes`.

- [ ] **Step 2: Write `retrieval-overlay.mdx`**

Sections: what the overlay runs and why (D3, D4, D9 — one engine per stack, local inference, internal network only); enabling (`make init` keys, `./orkestra.sh retrieval up`, what the backend gets); opting out (empty keys); pointing at external services (GPU host or existing Qdrant via `.env`, TLS and an honest `boundary`); **Operating**: sizing table from Step 1 (RAM idle/under load per container, disk per volume and image, host description, date) with the note that the figures come from the synthetic smoke fixture and the fork's representative benchmark decides defaults (D14); weight pre-seeding for offline hosts (seed on a connected machine with `./orkestra.sh retrieval up`, export `tei-weights` with `docker run --rm -v <project>_tei-weights:/data -v "$PWD":/out alpine tar czf /out/tei-weights.tgz -C /data .`, import on the offline host into the same volume name, set `RETRIEVAL_SKIP_SEED=true`); **Model inventory** table (component, image tag + digest or model id + revision, license: Qdrant Apache-2.0, TEI Apache-2.0, `BAAI/bge-m3` MIT, `BAAI/bge-reranker-v2-m3` Apache-2.0) and how to bump a pin (both places, hardening test); **Benchmark** (`make retrieval-bench`, flags, reading the report, `-embed-url` for smaller models, the representative corpus stays in the fork); troubleshooting (seed timeout, unhealthy offline start, `retrieval.not_configured`).

- [ ] **Step 3: Update the AGENTS.md files and the other docs pages**

`docker/AGENTS.md`: overlay purpose, services/volumes/networks, seed-vs-serve egress rule, CLI, the hardening test and what it pins, the variables, "dev-slot wiring lives in the octolabs-worktree kit (out of scope here)". `scripts/AGENTS.md`: the new env-validate rules. Root and `backend/AGENTS.md`: "plus opt-in `docker-compose.observability.yml` and `docker-compose.retrieval.yml` overlays". `retrieval/AGENTS.md`: env variables and where they come from, bench package and command.

- [ ] **Step 4: Run the full gates**

Run: `MONGO_TEST_URI=<replica-set uri> QDRANT_TEST_URL=<url> QDRANT_TEST_API_KEY=<key> make ci-backend && bash scripts/test-orkestra-helpers.sh && make agents-check`
Expected: `Backend CI: OK` (it now includes `backend-retrieval-hardening` and the extended script tests), helper suite 0 failed, agents check OK. Render the docs site locally per `docs/site/README.md` and open the new page.

- [ ] **Step 5: Commit**

```bash
git add docs/site/ docker/AGENTS.md scripts/AGENTS.md AGENTS.md backend/AGENTS.md backend/internal/core/retrieval/AGENTS.md
git commit -m "docs(retrieval): operating guide, sizing, model inventory and benchmark"
```
