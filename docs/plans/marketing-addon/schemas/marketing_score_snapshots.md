---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_score_snapshots
tags: [orkestra, marketing, schema, db]
---

# `marketing_score_snapshots`

## Scopo

**Cache ricomputabile** del valore di score calcolato per la coppia
`(person_id, profile_id)`.

Non è una verità primaria: la verità è nell'Activity log
(`marketing_activities`). Se la collection viene cancellata
interamente, può essere ricostruita per intero senza perdita di dati
(decisione D13).

Serve per:

- evitare di ricalcolare lo score a ogni read;
- permettere query "tutti i Person con score > X per profile Y"
  (ordinamento, filter) senza aggregation pesante.

## Schema

| Campo            | Tipo                     | Required | Default | Note                                                                            |
| ---------------- | ------------------------ | -------- | ------- | ------------------------------------------------------------------------------- |
| `_id`            | ObjectID                 | yes      | auto    |                                                                                 |
| `tenant_id`      | string                   | yes      | ctx     |                                                                                 |
| `person_id`      | ObjectID                 | yes      | —       | FK a `marketing_persons._id`                                                    |
| `profile_id`     | ObjectID                 | yes      | —       | FK a `marketing_score_profiles._id`                                             |
| `profile_version`| int                      | yes      | —       | Version del profile usata per questo calcolo                                    |
| `value`          | float                    | yes      | —       | Valore di score numerico                                                        |
| `breakdown`      | array<BreakdownEntry>    | no       | `[]`    | Spiegazione: quali activity hanno contribuito e quanto. Vedi sotto              |
| `as_of`          | datetime                 | yes      | now     | Snapshot calcolato considerando activity con `occurred_at ≤ as_of`              |
| `computed_at`    | datetime                 | yes      | now     | Quando il calcolo è stato eseguito                                              |
| `applicable`     | boolean                  | yes      | `true`  | `false` se la Person non passa più i filter del profile (ma snapshot tenuto)    |
| `stale`          | boolean                  | yes      | `false` | `true` se profile è stato modificato dopo `computed_at`. Triggera ricalcolo     |
| `activity_count` | int                      | yes      | `0`     | N activity considerate. Utile per debug                                         |
| `last_activity_at` | datetime               | no       | —       | `occurred_at` dell'activity più recente considerata. Usato per skip eager       |

### Sotto-struttura `BreakdownEntry`

```json
{
  "activity_id": "6603aa1234567890abcdef01",
  "activity_kind": "event_attended",
  "occurred_at": "2025-09-12T14:00:00Z",
  "rule_index": 2,
  "raw_points": 25,
  "applied_decay": 0.62,
  "points_contributed": 15.5
}
```

## Indici

| Campi                                    | Tipo            | Note                                                                       |
| ---------------------------------------- | --------------- | -------------------------------------------------------------------------- |
| `tenant_id + person_id + profile_id`     | unique          | Un solo snapshot corrente per coppia                                       |
| `tenant_id + profile_id + value desc`    | btree           | Leaderboard: top score per profile                                         |
| `tenant_id + profile_id + stale`         | btree           | Trova snapshot da ricalcolare                                              |
| `tenant_id + person_id`                  | btree           | Lista score di una Person su tutti i profile                               |

## Esempio documento

```json
{
  "_id": "6606aa1234567890abcdef01",
  "tenant_id": "<tenant-id>",
  "person_id": "65f1a2b3c4d5e6f7a8b9c0e0",
  "profile_id": "6605aa1234567890abcdef01",
  "profile_version": 1,
  "value": 95.5,
  "breakdown": [
    {
      "activity_id": "6603aa...01",
      "activity_kind": "event_attended",
      "occurred_at": "2025-09-12T14:00:00Z",
      "rule_index": 2,
      "raw_points": 25,
      "applied_decay": 0.62,
      "points_contributed": 15.5
    },
    {
      "activity_id": "6603aa...02",
      "activity_kind": "form_submitted",
      "occurred_at": "2026-03-15T14:00:00Z",
      "rule_index": 1,
      "raw_points": 30,
      "applied_decay": 1.0,
      "points_contributed": 30.0
    },
    {
      "activity_id": "6603aa...03",
      "activity_kind": "meeting_held",
      "occurred_at": "2026-04-20T10:00:00Z",
      "rule_index": 0,
      "raw_points": 50,
      "applied_decay": 1.0,
      "points_contributed": 50.0
    }
  ],
  "as_of": "2026-05-18T10:30:00Z",
  "computed_at": "2026-05-18T10:30:00Z",
  "applicable": true,
  "stale": false,
  "activity_count": 3,
  "last_activity_at": "2026-04-20T10:00:00Z"
}
```

## Note / vincoli

- **`breakdown` ricco di default**: include una entry per ogni activity
  che ha contribuito (anche con 0 punti se filtrata da `cap`). Permette
  di rispondere a "perché Jane ha 95.5 punti?" senza riaprire il
  calcolo. Tagliato a max N entries (configurabile, default 100) per
  contenere dimensione documento; activity sotto soglia rappresentate
  in aggregato.
- **Time-travel**: per ricostruire uno score storico si lancia compute
  con `as_of = data_passata`. Lo snapshot risultante può essere salvato
  in una collection separata di **archivio**
  (`marketing_score_snapshots_history`, opzionale, fuori da Fase 2) o
  semplicemente restituito on-the-fly.
- **Invalidation**:
  - Modifica del profile → `stale = true` per tutti gli snapshot di
    quel profile.
  - Nuova activity sulla Person → eager recompute di tutti gli
    snapshot della Person che hanno il profile applicabile.
  - Cambio dei `custom_fields` o `tags` della Person → re-valuta
    `applicable` per tutti i profile.
- **Job notturno** (`Startable` interface): scansiona `stale = true` e
  ricalcola; resetta `stale = false`. Riassorbe eventuali skipped da
  eager.
- **Storage**: ordine di grandezza tipico = N Person × M profile.
  Trascurabile per i numeri attesi (decine di migliaia di document).
