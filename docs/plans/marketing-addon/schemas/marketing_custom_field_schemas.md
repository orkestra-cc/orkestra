---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_custom_field_schemas
tags: [orkestra, marketing, schema, db]
---

# `marketing_custom_field_schemas`

## Scopo

Definizione **per-tenant** dello schema dei `custom_fields` di Person e
Organization.

Decisione D05: il motore è generico (bag JSON validato), i dati sono
shaped dal tenant. Ogni tenant decide quali attributi strutturati
servono al proprio dominio e li dichiara qui; l'addon valida le
scritture su Person/Organization contro questa configurazione.

Un documento per `(tenant_id, target_collection)`. Cambi al campo
`fields[]` si applicano alle scritture successive — i record vecchi
non vengono migrati (i campi rimossi restano nel bag JSON ma non sono
più typed in API).

## Schema

| Campo                 | Tipo                  | Required | Default | Note                                                                  |
| --------------------- | --------------------- | -------- | ------- | --------------------------------------------------------------------- |
| `_id`                 | ObjectID              | yes      | auto    |                                                                       |
| `tenant_id`           | string                | yes      | ctx     |                                                                       |
| `target_collection`   | enum                  | yes      | —       | `persons` · `organizations`                                           |
| `fields`              | array<FieldDef>       | yes      | `[]`    | Vedi sotto                                                            |
| `version`             | int                   | yes      | `1`     | Incrementato ad ogni modifica per audit/log                           |
| `allow_unknown_fields`| boolean               | yes      | `false` | Se true, accetta nei record valori non dichiarati nello schema        |
| `created_at`          | datetime              | yes      | now     |                                                                       |
| `updated_at`          | datetime              | yes      | now     |                                                                       |
| `updated_by`          | string (user_id)      | no       | —       |                                                                       |

### Sotto-struttura `FieldDef`

```json
{
  "key": "industry",
  "label": "Industry",
  "type": "enum",
  "required": false,
  "options": [
    { "value": "manufacturing", "label": "Manufacturing" },
    { "value": "services",      "label": "Services" },
    { "value": "retail",        "label": "Retail" },
    { "value": "tech",          "label": "Technology" },
    { "value": "other",         "label": "Other" }
  ],
  "default": null,
  "description": "Industry / sector dell'organizzazione"
}
```

Tipi supportati per `type`:

- `string` — testo libero
- `int` — numero intero
- `float` — numero decimale
- `bool` — booleano
- `date` — data ISO 8601
- `datetime` — datetime ISO 8601
- `enum` — singolo valore da `options[]`
- `multi_enum` — array di valori da `options[]`
- `ref:<collection>` — riferimento a un `_id` di un'altra collection
  (es. `ref:marketing_tags`)

## Indici

| Campi                                  | Tipo     | Note                                                |
| -------------------------------------- | -------- | --------------------------------------------------- |
| `tenant_id + target_collection`        | unique   | Una sola configurazione attiva per target           |

## Esempio documento (`target_collection: organizations`)

```json
{
  "_id": "6602aa1234567890abcdef01",
  "tenant_id": "<tenant-id>",
  "target_collection": "organizations",
  "allow_unknown_fields": false,
  "fields": [
    {
      "key": "industry",
      "label": "Industry",
      "type": "enum",
      "required": false,
      "options": [
        { "value": "manufacturing", "label": "Manufacturing" },
        { "value": "services",      "label": "Services" },
        { "value": "retail",        "label": "Retail" },
        { "value": "tech",          "label": "Technology" },
        { "value": "other",         "label": "Other" }
      ]
    },
    {
      "key": "size_band",
      "label": "Company size band",
      "type": "enum",
      "required": false,
      "options": [
        { "value": "micro",   "label": "1-9 employees" },
        { "value": "small",   "label": "10-49" },
        { "value": "medium",  "label": "50-249" },
        { "value": "large",   "label": "250+" }
      ]
    },
    {
      "key": "region",
      "label": "Region",
      "type": "string",
      "required": false
    },
    {
      "key": "preferred_channel",
      "label": "Preferred contact channel",
      "type": "enum",
      "required": false,
      "options": [
        { "value": "email" },
        { "value": "phone" },
        { "value": "in_person" }
      ]
    }
  ],
  "version": 1,
  "created_at": "2026-05-18T10:00:00Z",
  "updated_at": "2026-05-18T10:00:00Z"
}
```

Analogamente per `target_collection: persons` un tenant dichiarerà i
campi che modellano i propri attributi tipici (job function, seniority,
interessi, opt-in canali aggiuntivi, ...).

## Note / vincoli

- **Validazione write-time**: ogni write su `marketing_persons` o
  `marketing_organizations` valida `custom_fields` contro lo schema
  corrente. Campi sconosciuti vengono rifiutati di default
  (configurabile con flag `allow_unknown_fields`).
- **No data migration su schema change**: se rimuovi un campo dallo
  schema, i record che lo contenevano mantengono il valore nel bag JSON.
  L'API typed non lo espone più, ma resta recuperabile in raw. Aggiunta
  di campo `required` su schema esistente **non** forza popolamento
  retroattivo.
- **`version` incrementato a ogni modifica**: utile per audit (log
  separato può conservare le versioni precedenti se serve).
- **Indici secondari sui custom_fields**: definibili manualmente per
  campi ad alto volume di filter. La collection
  `marketing_persons` espone un meccanismo per dichiarare indici
  multikey su `custom_fields.<key>` specifici.
- **Lock-in degli enum**: una volta che un valore enum è usato in
  record reali, rimuoverlo dallo schema **non** lo cancella dai
  documenti esistenti. L'admin UI deve segnalare lo stato "deprecato"
  invece di rimuovere silenziosamente l'option.
