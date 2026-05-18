---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_tags
tags: [orkestra, marketing, schema, db]
---

# `marketing_tags`

## Scopo

**Tag gerarchici** riutilizzabili applicabili a Person e Organization.

Distinti dai `custom_fields`:

- `custom_fields` modellano attributi strutturati con tipo (es.
  `industry = "manufacturing"`), validati contro schema.
- `tags` modellano etichette libere ma controllate (gerarchia,
  rename, color), buone per segmentazione esplorativa, classificazione
  ad-hoc, sub-categorie ("Industry > Manufacturing > Automotive").

Decisione D07: entità separata, non stringa libera, per permettere
rename e color senza migrazione dei record che li referenziano.

## Schema

| Campo         | Tipo              | Required | Default | Note                                                                          |
| ------------- | ----------------- | -------- | ------- | ----------------------------------------------------------------------------- |
| `_id`         | ObjectID          | yes      | auto    |                                                                               |
| `tenant_id`   | string            | yes      | ctx     |                                                                               |
| `name`        | string            | yes      | —       | Nome visualizzato. Modificabile (rename non rompe i riferimenti)              |
| `slug`        | string            | yes      | auto    | Slug stabile, derivato da `name` al primo create. Modificabile con cautela    |
| `description` | string            | no       | —       |                                                                               |
| `color`       | string            | no       | —       | Esadecimale `#RRGGBB` per UI                                                  |
| `parent_id`   | ObjectID          | no       | —       | Self-reference. Null = tag radice. Profondità arbitraria                      |
| `path`        | string            | yes      | derived | Materialized path "/Industry/Manufacturing/Automotive" per query di sottoalberi |
| `created_at`  | datetime          | yes      | now     |                                                                               |
| `updated_at`  | datetime          | yes      | now     |                                                                               |
| `created_by`  | string (user_id)  | no       | —       |                                                                               |

## Indici

| Campi                   | Tipo                | Note                                              |
| ----------------------- | ------------------- | ------------------------------------------------- |
| `tenant_id + slug`      | unique              | Slug unico per tenant                             |
| `tenant_id + parent_id` | btree               | Lista figli di un tag                             |
| `tenant_id + path`      | btree (prefix scan) | Query "tutti i discendenti di /Industry"          |
| `tenant_id + name`      | text                | Ricerca per nome                                  |

## Esempio documento

```json
{
  "_id": "6601aa1234567890abcdef01",
  "tenant_id": "<tenant-id>",
  "name": "Automotive",
  "slug": "automotive",
  "description": "Aziende del settore automotive",
  "color": "#1f6feb",
  "parent_id": "6601aa1234567890abcdef00",
  "path": "/Industry/Manufacturing/Automotive",
  "created_at": "2026-04-10T09:23:00Z",
  "updated_at": "2026-04-10T09:23:00Z"
}
```

Con il genitore:

```json
{
  "_id": "6601aa1234567890abcdef00",
  "tenant_id": "<tenant-id>",
  "name": "Manufacturing",
  "slug": "manufacturing",
  "parent_id": "6601aaINDUSTRY00000000",
  "path": "/Industry/Manufacturing"
}
```

## Note / vincoli

- **`name` mutabile, `slug` stabile**: il rename di un tag aggiorna
  solo `name` e `path` (e `path` di tutti i discendenti — operazione
  batch). I record Person/Org che referenziano via `_id` non vengono
  toccati. È esattamente per evitare migrazione dati di massa che si
  sceglie entità separata invece di stringa libera.
- **`path` rigenerato su sposta-sottoalbero**: se si cambia `parent_id`
  di un tag X, tutti i suoi discendenti hanno il `path` riscritto.
  Operazione esposta come `move_subtree` esplicita, non via update
  generico.
- **Cascade su delete**: hard delete di un tag rimuove i riferimenti
  nei `tags[]` di Person/Org (cascade gestito dal service). I figli
  diretti perdono il parent (diventano root) — eccezione: l'admin UI
  chiederà conferma e offrirà "delete with subtree" o "promote
  children".
- **Seed tag-tree per tenant**: l'addon non popola tag di default. Ogni
  tenant è libero di costruire la propria tassonomia (settori,
  geografie, segmenti, fasce di valore, ecc.) via admin UI o
  importer mapping. Buon punto di partenza: 2-3 radici (es.
  `/Industry`, `/Region`, `/Segment`) con sotto-alberi cresciuti
  dall'uso reale.
