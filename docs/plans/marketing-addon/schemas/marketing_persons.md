---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_persons
tags: [orkestra, marketing, schema, db]
---

# `marketing_persons`

## Scopo

Anagrafica delle **persone fisiche**: contatti, lead, referenti
aziendali, membri di una community, utenti finali, beneficiari di
un servizio.

Una Person può appartenere a N Organization via
`marketing_memberships`, di cui una contrassegnata `primary` per
denormalizzare `org_id` nelle Activity (decisione D12).

Può detenere una o più card (`marketing_cards`) — referenziate per
denormalizzazione in `active_card_ids[]`.

## Schema

| Campo             | Tipo                  | Required | Default  | Note                                                                                              |
| ----------------- | --------------------- | -------- | -------- | ------------------------------------------------------------------------------------------------- |
| `_id`             | ObjectID              | yes      | auto     | PK Mongo                                                                                          |
| `tenant_id`       | string                | yes      | ctx      | Scope multi-tenant Orkestra                                                                       |
| `first_name`      | string                | no       | —        | A livello service: almeno uno tra `(first_name + last_name)` o un'email primary è richiesto       |
| `last_name`       | string                | no       | —        |                                                                                                   |
| `title`           | string                | no       | —        | Onorifico/qualifica (es. "Dott.", "Ing.")                                                          |
| `emails`          | array<EmailEntry>     | no       | `[]`     | `{ address, label?, primary?, verified?, opt_in?, opt_in_at?, opt_in_source? }`                   |
| `phones`          | array<PhoneEntry>     | no       | `[]`     | `{ number, label?, primary? }`                                                                    |
| `language`        | string (BCP47)        | no       | `en`     | Per personalizzazione invii futuri (sovrascrivibile per tenant)                                   |
| `birthdate`       | date                  | no       | —        | Solo se rilevante e con base giuridica adeguata                                                   |
| `tags`            | array<ObjectID>       | no       | `[]`     | Riferimenti a `marketing_tags`                                                                    |
| `custom_fields`   | object                | no       | `{}`     | Validato contro schema per `target_collection = persons`                                          |
| `consent`         | Consent               | no       | —        | Vedi sotto                                                                                        |
| `active_card_ids` | array<ObjectID>       | no       | `[]`     | Denormalizzato. IDs di tutte le card con `status = active` di questa Person (vedi `marketing_cards`)|
| `sources`         | array<Source>         | no       | `[]`     | Stessa struttura di `marketing_organizations.sources`                                              |
| `notes`           | string                | no       | —        | Markdown                                                                                          |
| `created_at`      | datetime              | yes      | now      |                                                                                                   |
| `updated_at`      | datetime              | yes      | now      |                                                                                                   |
| `created_by`      | string (user_id)      | no       | —        |                                                                                                   |
| `updated_by`      | string (user_id)      | no       | —        |                                                                                                   |

### Sotto-struttura `EmailEntry`

```json
{
  "address": "name@example.com",
  "label": "work" | "personal" | null,
  "primary": true,
  "verified": false,
  "opt_in": true,
  "opt_in_at": "2026-04-10T09:23:00Z",
  "opt_in_source": "form_homepage"
}
```

### Sotto-struttura `Consent` (per GDPR)

```json
{
  "marketing_email": {
    "given": true,
    "basis": "consent" | "legitimate_interest",
    "given_at": "2026-04-10T09:23:00Z",
    "source": "form_homepage",
    "revoked_at": null
  },
  "marketing_phone": { ... },
  "profiling": { ... }
}
```

## Indici

| Campi                                    | Tipo            | Note                                                                                       |
| ---------------------------------------- | --------------- | ------------------------------------------------------------------------------------------ |
| `tenant_id + emails.address`             | unique sparse   | Una email è unica per tenant — match anche se non primary. Sparse per Person senza email   |
| `tenant_id + last_name + first_name`     | text            | Ricerca per nome                                                                           |
| `tenant_id + tags`                       | multikey        | Filter per tag                                                                             |
| `tenant_id + active_card_ids`            | multikey sparse | Filter "ha questa card" / "ha almeno una card attiva"                                      |
| `tenant_id + updated_at`                 | btree (desc)    |                                                                                            |
| `tenant_id + custom_fields.<key>`        | multikey        | Indici secondari su campi custom ad alto volume di filter (definiti per tenant)            |

## Esempio documento

```json
{
  "_id": "65f1a2b3c4d5e6f7a8b9c0e0",
  "tenant_id": "<tenant-id>",
  "first_name": "Jane",
  "last_name": "Doe",
  "title": null,
  "emails": [
    {
      "address": "jane.doe@acme.example",
      "label": "work",
      "primary": true,
      "verified": true,
      "opt_in": true,
      "opt_in_at": "2026-03-15T14:00:00Z",
      "opt_in_source": "form_event_registration"
    }
  ],
  "phones": [],
  "language": "en",
  "tags": ["6601aa...lead-hot", "6601bb...industry-mfg"],
  "custom_fields": {
    "job_function": "operations",
    "seniority": "director",
    "preferred_channel": "email"
  },
  "consent": {
    "marketing_email": {
      "given": true,
      "basis": "consent",
      "given_at": "2026-03-15T14:00:00Z",
      "source": "form_event_registration",
      "revoked_at": null
    }
  },
  "active_card_ids": [],
  "sources": [
    {
      "importer": "csv",
      "job_id": "65f1...job1",
      "external_id": "row_98",
      "imported_at": "2026-04-10T09:23:00Z"
    }
  ],
  "created_at": "2026-04-10T09:23:00Z",
  "updated_at": "2026-04-10T09:23:00Z"
}
```

## Note / vincoli

- **Identità minima richiesta**: a livello service almeno uno tra
  `(first_name + last_name)` o un'email primary deve essere presente.
  Un record completamente vuoto è rifiutato.
- **Dedup primario via email**: il match in import si fa su
  `emails.address` (case-insensitive). Match cross-email (incoming porta
  una email che è alt-email di un Person esistente) → match valido.
- **Email normalizzata**: lowercase, trim. La normalizzazione avviene al
  write.
- **Opt-in tracciato a livello email**, non solo a livello consent block:
  permette gestione di doppio opt-in selettivo (es. la persona ha
  consentito email lavoro ma non email personale).
- **`active_card_ids`** è denormalizzato per performance. La fonte di
  verità è `marketing_cards.person_id` con `status = active`.
  Aggiornamento atomico insieme alle transizioni di stato della card
  (emit / suspend / revoke / expire). Array vuoto = nessuna card attiva.
- **No soft-delete** nella prima implementazione. Cancellazione GDPR =
  hard delete + cascade su Activity + record audit in collection core
  separata.
