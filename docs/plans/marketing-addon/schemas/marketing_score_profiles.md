---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_score_profiles
tags: [orkestra, marketing, schema, db]
---

# `marketing_score_profiles`

## Scopo

Configurazione di un **profilo di scoring**: regole punto-per-`kind`,
decay nel tempo, filtri di applicabilità.

Una Person può avere N score in parallelo, uno per profile attivo
(decisione D14). Ogni tenant configura tanti profile quanti ne servono
alla propria segmentazione (es. uno per ogni "audience" o "lead
quality lane").

Lo score è una **funzione pura** del log Activity + di questo profile
(D13). Modificare un profile invalida i `marketing_score_snapshots`
correlati, che verranno ricalcolati al prossimo eager o nel job
notturno.

## Schema

| Campo          | Tipo                  | Required | Default | Note                                                                            |
| -------------- | --------------------- | -------- | ------- | ------------------------------------------------------------------------------- |
| `_id`          | ObjectID              | yes      | auto    |                                                                                 |
| `tenant_id`    | string                | yes      | ctx     |                                                                                 |
| `name`         | string                | yes      | —       | Identificatore leggibile, slug-like (es. `hot_lead`, `newsletter_engagement`)   |
| `description`  | string                | no       | —       |                                                                                 |
| `active`       | boolean               | yes      | `true`  | Profilo inattivo non viene calcolato                                            |
| `rules`        | array<Rule>           | yes      | `[]`    | Vedi sotto                                                                      |
| `filters`      | ProfileFilter         | no       | `{}`    | Limita a quali Person si applica                                                |
| `default_decay`| DecayFn               | no       | `none`  | Decay default ereditato dalle Rule che non lo specificano                       |
| `version`      | int                   | yes      | `1`     | Incrementato a ogni modifica. Lo snapshot tiene traccia con quale version è stato calcolato |
| `created_at`   | datetime              | yes      | now     |                                                                                 |
| `updated_at`   | datetime              | yes      | now     |                                                                                 |
| `created_by`   | string (user_id)      | no       | —       |                                                                                 |
| `updated_by`   | string (user_id)      | no       | —       |                                                                                 |

### Sotto-struttura `Rule`

```jsonc
{
  "activity_kind": "event_attended",   // string, array, o "*" per wildcard
  "match_payload": {                    // opzionale, restringe la rule
    "location": "Milano"
  },
  "points": 25,
  "decay": {                            // opzionale, override del default_decay
    "fn": "exponential",
    "half_life_days": 180
  },
  "cap": 100,                           // opzionale: il contributo di questa rule è cappato
  "window_days": null                   // opzionale: ignora activity più vecchie di N giorni
}
```

### Sotto-struttura `DecayFn`

```jsonc
{
  "fn": "none" | "linear" | "exponential",
  "window_days": 365,            // solo per linear: punteggio → 0 dopo N giorni
  "half_life_days": 180           // solo per exponential
}
```

### Sotto-struttura `ProfileFilter`

```jsonc
{
  "tags_include": ["6601aa...lead-hot"],   // Person deve avere ALMENO uno
  "tags_exclude": [],                       // Person NON deve avere nessuno
  "custom_field_filters": {                 // arbitrari, Mongo-style
    "industry": { "$in": ["manufacturing", "tech"] }
  }
}
```

## Indici

| Campi                          | Tipo     | Note                                  |
| ------------------------------ | -------- | ------------------------------------- |
| `tenant_id + name`             | unique   | Profile name unico per tenant         |
| `tenant_id + active`           | btree    | Lista profile da calcolare nel batch  |

## Esempio documento (profilo generico "hot lead")

```json
{
  "_id": "6605aa1234567890abcdef01",
  "tenant_id": "<tenant-id>",
  "name": "hot_lead",
  "description": "Generic lead-quality score: pesa più gli engagement vicini alla conversione (meeting, form, demo) e meno gli engagement passivi (open, page view).",
  "active": true,
  "rules": [
    {
      "activity_kind": "meeting_held",
      "points": 50
    },
    {
      "activity_kind": "form_submitted",
      "points": 30
    },
    {
      "activity_kind": "event_attended",
      "points": 25,
      "decay": { "fn": "exponential", "half_life_days": 365 }
    },
    {
      "activity_kind": "content_downloaded",
      "points": 15,
      "decay": { "fn": "exponential", "half_life_days": 180 }
    },
    {
      "activity_kind": "email_clicked",
      "points": 3,
      "decay": { "fn": "linear", "window_days": 180 }
    },
    {
      "activity_kind": "email_opened",
      "points": 1,
      "cap": 10,
      "decay": { "fn": "linear", "window_days": 90 }
    },
    {
      "activity_kind": "page_visited",
      "points": 0.5,
      "cap": 5,
      "decay": { "fn": "linear", "window_days": 60 }
    }
  ],
  "filters": {
    "custom_field_filters": {
      "industry": { "$in": ["manufacturing", "tech"] }
    }
  },
  "default_decay": { "fn": "none" },
  "version": 1,
  "created_at": "2026-05-18T10:00:00Z",
  "updated_at": "2026-05-18T10:00:00Z"
}
```

## Note / vincoli

- **Modifica del profile invalida tutti gli snapshot correlati**: al
  save, `version` viene incrementato e tutti i `marketing_score_snapshots`
  con `profile_version < new_version` sono marcati `stale = true`.
  Verranno ricalcolati al prossimo read (eager) o nel job notturno.
- **`match_payload` come Mongo-style filter**: supporta `$in`, `$eq`,
  `$exists`, ecc. su sottocampi del payload. Permette rule fini (es.
  premia solo eventi in una certa location).
- **`activity_kind` come wildcard `*`**: utile per "ogni interazione
  vale 1 punto" come baseline.
- **Combinazione decay**: se una rule specifica `decay`, override
  `default_decay`. Se rule non specifica, eredita.
- **Filter su Person dinamici**: i filter vengono valutati al momento
  del compute. Se una Person esce dal filter, il suo snapshot per quel
  profile viene marcato `applicable = false` (o cancellato — TBD a
  implementazione).
- **Best practice**: 1 profile per ogni audience / segmento per cui hai
  bisogno di un ranking distinto. Tipico tenant: 2-6 profile. Più di
  10 profile suggerisce ridondanza nelle regole — meglio
  consolidare.
