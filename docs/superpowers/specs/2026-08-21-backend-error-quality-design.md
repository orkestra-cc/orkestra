# Backend error quality: an analyzer-first sweep

**Date:** 2026-08-21
**Status:** Approved — ready for implementation planning
**Area:** `backend/tools/errquality/` (new), `backend/internal/shared/errcode/`, `backend/internal/core/**/handlers/`, root `Makefile`, `frontend-admin/src/locales/`

## Problem

On 2026-08-21 the commons dev stack lost its JWT keys (the `backend/keys` mountpoint was
deleted from the host, so `docker/keys` no longer projected into `/app/keys`). Every
operator login then answered:

```
POST /v1/auth/operator/login  →  400
{"title":"Bad Request","status":400,"detail":"Request failed"}
```

The message is worse than unhelpful — it is *wrong about whose fault it is*. A server
that cannot load its signing keys reported a **client** error, indistinguishable from a
mistyped password. The operator had no way to tell the two apart from the response, and
the real cause was only visible in `docker logs`.

The mechanism is one line: `mapPasswordError` in
`internal/core/auth/handlers/password_handler.go` maps a dozen sentinels precisely
(`ErrInvalidCredentials` → 401 "Invalid email or password", `ErrNotificationDown` → 503
with an actionable message) and then collapses **everything unmapped** into
`huma.Error400BadRequest("Request failed")`. `services.ErrJWTKeysNotLoaded` already
exists as a typed sentinel in `internal/core/auth/services/jwt_service.go`; it simply is
not in the switch.

That single site is a symptom. A census of the backend found:

| Mechanism | Adoption |
| --- | --- |
| `errcode.Forbidden(code, detail)` / `.Conflict(…)` — stable machine code, 74-code catalog, golden test in CI, translated by the SPA via `errors.<code>` | 23 files |
| `internal/shared/errors` — the `AppError` / `ErrorBuilder` / `Manager` framework | 15 files, nearly all middleware — effectively dormant |
| Hand-written `huma.ErrorNNN("…")` | **842 call sites** (329 core, 499 addons, 14 shared/SDK) |
| `codedError` — a private struct duplicating what `errcode` already provides | 1 file (`password_handler.go`, the file above) |

Within the 329 core sites, **24** pass `err.Error()` straight to the client (unreadable
*and* a disclosure risk) and **36** use a semantically empty detail. So the convention
does not need inventing — it exists twice and is followed almost nowhere.

## Goals

- Every error the backend returns to a client is **honest**: a real sentence, and a
  status code from the right category (a server fault is never a 4xx).
- New code cannot reintroduce the defect: a **CI gate lands before any message is
  touched**, so the floor only ever rises.
- The gate's remedy is the convention that already exists (`errcode`), not a third one.

## Non-goals

- **The 499 addon call sites.** They are `Prop: addon` (commons-only) while core is
  `Prop: upstream`; ADR-0010 allows one category per commit, so they are a separate
  round, sequenced after this one.
- **A stable code for every error.** Codes are added where the SPA must discriminate a
  case, not as a blanket requirement — naming ~800 codes and their i18n keys is work
  without a demonstrated consumer.
- **Retiring `internal/shared/errors`.** It stays where it is. This design does settle,
  implicitly, that `errcode` is the convention and `AppError` is legacy; converting the
  15 dormant files is not part of this work.

## The rule

An error returned to a client satisfies all of:

1. **No `err.Error()` on the wire.** The underlying error is logged server-side; the
   client gets a sentence written for a human.
2. **No semantically empty detail.** "Request failed", "Internal server error", "Failed",
   "Something went wrong" and their kin say nothing the status code did not.
3. **Honest category.** An unrecognized error is a server fault (5xx), not a client one.
   A misconfiguration is 503 with an actionable sentence, following the
   `ErrNotificationDown` precedent already in the auth handler.
4. **A code where the SPA discriminates.** When the frontend must branch on the case
   (not merely display it), the response carries an `errcode` constant and the SPA
   translates it via `errors.<code>`.

## Component 1 — `backend/tools/errquality`

Layout mirrors `tools/tenantscope` exactly, because that analyzer already solved the
same problems (scoping, opt-out, baseline, CI wiring) in this repo:

```
backend/tools/errquality/
├── CLAUDE.md          # what it enforces and why, for future contributors
├── analyzer.go        # the three rules
├── analyzer_test.go   # synthetic sources parsed per rule, allow-comments included
├── baseline.txt       # pre-existing violations, frozen
└── cmd/errquality/    # main, -baseline= and -write-baseline flags
```

### The three rules

**R1 — no raw error text.** Report any client-facing error constructor
(`huma.ErrorNNN(…)`, `errcode.*(…)`) whose detail argument is `err.Error()`, or a
`fmt.Sprintf`/concatenation that embeds an `error`-typed value. AST shape: call
expression → argument is either a `CallExpr` selecting `.Error()` on an `error`, or a
formatting call with such a value among its args.

**R2 — no empty detail.** Report a string-literal detail matching a denylist
(case-insensitive, whole-string): `Request failed`, `Internal server error`,
`Internal error`, `Error`, `Failed`, `Operation failed`, `Invalid request`,
`Bad request`, `Something went wrong`. The list lives in one exported var so adding a
phrase is a one-line change with a test.

**R3 — no 4xx from a fallback.** Report a 4xx constructor returned from the `default:`
clause of a tagless `switch` whose other clauses call `errors.Is(err, …)`. This is the
narrow AST shape of Go's idiomatic error-mapping function, and it is exactly what
`mapPasswordError` is. R3 is the rule that would have failed CI on
`password_handler.go:487`.

### Opt-out

`//errquality:allow <reason>` on the line above the call, matching `tenantscope:allow`
semantics: it suppresses the report and marks the site as a review checkpoint. Legitimate
uses exist — e.g. surfacing a validation library's own message, which *is* the
human-readable sentence.

### Baseline

`-write-baseline` emits `path:line: rule` for every current violation; CI fails only on
violations **absent** from the file. New code is therefore clean from day one while the
~60 existing core sites wait their turn. Deleting a line from `baseline.txt` is the
definition of progress, and it is countable.

### CI wiring

A `backend-errquality` target in the root `Makefile`, added to the `ci-backend` chain
beside `backend-tenantscope`:

```make
backend-errquality:
	@cd backend && go test ./tools/errquality/...
	@cd backend && go run ./tools/errquality/cmd/errquality \
	  -baseline=tools/errquality/baseline.txt ./internal/...
```

Scope is `./internal/...` from the start — including addons — so addon code is *frozen*
at today's level by the baseline even though its burn-down is a later round.

## Component 2 — 5xx constructors in `errcode`

`internal/shared/errcode/errcode.go` exposes constructors for 4xx only
(`BadRequest` … `UnprocessableEntity`). R3's remedy needs the other half:

```go
func ServiceUnavailable(code, detail string) *Error // 503
func Internal(code, detail string) *Error           // 500
```

plus the first two catalog entries they serve, following the naming rule already stated
in `codes.go` (`<module>.<situation>`, snake_case):

- `auth.jwt_not_configured` — signing keys unreadable; authentication cannot run. 503.
- `auth.unavailable` — the honest fallback for `mapPasswordError`'s `default:`. 500.

`codes_test.go` pins both against its golden snapshot, as it does for the existing 74.

## Component 3 — the burn-down

One module per commit, every commit `Prop: upstream`, in this order:

| # | Module | R1 (raw `err.Error()`) | R2 (empty detail) | Total |
| --- | --- | --- | --- | --- |
| 1 | `auth` | 1 | 17 | 18 |
| 2 | `tenant` | 18 | 0 | 18 |
| 3 | `authz` | 5 | 0 | 5 |
| 4 | `user` | 0 | 19 | 19 |

That is the whole core burn-down: **60 sites across 4 modules**. `compliance`,
`notification`, `logging` and `navigation` are already clean under the rule — the last
two have no hand-written `huma.ErrorNNN` sites at all, having moved to `errcode`
already, which is the end state this design generalizes.

`auth` is first for two reasons: it carries the incident (the 503 case, the `default:`
category fix, and deleting the private `codedError` in favour of `errcode`), and the
analyzer-first sequence means the login path keeps misreporting configuration faults
until that commit lands.

### Contract effects each commit must carry

- **OpenAPI.** Changed statuses alter the published contract: regenerate with
  `make openapi-dump`, never hand-edit `backend/openapi/enterprise.json`.
- **i18n.** Every new code gets `errors.<code>` in `frontend-admin/src/locales/{en,it}.json`.
  The SPA already falls back to `detail` for unknown codes, so a missing key degrades to
  the English sentence rather than a raw key path.

## Testing

- **Analyzer:** `analyzer_test.go` parses synthetic sources per rule — a positive and a
  negative case each, plus one allow-comment suppression and one baselined violation —
  following the emulated-`Pass` approach `tenantscope/analyzer_test.go` uses, so no
  `analysistest` testdata module is needed.
- **Burn-down:** each commit extends the module's existing handler tests.
  `internal/core/auth/handlers/error_mapping_test.go` already exists and gains a case per
  newly mapped sentinel, asserting status **and** code.
- **Gate:** `make ci-backend` is the acceptance check for every commit.

## Risks

- **Status changes are observable.** 400 → 503/500 changes what clients see. Mitigated by
  burning down one module per commit with regenerated OpenAPI, never a global rewrite.
- **R3's shape is heuristic.** A mapping function written differently (if/else chains,
  a map lookup) will not be caught. Accepted: R3 targets the idiom this codebase actually
  uses, and R1/R2 catch the resulting messages anyway.
- **Baseline rot.** A baseline nobody burns down becomes permanent. Mitigated by the
  per-module commits above being part of this work, not a follow-up.

## Definition of done

- `make ci-backend` runs `backend-errquality` and fails on any violation outside the
  baseline.
- `baseline.txt` contains zero entries under `internal/core/`.
- A login attempt against a backend with unreadable keys answers **503** with
  `auth.jwt_not_configured` and a sentence naming the cause.
- The addon entries remain in `baseline.txt`, explicitly deferred to the `Prop: addon`
  round.
