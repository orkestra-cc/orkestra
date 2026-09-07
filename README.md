<div align="center">

<img src=".github/assets/orkestra-logo.webp" alt="Orkestra" width="480" />

**The open-source foundation for building multi-tenant SaaS products.**

Users, authentication, authorization, tenancy, notifications, navigation,
logging, and compliance are ready before you write your product-specific code.

[![Backend CI](https://github.com/orkestra-cc/orkestra/actions/workflows/backend.yml/badge.svg?branch=dev)](https://github.com/orkestra-cc/orkestra/actions/workflows/backend.yml)
[![Frontend Admin CI](https://github.com/orkestra-cc/orkestra/actions/workflows/frontend-admin.yml/badge.svg?branch=dev)](https://github.com/orkestra-cc/orkestra/actions/workflows/frontend-admin.yml)
[![Security CI](https://github.com/orkestra-cc/orkestra/actions/workflows/security.yml/badge.svg?branch=dev)](https://github.com/orkestra-cc/orkestra/actions/workflows/security.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[Try it](#try-orkestra) · [Features](#whats-included) · [Documentation](https://docs.orkestra.cc) · [Contributing](CONTRIBUTING.md)

</div>

> [!WARNING]
> **Orkestra is under active development and has not reached 1.0.** APIs,
> configuration, migrations, and extension points can change between minor
> releases. Pin the version you deploy, read the [changelog](CHANGELOG.md)
> before upgrading, and evaluate the project against your production
> requirements.

## What is Orkestra?

Every SaaS product needs the same foundations: accounts, login, permissions,
organizations, administration, audit trails, and operational tooling. Building
them repeatedly delays the features that make the product unique.

Orkestra provides those foundations as a modular Go backend, an operator
console, a client-facing starter application, and a mobile application. Fork
the repository, keep the core, and build your own product modules through the
same module contract used by Orkestra itself.

Orkestra is a **core-only base**. It does not bundle product verticals such as
billing, subscriptions, CRM, or AI. A fork adds only the modules it needs,
in-tree and against the included SDK. See
[ADR-0006](docs/adr/0006-collapse-to-core-only-base.md) for the rationale.

The data model supports two tenant tiers:

- **Internal organizations** operate the platform and manage staff, roles, and
  module configuration.
- **External client organizations** manage their own users, memberships, and
  sub-tenants.

## What's included

| Area | Capabilities |
| --- | --- |
| **Users** | Separate operator and client user stores, profiles, memberships, OAuth links, and avatar storage |
| **Authentication** | Argon2id passwords, Google/Apple/GitHub/Discord OAuth 2.1, RS256 JWTs, sessions, service accounts, TOTP MFA, and passkeys |
| **Authorization** | Platform and tenant roles, permission bindings, route protection, and Cedar policies |
| **Tenancy** | Internal and external organizations, memberships, divisions, and tenant-scoped data boundaries |
| **Notifications** | Email delivery, templates, preferences, and unsubscribe flows; local development starts in `noop` mode |
| **Navigation and modules** | Backend-driven navigation, per-tenant module configuration, health checks, and runtime start/stop for optional modules |
| **Logging and observability** | Structured logs, request and tenant correlation, runtime log controls, OpenTelemetry, Prometheus, and an optional self-hosted Grafana stack |
| **Compliance** | Audit trail, GDPR data-subject workflows, per-tenant encryption, retention, and legal-hold controls |

The repository also includes:

- a React 19 operator console for Tier-1 administration;
- a React 19 client SPA showing the Tier-2 login and account flows;
- an early-stage Flutter mobile application;
- MongoDB, Redis, and S3-compatible object storage for local development;
- an OpenAPI 3.1 API and CI checks for tenant scope, policy coverage, tests,
  vulnerabilities, and builds.

## Try Orkestra

You need Git, Docker with Compose v2, and OpenSSL. Clone the repository, create
the local configuration, and start the development stack:

```bash
git clone https://github.com/orkestra-cc/orkestra.git
cd orkestra
./orkestra.sh init --quick
./orkestra.sh deploy --yes
```

The initialization step creates `docker/.env`, generates random development
secrets and RS256 keys, and chooses free host ports. It is idempotent and keeps
existing values on subsequent runs.

When the stack is ready, open:

| Surface | Default URL |
| --- | --- |
| Operator console | <http://localhost:8080> |
| Client application | <http://client.localhost:8081> |
| Backend API | <http://localhost:3000> |
| API documentation | <http://localhost:3000/docs> |

On a fresh installation, the operator console starts a setup wizard that
creates the first administrator. Email and password authentication works
without external services. OAuth providers and SMTP are optional and can be
configured later from `/admin/modules`.

Useful stack commands:

```bash
./orkestra.sh status
./orkestra.sh logs backend -f
./orkestra.sh stop --with-infra
```

Run `./orkestra.sh` for the interactive interface or
`./orkestra.sh --help` for all commands. For manual Compose commands,
multi-stack setups, and deployment configuration, see the
[installation guide](https://docs.orkestra.cc/getting-started/installation).

## How it fits together

| Layer | Technology |
| --- | --- |
| Backend | Go 1.26, Huma v2, modular monolith, single Go module |
| Operator console | React 19, TypeScript 5.9, Vite 8, Redux Toolkit |
| Client application | React 19, TypeScript 5.9, Vite 7 |
| Mobile | Flutter 3.44+, Riverpod |
| Data | MongoDB 8, Redis 8, S3-compatible object storage |

Eight core modules always load: `user`, `notification`, `tenant`, `authz`,
`auth`, `navigation`, `logging`, and `compliance`. Optional modules added by a
fork implement the `Module` interface and register through the module catalog.
They can declare routes, collections, permissions, configuration, navigation,
dependencies, and background processes.

Cross-module integrations use the interfaces in `backend/pkg/sdk/iface`, which
keeps modules independent of each other's service and repository packages.
Start with the [addon authoring guide](https://docs.orkestra.cc/sdk/build-your-first-addon)
or read the [backend contract](backend/CLAUDE.md).

## Repository map

```text
backend/          Go API, core modules, and in-tree SDK
frontend-admin/   Tier-1 operator console
frontend-client/  Tier-2 client starter application
mobile/           Flutter application
docker/           Development and deployment Compose files
docs/site/        Source for docs.orkestra.cc
docs/adr/         Architecture Decision Records
```

The [project documentation](https://docs.orkestra.cc) covers installation,
architecture, the module SDK, authentication, deployment, and operations. The
in-repository contracts in [CLAUDE.md](CLAUDE.md) describe the invariants that
contributors must preserve.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) for local
toolchain setup, checks, commit conventions, and the pull-request workflow.
Use [GitHub Issues](https://github.com/orkestra-cc/orkestra/issues) for bugs and
feature proposals, and follow [SECURITY.md](SECURITY.md) for private security
reports.

## License

Orkestra is licensed under the [Apache License 2.0](LICENSE). See
[NOTICE](NOTICE) for attribution.
