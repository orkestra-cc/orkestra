# Contributing to Orkestra

Thanks for considering a contribution. Orkestra is a Go + React + Flutter monorepo, but **you only need the toolchains for the surface you're touching** — a backend-only PR doesn't require Node, a frontend-only PR doesn't require Go.

## Quick start

```bash
# 1. Install mise — a single static binary that manages every toolchain.
curl https://mise.run | sh && exec $SHELL

# 2. Provision the languages at the versions pinned in .mise.toml
mise install                # Go 1.25.10, Node 22, Flutter 3.35, golangci-lint, pre-commit, ...

# 3. Bootstrap dependencies for the surface(s) you'll touch
make install                # everything; or scope manually:
                            #   cd backend         && go mod download
                            #   cd frontend-admin  && npm ci
                            #   cd frontend-client && npm ci
                            #   cd mobile          && flutter pub get

# 4. Install git hooks (auto-format on commit, run CI on push)
pre-commit install --install-hooks
```

Optional but recommended — make `git blame` skip the line-ending-normalization commit:

```bash
git config blame.ignoreRevsFile .git-blame-ignore-revs
```

## Before you push

```bash
make ci      # auto-detects which surfaces you changed; runs only those checks
```

The same `make` targets are what GitHub Actions runs. If `make ci` is green locally, CI will be green.

| Command | What it runs |
|---------|--------------|
| `make ci` | CI for changed surfaces (default base: `origin/dev`; override with `BASE_REF=origin/main`) |
| `make ci-all` | Every surface — what CI does on `dev`/`main` |
| `make ci-backend` | Backend: lint, tenant-scope, policy-coverage, vuln, tests, enterprise build |
| `make ci-backend-matrix` | Backend + all 6 build profiles (slow; matches the CI matrix) |
| `make ci-frontend-admin` | Admin SPA: typecheck, lint, tests, audit, build |
| `make ci-frontend-client` | Client SPA: typecheck, lint, build |
| `make ci-mobile` | Flutter: analyze, test |
| `make fmt` | Run every formatter in write mode (gofmt, prettier, dart format) |
| `make ci-help` | Full CI target list |
| `make help` | Dev/orchestration targets (infra, services, ports) |

## Repo layout

| Path | What | When you'd touch it |
|------|------|---------------------|
| `backend/` | Go modular monolith — 6 core + 13 optional modules, 6 build profiles | API changes, new modules, AI sidecar |
| `frontend-admin/` | React 19 operator console (Tier-1, internal users) | Internal admin UI |
| `frontend-client/` | React 19 customer SPA (Tier-2, external clients) | External-facing client UI |
| `mobile/` | Flutter 3.35+ cross-platform app | Mobile features |
| `docker/` | Compose configs (dev/staging/prod/infra) | Local dev orchestration |
| `docs/` | Architecture, ADRs, plans | Design docs, RFCs |

Every subdirectory has its own `CLAUDE.md` / `README.md` with module-specific guidance — start there before diving in.

## Two-tier tenancy reminder

Orkestra has **two distinct tiers of tenants**:

- **Tier 1** — internal operator organizations (the companies running Orkestra).
- **Tier 2** — external customer organizations that register on the platform and subscribe to its services.

Every endpoint, collection, and RBAC check must declare its tier. See [`CLAUDE.md`](CLAUDE.md#tenancy-model) for the full model. When in doubt about which tier a resource belongs to, ask before implementing.

## Commit conventions

We use [Conventional Commits](https://www.conventionalcommits.org/), enforced by the `commit-msg` hook:

```
feat(auth): add backup codes for MFA
fix(billing): handle missing OpenAPI bearer token
chore(docs): refresh README badges
ci(workflows): split coverage badge into its own job
```

Allowed types: `feat`, `fix`, `chore`, `docs`, `refactor`, `test`, `ci`, `build`, `perf`, `style`, `revert`.

Commits that touch a module **must** update affected `CLAUDE.md` / `README.md` files in the same commit. Documentation drift is treated as a bug.

## PR checklist

- [ ] `make ci` passes locally
- [ ] Affected `CLAUDE.md` / `README.md` files updated in the same commit
- [ ] New endpoints declare their tenancy tier and enforce org-scoped RBAC
- [ ] New MongoDB collections follow the [module-prefix naming convention](backend/CLAUDE.md)
- [ ] No secrets in code, logs, or env-file examples — module secrets live in `ConfigService` (AES-256-GCM encrypted)
- [ ] If the change crosses backend and a frontend (e.g., OpenAPI shape change), the generated TypeScript clients are regenerated in the same PR

## Escape hatch — full CI parity locally

If `make ci` passes but GitHub Actions fails, reproduce the exact workflow in Docker via [`nektos/act`](https://github.com/nektos/act):

```bash
act -W .github/workflows/backend.yml
act -W .github/workflows/frontend-admin.yml
```

The repo ships a `.actrc` with the recommended runner image.

## Reporting issues

- **Security**: email <salvatore.balestrino@gmail.com> — do **not** open a public issue. We respond within 72 hours.
- **Bugs / features**: GitHub Issues with the appropriate area label (`backend`, `frontend-admin`, `frontend-client`, `mobile`, `docs`, `ci`).
- **Architecture discussions**: open a `docs(rfc): <title>` PR with the proposal under `docs/rfcs/` before writing code.

## License

By contributing you agree your work is licensed under the same terms as the project itself (see [`LICENSE`](LICENSE)).
