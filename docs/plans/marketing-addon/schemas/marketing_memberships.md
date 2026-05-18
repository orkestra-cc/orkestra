---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_memberships
tags: [orkestra, marketing, schema, db]
---

# `marketing_memberships`

## Scopo

Relazione **Person ↔ Organization** con metadati di ruolo e periodo.

Una Person può appartenere a più Organization simultaneamente (es.
consulente che lavora per più clienti, professionista con cariche
multiple), e la stessa Organization ha tipicamente molte Person.

Esattamente una membership per Person è marcata `primary = true`:
viene usata per denormalizzare `org_id` nelle Activity (decisione
D12).

## Schema

| Campo         | Tipo              | Required | Default | Note                                                                              |
| ------------- | ----------------- | -------- | ------- | --------------------------------------------------------------------------------- |
| `_id`         | ObjectID          | yes      | auto    |                                                                                   |
| `tenant_id`   | string            | yes      | ctx     |                                                                                   |
| `person_id`   | ObjectID          | yes      | —       | FK a `marketing_persons._id`                                                      |
| `org_id`      | ObjectID          | yes      | —       | FK a `marketing_organizations._id`                                                |
| `role`        | string            | no       | —       | Free text: titolo / ruolo (es. "CEO", "HR Director", "Founder")                   |
| `department`  | string            | no       | —       | Free text                                                                         |
| `since`       | date              | no       | —       | Inizio del ruolo, se noto                                                         |
| `until`       | date              | no       | —       | Fine del ruolo. Null = ancora attivo                                              |
| `active`      | boolean           | yes      | `true`  | Convenienza: `active = (until == null OR until > now)`                            |
| `primary`     | boolean           | yes      | `false` | Vincolo: esattamente una `primary = true` per ogni `person_id` con `active=true`  |
| `notes`       | string            | no       | —       |                                                                                   |
| `created_at`  | datetime          | yes      | now     |                                                                                   |
| `updated_at`  | datetime          | yes      | now     |                                                                                   |
| `created_by`  | string (user_id)  | no       | —       |                                                                                   |

## Indici

| Campi                                  | Tipo               | Note                                                                                       |
| -------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------ |
| `tenant_id + person_id`                | btree              | Lista organizzazioni di una persona                                                        |
| `tenant_id + org_id`                   | btree              | Lista persone di un'organizzazione                                                         |
| `tenant_id + person_id + org_id`       | unique partial     | Partial filter expression `active: true` — al massimo una membership attiva per coppia     |
| `tenant_id + person_id + primary`      | unique partial     | Partial filter `primary: true, active: true` — esattamente una primary per persona attiva  |
| `tenant_id + active`                   | btree              |                                                                                            |

## Esempio documento

```json
{
  "_id": "65f1a2b3c4d5e6f7a8b9c0f0",
  "tenant_id": "<tenant-id>",
  "person_id": "65f1a2b3c4d5e6f7a8b9c0e0",
  "org_id": "65f1a2b3c4d5e6f7a8b9c0d1",
  "role": "Director of Operations",
  "department": "Operations",
  "since": "2024-06-01",
  "until": null,
  "active": true,
  "primary": true,
  "notes": "Decision maker su acquisti SaaS.",
  "created_at": "2026-04-10T09:23:00Z",
  "updated_at": "2026-04-10T09:23:00Z"
}
```

## Note / vincoli

- **Storia preservata**: quando una persona cambia ruolo o azienda,
  **non** si modifica la membership esistente. Si chiude
  (`until = today`, `active = false`) e se ne crea una nuova. Permette
  aggregazioni storiche sui ruoli.
- **`primary` per denormalizzazione activity**: al momento dell'insert
  di una `marketing_activities`, il service legge la membership
  `primary + active = true` della Person e copia l'`org_id` nel campo
  denormalizzato dell'activity. Se nessuna primary è impostata,
  `activity.org_id = null` (la Person è "indipendente" in quel momento).
- **Vincolo unique partial** su `(person_id, primary=true, active=true)`:
  impedisce a livello DB l'esistenza di due primary attive. Enforce
  ulteriore a livello service quando si crea/cambia membership.
- **Cascade su delete**: hard delete di Person o Org cancella anche le
  memberships. Le Activity invece **non vengono toccate** (mantengono
  `person_id` orfano solo se la Person stessa è cancellata via flusso
  GDPR esplicito).
