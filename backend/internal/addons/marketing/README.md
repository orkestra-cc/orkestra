<div align="center">

# Orkestra Marketing addon

**Contact base, importer pipeline, immutable activity log, multi-profile scoring engine, and card/membership lifecycle for the [Orkestra](https://github.com/orkestra-cc/orkestra) modular monolith.**

[![Go Reference](https://pkg.go.dev/badge/github.com/orkestra-cc/orkestra-addon-marketing.svg)](https://pkg.go.dev/github.com/orkestra-cc/orkestra-addon-marketing)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white&style=flat-square)](https://go.dev)
[![Module](https://img.shields.io/badge/module-github.com%2Forkestra--cc%2Forkestra--addon--marketing-blue?style=flat-square)](https://github.com/orkestra-cc/orkestra-addon-marketing)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg?style=flat-square)](LICENSE)

[SDK](https://github.com/orkestra-cc/orkestra-sdk) · [Monorepo](https://github.com/orkestra-cc/orkestra) · [Module docs](CLAUDE.md)

</div>

---

## Status

**Phase 1 (Fondazione anagrafica MVP)** — _in progress on
[`feature/marketing-addon`](https://github.com/orkestra-cc/orkestra/tree/feature/marketing-addon)_.

The first phase ships the contact base (Organizations, Persons,
Memberships, Tags, Custom Field Schemas), a CSV importer with dedup
and auto-merge, and admin UI pages. Subsequent phases add the
immutable activity log + scoring engine (Phase 2), additional
importers + conflict-review queue (Phase 3), and the card lifecycle
(Phase 4). The full design + roadmap lives in the orkestra monorepo at
`docs/plans/marketing-addon/`.

## How it ships

A self-contained Orkestra addon implementing the `Module` interface
from [`orkestra-cc/orkestra-sdk`](https://github.com/orkestra-cc/orkestra-sdk).
Every MongoDB collection it owns is prefixed `marketing_`.

## Tagging

Tagged from `v0.1.0` against the public mirror at
`github.com/orkestra-cc/orkestra-addon-marketing` once Phase 1 lands
in `dev` and CI is green across the full SKU matrix. Until then the
monorepo's `backend/go.mod` carries a `replace` directive pointing at
the in-tree source so cross-cutting work doesn't need a tag bump per
change.
