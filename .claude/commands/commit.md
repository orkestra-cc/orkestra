---
allowed-tools: Bash, Read, Edit, Glob, Grep
description: Update affected docs, then stage and commit with a conventional message
---

The point of this command is doc hygiene. Code-doc drift is a
regression, even if the build is green.

## 1. Read the change

- `git status` (no `-uall`)
- `git diff` for staged + unstaged changes
- `git log --oneline -10` to match the repo's scope-prefix style

## 2. Doc-impact pass (mandatory, blocking)

For every changed file, walk up its directory tree and collect every
`CLAUDE.md` and `README.md` until the repo root. Pull in any
`docs/*.md` cross-referenced from those files. List the full doc set
before deciding anything.

For each doc, look for references to:

- File paths or function/type/method names that were renamed or removed
- Endpoint tables (HTTP method + path)
- Env-var tables, `ConfigSchema` keys, default values
- Module `Dependencies()`, `RequiredServices`, `OptionalServices`
- ServiceRegistry keys, cross-module interfaces
- MongoDB collections, indexes, TTLs
- Permission catalogs, role gates, RBAC rules
- Embedded counts ("4 core modules", "12 addons") — easy to drift
- "Key invariants" / "Lifecycle" sections
- Code blocks that mirror real source

For each doc, print exactly one of:

- `doc impact: <path> — updating because <reason>`
- `no doc impact — change is <pure refactor / dead-code removal /
  internal test helper / bug fix restoring documented behavior /
  formatting>`

Silent skips are not allowed.

## 3. Update docs in the same commit

CLAUDE.md is a snapshot of current state, never a changelog. Do not
add "Recent changes" sections, "as of <date>" notes, or narrate the
diff — the git history is the changelog. Edit the documented shape so
it matches the new code.

For new surface (endpoint, collection, env var, service key,
permission), add a row to the relevant table and match the surrounding
formatting. After editing, re-read the doc top-to-bottom and verify
internal consistency: no stale forward references, no broken links,
counts still match.

## 4. Stage by explicit path

Never `git add -A` / `git add .` — root CLAUDE.md forbids it because
of secret/binary leakage risk. Stage docs and code together by name.

If untracked files exist, list them and ask which to include before
staging. If the diff spans unrelated areas, propose splitting into
multiple commits before staging anything.

## 5. Conventional commit message

`<type>(<scope>): <subject>` — imperative mood, ≤72 chars (matches the
repo's actual convention; 50 is too tight for this codebase). Scope
should match an existing scope from the last 10 commits unless the
change introduces a new module.

Body: what and why, wrapped at 72 chars, blank line after the subject.
Mention any docs that were updated alongside the code. Omit footer
unless there are breaking changes or issue refs.

Types: feat, fix, docs, style, refactor, perf, test, chore, ci.

Multi-line messages must use HEREDOC so newlines survive:

```
git commit -m "$(cat <<'EOF'
<subject>

<body>
EOF
)"
```

## 6. Confirm, commit, follow up

Show the proposed message and the staged file list. Wait for the
user's confirmation before running `git commit`.

If a pre-commit hook fails, fix the underlying issue and create a NEW
commit. Never `--amend` — the failed commit didn't happen, and
amending would mutate the previous good commit.

After committing, check `git branch --show-current`. If not on
`main`/`master`, remind the user to `git push` (note `-u origin <branch>`
if no upstream is configured).
