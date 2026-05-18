---
type: db-schema
author: claude
date: 2026-05-18
domain: orkestra
addon: marketing
collection: marketing_card_types
tags: [orkestra, marketing, schema, db]
---

# `marketing_card_types`

## Scopo

Definizione **per-tenant** di un tipo di **card** (tessera, pass,
membership card, credenziale, ...).

Un card type è un **template** che dichiara:

- nome di visualizzazione configurabile per tenant (es. "Tessera
  Premium", "Honorary Pass", "Founders Circle");
- tier ammessi (es. `bronze`, `silver`, `gold`);
- formato del codice univoco;
- benefit di default;
- se è ammessa una sola card attiva per persona o molteplici.

Le card concrete emesse sono in [`marketing_cards`](marketing_cards.md).

Una persona può detenere card di tipi diversi simultaneamente
(es. "Premium Member" + "Honorary Pass" + "Beta Tester Pass") e,
opzionalmente, più card dello stesso tipo se il tipo lo consente.

## Schema

| Campo                       | Tipo            | Required | Default | Note                                                                                              |
| --------------------------- | --------------- | -------- | ------- | ------------------------------------------------------------------------------------------------- |
| `_id`                       | ObjectID        | yes      | auto    |                                                                                                   |
| `tenant_id`                 | string          | yes      | ctx     |                                                                                                   |
| `key`                       | string          | yes      | —       | Slug stabile per riferimento programmatico (es. `premium_member`). Lowercase, unique per tenant   |
| `display_name`              | string          | yes      | —       | Nome visualizzato (es. "Tessera Premium"). Custom per tenant, modificabile                        |
| `description`               | string          | no       | —       | Markdown                                                                                          |
| `tiers`                     | array<string>   | no       | `[]`    | Lista tier ammessi. Vuoto = nessun tier richiesto                                                 |
| `code_format`               | string          | yes      | —       | Template per generare il code. Es. `PREM-{YYYY}-{seq:05}`. Vedi note                              |
| `default_benefits`          | array<string>   | no       | `[]`    | Benefit applicati di default alle nuove card. Sovrascrivibili per istanza                         |
| `allow_multiple_per_person` | boolean         | yes      | `false` | Se `true`, una persona può avere più card attive di questo tipo                                   |
| `active`                    | boolean         | yes      | `true`  | Disattivazione blocca nuove emissioni; card esistenti restano valide                              |
| `created_at`                | datetime        | yes      | now     |                                                                                                   |
| `updated_at`                | datetime        | yes      | now     |                                                                                                   |
| `created_by`                | string (user_id)| no       | —       |                                                                                                   |
| `updated_by`                | string (user_id)| no       | —       |                                                                                                   |

## Indici

| Campi                   | Tipo     | Note                            |
| ----------------------- | -------- | ------------------------------- |
| `tenant_id + key`       | unique   | Key unico per tenant            |
| `tenant_id + active`    | btree    | Lista tipi attivi               |

## Esempio documento

```json
{
  "_id": "6607aa1234567890abcdef00",
  "tenant_id": "<tenant-id>",
  "key": "premium_member",
  "display_name": "Premium Membership",
  "description": "Membership riservata a clienti premium con accesso a contenuti esclusivi e advisory annuale.",
  "tiers": ["standard", "gold", "platinum"],
  "code_format": "PREM-{YYYY}-{seq:05}",
  "default_benefits": [
    "exclusive_events",
    "premium_newsletter",
    "yearly_advisory_session"
  ],
  "allow_multiple_per_person": false,
  "active": true,
  "created_at": "2026-05-18T10:00:00Z",
  "updated_at": "2026-05-18T10:00:00Z"
}
```

## Note / vincoli

- **Template `code_format`** supporta:
  - `{YYYY}` · `{YY}` · `{MM}` · `{DD}` — componenti data corrente
  - `{seq:N}` — sequenza zero-padded a N cifre, contatore per
    `(tenant_id, card_type_id)`
  - `{rand:N}` — N caratteri alfanumerici random (per code
    non-prevedibili)

  Validato al create del card type.
- **Sequence counter** mantenuto in collection counter dedicata (o
  via `findAndModify` atomico) per evitare race condition durante
  emissioni concorrenti.
- **Rinomina `display_name`**: sicura, non rompe riferimenti (le
  card puntano via `card_type_id`, non via nome).
- **Rinomina `key`**: sconsigliata se ci sono card emesse (rompe
  query programmatiche e dashboard). Possibile, ma admin UI mostra
  warning.
- **Disattivazione (`active: false`)**: blocca emissioni di card
  nuove. Le card esistenti di quel tipo restano funzionanti
  (`marketing_cards.status` non viene toccato). Re-attivare il tipo
  ripristina la possibilità di emettere.
- **Cancellazione tipo**: consentita solo se non ci sono card emesse
  di quel tipo (anche revocate). In caso contrario, l'admin UI
  suggerisce `active: false`.
- **Cambio `tiers`**: rimuovere un tier dall'elenco non migra le
  card esistenti che lo usano (continuano a esporre il tier
  rimosso, ma nuove card non potranno più usarlo). Aggiungere tier:
  sicuro.
- **`default_benefits` come stringhe libere**: in fase iniziale.
  Validazione contro un catalogo strutturato è feature futura.
