---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_import_jobs
tags: [orkestra, marketing, schema, db]
---

# `marketing_import_jobs`

## Scopo

**Audit** di ogni esecuzione di un import.

Un Job rappresenta una singola pipeline `extract → normalize → map →
dedup → conflict → commit` (vedi `Orkestra_marketing_addon.md` §5).

Usato per:

- tracking dello stato durante l'esecuzione (è in corso? è in pausa
  per review? è fallito?);
- audit retrospettivo (cosa è stato importato, da chi, quando, con
  che esito);
- collegamento con `marketing_conflict_reviews` (i conflict
  riferiscono al job che li ha generati);
- riferimento da `marketing_activities.refs.import_job_id` per
  ricostruire da dove un'activity è entrata.

## Schema

| Campo                | Tipo                | Required | Default     | Note                                                                                |
| -------------------- | ------------------- | -------- | ----------- | ----------------------------------------------------------------------------------- |
| `_id`                | ObjectID            | yes      | auto        |                                                                                     |
| `tenant_id`          | string              | yes      | ctx         |                                                                                     |
| `importer`           | enum                | yes      | —           | `csv` · `excel` · `odoo` (estendibile)                                              |
| `config`             | object              | yes      | —           | Snapshot del config usato (mapping colonne, file ref, connection params, …)         |
| `source_descriptor`  | object              | yes      | —           | Descrizione della sorgente: filename, file size, sha256, oppure endpoint + DB       |
| `status`             | enum                | yes      | `pending`   | `pending` · `running` · `paused_for_review` · `completed` · `failed` · `cancelled`  |
| `stats`              | JobStats            | yes      | derived     | Contatori aggregati                                                                 |
| `errors`             | array<JobError>     | no       | `[]`        | Errori durante l'esecuzione (parziali, non bloccanti)                               |
| `fatal_error`        | string              | no       | —           | Errore fatale che ha causato `failed`                                               |
| `started_at`         | datetime            | no       | —           |                                                                                     |
| `finished_at`        | datetime            | no       | —           |                                                                                     |
| `triggered_by`       | string (user_id)    | yes      | ctx         | Chi ha lanciato il job                                                              |
| `created_at`         | datetime            | yes      | now         |                                                                                     |
| `updated_at`         | datetime            | yes      | now         |                                                                                     |

### Sotto-struttura `JobStats`

```json
{
  "records_extracted": 1542,
  "records_normalized": 1538,
  "records_committed_new": 1102,
  "records_committed_merged": 412,
  "records_skipped_invalid": 4,
  "conflicts_open": 24,
  "conflicts_resolved": 0,
  "activities_emitted": 8217
}
```

### Sotto-struttura `JobError`

```json
{
  "phase": "normalize" | "map" | "commit",
  "record_ref": "row_142",
  "message": "Invalid email format",
  "occurred_at": "2026-05-18T10:35:12Z"
}
```

## Indici

| Campi                              | Tipo            | Note                                       |
| ---------------------------------- | --------------- | ------------------------------------------ |
| `tenant_id + status`               | btree           | Lista job per stato                        |
| `tenant_id + started_at desc`      | btree           | Cronologia                                 |
| `tenant_id + importer + started_at desc` | btree     | Filtro per tipo importer                   |
| `tenant_id + triggered_by`         | btree           | Audit "cosa ha lanciato l'utente X"        |

## Esempio documento (import CSV di una storia engagement da un ESP esterno)

```json
{
  "_id": "6604aa1234567890abcdef02",
  "tenant_id": "<tenant-id>",
  "importer": "csv",
  "config": {
    "preset": "esp_engagement_export_v2",
    "column_mapping": {
      "Contact Email": "primary_email",
      "First Name": "first_name",
      "Last Name": "last_name",
      "Campaign": "_activity:campaign_name",
      "Opened At": "_activity:occurred_at",
      "Status": "_activity:kind"
    },
    "activity_kind_map": {
      "Opened": "email_opened",
      "Clicked": "email_clicked",
      "Bounced": "email_bounced"
    },
    "dedup_keys": {
      "person": ["primary_email"]
    },
    "conflict_review_required_fields": ["primary_email"]
  },
  "source_descriptor": {
    "type": "uploaded_file",
    "filename": "esp_engagement_export_2024_full.csv",
    "size_bytes": 4823109,
    "sha256": "9af4b1...",
    "uploaded_at": "2026-05-18T10:32:00Z"
  },
  "status": "completed",
  "stats": {
    "records_extracted": 1542,
    "records_normalized": 1538,
    "records_committed_new": 1102,
    "records_committed_merged": 412,
    "records_skipped_invalid": 4,
    "conflicts_open": 0,
    "conflicts_resolved": 24,
    "activities_emitted": 8217
  },
  "errors": [
    {
      "phase": "normalize",
      "record_ref": "row_877",
      "message": "Email format invalid: 'jane..doe@example..com'",
      "occurred_at": "2026-05-18T10:35:12Z"
    }
  ],
  "fatal_error": null,
  "started_at": "2026-05-18T10:32:01Z",
  "finished_at": "2026-05-18T10:39:48Z",
  "triggered_by": "user_admin_01",
  "created_at": "2026-05-18T10:32:00Z",
  "updated_at": "2026-05-18T10:39:48Z"
}
```

## Note / vincoli

- **Snapshot del `config`**: salviamo per intero il config usato al
  momento dell'esecuzione, anche se nel frattempo viene modificato il
  preset di mapping. Permette di rispondere a "con quale mapping è
  stato fatto questo import?" anche dopo mesi.
- **`source_descriptor` con hash**: per i file uploadati salviamo
  sha256 del contenuto. Permette di rilevare se è stato uploadato due
  volte lo stesso file (warning UI: "questo file è già stato
  importato il …"). Per source connesse via API include endpoint +
  database/account name + filter applicati alla query.
- **`status` transizioni**:
  ```
  pending → running → completed
                   ↓
                   paused_for_review → running → completed
                   ↓
                   failed (fatal)
                   ↓
                   cancelled (richiesta utente)
  ```
- **Riavvio job paused_for_review**: quando tutte le review della coda
  vengono risolte/dismissed, il job riparte automaticamente dalla
  phase `commit` per i record sbloccati, oppure resta in
  `paused_for_review` finché ne resta almeno una pending.
- **Conservazione storica**: i job restano per sempre (sono audit).
  Retention configurabile in Fase 5 (es. 7 anni per GDPR audit).
- **Raw payload archivi**: il `source_descriptor` riferisce il file
  originale; la sua storage location effettiva (es. S3 / disk path) è
  fuori dallo scope di Mongo. Orkestra ha già un `documents` addon che
  potrebbe servire da storage layer in fase futura.
