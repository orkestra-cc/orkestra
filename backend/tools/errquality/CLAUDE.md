# Tool: errquality

_Path: `/backend/tools/errquality`_
_Parent: [../../CLAUDE.md](../../CLAUDE.md)_

You are probably here because `make ci-backend` (or `make backend-errquality`
on its own) just failed and printed something like:

```
internal/core/user/handlers/user_handler.go:750:49: [R1] the underlying error's text reaches the client — log err, return a written sentence
```

This page tells you what that means and exactly what to change.

## What it does

Static analyzer, modelled on `tools/tenantscope`, that keeps client-facing
error responses honest. It walks every `internal/` package looking for calls
that build an HTTP error response — `huma.ErrorNNNXxx(detail, ...)` and the
`errcode.<Status>(code, detail)` / `errcode.New(status, code, detail)`
builders — and reports three defects at the call site.

The rule this analyzer exists to enforce was violated on 2026-08-21: the
commons dev stack lost its JWT signing keys, and every operator login
answered `400 {"detail":"Request failed"}` — a server fault, reported as if
the caller had mistyped a password. See
[the design doc](../../../docs/superpowers/specs/2026-08-21-backend-error-quality-design.md)
for the full incident writeup. The analyzer's `URL` field points at
`backend/CLAUDE.md#error-code-contract` — the doc section that names this
gate for the backend as a whole.

## The three rules

**R1 — the detail is the underlying error's text.** The call passes
`err.Error()` (bare, or embedded in `fmt.Sprintf`/similar) as the detail
argument. That text was written for a log line, not a person: it can leak
internal identifiers, driver error strings, file paths — and it usually
reads like nonsense to whoever is staring at the SPA.

```go
// R1
return nil, huma.Error500InternalServerError(err.Error())
```

**R2 — the detail is semantically empty.** The detail string, once
lower-cased and trimmed of surrounding whitespace and trailing punctuation,
exactly matches an entry on a small denylist of phrases that repeat the
status code and say nothing else: `"request failed"`, `"internal server
error"`, `"internal error"`, `"error"`, `"failed"`, `"operation failed"`,
`"invalid request"`, `"bad request"`, `"something went wrong"`,
`"unexpected error"`. (See `emptyDetails` in `analyzer.go` for the exact
list — it's deliberately small and literal, not a fuzzy-match; a detail that
isn't on the list passes R2 even if a human would still call it vague.)

```go
// R2
return nil, huma.Error400BadRequest("Request failed")
```

**R3 — a 4xx returned from the `default:` of a tagless `errors.Is` switch.**
The idiom this catches is an error mapper: a `switch { case errors.Is(err,
ErrX): ...; case errors.Is(err, ErrY): ...; default: return
huma.Error400BadRequest(...) }`. Reaching `default:` means none of the named
sentinels matched — the handler could not identify what went wrong. An
error the handler cannot name is a server fault, not the caller's; a 4xx
there blames whoever is holding the phone, and an operator ends up hunting
in the wrong place (this is the rule that would have caught the 2026-08-21
incident — the unmapped `ErrJWTKeysNotLoaded` sentinel fell through to
`default:` and came back as 400).

```go
switch {
case errors.Is(err, ErrInvalidCredentials):
	return nil, huma.Error401Unauthorized("Invalid email or password")
default:
	return nil, huma.Error400BadRequest("Request failed") // R3 (and R2)
}
```

R3 only judges what the `default:` branch **literally constructs and
returns** — `return huma.Error400BadRequest(...)`. It does not trace a
value bound earlier and returned by name (`x := huma.Error400BadRequest(...);
return x`) or a naked `return` of a named result. That gap is a deliberate,
documented decision (see the long comment above `inspectFile`'s switch walk
in `analyzer.go`), not an oversight — an earlier revision tried to resolve
identifiers and it produced false positives from Go's block scoping. Nothing
in this codebase uses either escaping shape today. If you introduce one,
R1/R2 still judge the constructor's own message wherever it's written, but
R3 will not flag the `default:` branch — write the 5xx by hand and be
skeptical of a mapper that needs the indirection at all.

A nested `switch`/type-`switch` inside a `default:` clause judges its own
`default:` independently — it is not treated as part of the outer mapper's
fallback.

## How to fix a finding — the decision table

Apply this at the specific call site the diagnostic points at. This table
is reproduced verbatim from **Global Constraints** in the implementation
plan (`docs/superpowers/plans/2026-08-21-backend-error-quality.md`) — the
burn-down tasks (Tasks 7–10) apply it mechanically, and so should you.

| Situation at the site | Fix |
| --- | --- |
| The caller did something the caller can correct | 4xx, detail naming the field or the rule that was broken |
| A dependency or configuration is absent or down | `errcode.ServiceUnavailable(code, …)`, detail naming which one and who fixes it |
| The handler cannot name the error at all | `errcode.Internal(code, …)`, detail saying what operation failed; log the cause with `slog` |
| The detail was `err.Error()` | Move the error into the existing `slog` call (add one if absent), replace the detail with a written sentence |
| The SPA must branch on the case, not merely display it | Add an `errcode` const + a `codes_test.go` golden row + `errors.<code>` in both locale files |

Concretely, for each rule:

- **R1** almost always resolves to the fourth row: `slog.ErrorContext(ctx,
  "what failed", "err", err)` (or fold `err` into an existing log call
  already at the site) and replace the detail argument with a sentence a
  person can act on.
- **R2** resolves to the first three rows depending on *whose* fault the
  situation actually is — the phrase was hiding that judgment, not making
  it. Don't reach for a marginally-less-generic synonym; name the actual
  field, dependency, or failure.
- **R3** resolves to row two or three: the `default:` branch of an
  `errors.Is` mapper is, by construction, the "handler cannot name the
  error" case. Return `errcode.Internal` (the handler has no idea what
  happened) or `errcode.ServiceUnavailable` (the handler knows *what* is
  down, just not why) — never a 4xx. `errcode.Internal` / `.ServiceUnavailable`
  are the two 5xx builders added in `internal/shared/errcode/errcode.go`
  specifically to give R3 fixes somewhere to land.

New `errcode` consts follow `<module>.<situation>` snake_case in
`internal/shared/errcode/codes.go`, and every new const needs a matching row
in `codes_test.go`'s `goldenCodes` map in the *same* commit — that golden
test fails CI independently of this analyzer.

## When `//errquality:allow` is legitimate

Put the comment on the line directly above the flagged call, with a reason
of at least 5 characters after the marker:

```go
//errquality:allow surfaces the validation library's own message, which is
// already written for a human and does not leak internals
return nil, huma.Error400BadRequest(err.Error())
```

A bare `//errquality:allow` with no reason (or fewer than 5 characters of
one) does **not** suppress — the analyzer requires the reason to exist, not
just the marker.

This is an escape hatch for the rare case where the flagged pattern is
actually correct, not a shortcut for "I don't want to fix this right now"
(that's what the baseline is for — see below). Legitimate cases are narrow:
the underlying error genuinely *is* a hand-written, human-safe sentence that
merely happens to reach the client via `err.Error()` (e.g. a validation
library that constructs its own user-facing message and returns it as the
error), or an `errors.Is` `default:` branch that has an independently
reasoned-through reason to return a 4xx that isn't captured by the rule's
general case. If you're reaching for `//errquality:allow` because fixing the
site properly is inconvenient right now, use the baseline instead and file
it as backlog — don't allow-comment your way around the rule.

No production code in this tree uses the marker yet; the burn-down tasks
(7–10) fix sites rather than exempt them.

## The baseline

`baseline.txt` holds one `relpath:line:rule` entry per line for every
violation that existed under `internal/` the day the gate landed
(2026-08-21). CI fails on any violation the analyzer finds that is **not**
in this file and **not** covered by an allow-comment — so the floor is
frozen at today's backlog and can only shrink.

`internal/core/` is the part of the backlog this plan commits to clearing —
Tasks 7–10 burn it down module by module (auth, tenant, authz, user), and
each burn-down commit **deletes** that module's lines from `baseline.txt` as
part of fixing the sites, not as a separate cleanup step.
`internal/addons/` is frozen here on purpose: it is `Prop: addon`
(commons-only) while this gate is `Prop: upstream`, so it is out of scope
for this round and burns down later, separately.

Line numbers drift whenever a file above a flagged line is edited. If a
rebase leaves stale entries (lines that no longer point at a real
violation, or a shifted line number), regenerate the whole file:

```bash
cd backend
go run ./tools/errquality/cmd/errquality ./internal/... 2>&1 \
  | sed -n 's#^.*/\(internal/[^:]*\):\([0-9]*\):[0-9]*: \[\([A-Z0-9]*\)\].*#\1:\2:\3#p' \
  | sort -u > tools/errquality/baseline.txt
```

then restore the header comment block at the top of the file (it is not
produced by the pipeline above).

**The one forbidden move: never regenerate the baseline to make a NEW
finding disappear.** The whole point of this gate is that new code is held
to the rule unconditionally. If `make backend-errquality` fails on a site
you just wrote or touched, fix that site using the decision table above (or
add a genuine `//errquality:allow`, per the section above). Regenerating the
baseline is only for the mechanical line-number drift that happens when
unrelated edits shift a still-real, still-baselined violation up or down in
its file — never to launder a finding on code that did not have one before.

## Running locally

```bash
cd backend

# Unit tests (parse synthetic sources, assert findings per rule):
go test ./tools/errquality/...

# Same invocation CI / make uses:
go run ./tools/errquality/cmd/errquality \
    -baseline=tools/errquality/baseline.txt ./internal/...

# Or both, the way `make backend-errquality` runs them:
cd .. && make backend-errquality
```

No output and exit 0 means clean. A non-empty baseline path that doesn't
exist is a hard error (`errquality: open baseline <path>: ...`), not a
silent pass-through — don't typo the flag.

## CI wiring

`backend-errquality` is a target in the root `Makefile`, listed in
`ci-backend` right after `backend-tenantscope`. `.github/workflows/backend.yml`
does not call this tool directly — it runs `make ci-backend`, which is the
single source of truth both locally and in CI, so there is nothing to keep
in sync between a workflow file and a Makefile target.

## Files

| File | Purpose |
|---|---|
| `analyzer.go` | The three rules, baseline loading, and allow-comment suppression. Exposes the `go/analysis` `Analyzer` and the pure `inspectFile` that tests call directly. |
| `analyzer_test.go` | Unit tests against inline Go source fixtures — no `analysistest`, no testdata module. |
| `baseline.txt` | Accepted historical drift; shrinks as `internal/core/` is burned down. |
| `cmd/errquality/main.go` | `singlechecker` CLI wrapper. |
| `CLAUDE.md` | This file. |

## Related

- [Design doc — the 2026-08-21 incident and the census](../../../docs/superpowers/specs/2026-08-21-backend-error-quality-design.md)
- [Implementation plan — Global Constraints (source of the decision table) and the task-by-task burn-down](../../../docs/superpowers/plans/2026-08-21-backend-error-quality.md)
- [`../../CLAUDE.md`](../../CLAUDE.md) — "Error-code contract" section, where this gate is documented for the backend as a whole
- [`internal/shared/errcode/`](../../internal/shared/errcode/) — the `New`/`BadRequest`/.../`ServiceUnavailable`/`Internal` builders R3 fixes land on
- [`../tenantscope/CLAUDE.md`](../tenantscope/CLAUDE.md) — the sibling analyzer this tool's layout and baseline mechanism are modelled on
