---
type: spec
author: claude
date: 2026-05-18
domain: orkestra
component: marketing-addon
status: design
tags: [orkestra, marketing, spec]
---

# Orkestra — Addon `marketing`

Documento di design dell'addon `marketing` per la piattaforma Orkestra
(<https://github.com/orkestra-cc/orkestra>).

Addon **generico**, multi-tenant: ogni tenant configura i propri
custom field, tag, score profiles, card types. Nessun assunto
hard-coded sul dominio specifico di un cliente.

> Tutte le collection MongoDB di questo addon hanno il prefisso
> `marketing_`. Il dettaglio campo-per-campo di ciascuna è in
> [`schemas/`](schemas/).

---

## 1. Obiettivo

Costruire la fondazione marketing che oggi manca in Orkestra:

- **anagrafica unificata** (Organizzazioni + Persone) multi-tenant;
- **importer multi-sorgente** per consolidare dati frammentati su
  CSV, Excel, sistemi esterni via API;
- **storico immutabile delle attività** di ciascun contatto (event
  sourcing), per ricostruire qualunque lead score in qualunque momento
  storico;
- **profili di scoring multipli** in parallelo, ciascuno tarato su un
  segmento / audience diverso;
- **card / membership** come asset emessi dallo staff (non
  auto-registrazione), con tipi configurabili per tenant e possibilità
  di possedere più card per persona;
- **review queue** per gestire i conflitti di deduplica in import.

Tutto il resto del marketing (campagne, segmenti dinamici, form di lead
capture, dashboard analytics) si costruirà sopra questa fondazione in
fasi successive (vedi §10).

### 1.1 Perché un addon, perché ora

Orkestra ha già 13 addon (`agents`, `aimodels`, `billing`, `company`,
`compliance`, `dev`, `documents`, `graph`, `identity`, `payments`, `rag`,
`sales`, `subscriptions`), un SDK pubblico, e un sistema di moduli
pluggable maturo. **Nessuno** di questi gestisce contatti/anagrafiche
riusabili: il `sales` addon ha solo job AI di scraping che non
persistono lead; il core `notification` invia solo mail transazionali,
senza liste o segmenti. Quindi qualsiasi feature marketing — campagne,
inviti, card/membership, gating contenuti — poggia oggi sul vuoto.

L'addon `marketing` colma esattamente quel buco.

---

## 2. Decisioni prese

Tabella catalogo di tutte le decisioni di design discusse e validate.
Cambiarne una significa ridiscuterla esplicitamente — non sono default,
sono scelte motivate.

| #   | Area                  | Decisione                                                                                                  | Motivo                                                                                                                                                  |
| --- | --------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D01 | Scope addon           | Singolo addon `marketing` (non split `contacts` + `marketing`)                                             | Prefisso unico `marketing_` su tutte le collection; superficie API coerente                                                                             |
| D02 | Design                | Modello generico riusabile, configurabile per tenant                                                       | Filosofia Orkestra: tutti gli addon esistenti sono generici. Le specificità di dominio entrano via `custom_field_schemas`, `tags`, `card_types`, `score_profiles` |
| D03 | Entità anagrafica     | `Organization` + `Person` + `OrganizationMembership` separate                                              | Modello B2B + B2C senza collasso: un'azienda non è una persona, e una persona può afferire a più aziende nel tempo                                      |
| D04 | Multi-appartenenza    | Una Person può avere N tag e N valori in custom_fields multi_enum contemporaneamente                       | I segmenti reali raramente sono mutualmente esclusivi; classificazione gerarchica + multi-tag è espressiva e leggera                                    |
| D05 | Custom field          | Bag JSON tipizzato per tenant, schema definito in `marketing_custom_field_schemas`                         | Generico nel motore, shaped dai dati del tenant. Validazione write-time, evoluzione senza migrazione                                                    |
| D06 | Provenienza           | Array `sources[]` su Person/Org, non singolo campo                                                         | Un contatto può arrivare da più sorgenti nel tempo; serve traccia completa per debug, audit e re-import idempotente                                     |
| D07 | Tag                   | Entità separata `marketing_tags` con gerarchia parent/child                                                | Permette rename, color, alberatura senza migrazione dati                                                                                                |
| D08 | Card / membership     | Entità strutturata in `marketing_card_types` (template) + `marketing_cards` (istanze), gestite solo da staff | Asset emessi, non auto-registrazione. Tipo configurabile per tenant (nome, tier, code format, benefits). Una persona può avere più card di tipi diversi |
| D09 | Card nello score      | **Solo flag qualitativo, non punti**                                                                       | Le card sono privilegi/asset assegnati dallo staff, non comportamento misurabile. Filtrabili sui Person via `active_card_ids[]`                          |
| D10 | Activity log          | Append-only, immutabile, event sourcing puro                                                               | Replayability dello score: cambi formula → ricalcolo storico esatto; auditabilità GDPR                                                                  |
| D11 | Timestamp             | Doppio: `occurred_at` (nel mondo) + `recorded_at` (in DB)                                                  | Import storici di engagement (es. open email del 2024 importati nel 2026) hanno `occurred_at` 2024, `recorded_at` oggi. Senza doppio timestamp non si ricostruisce lo storico |
| D12 | Scope activity        | Solo `person_id`, con `org_id` denormalizzato per filter/aggregate                                         | Più semplice. Engagement aziendale via query aggregata, non duplicazione dati                                                                           |
| D13 | Modello score         | Score = funzione pura `f(Activity[occurred_at ≤ T], Profile) → value`                                      | Vista derivata, non stato. Snapshot è cache ricomputabile dall'Activity log                                                                             |
| D14 | Profili score         | Multipli paralleli per tenant                                                                              | Audience diverse hanno segnali drasticamente diversi: un download di whitepaper pesa molto per un profilo di lead-quality e poco per uno di newsletter engagement |
| D15 | Cadenza score         | Eager incrementale on-activity-insert + recompute notturno full                                            | Bilanciamento freshness/costo; in ogni caso la verità è sempre l'Activity log                                                                           |
| D16 | Importer              | Multi-source pluggabile: `csv`, `excel`, `odoo` (estendibile)                                              | Sorgenti reali tipiche; nuovi adapter = nuovi file `importers/<name>.go`, zero modifiche al resto                                                       |
| D17 | Sistemi via CSV       | Tool che esportano CSV (es. piattaforme campaign con export nativo) **non** ricevono adapter dedicato      | Riduce superficie: niente OAuth/client API da mantenere. Il loro export CSV passa per l'adapter `csv` generico                                          |
| D18 | Modalità import       | Solo one-shot, no sync periodico                                                                           | I dati esterni vengono **ingoiati** una volta e da quel momento la verità sta in `marketing_*`. Niente reconcile bidirezionale, niente webhook in ingresso |
| D19 | Output importer       | Stream duplice: `[Org, Person, Membership]` + `[Activity, ...]`                                            | Per importare storia engagement (open/click/send di campagne passate) serve generare Activity con `occurred_at` originale, non solo l'anagrafica         |
| D20 | Dedup conflict        | Merge automatico dei campi non in conflitto + review queue per i campi in conflitto                        | Massimizza riconciliazione automatica, escalation all'admin solo su collisioni reali                                                                    |
| D21 | Idempotenza activity  | `dedup_key` = hash di `(person_id, kind, occurred_at, external_id)`, unique                                | Re-import sicuro della stessa CSV non produce duplicati                                                                                                 |
| D22 | Append-only           | Activity non si UPDATE né DELETE — correzioni via `kind: corrected_by` che punta all'errata                | Non-negoziabile per replayability: modificare il passato rompe il ricalcolo storico                                                                     |
| D23 | Identità vs dati      | `marketing_persons` **non è un tier** della tenancy model di Orkestra — è data subject, non identità autenticata | I tier modellano chi si logga e agisce sulla piattaforma; un Person è dato di un cliente tier-2. Mantenere i due assi ortogonali evita conflitti di permission e lascia il design pronto a un bridge futuro verso un'identità separata se serve self-service (§3.7) |

---

## 3. Architettura addon

### 3.1 Integrazione con Orkestra

Layout standard di un addon Orkestra (riferimento: `backend/internal/addons/*` nel repo orkestra, doc `docs/onboarding/orkestra-sdk.md`):

```
backend/internal/addons/marketing/
├── module.go              # implementa l'interfaccia Module
├── routes.go              # registra le route Huma
├── config/                # ConfigSchema dell'addon
├── handlers/              # HTTP handlers Huma
├── services/              # logica di business (importer, scoring, conflict resolver, card lifecycle)
├── repository/            # accesso Mongo via tenantrepo
├── models/                # tipi Go (mappano le collection)
├── importers/             # adapter per-source (csv, excel, odoo)
└── scoring/               # engine di calcolo score
```

Build tag: `//go:build !no_addons || addon_marketing`.

Registrazione nel catalog: `backend/cmd/server/catalog_marketing.go` + voce in `allOptionalModuleNames`.

### 3.2 Sub-interfaces Module implementate

Dall'SDK Orkestra l'addon implementa, opt-in:

- `Module` (base): `Name() = "marketing"`, `Category() = "growth"`, `Init(deps)`.
- `HasCollections`: dichiara le 12 collection MongoDB (tutte con prefisso `marketing_`, vedi §4).
- `HasConfigSchema`: schema config dell'addon (vedi §3.5).
- `HasPermissions`: definisce le permission Cedar (vedi §3.6).
- `HasNavItems`: voci di menu nell'admin UI ("Contacts", "Card types", "Cards", "Imports", "Scoring").
- `Routable`: `RegisterRoutes(api)` registra gli endpoint Huma.
- `Startable` / `Stoppable`: avvia/ferma lo scheduler di recompute score notturno e il job di scadenza card.
- `HealthCheckable`: check su DB + worker scheduler.

### 3.3 Dipendenze su core Orkestra

- **`notification.Sender`** (core, già esistente): usato in fase 5+ per inviare email di campagna. In Fase 1-4 non viene toccato.
- **`tenantrepo.Scope` / `StampInsertM`**: scoping multi-tenant obbligatorio su ogni query. Il CI gate `tenantscope` di Orkestra blocca PR che non lo rispettano.
- **`ctxauth`**: identità utente dal JWT per audit (`created_by`, `issued_by`, `resolved_by`, ...).
- **Cedar policies**: ACL fine-grained sulle entità (chi può emettere card, chi può vedere quale segmento, ...).

### 3.4 Dipendenze su altri addon

- **`company` addon** (esistente in Orkestra): espone già una API di lookup P.IVA / VAT con cache `company_lookups`. L'importer Odoo (e in futuro altri) lo riusa per arricchire `Organization.legal_name` partendo da VAT, evitando duplicazione di logica.

Nessuna dipendenza su `sales`, `agents`, `documents`, `rag` in Fase 1-4. In fasi successive: potenziale integrazione con `agents` per scoring AI-assisted.

### 3.5 Config schema (per tenant)

L'addon espone in `HasConfigSchema` un piccolo set di parametri per tenant, modificabili da `/admin/modules`:

- `score.recompute_cron` — cron expression per il recompute notturno (default `0 3 * * *`).
- `score.eager_on_insert` — abilita ricalcolo incrementale on-activity-insert (default `true`).
- `card.expiration_check_cron` — cron per il job di revoca automatica delle card con `expires_at` raggiunto (default `0 2 * * *`).
- `import.dedup_keys.person` — campi che identificano una Person come "stessa" (default `["primary_email"]`).
- `import.dedup_keys.organization` — idem per Organization (default `["vat", "tax_code"]`).
- `import.conflict_review_required_fields` — campi su cui un conflict richiede review umana invece di auto-resolve (default `["primary_email", "vat", "tax_code"]`).

I parametri di card type (nome, tier, code format, benefits, multi-card per persona) **non** stanno qui: sono per-record in `marketing_card_types`, configurabili dall'admin UI senza toccare il config dell'addon.

### 3.6 Permessi Cedar

Definizione di permission applicate per risorsa:

- `marketing.contact.read` / `write` / `delete` — su Person e Organization.
- `marketing.activity.read` / `write` — write riservato a system + admin.
- `marketing.card_type.write` — chi può creare/modificare i tipi di card.
- `marketing.card.issue` / `revoke` / `suspend` — chi può emettere o cambiare stato delle card.
- `marketing.import.run` — chi può lanciare un import job.
- `marketing.conflict.resolve` — chi può chiudere review queue.
- `marketing.score_profile.write` — chi può creare/modificare profili di scoring.

### 3.7 Identità vs dati: perché `marketing_persons` non è un tier

Decisione di design (D23) che chiarisce la posizione di
`marketing_persons` nella tenancy model di Orkestra.

Orkestra distingue due tier di **identità autenticate**:

- **Tier-1**: operatori che gestiscono moduli e infrastruttura.
- **Tier-2**: clienti finali che consumano la piattaforma via
  subscription.

Entrambi hanno credenziali, JWT con audience distinte, permessi Cedar,
e **agiscono attivamente** sulla piattaforma.

`marketing_persons` non è un tier-3. Sono **data subject**, non
identità autenticate: la maggior parte non saprà mai che Orkestra
esiste — esistono in tabella perché un cliente tier-2 li ha importati
o registrati nel proprio CRM.

#### Due assi ortogonali

| Asse                    | Concetto                                                  | Modellato in                                      |
| ----------------------- | --------------------------------------------------------- | ------------------------------------------------- |
| **Identità** (tier)     | Chi può loggarsi e fare azioni sulla piattaforma          | `core.users`, tier nel JWT, permessi Cedar         |
| **Dati** (tenant_id)    | A quale cliente tier-2 appartiene il record               | `tenant_id` su ogni record `marketing_*`           |

Conflarli produce confusione semantica: un Person diventerebbe
contemporaneamente "scoped a tenant X" (dato di X) e "tier-3 autonomo"
(identità con permessi propri), con due regole di authz diverse sullo
stesso record.

#### Quando bridge a un'identità (futuro)

L'unico caso che giustificherebbe un'identità per i Person è
l'autenticazione per use case reali:

- **preference center / portale GDPR** (gestione consenso, export
  dati, right-to-be-forgotten self-service);
- **area riservata membri** (accesso a contenuti gated);
- **self-service profile update**.

In quel caso il pattern corretto **non è** promuovere
`marketing_persons` a tier, ma introdurre un'identità separata in core
e linkarla:

```
core.users                              marketing_persons
─ id, email, password_hash         ←→   ─ id, ..., user_id? (opzionale)
─ tier (operator | client | member)
─ tenant_id
```

Il Person resta dato del cliente tier-2. Lo User è identità separata
in core. Il link è opzionale e si popola solo per i (pochi) contatti
che davvero si autenticano.

Pattern noto in altri sistemi: Salesforce Contacts ↔ Community users,
HubSpot contacts ↔ membership accounts. **Mai conflati, sempre
linkati**.

#### Implicazione per il design corrente

Nessuna modifica necessaria al modello dati. Il design dell'addon è
già pronto per un bridge futuro:

- `marketing_persons` non ha campi di autenticazione (password, JWT
  info, MFA, ...).
- L'eventuale `user_id` arriverà come campo opzionale aggiunto in una
  fase successiva, senza migrazione.
- L'autenticazione e il portale data-subject vivranno in un addon
  dedicato (es. `marketing-portal`) o in un'estensione del core
  `identity`, **non** nell'addon `marketing` di base.

---

## 4. Modello dati

### 4.1 Collection (tutte con prefisso `marketing_`)

| Collection                          | Scopo                                                                  | Mutabilità  | Schema                                                                  |
| ----------------------------------- | ---------------------------------------------------------------------- | ----------- | ----------------------------------------------------------------------- |
| `marketing_organizations`           | Anagrafica enti (aziende, PA, fondazioni, associazioni)                | mutable     | [schemas/marketing_organizations.md](schemas/marketing_organizations.md)             |
| `marketing_persons`                 | Anagrafica persone fisiche                                              | mutable     | [schemas/marketing_persons.md](schemas/marketing_persons.md)                         |
| `marketing_memberships`             | Relazione Person ↔ Organization (con ruolo, periodo, primary)          | mutable     | [schemas/marketing_memberships.md](schemas/marketing_memberships.md)                 |
| `marketing_tags`                    | Tag gerarchici riusabili                                                | mutable     | [schemas/marketing_tags.md](schemas/marketing_tags.md)                               |
| `marketing_custom_field_schemas`    | Definizione dei custom field per tenant (per Person e Org)              | mutable     | [schemas/marketing_custom_field_schemas.md](schemas/marketing_custom_field_schemas.md) |
| `marketing_activities`              | Event log immutabile di tutte le interazioni                            | **append-only** | [schemas/marketing_activities.md](schemas/marketing_activities.md)               |
| `marketing_score_profiles`          | Configurazione di un profilo di scoring                                 | mutable     | [schemas/marketing_score_profiles.md](schemas/marketing_score_profiles.md)           |
| `marketing_score_snapshots`         | Cache dei valori di score calcolati                                     | rebuildable | [schemas/marketing_score_snapshots.md](schemas/marketing_score_snapshots.md)         |
| `marketing_card_types`              | Template di tipo card (nome, tier, code format, benefits)               | mutable     | [schemas/marketing_card_types.md](schemas/marketing_card_types.md)                   |
| `marketing_cards`                   | Card concrete emesse a una Person                                       | mutable     | [schemas/marketing_cards.md](schemas/marketing_cards.md)                             |
| `marketing_import_jobs`             | Audit di ogni esecuzione di import                                      | mutable (stato) | [schemas/marketing_import_jobs.md](schemas/marketing_import_jobs.md)             |
| `marketing_conflict_reviews`        | Coda dei conflitti di dedup in attesa di risoluzione                    | mutable (stato) | [schemas/marketing_conflict_reviews.md](schemas/marketing_conflict_reviews.md)   |

### 4.2 Relazioni

```
┌──────────────────────────┐         ┌───────────────────────────┐
│  marketing_organizations │◄────┐   │  marketing_persons        │
│                          │     │   │                           │
│  - legal_name            │     │   │  - first_name, last_name  │
│  - vat / tax_code        │     │   │  - emails[]               │
│  - kind                  │     │   │  - tags[] ────────────┐   │
│  - tags[] ───────────┐   │     │   │  - custom_fields {}   │   │
│  - custom_fields {}  │   │     │   │  - sources[]          │   │
│  - sources[]         │   │     │   │  - active_card_ids[]──┐   │
└──────────────────────┼───┘     │   └───────────────────┬───┼───┘
                       │         │                       │   │
                       │   ┌─────┴─────────────────┐     │   │
                       │   │ marketing_memberships │     │   │
                       │   │                       │     │   │
                       │   │  - person_id ─────────┼─────┘   │
                       │   │  - org_id  ───────────┼─────────┘
                       │   │  - role, since, until │
                       │   │  - primary (bool)     │
                       │   └───────────────────────┘
                       │
                       │   ┌───────────────────────┐
                       └───┤  marketing_tags       │
                           │  (parent_id → self)   │
                           └───────────────────────┘

┌──────────────────────────┐
│  marketing_activities    │
│                          │
│  - person_id ────────────┼──→ marketing_persons
│  - org_id (denormalized) │──→ marketing_organizations
│  - kind                  │
│  - occurred_at           │
│  - recorded_at           │
│  - payload {}            │
│  - refs {card_id, ...}   │
│  - dedup_key (unique)    │
└──────────────────────────┘
              │
              ▼
┌──────────────────────────┐         ┌──────────────────────────┐
│  marketing_score_profiles│◄────────┤ marketing_score_snapshots│
│                          │         │                          │
│  - rules[]               │         │  - person_id             │
│  - filters               │         │  - profile_id            │
│  - decay_fn              │         │  - value, breakdown[]    │
└──────────────────────────┘         │  - as_of, computed_at    │
                                     └──────────────────────────┘

┌──────────────────────────┐         ┌──────────────────────────┐
│  marketing_card_types    │◄────────┤  marketing_cards         │
│                          │         │                          │
│  - key, display_name     │         │  - card_type_id          │
│  - tiers[]               │         │  - person_id             │
│  - code_format           │         │  - code (unique)         │
│  - default_benefits[]    │         │  - tier, status          │
│  - allow_multiple_per_   │         │  - benefits[]            │
│      person              │         │  - issued_by / at        │
└──────────────────────────┘         └──────────────────────────┘

┌──────────────────────────┐         ┌──────────────────────────┐
│  marketing_import_jobs   │         │ marketing_conflict_      │
│                          │         │ reviews                  │
│  - importer (csv/...)    │────────►│                          │
│  - status                │         │  - import_job_id         │
│  - stats {}              │         │  - target_kind           │
└──────────────────────────┘         │  - existing_id           │
                                     │  - incoming_payload      │
                                     │  - conflicts[]           │
                                     │  - status                │
                                     └──────────────────────────┘
```

---

## 5. Pipeline importer

### 5.1 Contratto comune (interface Go)

```go
type Importer interface {
    // Metadata statici
    Describe() Descriptor // { Name, Capabilities, ConfigSchema }

    // Validazione del config prima dell'esecuzione
    ValidateConfig(cfg Config) error

    // Anteprima senza scrivere: mostra cosa farebbe l'import
    DryRun(ctx context.Context, cfg Config, src Source) (PreviewReport, error)

    // Esecuzione effettiva, scrive in DB
    Run(ctx context.Context, cfg Config, src Source, job *Job) (Result, error)
}
```

Tutti gli importer implementano questa interfaccia. L'aggiunta di una nuova sorgente in futuro = un nuovo file `importers/<name>.go`, zero modifiche al resto del codice.

### 5.2 Pipeline interna (uguale per ogni adapter)

```
source ──► extract     (per-adapter: legge righe/record dalla sorgente)
        ──► normalize   (per-adapter: produce CanonicalRecord + CanonicalActivity[])
        ──► map         (canonical → Person/Org/Membership/Activity)
        ──► dedup       (chiavi: email per Person, vat/tax_code per Org)
        ──► conflict    (auto-merge non-conflicting; per i conflict → ConflictReview)
        ──► commit      (scrive in DB + popola sources[] + dedup_key per activity)
```

### 5.3 Output duplice (decisione D19)

Ogni importer produce **due stream** di output:

1. **Canonical records**: `[Organization, Person, OrganizationMembership]` → dedup/merge.
2. **Canonical activities**: `[Activity, Activity, ...]` con `occurred_at` originale → append idempotente via `dedup_key`.

Il secondo stream è ciò che rende possibile importare lo storico engagement (open/click di una campagna inviata anni fa, partecipazioni a eventi pregressi, ...).

### 5.4 Adapter previsti

| Adapter | Fonte                              | Note                                                                                                       |
| ------- | ---------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `csv`   | File CSV generico                  | UI di mapping colonne → campi canonici. Usato anche per qualunque sistema esterno che esporta CSV          |
| `excel` | File .xlsx                         | Wrapper su `csv` con sheet picker; supporto a celle formattate                                             |
| `odoo`  | Odoo `res.partner` via XML-RPC     | Gestisce `parent_id` (relazione company↔contact) e mappa `category_ids` di Odoo a `marketing_tags`         |

### 5.5 Strategia di deduplica

**Person**:

1. Chiave primaria di match: `primary_email` (normalizzato lowercase).
2. Se incoming porta un'email che esiste già su un altro Person → match.
3. Se incoming porta `first_name + last_name + phone` matching → soft-match (review queue, mai auto-merge).

**Organization**:

1. Chiave primaria di match: `vat` (normalizzato).
2. Fallback: `tax_code`.
3. Soft-match: `legal_name` esatto (case-insensitive, trim) → review queue.

### 5.6 Strategia di conflict resolution (D20)

Per ogni record matched:

- **Auto-merge** di tutti i campi non in conflitto (incoming porta un campo che è `null` o vuoto sull'esistente → si scrive).
- **Auto-merge** dei campi additivi (`tags[]`, `emails[]`, `phones[]`, `sources[]`) — sempre unione, mai sovrascrittura.
- **Conflict review** per i campi a sovrascrittura: se incoming e esistente hanno valori diversi e non vuoti sui campi listati in `import.conflict_review_required_fields` (default: `primary_email`, `vat`, `tax_code`) → record finisce in `marketing_conflict_reviews` con stato `pending`, **non viene committato** finché un admin non risolve.

### 5.7 Idempotenza re-import (D21)

Le **Activity** generate da un importer hanno `dedup_key` = `sha256(person_id + kind + occurred_at + external_id)`, con indice **unique**.

Conseguenza: ri-lanciare lo stesso import (stesso file CSV, stesso esportato Odoo) non duplica activity. Permette di:

- Rifare un import dopo aver corretto il mapping.
- Reimportare incrementalmente lo stesso esportato mensile.
- Recuperare da un import job fallito a metà.

---

## 6. Activity log & scoring

### 6.1 Append-only event log (D10, D22)

`marketing_activities` è la **single source of truth** per qualsiasi computazione di engagement / scoring / analytics. Regole inderogabili:

- **No UPDATE**: una activity registrata non si modifica.
- **No DELETE**: una activity registrata non si cancella.
- **Correzioni**: nuova activity con `kind: corrected_by` e `refs.corrects_activity_id` puntando all'errata.
- **Cancellazione GDPR** (right to be forgotten): hard-delete della Person + cascade su activity (questa è l'unica eccezione e produce un audit record permanente in un log separato).

### 6.2 Tassonomia dei `kind` previsti

Definita inizialmente come enum, estensibile per addon futuri:

**Email**: `email_sent`, `email_opened`, `email_clicked`, `email_bounced`, `email_unsubscribed`, `email_complained`.

**Eventi**: `event_invited`, `event_registered`, `event_attended`, `event_no_show`, `event_cancelled`.

**Web/form**: `form_submitted`, `page_visited`, `content_downloaded`.

**Diretto**: `call_made`, `meeting_held`, `note_added`.

**Sistema**: `imported`, `merged`, `tag_added`, `tag_removed`, `card_issued`, `card_status_changed`, `corrected_by`.

Per ogni `kind` lo schema del `payload` è dichiarato e validato (vedi `marketing_activities` per dettaglio).

### 6.3 Score = funzione pura (D13)

```
Score(person_id, profile_id, as_of=T) :=
    Σ_{a ∈ Activities(person_id, occurred_at ≤ T)}
        rule(a.kind, profile).points
        × decay(profile.decay_fn, T - a.occurred_at)
        capped to rule.cap
        filtered by profile.filters
```

Conseguenze pratiche:

- **Replayability**: "qual era lo score di X al 2026-01-15?" → si replay-a con `as_of = 2026-01-15`. Risposta esatta.
- **Cambio formula**: si modifica `marketing_score_profiles.rules` → snapshot vengono invalidati → al prossimo read si ricalcolano dall'Activity log. Nessuna migrazione dati.
- **Profili multipli** (D14): si calcolano in parallelo. Una Person ha N score, uno per profile attivo.
- **Spiegabilità**: lo snapshot include `breakdown[]` con `(activity_id, points_contributed, applied_decay)` — per ogni activity quanto ha contribuito allo score finale.

### 6.4 Decay functions supportate

- `none`: nessun decay, il punteggio è permanente.
- `linear`: `points × max(0, 1 - age_days / window_days)`.
- `exponential`: `points × exp(-age_days / half_life_days × ln(2))`.

Configurabili per rule (alcune attività decadono velocemente, es. apertura email; altre lentamente, es. partecipazione evento).

### 6.5 Cadenza di calcolo (D15)

**Due strategie combinate**:

1. **Eager incrementale on-insert**: ogni nuova Activity scatena un ricalcolo dello score della Person impattata per tutti i profile attivi. Aggiornamento snapshot immediato. Configurabile con `score.eager_on_insert`.

2. **Recompute notturno full**: un job schedulato (cron `score.recompute_cron`, default `0 3 * * *`) ricalcola tutti gli snapshot per tutti i profile attivi del tenant. Recupera eventuali eager skipped e riassorbe modifiche ai profile.

La verità è sempre nell'Activity log. Lo snapshot è solo cache.

### 6.6 Linee guida sui profili score multipli

Un tenant tipico configurerà tra 2 e 6 profile, uno per ogni audience /
segmento per cui ha bisogno di un ranking distinto. Esempi di profile
ben formati:

- **`hot_lead`** — generico lead-quality: pesa forte gli engagement
  vicini alla conversione (meeting, form, demo) e leggero gli
  engagement passivi (open, page view).
- **`newsletter_engagement`** — solo segnali email; serve per decidere
  liste di newsletter / unsubscribe propattivo.
- **`event_affinity`** — pesa partecipazione eventi e content
  download legato a eventi; usato per inviti a eventi successivi.
- **`<audience>_lead`** — filtro per `custom_field_filters` su un
  attributo discriminante (es. `industry`, `region`,
  `target_segment`), rules pesate per il comportamento tipico
  dell'audience.

Più di 10 profile suggerisce ridondanza nelle regole — meglio
consolidare. I profile sono editabili da admin UI senza redeploy.

---

## 7. Card / membership

### 7.1 Modello a due collection

Per supportare card di tipi diversi e configurabili per tenant
(decisione D08), l'addon usa due collection:

- **[`marketing_card_types`](schemas/marketing_card_types.md)** —
  **template** che definiscono un tipo di card: nome di
  visualizzazione personalizzato per tenant (es. "Premium
  Membership", "Honorary Pass", "Founders Circle"), tier ammessi,
  formato del codice, benefit di default, regola "una sola attiva per
  persona" o "multiple consentite".
- **[`marketing_cards`](schemas/marketing_cards.md)** — **istanze**
  emesse a una Person, referenziano un card_type via `card_type_id`.

Conseguenze:

- Un tenant può definire 0..N card types.
- Una Person può detenere card di tipi diversi simultaneamente
  (`active_card_ids[]` su Person — denormalizzato per filter rapido).
- Una Person può detenere più card dello **stesso tipo** se il tipo
  ha `allow_multiple_per_person = true`; altrimenti una sola attiva.

### 7.2 Workflow staff-only

Le card **non** sono auto-emesse dall'utente finale. Il flusso è:

1. Staff naviga in `marketing_persons`, trova il contatto.
2. Click "Issue card" → seleziona il `card_type`, eventuale `tier`,
   override dei `benefits` se serve.
3. Server genera `code` univoco dal `card_type.code_format`, salva in
   `marketing_cards` con `status = active`, `issued_at = now()`,
   `issued_by = current_user_id`.
4. Aggiorna `marketing_persons.active_card_ids` (denormalizzato).
5. Emette automaticamente una Activity `kind: card_issued` con payload
   `{card_id, card_type_id, card_type_key, code, tier}` — **per
   audit** (non concorre allo score, D09).

### 7.3 Sospensione / revoca / scadenza

- **Suspend**: `status = suspended`, motivo opzionale. Reversibile.
  Activity `card_status_changed {from: active, to: suspended}`.
- **Revoke**: `status = revoked`, `revoked_at`, `revoked_by`,
  `revoke_reason`. **Non reversibile** (per emettere nuova si crea
  record nuovo). Activity `card_status_changed {from: *, to: revoked}`.
- **Expire**: job schedulato `card.expiration_check_cron` revoca
  automaticamente le card con `expires_at` raggiunto, ponendo
  `revoke_reason: "expired"`. Activity emessa come per revoca manuale.

In tutti e tre i casi `active_card_ids` su Person viene aggiornato
atomicamente.

### 7.4 Permessi (Cedar)

- `marketing.card_type.write` — chi può creare/modificare i tipi.
- `marketing.card.issue` — emissione.
- `marketing.card.suspend` / `revoke` — cambio di stato.
- Tipicamente assegnate a ruoli `staff_marketing` o `admin`, non a
  `member` o ruoli generici.

### 7.5 Relazione con score (D09)

Le card **non** entrano nel calcolo punti. Sono **flag qualitativi**
consultabili via API/UI:

- Filter contatti: `active_card_ids` non vuoto, oppure
  `active_card_ids contains card_type_X`.
- Segmenti dinamici (Fase 5): "tutti i tesserati di tipo Y", "tutti i
  Gold tier".
- Le activity `card_issued` / `card_status_changed` esistono solo per
  audit log.

---

## 8. Conflict review queue

### 8.1 Quando si crea una review

Un import job crea un record `marketing_conflict_reviews` quando, durante la pipeline §5.6:

- Un campo in `conflict_review_required_fields` (default: `primary_email`, `vat`, `tax_code`) ha valore diverso e non vuoto tra esistente e incoming.

Il record incoming **non viene committato** finché la review non è risolta.

### 8.2 Stati

- `pending`: nuova review creata, in attesa di risoluzione.
- `resolved`: admin ha scelto una resolution (vedi sotto), il commit è stato applicato.
- `dismissed`: admin ha deciso di scartare l'incoming senza fare nulla.

### 8.3 Resolution disponibili

- `keep_existing`: si scartano i campi conflittuali dell'incoming; resta com'era.
- `take_incoming`: i campi conflittuali dell'incoming sovrascrivono l'esistente.
- `manual_merge`: admin specifica campo-per-campo quali valori tenere (struttura `field_overrides {field → value}`).

In tutti e tre i casi i campi non-conflictuali sono già stati auto-mergiati al momento della creazione della review.

### 8.4 UX

Lo stato `paused_for_review` di un `marketing_import_jobs` indica che ci sono review pending. L'admin UI mostra:

- Lista review pending raggruppate per import job.
- Per ogni review: side-by-side dei campi conflittuali, payload incoming completo, riferimento al record esistente.
- Bulk actions: "take all incoming", "keep all existing" per import job interi.

---

## 9. Roadmap di implementazione

Fasi sequenziali. Ogni fase produce valore autonomo e può andare in produzione da sola.

### Fase 1 — Fondazione anagrafica (MVP)

**Obiettivo**: un tenant può importare i propri contatti frammentati e gestirli da admin UI.

- [ ] Scaffold modulo (`module.go`, build tag, registrazione catalog).
- [ ] Collections: `marketing_organizations`, `marketing_persons`, `marketing_memberships`, `marketing_tags`, `marketing_custom_field_schemas`.
- [ ] API CRUD base (Huma routes) per le 5 collection.
- [ ] Permessi Cedar (`marketing.contact.*`).
- [ ] Importer `csv` con UI di mapping colonne.
- [ ] Dedup base (email per Person, VAT/tax_code per Org).
- [ ] Auto-merge non-conflicting (senza review queue ancora).
- [ ] Tracking `sources[]` per provenienza.

**Verifica**: import di un CSV reale, dedup attivo, contatti consultabili.

### Fase 2 — Storicizzazione & scoring

**Obiettivo**: ricostruire/mantenere score multi-profilo replayable.

- [ ] Collection `marketing_activities` con vincolo append-only enforced a livello service.
- [ ] Collection `marketing_score_profiles` + `marketing_score_snapshots`.
- [ ] Compute engine (eager on-insert + scheduler notturno via `Startable`).
- [ ] API: timeline per contatto, lista score per Person, breakdown di uno score.
- [ ] Almeno 1 profile esempio configurato (per validare engine).
- [ ] Activity emesse automaticamente da CRUD esistente (es. `tag_added` quando si aggiunge un tag).

**Verifica**: time-travel di uno score storico funziona; cambio rules → snapshot si ricalcola.

### Fase 3 — Import avanzati

**Obiettivo**: assorbire le altre sorgenti (Excel, Odoo) e lo storico engagement (CSV export di sistemi campaign / CRM esterni).

- [ ] Importer `excel` con sheet picker.
- [ ] Importer `odoo` (XML-RPC, mapping `parent_id`, `category_ids` → tag).
- [ ] Importer `csv` esteso per produrre Activity da export di engagement (riconoscimento colonne open/click/bounce).
- [ ] Collection `marketing_conflict_reviews` + workflow review queue.
- [ ] Admin UI per gestire review (side-by-side, bulk actions).

**Verifica**: import Odoo reale produce Org+Person+Membership coerenti; import di engagement export produce activity storiche con `occurred_at` corretto.

### Fase 4 — Card / membership

**Obiettivo**: lo staff può configurare tipi di card per il proprio dominio e emetterli/revocarli.

- [ ] Collection `marketing_card_types` + API admin per la gestione dei tipi.
- [ ] Collection `marketing_cards`.
- [ ] API staff: issue / suspend / revoke / reinstate.
- [ ] Generatore codice univoco con template per-type.
- [ ] Permessi Cedar (`marketing.card_type.*`, `marketing.card.*`).
- [ ] Activity di audit (`card_issued`, `card_status_changed`).
- [ ] Filter Person `has_any_active_card` + `active_card_ids contains <type_id>`.
- [ ] Job schedulato di scadenza automatica (`expires_at`).

**Verifica**: un tenant definisce 1-2 card types, emette card a contatti, audit visibile nella timeline del contatto, multiple card per Person funzionano.

### Fase 5 — (Future) Marketing operativo

Out of scope per la fondazione, sketchato per orientamento futuro:

- Segmenti dinamici (filtri salvati come entità).
- Form di lead capture embeddabili (snippet JS o endpoint REST per form esterni).
- Campagne email (integrazione `notification.Sender`).
- Webhook ESP (Sendgrid/Postmark/Mailgun) per popolare `email_opened`/`email_clicked` in real-time invece che da import.
- Eventuale sync periodico read-only di sistemi esterni (se decisione D18 cambia).
- Analytics dashboard per segmenti.
- AI-assisted scoring (integrazione `agents` addon).

---

## 10. Note GDPR / compliance

L'addon tratta dati personali e va progettato consapevolmente:

- **Base giuridica**: legittimo interesse per anagrafica B2B (qualifiche, contatti pubblici aziendali); consenso esplicito per persone fisiche prima del marketing diretto. Il campo `consent` su Person tiene traccia di base, data, fonte.
- **Right of access**: API export di tutto ciò che riguarda un person_id (anagrafica + memberships + activity + score history + card).
- **Right to be forgotten**: cancellazione hard di Person con cascade su Activity. Audit permanente in collection separata (out of `marketing_*`, in core).
- **Append-only Activity** è un **vantaggio** per audit, non un ostacolo: ogni interazione è tracciabile con timestamp e source. La cancellazione (eccezione GDPR) è esplicita e loggata.
- **Retention**: configurabile per `kind` di Activity (es. `page_visited` decade dopo N mesi). Out of scope Fase 1-4, da disegnare in Fase 5.

Riferimenti normativi rilevanti (variano per giurisdizione del tenant): GDPR (Reg. UE 2016/679) per l'EU; CCPA per la California; LGPD per il Brasile; equivalenti locali.

---

## 11. Riferimenti

- Repo Orkestra: <https://github.com/orkestra-cc/orkestra>
- SDK Orkestra: <https://github.com/orkestra-cc/orkestra-sdk>
- Doc canonica modello addon: `docs/onboarding/orkestra-sdk.md` (nel repo orkestra).
- Schemi DB campo-per-campo: [`schemas/`](schemas/).

---

*Documento di design. L'implementazione produrrà l'addon `backend/internal/addons/marketing/` del repo orkestra.*
