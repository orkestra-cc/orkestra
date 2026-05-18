---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_activities
tags: [orkestra, marketing, schema, db]
---

# `marketing_activities`

## Scopo

**Event log immutabile, append-only** di tutte le interazioni che
riguardano una Person.

È la **single source of truth** dell'addon: gli score
(`marketing_score_snapshots`) sono viste derivate ricomputabili in
qualsiasi momento da questa collection (decisioni D10, D13).

> ⚠️ **Append-only enforced a livello service**:
> nessun UPDATE, nessun DELETE (eccezione singola: cancellazione GDPR
> della Person, che produce cascade documentato e audit permanente).
> Correzioni si gestiscono inserendo nuove activity di
> `kind = corrected_by`.

## Schema

| Campo         | Tipo                   | Required | Default | Note                                                                              |
| ------------- | ---------------------- | -------- | ------- | --------------------------------------------------------------------------------- |
| `_id`         | ObjectID               | yes      | auto    |                                                                                   |
| `tenant_id`   | string                 | yes      | ctx     |                                                                                   |
| `person_id`   | ObjectID               | yes      | —       | FK a `marketing_persons._id`                                                      |
| `org_id`      | ObjectID               | no       | derived | Denormalizzato dalla membership `primary + active` della Person al write-time     |
| `kind`        | enum                   | yes      | —       | Vedi tassonomia §kind                                                             |
| `occurred_at` | datetime               | yes      | —       | Quando è successo nel mondo reale (può essere passato — vedi D11)                 |
| `recorded_at` | datetime               | yes      | now     | Quando è stato inserito nel DB                                                    |
| `source`      | enum                   | yes      | —       | `importer` · `campaign_engine` · `webhook` · `manual` · `system`                  |
| `payload`     | object                 | no       | `{}`    | Schema variabile per `kind`. Validato write-time. Vedi §payload                   |
| `refs`        | object                 | no       | `{}`    | FK opzionali: `campaign_id?`, `event_id?`, `form_id?`, `content_id?`, `import_job_id?`, `card_id?`, `corrects_activity_id?` |
| `dedup_key`   | string                 | yes      | derived | `sha256(person_id + kind + occurred_at_iso + external_id)` — vedi sotto. Indice unique |
| `external_id` | string                 | no       | —       | ID nella sorgente originale (es. message_id ESP, event_id calendar, ...)          |
| `created_by`  | string (user_id)       | no       | —       | Null se source = importer / system / webhook                                      |

### Tassonomia `kind` (iniziale, estensibile)

**Email**: `email_sent`, `email_opened`, `email_clicked`, `email_bounced`, `email_unsubscribed`, `email_complained`.

**Eventi**: `event_invited`, `event_registered`, `event_attended`, `event_no_show`, `event_cancelled`.

**Web / form**: `form_submitted`, `page_visited`, `content_downloaded`.

**Diretto**: `call_made`, `meeting_held`, `note_added`.

**Sistema**: `imported`, `merged`, `tag_added`, `tag_removed`, `card_issued`, `card_status_changed`, `corrected_by`.

### Schema `payload` per `kind` (estratto)

```jsonc
// email_sent
{
  "campaign_id": "65...",
  "message_id": "<smtp message id>",
  "subject": "...",
  "from": "marketing@example.com"
}

// email_opened / email_clicked
{
  "campaign_id": "65...",
  "message_id": "...",
  "user_agent": "...",
  "ip_country": "IT",
  "url": "https://..."   // solo email_clicked
}

// event_attended
{
  "event_id": "65...",
  "event_title": "Annual Conference 2026",
  "location": "Milano",
  "duration_minutes": 90,
  "check_in_method": "qr" | "manual"
}

// form_submitted
{
  "form_id": "65...",
  "form_slug": "contact_us",
  "page_url": "https://www.example.com/contact",
  "fields_submitted": ["name", "email", "company", "interest"]
}

// content_downloaded
{
  "content_id": "65...",
  "content_type": "whitepaper" | "report" | "ebook" | ...,
  "content_title": "Industry Report 2026 Q1"
}

// tag_added / tag_removed
{
  "tag_id": "65...",
  "tag_path": "/Segment/Hot"
}

// card_issued
{
  "card_id": "65...",
  "card_type_id": "65...",
  "card_type_key": "premium_member",
  "code": "PREM-2026-00042",
  "tier": "gold"
}

// card_status_changed
{
  "card_id": "65...",
  "card_type_key": "premium_member",
  "code": "PREM-2026-00042",
  "from": "active",
  "to": "suspended" | "revoked" | "active",
  "reason": "..."
}

// imported
{
  "importer": "csv",
  "external_id": "row_142"
}

// corrected_by
{
  "reason": "errato matching dedup",
  "previous_activity_id": "65..."
}
```

## Indici

| Campi                                            | Tipo                | Note                                                                                  |
| ------------------------------------------------ | ------------------- | ------------------------------------------------------------------------------------- |
| `tenant_id + person_id + occurred_at desc`       | btree               | Timeline di un contatto (query più frequente)                                         |
| `tenant_id + org_id + occurred_at desc`          | btree sparse        | Aggregato engagement aziendale                                                        |
| `tenant_id + kind + occurred_at desc`            | btree               | Score computation per profile (filtra per kind, ordina cronologicamente)              |
| `dedup_key`                                      | **unique**          | Idempotenza re-import (D21)                                                           |
| `tenant_id + refs.campaign_id`                   | sparse              | Analytics per campagna                                                                |
| `tenant_id + refs.event_id`                      | sparse              | Analytics per evento                                                                  |
| `tenant_id + refs.card_id`                       | sparse              | Storia di una card                                                                    |
| `tenant_id + source + recorded_at desc`          | btree               | Audit per fonte                                                                       |

## Esempio documento

```json
{
  "_id": "6603aa1234567890abcdef01",
  "tenant_id": "<tenant-id>",
  "person_id": "65f1a2b3c4d5e6f7a8b9c0e0",
  "org_id": "65f1a2b3c4d5e6f7a8b9c0d1",
  "kind": "email_opened",
  "occurred_at": "2024-11-15T08:42:13Z",
  "recorded_at": "2026-05-18T10:30:00Z",
  "source": "importer",
  "payload": {
    "campaign_id": "esp_camp_2024_11_newsletter",
    "message_id": "<esp-camp-2024-11-msg-9382@provider>",
    "user_agent": "Mozilla/5.0 (iPhone...)",
    "ip_country": "IT"
  },
  "refs": {
    "import_job_id": "6604aa1234567890abcdef02"
  },
  "dedup_key": "f7e3b9...sha256",
  "external_id": "esp:camp:9382:open:jane.doe@acme.example",
  "created_by": null
}
```

## Note / vincoli

- **Computazione `dedup_key`** (deterministic, side-effect free):
  ```
  sha256(
      person_id
      + "|" + kind
      + "|" + occurred_at.UTC().Format(RFC3339)
      + "|" + (external_id OR "")
  )
  ```
  Se `external_id` è vuoto e `occurred_at` ha precisione al secondo,
  il rischio di collisione naturale (due interazioni dello stesso
  kind al millisecondo) è trascurabile. Per kind di tipo `manual` con
  `external_id` vuoto, l'admin UI assegna un UUID a `external_id` al
  momento dell'insert per garantire univocità.
- **`org_id` denormalizzato al write**: il service legge la membership
  `primary + active` della Person al momento dell'insert. Se in futuro
  la primary cambia, le activity passate **mantengono** il vecchio
  `org_id` (corretto: l'engagement è successo quando lavorava per
  quell'azienda).
- **Append-only enforcement**: il repository `marketing_activities` non
  espone metodi `UpdateOne` né `DeleteOne` se non un singolo
  `HardDeleteByPersonID(person_id)` riservato al flusso GDPR e
  protetto da permission dedicata.
- **Retention** (out of scope Fase 1-4, da disegnare in Fase 5):
  configurabile per `kind`. Tipico: page visits decadono dopo N
  mesi, eventi/meeting/card mai.
- **Volume atteso** (stima ordine grandezza per un tenant con
  ~10k Person): 50 activity medie/anno → 500k document/anno.
  MongoDB gestisce comodamente con gli indici sopra. Aggregazioni di
  score su decine di migliaia di Person rimangono sub-secondo con
  eager incrementale (non serve aggregation pipeline pesante a
  runtime).
- **Estensione enum `kind` da addon futuri**: l'enum è dichiarato come
  validator MongoDB ma è esteso programmaticamente — altri addon
  Orkestra potrebbero registrare nuovi kind al boot. L'addon `marketing`
  espone un registry interno.
