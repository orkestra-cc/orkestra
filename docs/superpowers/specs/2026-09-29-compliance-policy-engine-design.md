# Compliance: policy configurabili di logging e conservazione — Design

Branch: `docs/compliance-policy-engine` (da `dev` @ `64258483e`).
Brainstorming del 2026-09-29 con Salvatore, dopo un'analisi del logging su
tutto lo stack (backend, infrastruttura, frontend, mobile).

## Obiettivo

Orkestra è un SaaS: clienti diversi hanno obblighi diversi (GDPR, norme del
Garante, ISO/IEC 27001:2022, clausole contrattuali). Oggi le regole su **cosa
finisce nei log** e **per quanto si conservano audit e security events** sono
cablate nel codice e negli indici TTL, e non esiste una policy documentata.

Questo progetto introduce un **motore di policy di conformità** nel modulo
core `compliance`:

1. un **catalogo di policy con nome e versione**, di cui una è la policy di
   **piattaforma**;
2. l'**assegnazione di una policy a un tenant** (Tier-1 o Tier-2) come
   override;
3. l'**applicazione** delle policy a due dimensioni:
   - contenuto dei log operativi (IP, user agent, identificativi, dati
     personali, testo libero);
   - conservazione di `compliance_audit_events` e `auth_security_events`;
4. **evidenze** per l'auditor: storico immutabile delle versioni, audit di
   ogni modifica con motivazione, export.

Criterio di successo: un operatore crea la policy «Cliente sanità» con
`ipAddress=omitted` e audit a 400 giorni, la assegna al tenant T scrivendo una
motivazione; entro 5 secondi le righe di log delle richieste di T (compresa
`http_request`, che ora porta `tenant_id`) non contengono l'IP, le righe senza
tenant sono mascherate secondo la policy più restrittiva attiva, il job
notturno cancella gli eventi di audit di T più vecchi di 400 giorni tranne
quelli dei soggetti sotto legal hold, e l'export per l'auditor mostra policy,
versioni e assegnazione con autore, data e motivazione.

### Obblighi coperti

| Riferimento | Come |
|---|---|
| GDPR art. 5.1.c (minimizzazione) | `logContent`: IP/UA/identificativi ridotti, dati personali mascherati |
| GDPR art. 5.1.e (limitazione della conservazione) | `retention` applicata anche agli eventi già scritti |
| GDPR art. 5.2 (responsabilizzazione) | versioni immutabili, audit con motivazione, export |
| GDPR art. 32 | maschera sempre attiva dei segreti, garanzia sulle righe senza tenant |
| Garante, provvedimento amministratori di sistema (2008) | avviso sotto i 6 mesi di audit; il job rispetta le legal hold |
| ISO 27001 A.8.10 (cancellazione) | job di conservazione con evidenza `compliance.retention.purged` |
| ISO 27001 A.8.11 (mascheramento) | `PolicyHandler` slog |
| ISO 27001 A.8.15 (logging) / A.5.28 (evidenze) | storico versioni + export; correzione del request logger |

## Decisioni del brainstorming

- **D1** — Livello: **piattaforma + override per tenant**. Le dimensioni
  applicabili per tenant (contenuto dei log, conservazione dell'audit) si
  possono assegnare ai tenant; quelle legate all'installazione restano sulla
  policy di piattaforma.
- **D2** — Un override può andare in **entrambe le direzioni, con
  motivazione**: niente è vietato, ma ciò che è meno restrittivo della
  piattaforma (o sotto soglie note) genera avvisi che vanno confermati.
- **D3** — Assegnazione e modifica **solo da operatori Tier-1**. Il cliente
  Tier-2 vede in sola lettura la policy che gli si applica (solo API in questo
  giro).
- **D4** — Primo sottoprogetto = motore + contenuto dei log + conservazione di
  audit e security events. Conservazione infrastrutturale (Loki, Tempo,
  rotazione dei container), errori dal client e alert sono sottoprogetti
  successivi.
- **D5** — Approccio: **catalogo con nome dentro `compliance`**, non un nuovo
  modulo core e non un'estensione di `ConfigService` (piatto, senza versioni,
  senza riuso tra tenant).
- **D6** — Le righe di log **senza tenant** usano la **policy più restrittiva
  tra quelle attive**, non quella di piattaforma.
- **D7** — La correzione del request logger (`tenant_id`/`user_id` mai
  presenti su `http_request`) fa parte di questo progetto: senza, la policy per
  tenant non si applicherebbe alle righe più frequenti.
- **D8** — La conservazione passa da **indici TTL** a un **job giornaliero**
  che si applica anche agli eventi già scritti e rispetta le legal hold.
- **D9** — Motivazione **obbligatoria su ogni salvataggio**; la conferma
  esplicita serve in più quando ci sono avvisi.
- **D10** — I metadati di audit di una modifica contengono i valori **prima e
  dopo** di ogni campo cambiato (la policy non contiene dati personali).
- **D11** — I **segreti** (password, token, cookie, chiavi…) sono sempre
  mascherati e non sono configurabili: è sicurezza, non una scelta di privacy.

## 1. Modello

### 1.1 Policy

Tipi condivisi in `pkg/sdk/iface` (li usano `compliance` e
`internal/shared/utils`, che non può importare `compliance`):

```go
// iface/compliance_policy.go
type IPAddressMode string   // "full" | "truncated" | "hashed" | "omitted"
type UserAgentMode string   // "full" | "omitted"
type SubjectIDMode string   // "uuid" | "hashed" | "omitted"

type LogContentPolicy struct {
    IPAddress    IPAddressMode `bson:"ipAddress" json:"ipAddress"`
    UserAgent    UserAgentMode `bson:"userAgent" json:"userAgent"`
    SubjectIDs   SubjectIDMode `bson:"subjectIds" json:"subjectIds"`
    PIIKeys      []string      `bson:"piiKeys" json:"piiKeys"`
    ScanFreeText bool          `bson:"scanFreeText" json:"scanFreeText"`
}

type RetentionPolicy struct {
    AuditEventsDays    int  `bson:"auditEventsDays" json:"auditEventsDays"`
    SecurityEventsDays *int `bson:"securityEventsDays,omitempty" json:"securityEventsDays,omitempty"` // solo piattaforma
}
```

Modello persistito in `internal/core/compliance/models/policy.go`:

```go
const (
    PoliciesCollection          = "compliance_policies"
    PolicyVersionsCollection    = "compliance_policy_versions"
    PolicyAssignmentsCollection = "compliance_policy_assignments"
    PolicyAssignmentHistoryCollection = "compliance_policy_assignment_history"
)

type Policy struct {
    UUID              string                 `bson:"uuid"`
    Name              string                 `bson:"name"`
    Description       string                 `bson:"description,omitempty"`
    IsPlatformDefault bool                   `bson:"isPlatformDefault"`
    Version           int                    `bson:"version"`
    LogContent        iface.LogContentPolicy `bson:"logContent"`
    Retention         iface.RetentionPolicy  `bson:"retention"`
    CreatedBy         string                 `bson:"createdBy"`
    CreatedAt         time.Time              `bson:"createdAt"`
    UpdatedBy         string                 `bson:"updatedBy"`
    UpdatedAt         time.Time              `bson:"updatedAt"`
    ChangeReason      string                 `bson:"changeReason"`
}

type PolicyVersion struct {
    PolicyUUID           string    `bson:"policyUuid"`
    Version              int       `bson:"version"`
    Snapshot             Policy    `bson:"snapshot"`
    ChangedBy            string    `bson:"changedBy"`
    ChangedAt            time.Time `bson:"changedAt"`
    Reason               string    `bson:"reason"`
    AcknowledgedWarnings []string  `bson:"acknowledgedWarnings,omitempty"` // codici avviso
    Deleted              bool      `bson:"deleted,omitempty"`             // versione-lapide scritta alla cancellazione
}

type PolicyAssignment struct {
    TenantID   string    `bson:"tenantId"`
    PolicyUUID string    `bson:"policyUuid"`
    AssignedBy string    `bson:"assignedBy"`
    AssignedAt time.Time `bson:"assignedAt"`
    Reason     string    `bson:"reason"`
}

// Una riga per ogni assegnazione, cambio o rimozione; solo inserimenti.
type PolicyAssignmentChange struct {
    TenantID             string    `bson:"tenantId"`
    PolicyUUID           string    `bson:"policyUuid,omitempty"` // vuoto = tornato alla piattaforma
    PreviousPolicyUUID   string    `bson:"previousPolicyUuid,omitempty"`
    ChangedBy            string    `bson:"changedBy"`
    ChangedAt            time.Time `bson:"changedAt"`
    Reason               string    `bson:"reason"`
    AcknowledgedWarnings []string  `bson:"acknowledgedWarnings,omitempty"`
}
```

Indici:
- `compliance_policies`: `uuid` unico; `name` unico; indice parziale unico su
  `isPlatformDefault` con filtro `{isPlatformDefault: true}` (una sola policy
  di piattaforma).
- `compliance_policy_versions`: `(policyUuid, version)` unico.
- `compliance_policy_assignments`: `tenantId` unico; `policyUuid`.
- `compliance_policy_assignment_history`: `(tenantId, changedAt)`;
  `changedAt`.

Le quattro collection sono stato di piattaforma gestito da operatori Tier-1: i
repository portano `//tenantscope:allow` con la motivazione (le assegnazioni
hanno comunque `tenantId` e sono lette per tenant).

### 1.2 Policy di piattaforma

- Esiste **esattamente una** policy con `isPlatformDefault=true`, creata al
  primo avvio (§5.1). Il flag non si sposta: per cambiare il comportamento di
  piattaforma si modifica quella policy.
- Non si può cancellare.
- È l'unica che può avere `retention.securityEventsDays` (gli eventi di
  sicurezza sono per utente, e un utente può appartenere a più tenant). Su una
  policy non di piattaforma il campo è un **errore** di validazione.

Default:

| Campo | Default |
|---|---|
| `logContent.ipAddress` | `truncated` |
| `logContent.userAgent` | `full` |
| `logContent.subjectIds` | `uuid` |
| `logContent.piiKeys` | `email`, `emailaddress`, `phone`, `phonenumber`, `mobile`, `firstname`, `lastname`, `fullname`, `displayname`, `username`, `address`, `streetaddress`, `codicefiscale`, `fiscalcode`, `taxcode`, `iban`, `birthdate`, `dateofbirth` |
| `logContent.scanFreeText` | `true` |
| `retention.auditEventsDays` | 730 |
| `retention.securityEventsDays` | 365 |

Gli stessi valori sono i **default statici** usati prima che compliance
finisca l'`Init` (§2.1).

### 1.3 Validazione

`POST /v1/admin/compliance/policies/validate` e ogni salvataggio restituiscono
`errors[]` e `warnings[]`, ciascuno con `code`, `field` e parametri per la
traduzione.

**Errori** (bloccano):

| Codice | Condizione |
|---|---|
| `invalid_enum` | valore fuori dall'insieme per `ipAddress`/`userAgent`/`subjectIds` |
| `retention_out_of_range` | giorni < 1 o > 3650 |
| `security_retention_not_platform` | `securityEventsDays` su una policy non di piattaforma |
| `security_retention_missing` | `securityEventsDays` assente sulla policy di piattaforma |
| `invalid_pii_key` | chiave non `[a-z0-9]{2,40}` dopo normalizzazione (§2.3), oppure più di 100 chiavi |
| `name_invalid` / `name_taken` | nome vuoto, oltre 80 caratteri, o già usato |
| `reason_required` | motivazione vuota o oltre 500 caratteri |

**Avvisi** (richiedono conferma esplicita):

| Codice | Condizione |
|---|---|
| `less_restrictive_than_platform` | policy non di piattaforma meno restrittiva della piattaforma su almeno un campo (§2.2); parametri: elenco campi |
| `audit_retention_below_admin_minimum` | `auditEventsDays` < 183 (minimo di 6 mesi per i log degli amministratori di sistema) |
| `ip_full` | `ipAddress=full` |

Gli stessi avvisi si calcolano all'**assegnazione** a un tenant
(`less_restrictive_than_platform` sulla policy scelta).

Se la policy di piattaforma diventa più restrittiva, una policy di tenant può
diventare «meno restrittiva» senza essere stata toccata. Non è un errore: la
tabella del catalogo lo mostra con un badge calcolato al momento (§4.1).

## 2. Risoluzione e applicazione

### 2.1 `PolicyService` e `iface.CompliancePolicyProvider`

```go
// iface
type CompliancePolicyProvider interface {
    // LogContentFor restituisce la policy di contenuto per il tenant;
    // tenantID == "" → la più restrittiva tra le policy attive (D6).
    LogContentFor(tenantID string) LogContentPolicy
    AuditRetentionFor(tenantID string) time.Duration
    SecurityEventsRetention() time.Duration
}
```

`compliance/services/policy_service.go` tiene in memoria un'**istantanea
immutabile**, sostituita atomicamente (`atomic.Pointer`):

- policy di piattaforma;
- mappa `tenantID → policy effettiva` per i tenant assegnati;
- **policy più restrittiva attiva**: calcolata tra la piattaforma e le policy
  *assegnate ad almeno un tenant* (§2.2).

Refresh da Mongo ogni **5 secondi** nel loop di manutenzione del modulo, e
subito sull'istanza che ha fatto una scrittura. Con più repliche l'effetto si
propaga entro 5 secondi.

Pubblicazione: la chiave di servizio `module.ServiceCompliancePolicy` in
`ProvidedServices()`, e in `Init` la chiamata
`utils.SwapLogPolicyResolver(policySvc)` sul modello di
`utils.SwapLevelResolver`. Prima di quella chiamata l'handler usa un
risolutore statico con i default di §1.2.

### 2.2 Ordine di restrittività

Per campo, dal meno al più restrittivo:

- `ipAddress`: `full` < `truncated` < `hashed` < `omitted`
- `userAgent`: `full` < `omitted`
- `subjectIds`: `uuid` < `hashed` < `omitted`
- `piiKeys`: la restrittiva è l'**unione**; una policy è meno restrittiva se
  manca una chiave presente nell'altra
- `scanFreeText`: `false` < `true`
- `auditEventsDays` (ottica privacy): più giorni = meno restrittivo

La policy più restrittiva attiva prende, campo per campo, il valore più
restrittivo; per `auditEventsDays` non serve (le righe di log non hanno
conservazione), si calcola solo il contenuto.

### 2.3 `PolicyHandler` slog

Nuovo `internal/shared/utils/policy_handler.go`. Catena risultante in
`SetupLogger`:

```
TraceContextHandler → PerModuleLevelHandler → PolicyHandler → Fanout → stdout / OTLP
```

Dopo il filtro di livello (i record scartati non costano nulla), prima del
fanout (stdout e OTLP ricevono lo stesso record mascherato).

**Tenant del record**: `ctxauth.GetTenantID(ctx)`, altrimenti le annotazioni
della richiesta (§2.4), altrimenti `""` → policy più restrittiva.

**`WithAttrs`/`WithGroup`**: l'handler conserva gli attributi grezzi e li
maschera in `Handle`, perché la policy dipende dal tenant del singolo record e
può cambiare a runtime.

**Normalizzazione delle chiavi**: minuscolo, rimozione di `_`, `-`, `.`; si
confronta la chiave foglia (dentro i gruppi).

**Regole, in quest'ordine, sul primo che corrisponde:**

1. **Segreti** (sempre, D11): chiave che contiene `password`, `passwd`,
   `secret`, `token`, `authorization`, `cookie`, `apikey`, `privatekey`,
   `credential` → `[REDACTED]`. L'elenco si sposta in
   `internal/shared/redact` e lo usano sia l'handler sia l'anteprima Loki
   (`core/logging/logquery/redact.go`), che oggi ha un proprio elenco.
2. **IP**: chiavi `remote`, `ip`, `ipaddress`, `clientip`, `remoteaddr`
   → modalità `ipAddress`:
   - `full`: invariato;
   - `truncated`: IPv4 `/24` → `192.168.1.0/24`; IPv6 `/48`;
   - `hashed`: `h:` + primi 16 caratteri esadecimali di
     `HMAC-SHA256(chiave, "ip:" + valore)`;
   - `omitted`: attributo rimosso.
3. **User agent**: chiavi `ua`, `useragent` → `full` o rimosso.
4. **Identificativi**: chiavi `userid`, `useruuid`, `actoruserid`,
   `subjectid` → `uuid` invariato, `hashed` con prefisso di dominio `"sub:"`,
   `omitted` rimosso.
5. **Dati personali**: chiave **uguale** (dopo normalizzazione) a una delle
   `piiKeys` → `[PII]`. Il confronto è esatto, non «contiene»: con
   «contiene», `name` coprirebbe anche `filename`, `hostname` e il nome del
   modulo. Per questo i default elencano le varianti in modo esplicito.
   (I segreti della regola 1 restano con «contiene»: mascherare troppo una
   chiave simile a un segreto è sicuro.)
6. **Testo libero** (se `scanFreeText`): sul messaggio e su tutti i valori
   stringa rimasti, sostituzione di email → `[EMAIL]`, IBAN → `[IBAN]`, codice
   fiscale → `[CF]`, IPv4/IPv6 validati con `net.ParseIP` → secondo la
   modalità `ipAddress`.

**Tipi di valore**: stringhe; `LogValuer` risolti prima delle regole; `error`
→ `Error()` e poi le regole come stringa; `map[string]any` e `[]any`
visitati ricorsivamente. Altri `KindAny` passano invariati **salvo** che la
chiave corrisponda alle regole 1 o 5, nel qual caso il valore è sostituito
per intero. Limite documentato: il seguito naturale è il linter `logscope`
dell'ADR-0005.

**Chiave HMAC**: nuovo campo `FieldSecret` `log_hash_key` nel `ConfigSchema`
di compliance (cifrato AES-256-GCM), generato casualmente al primo avvio se
vuoto. La rotazione interrompe la correlazione con gli hash precedenti: è
voluto e documentato.

**Robustezza**: un panic durante il mascheramento è recuperato; l'attributo
diventa `[REDACTED:error]` e il record viene scritto comunque. Nessun record
esce senza maschera.

### 2.4 Request logger: tenant e utente (D7)

Oggi `RequestLogger` (montato a `cmd/server/middleware.go:55-64`) legge
tenant e utente dal proprio `r.Context()`, ma `RequireAuth` li mette in un
contesto derivato (`auth.go:552`) che il logger esterno non vede: i campi sono
sempre assenti, e il test lo nasconde precaricando il contesto.

Correzione in `internal/shared/middleware`:

- `RequestLogger` inserisce nel contesto un puntatore
  `*requestAnnotations{tenantID, tenantKind, userID, userRole, audience}`
  (chiave non esportata) prima di chiamare `next`.
- `RequireAuth` e `RequireAudience`, dopo aver risolto il principale, lo
  riempiono tramite `annotateRequest(ctx, …)`.
- Dopo `next`, `RequestLogger` legge le annotazioni e scrive i campi. Non
  serve un mutex: si scrive e si legge nella goroutine della richiesta, con
  la scrittura che precede la lettura.
- `PolicyHandler` usa le annotazioni come seconda fonte del tenant (§2.3):
  la riga `http_request` è mascherata con la policy del suo tenant.

### 2.5 Job di conservazione (D8)

Nuovo `compliance/services/event_retention.go`, eseguito una volta al giorno.
Con più repliche lo esegue una sola: il lease Redis di
`auth/services/maintenance_lease.go` si sposta in `internal/shared/lease` e
lo usano auth e compliance.

Per ogni esecuzione:

1. Legge le legal hold attive (`LegalHoldService.ListActive` su tutti i
   soggetti). **Se la lettura fallisce, l'esecuzione si interrompe** senza
   cancellare nulla (in dubbio non si cancella).
2. **Audit, tenant assegnati**: per ogni tenant T con policy assegnata,
   cancella da `compliance_audit_events` gli eventi con `tenantId=T`,
   `timestamp < now − auditEventsDays(T)`, esclusi quelli con `actorUserId`
   sotto hold o con `resourceType="user"` e `resourceId` sotto hold.
3. **Audit, resto**: stessa cancellazione con la conservazione di piattaforma
   su `tenantId ∉ {tenant assegnati}` (compresi gli eventi senza tenant).
4. **Security events**: `auth_security_events` con
   `timestamp < now − securityEventsDays`, esclusi `userUuid` sotto hold. La
   collection appartiene ad auth: compliance la raggiunge con il nome
   inlineato (stesso schema di `userTombstoneCollections` in
   `retention.go`).
5. Cancellazione a blocchi: si leggono fino a 1000 `_id` e si fa
   `deleteMany` su quelli, fino a 50 blocchi per ambito per esecuzione; il
   resto si fa il giorno dopo.
6. Scrive l'evento di audit `compliance.retention.purged` (autore `system`)
   con i conteggi per ambito e `outcome=success`, oppure `failure` con
   l'errore, senza dati personali.

Il `RetentionService` esistente (cancellazione delle lapidi anonimizzate) non
cambia.

## 3. Versioni, audit, evidenze, API, permessi

### 3.1 Versioni e concorrenza

- Ogni creazione e modifica scrive la policy corrente e **una nuova riga
  immutabile** in `compliance_policy_versions`. Ogni assegnazione, cambio o
  rimozione scrive una riga in `compliance_policy_assignment_history`. I
  repository di queste due collection hanno solo `Insert` e letture.
- Versioni e storico delle assegnazioni **non** sono toccati dal job di
  conservazione: sono l'evidenza di responsabilizzazione e contengono solo
  UUID di operatori e tenant, nessun dato degli interessati. Per questo
  l'export non dipende dagli eventi di audit, che invece scadono.
- `PUT` richiede `expectedVersion`; se la versione corrente è diversa → 409.
- `DELETE`: vietato sulla piattaforma (409 `platform_policy_protected`) e su
  una policy assegnata (409 `policy_in_use`). Altrimenti cancella la corrente
  e scrive una versione-lapide con `Deleted=true`.

### 3.2 Eventi di audit

Nuove costanti in `compliance/models/audit_event.go`, emesse dal sink di
compliance stesso:

| Azione | Metadati |
|---|---|
| `compliance.policy.created` | `policyUuid`, `version`, `name`, `reason` |
| `compliance.policy.updated` | `policyUuid`, `version`, `changes: [{field, before, after}]`, `reason`, `acknowledgedWarnings` |
| `compliance.policy.deleted` | `policyUuid`, `lastVersion`, `reason` |
| `compliance.policy.assigned` | `tenantId`, `policyUuid`, `previousPolicyUuid`, `reason`, `acknowledgedWarnings` |
| `compliance.policy.unassigned` | `tenantId`, `previousPolicyUuid`, `reason` |
| `compliance.retention.purged` | conteggi per ambito, errore eventuale |

### 3.3 API

Tier-1 operatore, sotto `/v1/admin/compliance/`:

| Metodo | Percorso | Permesso | Note |
|---|---|---|---|
| GET | `policies` | `policy.read` | con `assignedTenants` e `lessRestrictiveThanPlatform` calcolati |
| POST | `policies` | `policy.manage` | corpo: policy + `reason` + `acknowledgeWarnings` |
| GET | `policies/{id}` | `policy.read` | |
| PUT | `policies/{id}` | `policy.manage` | + `expectedVersion` |
| DELETE | `policies/{id}` | `policy.manage` | `reason` nel corpo |
| POST | `policies/validate` | `policy.read` | prova senza salvare, con `policyUuid` opzionale |
| GET | `policies/{id}/versions` | `policy.read` | |
| GET | `policies/effective?tenantId=` | `policy.read` | policy effettiva + `source: platform\|assigned` |
| GET | `policies/export?from=&to=` | `policy.read` | JSON: policy, versioni e assegnazioni in vigore nel periodo, ricostruite da `compliance_policy_versions` e `compliance_policy_assignment_history` |
| GET | `policy-assignments` | `policy.read` | |
| PUT | `policy-assignments/{tenantId}` | `policy.manage` | `policyUuid`, `reason`, `acknowledgeWarnings` |
| DELETE | `policy-assignments/{tenantId}` | `policy.manage` | `reason`; il tenant torna alla piattaforma |

Un salvataggio con avvisi e `acknowledgeWarnings=false` risponde 422 con
l'elenco degli avvisi.

Tier-2 cliente, audience client, limitato al tenant del percorso e ai suoi
membri:

- `GET /v1/tenants/{tenantId}/compliance/policy` → sintesi della policy
  effettiva: `ipAddress`, `userAgent`, `subjectIds`, `auditEventsDays`, data
  di validità. Niente nome interno, `piiKeys` né storico.

### 3.4 Permessi

In `compliance.Permissions()`:

- `system.compliance.policy.read` — super_admin, administrator, developer;
- `system.compliance.policy.manage` — super_admin, administrator.

In `authz/cedar/policies/tenant_scope.cedar` entrambi solo lato operatore
(interni), come `system.compliance.legalhold.manage`; il gate
`policycoverage` deve passare. L'endpoint cliente usa l'appartenenza al
tenant, non questi permessi.

## 4. Interfaccia operatore (frontend-admin)

Precedenti: `pages/admin/compliance/index.tsx` (tab `?tab=`),
`LegalHoldsTab.tsx` (form + `ComplianceTable`),
`pages/admin/modules/logging/PermanentLevelsPanel.tsx` (bozza/base, 409 →
editor bloccato). Stack obbligatorio: RTK Query su `complianceApi`,
`react-hook-form` + `yup`, `AdvanceTable`, `t()` con chiavi
`adminCompliance.policies.*` in `en.json` e `it.json`.

### 4.1 Tab «Policy» — `/admin/compliance?tab=policies`

- Tabella: nome; badge «Piattaforma»; badge «Meno restrittiva della
  piattaforma» (campo calcolato); versione; tenant assegnati; ultima modifica
  (autore, data); azioni *Apri*, *Duplica*.
- Pulsanti: **Nuova policy** (parte da una copia della piattaforma, senza
  `securityEventsDays`) ed **Esporta evidenze** (intervallo di date → download
  JSON).

### 4.2 Dettaglio — `/admin/compliance/policies/:id`

Tab sincronizzati con `?section=`:

- **Impostazioni**: card *Contenuto dei log* (radio per `ipAddress`,
  `userAgent`, `subjectIds`, ciascuna con una riga su cosa comporta; elenco a
  tag per `piiKeys`; interruttore `scanFreeText`) e card *Conservazione*
  (`auditEventsDays`; `securityEventsDays` solo sulla piattaforma).
- **Storico**: tabella delle versioni (versione, autore, data, motivazione,
  badge se sono stati confermati avvisi); il clic apre una modale con le
  differenze campo per campo.
- **Tenant**: tenant assegnati, con link al rispettivo dettaglio.

### 4.3 Salvataggio

1. *Salva* → `POST …/validate`; gli errori tornano sui campi.
2. Modale **Conferma modifica**: riepilogo delle differenze, avvisi con
   spiegazione, **motivazione obbligatoria**, casella «Confermo gli avvisi» se
   ce ne sono.
3. 409 → editor bloccato con «modificata da un altro operatore, ricarica».
4. Con modifiche non salvate `useBlocker` chiede conferma, **anche** quando
   cambia solo `?section=` (lo stato nell'URL è raggiungibile con Indietro).

### 4.4 Dettaglio tenant

Nella Panoramica di `pages/admin/clients/detail` e
`pages/admin/internal-tenants/detail`, una card **Policy di conformità**
(`pages/admin/compliance/CompliancePolicyCard.tsx`, un solo componente):
policy effettiva, provenienza (piattaforma o assegnata), conservazione
dell'audit risultante; pulsante **Cambia** → modale con scelta della policy
(o «Piattaforma», che rimuove l'assegnazione), motivazione obbligatoria,
avvisi se meno restrittiva.

### 4.5 Sola lettura

Con solo `policy.read` (developer): controlli disabilitati, nessun pulsante
di salvataggio, creazione, cancellazione o assegnazione.

## 5. Avvio, migrazione, errori

### 5.1 Primo avvio

In `Init` di compliance, se non esiste una policy di piattaforma: la crea con
i default di §1.2, versione 1, autore `system`, motivazione «policy iniziale»,
e scrive `compliance.policy.created`. L'operazione è idempotente (indice
parziale unico).

### 5.2 Migrazione degli indici

Il registry crea gli indici ma non li rimuove (`registry.go:~910`); dichiarare
un indice semplice su `timestamp` dove esiste l'indice TTL produrrebbe
`IndexOptionsConflict` e bloccherebbe l'avvio. Quindi, in `Init` di
compliance, **prima** della creazione degli indici dichiarati:

- su `compliance_audit_events`: se l'indice su `timestamp` ha
  `expireAfterSeconds`, lo rimuove; l'indice dichiarato diventa semplice
  (serve ai range del job);
- su `auth_security_events`: stessa cosa per l'indice TTL creato da
  `security_event_service.go:84`, e quella dichiarazione diventa un indice
  semplice.

Il passo è idempotente e ha un test su un DB che ha già i TTL. Si rimuove
anche il campo riservato e mai usato `Tenant.RetentionPolicyID`
(`tenant/models/tenant.go:212`).

### 5.3 Errori

| Situazione | Comportamento |
|---|---|
| Mongo irraggiungibile al refresh | si tiene l'ultima istantanea; `warn` al massimo una volta al minuto; mai verso una policy meno restrittiva |
| Nessuna istantanea valida | default statici di §1.2 |
| Lettura legal hold fallita nel job | esecuzione interrotta, nessuna cancellazione, `retention.purged` con `failure` |
| Errore su un blocco del job | `error` nel log, il resto riprova il giorno dopo |
| Panic nel mascheramento | recuperato, `[REDACTED:error]`, record scritto |
| Chiave HMAC assente | generata al primo avvio; se la generazione fallisce, `hashed` si comporta come `omitted` |

### 5.4 Costo

Benchmark su `PolicyHandler`: meno di 2 µs per record con 10 attributi senza
`scanFreeText`; con `scanFreeText` si misura e si documenta, senza soglia.

## 6. Test

**Backend, unitari**
- ordine di restrittività e calcolo della più restrittiva attiva;
- validazione: ogni errore e ogni avviso;
- `PolicyHandler`: gruppi annidati, `WithAttrs`/`WithGroup`, `LogValuer`,
  `error`, mappe e slice, segreti sempre mascherati, ogni modalità per IP
  (IPv4 e IPv6), UA e identificativi, testo libero, tenant dal contesto e
  dalle annotazioni, riga senza tenant → più restrittiva, recupero dal panic;
- benchmark di §5.4.

**Backend, integrazione (Mongo)**
- creazione, modifica con 409, cancellazione vietata su piattaforma e su
  policy assegnata, versioni immutabili;
- assegnazione con avvisi (422 senza conferma, successo con conferma) e riga
  di storico scritta per assegnazione, cambio e rimozione;
- export su un periodo: policy e assegnazioni in vigore corrette anche dopo
  che il job ha cancellato i relativi eventi di audit;
- job di conservazione: tenant assegnato, piattaforma, eventi senza tenant,
  **soggetto sotto legal hold non cancellato**, lettura hold fallita → nessuna
  cancellazione, blocchi;
- migrazione degli indici su un DB con i TTL esistenti;
- primo avvio idempotente.

**Backend, catena reale**
- una richiesta attraverso `RequestLogger` → `RequireAuth` → handler produce
  su `http_request` `tenant_id` e `user_id` mascherati secondo la policy del
  tenant (il test che oggi manca).

**Frontend**
- flusso validazione → conferma → motivazione, con e senza avvisi;
- editor bloccato su 409;
- `useBlocker` anche sul cambio di `?section=`;
- vista in sola lettura;
- `CompliancePolicyCard` nelle due pagine tenant;
- parità en/it.

**Gate CI**: `tenantscope` (`//tenantscope:allow` motivati), `policycoverage`
per i nuovi permessi, `openapi-check` per le nuove route.

## 7. Documentazione

- **ADR-0022 «Policy di conformità configurabili»**: supera l'audit
  «conservato per sempre» dell'ADR-0005 §1.3 e la conservazione fissa del
  compliance plane dell'ADR-0009; registra D1–D11.
- `backend/internal/core/compliance/AGENTS.md` e
  `docs/site/modules/core/compliance.mdx`: motore, API, permessi, job.
- `backend/internal/core/logging/AGENTS.md` e
  `docs/site/modules/core/logging.mdx`: correggere l'affermazione che
  `tenant_id` sia su ogni riga; descrivere il `PolicyHandler`.
- Nuova pagina `docs/site/operating/logging-policy.mdx`: la policy di logging
  richiesta dalla ISO 27001 (cosa si registra, dove, per quanto, chi accede,
  come si rivede), con il catalogo come fonte di verità.

## 8. Fuori da questo progetto

Sottoprogetti successivi, ciascuno con la propria spec:

1. **Conservazione infrastrutturale** generata dalla policy di piattaforma:
   Loki (compresa la correzione del filtro `level` in maiuscolo), Tempo,
   rotazione dei log dei container, `backup-cron.log`.
2. **Errori dal client**: endpoint interno di raccolta, error boundary,
   `X-Request-ID` esposto e mostrato all'utente, mobile.
3. **Alert**: error rate, backend giù, picchi di login falliti.
4. **Indipendente e da fare subito, in parallelo**: sicurezza dell'overlay di
   osservabilità (bind address, credenziali, autenticazione Loki, lifecycle
   Prometheus) e collegamento degli emitter di audit mancanti
   (impersonazione, ciclo di vita utenti, azioni admin di `authService`,
   account di servizio, modifiche dei livelli di log).

Esclusi anche: UI in frontend-client della policy; policy per tenant sulle
dimensioni infrastrutturali; linter `logscope`; migrazione delle chiamate
`slog.*` senza contesto a `*Context` (la garanzia D6 le copre).
