---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_organizations
tags: [orkestra, marketing, schema, db]
---

# `marketing_organizations`

## Scopo

Anagrafica degli **enti**: aziende, Pubbliche Amministrazioni,
fondazioni, associazioni e qualunque altro soggetto giuridico /
collettivo che possa essere oggetto di attività marketing.

Distinta da `marketing_persons` perché un addon marketing serio deve
modellare entrambe le dimensioni (B2B e persone fisiche) senza
collasso in un'unica entità: un'azienda non è una persona, e una
persona può afferire a più aziende nel tempo.

Una Organization si collega a una o più Person via
`marketing_memberships`.

## Schema

| Campo            | Tipo                  | Required | Default  | Note                                                                                              |
| ---------------- | --------------------- | -------- | -------- | ------------------------------------------------------------------------------------------------- |
| `_id`            | ObjectID              | yes      | auto     | PK Mongo                                                                                          |
| `tenant_id`      | string                | yes      | ctx      | Scope multi-tenant Orkestra. Stampato via `tenantrepo.StampInsertM`                               |
| `legal_name`     | string                | yes      | —        | Ragione sociale ufficiale                                                                         |
| `display_name`   | string                | no       | —        | Nome di cortesia se diverso dal legal_name                                                        |
| `vat`            | string                | no       | —        | Partita IVA (locale o internazionale). Normalizzata. Indice unique sparse                          |
| `tax_code`       | string                | no       | —        | Codice fiscale / tax identifier (può coincidere con `vat` o essere distinto a seconda della giurisdizione) |
| `kind`           | enum                  | yes      | `company`| `company` · `public_administration` · `foundation` · `association` · `other`                      |
| `website`        | string                | no       | —        | URL                                                                                               |
| `emails`         | array<EmailEntry>     | no       | `[]`     | Email aziendali. Struttura: `{ address, label?, primary? }`                                       |
| `phones`         | array<PhoneEntry>     | no       | `[]`     | Telefoni. Struttura: `{ number, label?, primary? }`                                               |
| `addresses`      | array<Address>        | no       | `[]`     | Indirizzi. Struttura: `{ street, city, province, postal_code, country, label?, primary? }`        |
| `tags`           | array<ObjectID>       | no       | `[]`     | Riferimenti a `marketing_tags._id`                                                                |
| `custom_fields`  | object                | no       | `{}`     | Bag JSON validato contro `marketing_custom_field_schemas` per `target_collection = organizations` |
| `sources`        | array<Source>         | no       | `[]`     | Provenienze (vedi nota §sources sotto)                                                            |
| `notes`          | string                | no       | —        | Note libere, markdown                                                                             |
| `created_at`     | datetime              | yes      | now      |                                                                                                   |
| `updated_at`     | datetime              | yes      | now      |                                                                                                   |
| `created_by`     | string (user_id)      | no       | —        | Da `ctxauth`. Null se creato da importer                                                          |
| `updated_by`     | string (user_id)      | no       | —        | Da `ctxauth`                                                                                      |

### Sotto-struttura `Source`

```json
{
  "importer": "csv" | "excel" | "odoo" | "manual" | "form",
  "job_id": "<ObjectID di marketing_import_jobs, opzionale>",
  "external_id": "<id originale nella sorgente, opzionale>",
  "imported_at": "<datetime>",
  "raw_payload_ref": "<riferimento al raw payload archiviato, opzionale>"
}
```

## Indici

| Campi                      | Tipo                | Note                                                       |
| -------------------------- | ------------------- | ---------------------------------------------------------- |
| `tenant_id + vat`          | unique sparse       | Una P.IVA è unica per tenant; sparse perché vat è opzionale|
| `tenant_id + tax_code`     | unique sparse       | Idem per codice fiscale                                    |
| `tenant_id + legal_name`   | text                | Ricerca full-text                                          |
| `tenant_id + kind`         | btree               | Filter per tipologia ente                                  |
| `tenant_id + tags`         | multikey            | Filter per tag                                             |
| `tenant_id + updated_at`   | btree (desc)        | Lista recenti                                              |

## Esempio documento

```json
{
  "_id": "65f1a2b3c4d5e6f7a8b9c0d1",
  "tenant_id": "<tenant-id>",
  "legal_name": "Acme Manufacturing S.p.A.",
  "display_name": "Acme",
  "vat": "IT01234567890",
  "tax_code": "01234567890",
  "kind": "company",
  "website": "https://www.acme.example",
  "emails": [
    { "address": "info@acme.example", "label": "general", "primary": true }
  ],
  "phones": [
    { "number": "+390123456789", "label": "switchboard", "primary": true }
  ],
  "addresses": [
    {
      "street": "Via Esempio 1",
      "city": "Milano",
      "province": "MI",
      "postal_code": "20100",
      "country": "IT",
      "primary": true
    }
  ],
  "tags": ["6601aa...industry-mfg", "6601bb...region-northwest"],
  "custom_fields": {
    "industry": "manufacturing",
    "size_band": "medium",
    "preferred_channel": "email"
  },
  "sources": [
    {
      "importer": "csv",
      "job_id": "65f1...job1",
      "external_id": "row_142",
      "imported_at": "2026-04-10T09:23:00Z"
    }
  ],
  "notes": "Cliente acquisito in Q1 2026.",
  "created_at": "2026-04-10T09:23:00Z",
  "updated_at": "2026-04-10T09:23:00Z",
  "created_by": null,
  "updated_by": null
}
```

## Note / vincoli

- **VAT/tax_code normalizzazione**: l'addon normalizza in fase di
  scrittura (uppercase, strip spaces, leading zeros preservati). Match
  in dedup avviene sul valore normalizzato.
- **`vat` opzionale**: alcune giurisdizioni o tipologie di ente
  (es. PA, persone fisiche con attività occasionale) possono non
  avere un VAT. L'unique index è `sparse` per non bloccare
  inserimenti senza `vat`.
- **`custom_fields`** validato al write contro la versione corrente di
  `marketing_custom_field_schemas`. Se uno schema cambia, i documenti
  vecchi non vengono migrati: i campi rimossi restano nel bag JSON ma
  non sono più letti dall'API typed.
- **No soft-delete** nello schema iniziale: hard delete diretto, con
  cascade su `marketing_memberships`. Eventuale `deleted_at` per
  retention/GDPR può arrivare in fase successiva.
