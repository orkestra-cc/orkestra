---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_cards
tags: [orkestra, marketing, schema, db]
---

# `marketing_cards`

## Scopo

**Card concreta** emessa a una Person. Istanza di un
[`marketing_card_types`](marketing_card_types.md).

Una Person può detenere:

- card di tipi diversi simultaneamente (es. "Premium Member" +
  "Honorary Pass");
- più card dello stesso tipo, se il tipo ha
  `allow_multiple_per_person = true`.

Decisione D08: le card sono asset emessi dallo **staff**, non
auto-registrazione utente.
Decisione D09: le card **non** contribuiscono allo score — sono
**flag qualitativi** filtrabili sui Person.

## Schema

| Campo            | Tipo                  | Required | Default      | Note                                                                                       |
| ---------------- | --------------------- | -------- | ------------ | ------------------------------------------------------------------------------------------ |
| `_id`            | ObjectID              | yes      | auto         |                                                                                            |
| `tenant_id`      | string                | yes      | ctx          |                                                                                            |
| `card_type_id`   | ObjectID              | yes      | —            | FK a `marketing_card_types._id`                                                            |
| `code`           | string                | yes      | derived      | Generato da `card_type.code_format`. Unique per tenant                                     |
| `person_id`     | ObjectID               | yes      | —            | FK a `marketing_persons._id`                                                               |
| `tier`           | string                | conditional | —         | Required se `card_type.tiers` non vuoto; valore deve essere uno di `tiers`                  |
| `status`         | enum                  | yes      | `active`     | `active` · `suspended` · `revoked`                                                          |
| `benefits`       | array<string>         | no       | from type    | Default: copia da `card_type.default_benefits` al moment di emit. Override per istanza      |
| `notes`          | string                | no       | —            | Markdown                                                                                   |
| `expires_at`     | datetime              | no       | —            | Opzionale: data di scadenza naturale (es. card annuale). `null` = nessuna scadenza         |
| `issued_at`      | datetime              | yes      | now          |                                                                                            |
| `issued_by`      | string (user_id)      | yes      | ctx          | Staff che emette                                                                            |
| `suspended_at`   | datetime              | no       | —            |                                                                                            |
| `suspended_by`   | string (user_id)      | no       | —            |                                                                                            |
| `suspend_reason` | string                | no       | —            |                                                                                            |
| `revoked_at`     | datetime              | no       | —            |                                                                                            |
| `revoked_by`     | string (user_id)      | no       | —            |                                                                                            |
| `revoke_reason`  | string                | no       | —            |                                                                                            |
| `updated_at`     | datetime              | yes      | now          |                                                                                            |

## Indici

| Campi                                                | Tipo               | Note                                                                                                                       |
| ---------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| `tenant_id + code`                                   | unique             | Codice univoco per tenant                                                                                                  |
| `tenant_id + person_id`                              | btree              | Tutte le card di una persona                                                                                               |
| `tenant_id + person_id + card_type_id + status`      | unique partial     | Partial filter `status: active`. Enforced quando `card_type.allow_multiple_per_person = false` (check a livello service)   |
| `tenant_id + card_type_id + status`                  | btree              | "Tutti gli active premium"                                                                                                 |
| `tenant_id + tier + status`                          | btree sparse       | Leaderboard per tier                                                                                                       |
| `tenant_id + status + expires_at`                    | btree sparse       | Job che scaduce card a `expires_at`                                                                                        |
| `tenant_id + issued_at desc`                         | btree              | Cronologia                                                                                                                 |

## Esempio documento

```json
{
  "_id": "6607aa1234567890abcdef01",
  "tenant_id": "<tenant-id>",
  "card_type_id": "6607aa1234567890abcdef00",
  "code": "PREM-2026-00042",
  "person_id": "65f1a2b3c4d5e6f7a8b9c0e0",
  "tier": "gold",
  "status": "active",
  "benefits": [
    "exclusive_events",
    "premium_newsletter",
    "yearly_advisory_session"
  ],
  "notes": "Emessa su delibera direzione del 2026-05-10.",
  "expires_at": "2027-05-12T00:00:00Z",
  "issued_at": "2026-05-12T11:00:00Z",
  "issued_by": "user_admin_01",
  "updated_at": "2026-05-12T11:00:00Z"
}
```

## Note / vincoli

- **Generazione `code`**: al moment dell'emit, il service legge
  `card_type.code_format` e risolve i placeholder. Lookup atomico
  sulla sequenza `(tenant_id, card_type_id)`. Validazione di
  univocità su `tenant_id + code` come fail-safe.
- **Workflow transizioni di stato**:
  - `active → suspended`: reversibile. Popola `suspended_at`,
    `suspended_by`, `suspend_reason`. Activity di audit
    `card_status_changed` (vedi `marketing_activities`).
  - `suspended → active`: ripristino. Pulisce campi `suspended_*`.
  - `active | suspended → revoked`: **terminale**. Popola
    `revoked_at`, `revoked_by`, `revoke_reason`. Activity di audit.
    Per emettere una nuova card alla stessa Person: si crea record
    nuovo, anche con lo stesso `card_type_id`, e nuovo `code`.
- **Scadenza automatica**: se `expires_at` è impostato, un job
  schedulato porta lo status a `revoked` con
  `revoke_reason: "expired"` alla data indicata. Emette activity
  `card_status_changed`.
- **Denormalizzazione su Person**: ogni Person ha
  `active_card_ids[]` con gli `_id` di tutte le sue card attive.
  Aggiornato atomicamente a ogni transizione di stato.
- **Vincolo "una sola attiva per tipo"**: quando
  `card_type.allow_multiple_per_person = false`, il service rifiuta
  un nuovo `active` per la stessa coppia
  `(person_id, card_type_id)` se già esiste. Per emettere comunque
  serve prima revocare la precedente.
- **`benefits` per istanza**: di default copiati dal type al
  moment di emit (snapshot). Modifiche successive a
  `card_type.default_benefits` **non** propagano alle card già
  emesse. L'admin può modificare i `benefits` di una singola card.
- **Audit completo via Activity**: emit, sospensione, revoca,
  scadenza emettono activity con payload pieno per ricostruzione
  storica completa anche dopo cancellazione del record card.
