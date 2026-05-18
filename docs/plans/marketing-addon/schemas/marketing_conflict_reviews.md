---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_conflict_reviews
tags: [orkestra, marketing, schema, db]
---

# `marketing_conflict_reviews`

## Scopo

**Coda dei conflitti di deduplica** in attesa di risoluzione umana.

Quando un import job (vedi §5 del documento principale) trova un
record incoming che matcha un esistente ma ha valori in conflitto su
campi protetti (`conflict_review_required_fields`), il record **non
viene committato** ma viene parcheggiato qui (decisione D20).

L'admin risolve manualmente scegliendo una resolution; al resolve, il
service applica il merge e libera l'import job.

## Schema

| Campo                | Tipo                  | Required | Default   | Note                                                                                |
| -------------------- | --------------------- | -------- | --------- | ----------------------------------------------------------------------------------- |
| `_id`                | ObjectID              | yes      | auto      |                                                                                     |
| `tenant_id`          | string                | yes      | ctx       |                                                                                     |
| `import_job_id`      | ObjectID              | yes      | —         | FK a `marketing_import_jobs._id`                                                    |
| `target_kind`        | enum                  | yes      | —         | `person` · `organization`                                                           |
| `existing_id`        | ObjectID              | yes      | —         | FK al record esistente (in `marketing_persons` o `marketing_organizations`)         |
| `existing_snapshot`  | object                | yes      | —         | Copia del documento esistente al momento della rilevazione del conflict (audit)     |
| `incoming_payload`   | object                | yes      | —         | Documento incoming completo (Person o Organization)                                 |
| `incoming_activities`| array<object>         | no       | `[]`      | Activity generate ma non ancora committate, in attesa del resolve                   |
| `conflicts`          | array<ConflictField>  | yes      | —         | Lista dei campi in conflitto. Vedi sotto                                            |
| `status`             | enum                  | yes      | `pending` | `pending` · `resolved` · `dismissed`                                                |
| `resolution`         | Resolution            | no       | —         | Compilato al resolve. Vedi sotto                                                    |
| `resolved_at`        | datetime              | no       | —         |                                                                                     |
| `resolved_by`        | string (user_id)      | no       | —         |                                                                                     |
| `resolved_notes`     | string                | no       | —         | Note dell'admin                                                                     |
| `created_at`         | datetime              | yes      | now       |                                                                                     |
| `updated_at`         | datetime              | yes      | now       |                                                                                     |

### Sotto-struttura `ConflictField`

```json
{
  "field": "primary_email",
  "existing_value": "jane.doe@acme.example",
  "incoming_value": "j.doe@acme-spa.example",
  "severity": "blocking" | "soft"
}
```

### Sotto-struttura `Resolution`

```jsonc
{
  "action": "keep_existing" | "take_incoming" | "manual_merge",
  "field_overrides": {           // solo per "manual_merge"
    "primary_email": "jane.doe@acme.example",
    "phones": "$append"          // append vs replace per array
  }
}
```

## Indici

| Campi                                       | Tipo     | Note                                       |
| ------------------------------------------- | -------- | ------------------------------------------ |
| `tenant_id + status`                        | btree    | Coda pending                               |
| `tenant_id + import_job_id`                 | btree    | Tutte le review di un job                  |
| `tenant_id + target_kind + status`          | btree    | Filtro per tipo                            |
| `tenant_id + existing_id`                   | btree    | "Questo record ha review pending"          |

## Esempio documento

```json
{
  "_id": "6608aa1234567890abcdef01",
  "tenant_id": "<tenant-id>",
  "import_job_id": "6604aa1234567890abcdef02",
  "target_kind": "person",
  "existing_id": "65f1a2b3c4d5e6f7a8b9c0e0",
  "existing_snapshot": {
    "first_name": "Jane",
    "last_name": "Doe",
    "emails": [
      { "address": "jane.doe@acme.example", "primary": true }
    ],
    "phones": [{ "number": "+390123456789" }],
    "tags": ["6601aa...lead-hot"]
  },
  "incoming_payload": {
    "first_name": "Jane",
    "last_name": "Doe",
    "emails": [
      { "address": "j.doe@acme-spa.example", "primary": true }
    ],
    "phones": [{ "number": "+390123777222" }],
    "tags": ["6601dd...industry-mfg"]
  },
  "incoming_activities": [
    {
      "kind": "email_opened",
      "occurred_at": "2024-09-22T08:14:00Z",
      "payload": { "campaign_id": "esp_camp_2024_09_newsletter" }
    }
  ],
  "conflicts": [
    {
      "field": "primary_email",
      "existing_value": "jane.doe@acme.example",
      "incoming_value": "j.doe@acme-spa.example",
      "severity": "blocking"
    }
  ],
  "status": "pending",
  "resolution": null,
  "resolved_at": null,
  "resolved_by": null,
  "created_at": "2026-05-18T10:35:30Z",
  "updated_at": "2026-05-18T10:35:30Z"
}
```

## Note / vincoli

- **Side-effect del resolve**:
  - `keep_existing`: i campi conflittuali dell'incoming vengono
    scartati. I campi non-conflictuali sono **già stati merged** al
    momento della creazione della review (vedi §5.6 del documento
    principale). Le `incoming_activities` vengono comunque committate
    (sono engagement reale, non in conflitto con anagrafica).
  - `take_incoming`: i campi conflittuali sovrascrivono. Activity
    committate.
  - `manual_merge`: applica `field_overrides` campo per campo. Le
    chiavi possibili sono i campi listati in `conflicts`. Per array si
    può specificare `"$append"` (unione, dedup interno) o un array
    esplicito (replace). Activity committate.
- **`dismissed`**: l'admin decide che l'incoming non corrispondeva
  davvero all'esistente (falso positivo del matcher). L'incoming
  viene scartato per intero (anagrafica + activity). Caso raro.
- **`existing_snapshot`** è una **copia immutabile** del record
  esistente al momento della creazione della review. Se nel frattempo
  l'esistente viene modificato da altra strada (manual edit, altro
  import), questo snapshot resta congelato per debug — ma il **resolve
  applica il merge sul record corrente**, non sullo snapshot.
- **Re-creazione su modifica esistente**: se mentre una review è
  pending l'esistente viene modificato in modo che il conflict sparisce
  (es. admin ha allineato manualmente l'email), il sistema **non**
  auto-risolve. La review resta pending finché un umano non la chiude
  esplicitamente (per evitare race condition silenziose).
- **Bulk actions in UI**: "applica `keep_existing` a tutte le review
  di questo job" è una shortcut comune, implementata come loop sul
  campo `resolution`.
