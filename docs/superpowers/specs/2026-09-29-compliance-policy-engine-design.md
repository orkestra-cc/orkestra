# Compliance: policy configurabili per log, evidenze di audit e retention — Design

Branch: `docs/compliance-policy-engine` (da `dev` @ `64258483e`).
Brainstorming del 2026-09-29 con Salvatore, dopo un'analisi del logging su
tutto lo stack. **Revisione 2** dello stesso giorno, dopo la review GDPR/ISO
`tmp/compliance-policy-engine-gdpr-iso27001-review.md` e le sezioni R1–R3
approvate in conversazione.
**Allineata alla T2** il 2026-09-30 (piano `docs/superpowers/plans/2026-09-30-compliance-t2-motore-di-policy.md`, sezione «Deviazioni dalla spec»).

## Obiettivo

Orkestra è un SaaS: clienti diversi hanno obblighi diversi (GDPR, norme del
Garante, ISO/IEC 27001:2022, clausole contrattuali). Oggi le regole su **cosa
finisce nei log**, **cosa contengono gli eventi di audit** e **per quanto si
conservano i dati in ogni sink** sono cablate nel codice, negli indici TTL e
nei file di configurazione dell'infrastruttura, e non esiste una policy
documentata.

Questo progetto introduce nel modulo core `compliance`:

1. un **catalogo di policy con nome e versione**, una delle quali è la policy
   di **piattaforma**, assegnabili ai tenant come override;
2. il **mascheramento dei log operativi e delle trace** secondo la policy del
   tenant della richiesta;
3. un'**evidenza di audit** con nucleo obbligatorio, contenuto controllato,
   durabilità (spool locale), integrità (hash chain verificata) ed export
   firmato;
4. **classi di retention** con finalità, base giuridica e minimi, applicate
   con `retainUntil` stampato alla scrittura;
5. il **governo della retention di tutti i sink**: Loki, Tempo, log dei
   container, backup, export, spool, più quelli esterni dichiarati;
6. **approvazione a quattro occhi** delle modifiche rischiose;
7. **monitoraggio e allarmi** generati dal backend, attivi anche dove
   l'overlay di osservabilità non gira.

Criterio di successo (esempio end-to-end): un operatore crea la policy
«Cliente sanità» (`ipAddress=omitted`, `privileged_change` a 400 giorni) e la
assegna al tenant T; la modifica ha un avviso, resta in attesa e un secondo
operatore la approva. Entro 5 secondi le righe di log e le trace di T non
contengono l'IP; gli eventi di audit di T scritti da quel momento hanno
`retainUntil` a 400 giorni; il job cancella solo gli eventi scaduti e mai
quelli sotto legal hold; Loki e Tempo ricevono la retention di piattaforma
dal manifest e la sonda giornaliera conferma che non restano dati più
vecchi; la verifica della hash chain passa; l'export firmato contiene policy,
versioni, richiesta di cambio con autore e approvatore, ancore delle catene
ed esiti delle verifiche, e un auditor lo verifica con la chiave pubblica.

### Controlli supportati

Il motore **supporta** i controlli qui sotto; la conformità si dimostra con
la valutazione del rischio e lo Statement of Applicability (SoA)
dell'organizzazione, non con questa tabella.

| Riferimento | Cosa fornisce il motore |
|---|---|
| GDPR art. 5.1.c (minimizzazione) | mascheramento di log e trace per tenant; `actorEmail` non salvato; allowlist dei metadati di audit |
| GDPR art. 5.1.e (limitazione della conservazione) | classi di retention, `retainUntil` alla scrittura, governo di tutti i sink, verifica della cancellazione |
| GDPR art. 5.2 e 30 (responsabilizzazione, registro) | finalità e base giuridica per classe, metadati di accountability sulla policy, versioni, quattro occhi, export firmato |
| GDPR art. 32 | segreti sempre mascherati, pseudonimizzazione con HMAC, overlay messo in sicurezza, integrità dell'audit |
| GDPR art. 33 | allarmi su condizioni anomale dell'evidenza e dei sink |
| Garante, provvedimento AdS (27/11/2008) | classe `admin_access` con minimo rigido di 6 mesi di calendario per gli accessi degli operatori all'**applicazione**, integrità verificabile, base per la verifica annuale |
| ISO 27001 A.5.28, A.8.15 | evidenza con nucleo obbligatorio, hash chain, export firmato |
| ISO 27001 A.8.10 | job di retention, sink governati, report di verifica |
| ISO 27001 A.8.11 | `PolicyHandler`, mascheratore delle trace |
| ISO 27001 A.8.16 | metriche e allarmi |
| ISO 27001 A.8.17 | metrica di scostamento dell'orologio; NTP nel runbook |
| ISO 27001 A.5.3 (separazione dei compiti) | quattro occhi |

### Non coperto (da dichiarare, mai da affermare come conforme)

- Access log AdS di **sistemi operativi, SSH e DBMS**: fuori dal codice
  applicativo; runbook e responsabilità dell'infrastruttura.
- Cancellazione presso **fornitori esterni** (OTLP, copie dei backup fuori
  sede, syslog dell'host): solo dichiarata e revisionata periodicamente.
- Atomicità fra l'evento di audit e la modifica di business per gli emitter
  **diversi** dal motore di policy: lo spool elimina la perdita, non rende
  atomica la coppia.
- I log dei container sono limitati per **dimensione**: Docker non offre una
  retention per età.

## Decisioni

Prima stesura:

- **D1** — Policy di **piattaforma + override per tenant**. Si assegnano ai
  tenant solo le dimensioni che un tenant può possedere (contenuto dei log,
  classi di retention per tenant); sink e classi solo di piattaforma restano
  sulla policy di piattaforma.
- **D2** — Un override può essere meno restrittivo, **con motivazione** e
  conferma degli avvisi. Eccezione: i minimi rigidi delle classi (D14).
- **D3** — Assegnazione e modifica **solo da operatori Tier-1**; il cliente
  Tier-2 vede una sintesi in sola lettura.
- **D5** — Catalogo con nome **dentro `compliance`**.
- **D6** — Le righe di log **senza tenant** usano la policy più restrittiva
  attiva.
- **D7** — Correzione del request logger (tenant e utente mai presenti su
  `http_request`) inclusa.
- **D9** — Motivazione **obbligatoria su ogni modifica**.
- **D10** — L'audit di una modifica contiene i valori prima e dopo.
- **D11** — I **segreti** sono sempre mascherati, non configurabili.

Revisione 2 (review GDPR/ISO e sezioni R1–R3):

- **D4 (sostituita)** — Tutti i sink sono nel perimetro di questo progetto:
  Loki, Tempo, log dei container, backup, export, spool, più quelli esterni
  dichiarati (§5).
- **D8 (sostituita)** — La retention si applica con `retainUntil` **stampato
  alla scrittura**. Allungare vale solo per i nuovi eventi; accorciare gli
  esistenti richiede un'azione esplicita, motivata e registrata, mai sotto il
  minimo della classe. Il job cancella per `retainUntil`, rispettando le
  legal hold. Gli indici TTL fissi vengono rimossi.
- **D12** — **Classi di retention** fisse nel codice, con finalità, base
  giuridica, eventi coperti e minimo (§1.3).
- **D13** — **Metadati di accountability** sulla policy: ruolo
  (titolare/responsabile/contitolare), riferimento RoPA, riferimento
  DPIA/LIA, owner, data della prossima revisione.
- **D14** — Minimo **rigido di 184 giorni** (copre sempre 6 mesi di
  calendario) sulla classe `admin_access`: errore, non avviso.
- **D15** — **Quattro occhi** configurabile (`four_eyes_enabled`, default
  attivo): le modifiche con avvisi diventano richieste da approvare da un
  secondo operatore.
- **D16** — **Nucleo di evidenza** obbligatorio su ogni evento di audit, fuori
  da ogni policy; `requestId` e `traceId` aggiunti.
- **D17** — `actorEmail` **non si salva più**; **allowlist dei metadati per
  azione**, chiavi fuori lista scartate.
- **D18** — Le modifiche del motore di policy scrivono l'evento di audit
  **nella stessa transazione**; gli altri emitter passano per uno **spool
  locale** con riprove e dead-letter.
- **D19** — **Hash chain** su tutto l'audit trail, per replica, con
  checkpoint alla cancellazione, verifica giornaliera e ancore nell'export.
- **D20** — **Export firmato Ed25519** con chiave dedicata
  (`COMPLIANCE_EVIDENCE_SIGNING_KEY`), permesso separato
  `system.compliance.evidence.export`, step-up, `no-store`, export registrato.
- **D21** — Gli emitter mancanti vengono collegati: logout, impersonazione,
  login dei clienti Tier-2, ciclo di vita degli utenti, azioni admin di
  `authService`, account di servizio, livelli di log, consultazione
  dell'anteprima dei log.
- **D22** — Una legal hold con `TenantID` sospende la cancellazione di tutto il
  tenant.
- **D23** — La riga `http_request` registra il **route template**; il linter
  `logscope` diventa un gate di CI con baseline.
- **D24** — Le **trace** in uscita vengono mascherate con la stessa policy.
- **D25** — I sink leggono un **manifest** e file di configurazione generati
  dal backend in una directory condivisa con l'host; gli script riportano la
  verifica nella stessa directory.
- **D26** — L'overlay di osservabilità viene **messo in sicurezza**.
- **D27** — **Allarmi generati dal backend** via email (modulo notification) e
  banner in console, ognuno aperto e chiuso con un evento di audit.

Correzioni emerse leggendo il codice (prima nel Task 1 del piano):

- La chiave HMAC per i valori pseudonimizzati è **derivata con HKDF** da
  `OAUTH_TOKEN_ENCRYPTION_KEY` (nessun segreto scritto al boot, che avrebbe
  segnato il modulo come «da riavviare»). L'HMAC è una
  **pseudonimizzazione**, non un'anonimizzazione.
- Il job di retention gira **una volta al giorno su una sola replica** con
  `SETNX` su una chiave Redis datata.
- Il risolutore della policy di log si sostituisce in `cmd/server/main.go`
  dopo `InitAll`, come `SwapLevelResolver`.
- Le route di scrittura richiedono lo **step-up**.
- La migrazione degli indici tiene conto che il registry li crea **prima**
  di ogni `Init` e non blocca l'avvio in caso di conflitto (§10.2).

## 1. Modello

### 1.1 Policy

Tipi condivisi in `pkg/sdk/iface/compliance_policy.go` (li usano `compliance`,
`internal/shared/utils` e `internal/shared/telemetry`, che non possono
importare `compliance`):

```go
type IPAddressMode string // "full" | "truncated" | "hashed" | "omitted"
type UserAgentMode string // "full" | "omitted"
type SubjectIDMode string // "uuid" | "hashed" | "omitted"

type LogContentPolicy struct {
    IPAddress    IPAddressMode `bson:"ipAddress" json:"ipAddress"`
    UserAgent    UserAgentMode `bson:"userAgent" json:"userAgent"`
    SubjectIDs   SubjectIDMode `bson:"subjectIds" json:"subjectIds"`
    PIIKeys      []string      `bson:"piiKeys" json:"piiKeys"` // normalizzate, ordinate
    ScanFreeText bool          `bson:"scanFreeText" json:"scanFreeText"`
}

type RetentionClass string // vedi §1.3

type CompliancePolicyProvider interface {
    LogContentFor(tenantID string) *LogContentPolicy // "" → la più restrittiva attiva
    RetentionFor(tenantID string, class RetentionClass) RetentionDecision
}

type RetentionDecision struct {
    Class         RetentionClass
    PolicyUUID    string
    PolicyVersion int
    Days          int
}
```

Modello persistito (`internal/core/compliance/models/policy.go`):

```go
type Policy struct {
    UUID              string
    Name              string
    Description       string
    IsPlatformDefault bool
    Version           int
    LogContent        iface.LogContentPolicy
    Retention         map[iface.RetentionClass]int // giorni per classe
    Sinks             *SinkPolicy                  // solo piattaforma
    Accountability    Accountability
    CreatedBy, UpdatedBy string
    CreatedAt, UpdatedAt time.Time
    ChangeReason      string
}

type Accountability struct {
    Role          string    // "controller" | "processor" | "joint_controller"
    RoPARef       string    // riferimento nel registro dei trattamenti
    AssessmentRef string    // DPIA o LIA
    Owner         string    // UUID dell'operatore responsabile
    ReviewDueAt   time.Time // prossima revisione
}
```

- Una policy di tenant contiene in `Retention` solo le classi impostabili per
  tenant (§1.3); le altre si ereditano dalla piattaforma.
- `Sinks` esiste solo sulla policy di piattaforma (§5).
- Esattamente una policy ha `IsPlatformDefault=true`; non si cancella e il
  flag non si sposta.

Collection e indici:

| Collection | Contenuto | Indici |
|---|---|---|
| `compliance_policies` | stato corrente | `uuid` unico; `name` unico; parziale unico su `isPlatformDefault=true` |
| `compliance_policy_versions` | copia immutabile di ogni versione | `(policyUuid, version)` unico; `changedAt` |
| `compliance_policy_assignments` | tenant → policy | `tenantId` unico; `policyUuid` |
| `compliance_policy_assignment_history` | una riga per assegnazione, cambio o rimozione | `(tenantId, changedAt)`; `changedAt` |
| `compliance_policy_change_requests` | richieste del quattro occhi (§1.5) | `uuid` unico; `(status, requestedAt)` |

I repository di versioni, storico e richieste espongono solo inserimenti,
letture e il cambio di stato delle richieste; portano
`//tenantscope:allow` motivati (stato di piattaforma gestito da Tier-1).

### 1.2 Default della piattaforma

| Campo | Default |
|---|---|
| `logContent.ipAddress` | `truncated` |
| `logContent.userAgent` | `full` |
| `logContent.subjectIds` | `uuid` |
| `logContent.piiKeys` | `address`, `birthdate`, `codicefiscale`, `dateofbirth`, `displayname`, `email`, `emailaddress`, `firstname`, `fiscalcode`, `fullname`, `iban`, `lastname`, `mobile`, `phone`, `phonenumber`, `streetaddress`, `taxcode`, `username` (confronto **esatto** sulla chiave normalizzata) |
| `logContent.scanFreeText` | `true` |
| `retention` | i default delle classi (§1.3) |
| `sinks` | §5.1 |
| `accountability.role` | `controller`; gli altri campi vuoti e `reviewDueAt` a un anno dal primo avvio |

Gli stessi valori di `logContent` sono i default statici usati prima che
compliance finisca l'`Init`.

### 1.3 Classi di retention

Catalogo fisso nel codice (`internal/core/compliance/retentionclass`): finalità,
base giuridica e minimo non si configurano; compaiono nella console,
nell'export e nel modello di RoPA della documentazione.

| Classe | Eventi | Finalità | Base giuridica | Minimo | Default (giorni) | Impostabile da |
|---|---|---|---|---|---|---|
| `admin_access` | login, logout, login falliti, MFA, impersonazione degli **operatori Tier-1** | controllo degli accessi degli amministratori | art. 6.1.c (provvedimento AdS) | **184, rigido** | 365 | piattaforma e tenant (mai sotto il minimo) |
| `privileged_change` | modifiche di configurazione, ruoli, utenti, tenant, policy fatte da operatori | responsabilizzazione e sicurezza | art. 6.1.f (LIA) | 1 | 730 | piattaforma e tenant |
| `client_activity` | azioni e accessi dei clienti Tier-2 | sicurezza del servizio verso il cliente | art. 6.1.b/f | 1 | 365 | piattaforma e tenant |
| `authentication_security` | documenti di `auth_security_events` | rilevazione di frodi e abusi | art. 6.1.f | 1 | 365 | solo piattaforma |
| `compliance_evidence` | versioni, storico, richieste di cambio, DSR, legal hold, esiti di job, verifiche e allarmi | dimostrare la conformità (art. 5.2, 30) | art. 6.1.c | 1 | 1825 | solo piattaforma |

**Classificazione** (`retentionclass.Classify(action, audience)`):

- `compliance.*`, `dsr.*`, `legalhold.*` → `compliance_evidence`;
- `auth.login.*`, `auth.logout`, `auth.mfa.*`, `admin.tenant.impersonate`:
  audience `operator` → `admin_access`, audience `client` → `client_activity`;
- ogni altra azione: audience `client` → `client_activity`, altrimenti
  `privileged_change` (anche audience `service` e sconosciuta, il caso più
  prudente).

L'audience arriva dalle annotazioni di richiesta (§2.4); un evento emesso
fuori da una richiesta usa l'audience che l'emitter dichiara nel campo
`Audience` dell'evento.

Versioni, storico delle assegnazioni e motivazioni contengono **dati
personali** (UUID di operatori, testo libero): stanno in
`compliance_evidence`, le motivazioni passano per l'analisi del testo libero
alla scrittura, e l'eccezione all'erasure (art. 17.3.b ed e) è documentata.

### 1.4 Validazione

Ogni salvataggio e `POST …/policies/validate` restituiscono `errors[]` e
`warnings[]` con `code`, `field`, `params`.

**Errori** (bloccano sempre, anche con quattro occhi):

| Codice | Condizione |
|---|---|
| `invalid_enum` | modalità di contenuto o ruolo di accountability non validi |
| `retention_out_of_range` | giorni < minimo della classe o > 3650 |
| `class_below_minimum` | `admin_access` < 184 |
| `class_not_tenant_settable` | classe solo di piattaforma su una policy di tenant |
| `sinks_not_platform` | sezione `sinks` su una policy di tenant |
| `sinks_missing` | policy di piattaforma senza `sinks` |
| `sink_invalid` | valore di sink fuori dai limiti (§5.1) o sink esterno senza nome, tipo o riferimento DPA |
| `invalid_pii_key` | chiave non `[a-z0-9]{2,40}` dopo normalizzazione, o più di 100 chiavi |
| `name_invalid` / `name_taken` | nome vuoto, oltre 80 caratteri, o già usato |
| `reason_required` | motivazione vuota o oltre 500 caratteri |

**Avvisi** (richiedono conferma e, con quattro occhi attivo, approvazione):

| Codice | Condizione |
|---|---|
| `less_restrictive_than_platform` | policy di tenant meno restrittiva della piattaforma (contenuto o retention più lunga) |
| `less_restrictive_than_current` | un'assegnazione, un cambio di assegnazione, una rimozione o una modifica rende la policy effettiva di un tenant meno restrittiva di quella attuale |
| `retention_longer_than_default` | una classe oltre il suo default |
| `retention_shorter_than_default` | una classe sotto il suo default: le evidenze di audit scadrebbero prima |
| `sink_retention_longer_than_default` | un valore di sink (§5.1) oltre il suo default |
| `external_sink_changed` | un sink esterno aggiunto o modificato rispetto alla policy di piattaforma attuale (GDPR art. 28 e 44) |
| `ip_full` | `ipAddress=full` |
| `review_overdue` | `accountability.reviewDueAt` nel passato o vuota |
| `accountability_incomplete` | `roPARef` o `assessmentRef` vuoti |

Gli stessi avvisi valgono all'**assegnazione** a un tenant e
all'**accorciamento retroattivo** (§4.2).

### 1.5 Quattro occhi

- Impostazione del modulo `four_eyes_enabled` (bool, default `true`,
  `ConfigSchema` di compliance). Disattivarla è una modifica di
  configurazione, già registrata nell'audit dal gestore di moduli dell'SDK.
- Con quattro occhi attivo, una modifica **con avvisi** (creazione, modifica,
  assegnazione, rimozione di assegnazione, accorciamento retroattivo) non si
  applica: diventa una `PolicyChangeRequest`:

```go
type PolicyChangeRequest struct {
    UUID            string
    Kind            string // create | update | assign | unassign | shorten_existing
    PolicyUUID      string
    TenantID        string
    Payload         ChangePayload // policy (create/update) oppure tenant e policy (assign/unassign), con l'assegnazione precedente
    ExpectedVersion int
    Warnings        []string
    Reason          string
    RequestedBy     string
    RequestedAt     time.Time
    Status          string // pending | approved | rejected | superseded | expired
    DecidedBy       string
    DecidedAt       *time.Time
    DecisionNote    string
}
```

- Un secondo operatore con `policy.manage`, **diverso** dall'autore, la
  approva o la rifiuta, con nota obbligatoria. L'approvazione rivalida contro
  lo stato attuale: se la versione è cambiata la richiesta diventa
  `superseded`; se è valida si applica in transazione (§3.3).
- Le richieste `pending` scadono dopo 14 giorni (`expired`).
- Le modifiche **senza avvisi** si applicano subito, come prima.
- Autore, approvatore, esito e nota finiscono nell'audit e nell'export.
- Allineamento T2: non esiste il tipo di richiesta `delete` (una policy si
  cancella solo se non è assegnata, quindi senza avvisi e subito, con
  motivazione e audit; la versione di cancellazione resta in
  `compliance_policy_versions` con `changeKind: "delete"`); `shorten_existing`
  arriva con la T4. Fino alla T3 l'audit delle modifiche passa dall'`AuditSink`
  dopo il commit (best effort); versioni e storico, scritti in transazione,
  sono la traccia durevole.

## 2. Log operativi e trace

### 2.1 Risoluzione (`PolicyService`)

- `compliance/services/policy_service.go` tiene un'**istantanea immutabile**
  (`atomic.Pointer`): policy di piattaforma, policy effettiva per tenant
  assegnato, **policy di contenuto più restrittiva attiva** (piattaforma più
  policy assegnate ad almeno un tenant).
- Refresh da Mongo ogni **5 secondi** e subito dopo ogni scrittura
  sull'istanza che l'ha fatta. I puntatori `*LogContentPolicy` restano gli
  stessi finché la versione della policy non cambia (l'handler ci tiene una
  cache).
- Se il refresh fallisce si tiene l'ultima istantanea (mai verso una policy
  meno restrittiva); senza istantanea valgono i default statici.
- Pubblicazione: `module.ServiceCompliancePolicy` in `ProvidedServices()`;
  dopo `InitAll`, `cmd/server/main.go` chiama
  `utils.SwapLogPolicyResolver(...)` e `telemetry.SwapSpanPolicyResolver(...)`.

### 2.2 Ordine di restrittività

- `ipAddress`: `full` < `truncated` < `hashed` < `omitted`
- `userAgent`: `full` < `omitted`
- `subjectIds`: `uuid` < `hashed` < `omitted`
- `piiKeys`: unione; manca una chiave → meno restrittiva
- `scanFreeText`: `false` < `true`
- retention (ottica privacy): più giorni = meno restrittivo

La più restrittiva attiva si calcola campo per campo sul **contenuto**; non
tocca l'evidenza di audit (§3.1), che ha un nucleo fuori da ogni policy.

### 2.3 `PolicyHandler` slog

Catena in `SetupLogger`:

```
TraceContextHandler → PerModuleLevelHandler → PolicyHandler → Fanout → stdout / OTLP
```

- **Tenant del record**: `ctxauth.GetTenantID(ctx)`, altrimenti le
  annotazioni di richiesta, altrimenti `""` → più restrittiva (D6).
- Gli attributi di `With`/`WithGroup` si conservano grezzi e si mascherano
  alla scrittura, con cache per puntatore di policy (al massimo 32 voci).
- Chiavi normalizzate (solo lettere e cifre, minuscole); regole in ordine:
  1. **segreti** (sempre, D11; confronto «contiene»): `password`, `passwd`,
     `secret`, `token`, `authorization`, `cookie`, `apikey`, `privatekey`,
     `credential` → `[REDACTED]`. Elenco in `internal/shared/redact`,
     condiviso con l'anteprima Loki;
  2. **IP** (`remote`, `ip`, `ipaddress`, `clientip`, `remoteaddr`,
     `remoteip`): la porta di `RemoteAddr` si toglie prima; `truncated` =
     IPv4 /24, IPv6 /48; `hashed` = `h:` + 16 caratteri esadecimali di
     `HMAC-SHA256(chiave, "ip:"+ip)`; `omitted` = attributo rimosso; valore
     non interpretabile in `truncated` → `[REDACTED]`;
  3. **user agent** (`ua`, `useragent`): intero o rimosso;
  4. **identificativi** (`userid`, `useruuid`, `actoruserid`, `subjectid`):
     `uuid`, `hashed` (dominio `sub:`), `omitted`;
  5. **dati personali**: chiave **uguale** a una delle `piiKeys` → `[PII]`;
  6. **testo libero** (se `scanFreeText`), sul messaggio e sui valori stringa
     rimasti: email → `[EMAIL]`, IBAN → `[IBAN]`, codice fiscale → `[CF]`,
     IPv6 e poi IPv4 validati con `net.ParseIP` → secondo `ipAddress`.
- Tipi: stringhe, `LogValuer` risolti, `error` come stringa, `map[string]any`
  e `[]any` ricorsivi; altri `KindAny` invariati salvo che la chiave cada
  nelle regole 1 o 5 (il resto lo intercetta il linter, §2.6).
- **Chiave HMAC**: HKDF-SHA256 da `OAUTH_TOKEN_ENCRYPTION_KEY`, info
  `orkestra/log-hash/v1`. Non si salva; è uguale su tutte le repliche; se la
  variabile manca o non è esadecimale da 32 byte, `hashed` equivale a
  `omitted`. Ruotarla interrompe la correlazione con gli hash precedenti.
- Un panic durante il mascheramento diventa `[REDACTED:error]`, il record
  viene scritto e una metrica conta l'evento.
- Costo: meno di 2 µs per record con 10 attributi senza `scanFreeText`.

### 2.4 Request logger

- `RequestLogger` installa `ctxauth.RequestAnnotations` nel contesto;
  `RequireAuth`, `OptionalAuth`, il validatore JWT e `RequireAudience` le
  riempiono (tenant, tipo di tenant, utente, ruolo, audience). Un test AST
  impedisce a un nuovo percorso di autenticazione di saltarle.
- La riga `http_request` registra `route` (il **template** chi, es.
  `/v1/admin/users/{id}`) invece del path reale. Solo quando non c'è un
  template (404, richieste fuori dal router) registra `path`, che passa
  comunque per il mascheramento.
- Le annotazioni danno all'`PolicyHandler` il tenant anche per la riga
  `http_request`, che gira fuori dal contesto derivato di `RequireAuth`.

### 2.5 Trace

- `TenantBaggage` e `otelhttp` mettono negli span tenant, utente e URL.
  Un **exporter wrapper** (`internal/shared/telemetry/masking_exporter.go`)
  applica la policy del tenant dello span (attributo `tenant.id`, altrimenti
  la più restrittiva) agli attributi e agli eventi prima di inoltrarli
  all'exporter OTLP, con le stesse regole di §2.3. Gli attributi
  `http.target`/`url.path` diventano il route template quando disponibile.
- Il wrapper usa `telemetry.SwapSpanPolicyResolver`, simmetrico a quello dei
  log.

### 2.6 Linter `logscope`

- Nuovo analizzatore `backend/tools/logscope` (gate `make backend-logscope`
  in `ci-backend`), con baseline come `piiscan`.
- Segnala: `slog.Any` con un valore di tipo struct, puntatore a struct o
  interfaccia diversa da `error` (i figli di `slog.Group` sono già attributi
  e passano per le stesse regole); chiavi di attributo che
  contengono frammenti di segreto con valore non costante.
- Le chiamate esistenti vanno in `tools/logscope/baseline.txt`; una nuova
  violazione fa fallire la CI.

## 3. Evidenza di audit

### 3.1 Nucleo obbligatorio

Ogni documento di `compliance_audit_events` ha questi campi, che nessuna
policy modifica:

| Campo | Origine |
|---|---|
| `uuid`, `timestamp` (UTC del server) | sink |
| `actorUserId` o id dell'account di servizio, `actorType` | emitter |
| `audience` | emitter, altrimenti annotazioni di richiesta |
| `action`, `resourceType`, `resourceId`, `outcome` | emitter |
| `tenantId`, `tenantKind` | emitter, altrimenti contesto |
| `ipAddress` **intero**, `userAgent` | emitter, altrimenti contesto |
| `requestId`, `traceId` | contesto (chi `RequestID`, span OTel) — **nuovi** |
| `retentionClass`, `policyUuid`, `policyVersion`, `retainUntil` | sink (§4.1) |
| `chainId`, `seq`, `prevHash`, `hash` | scrittore della catena (§3.4) |

L'IP resta intero nell'evidenza: serve a un'indagine, e il limite è la
retention della classe. Il mascheramento dell'IP riguarda solo log e trace.

### 3.2 Contenuto controllato alla scrittura

Regole fisse nel codice, uguali per tutti i tenant:

- **`actorEmail` non si salva più.** Il campo sparisce da `iface.AuditEvent`;
  i 34 emitter che lo passano vengono corretti. I documenti già scritti lo
  conservano fino alla loro scadenza.
- **Allowlist dei metadati per azione**
  (`compliance/services/audit_allowlist.go`), costruita con l'inventario
  degli emitter attuali. Una chiave fuori lista viene **scartata**
  (`compliance_audit_metadata_dropped_total{action}`); un'azione sconosciuta
  tiene i metadati, mascherati, con
  `compliance_audit_unknown_action_total{action}`. Un test cerca nel codice
  ogni `Action:` letterale di un `iface.AuditEvent` e fallisce se manca
  nell'allowlist.
- Sui metadati ammessi: segreti mascherati, chiavi di dati personali
  mascherate, analisi del testo libero, con la stessa logica di §2.3 e una
  policy di evidenza fissa (IP e UA interi, UUID in chiaro).
- **Eventi di sicurezza** (`auth_security_events`, di auth): stesse regole su
  `description` e `metadata`, tramite l'helper condiviso
  `utils.MaskEvidenceFields` chiamato da auth prima dell'insert; ricevono
  anche lo stamp di retention (§4.1) nella classe `authentication_security`.
  Non entrano nella hash chain.

### 3.3 Durabilità

- **Motore di policy**: ogni modifica (policy, versione, assegnazione,
  storico, richiesta di cambio, accorciamento) scrive nella **stessa
  transazione** un documento in `compliance_audit_outbox`. Lo scrittore
  della catena (§3.4) lo reclama con `findOneAndDelete`, lo inserisce
  nell'audit con la sua posizione di catena e, se l'insert fallisce, lo passa
  allo spool. Modifica ed evento sono atomici; l'evento non si perde.
- **Tutti gli altri emitter**: `AuditSink.Emit` mette l'evento nel canale
  dello scrittore; se l'insert fallisce, l'evento (con la posizione di catena
  già assegnata) va nello **spool locale**:
  - directory `COMPLIANCE_AUDIT_SPOOL_DIR` (default `/app/tmp/audit-spool`;
    in staging e produzione su un volume persistente del compose);
  - file JSON lines append-only con `fsync` a ogni evento;
  - riprova ogni 5 secondi; dopo 20 tentativi o 7 giorni l'evento passa in
    `dead-letter/`, conservato per `sinks.spool.deadLetterDays` e segnalato
    da un allarme;
  - allo shutdown si tenta di svuotare lo spool per al massimo 10 secondi;
  - metriche: eventi nello spool, riprove, dead-letter.
- L'atomicità fra evento e modifica di business per gli emitter diversi dal
  motore di policy **non** è garantita (vedi «Non coperto»).

### 3.4 Integrità: hash chain

- Un solo **scrittore per processo** (goroutine con canale) assegna
  `chainId` (UUID generato all'avvio), `seq` crescente, `prevHash` e
  `hash = SHA-256(prevHash ‖ JSON canonico dell'evento)`. Il JSON canonico
  esclude i campi di ciclo di vita (`retainUntil`) e i campi della catena
  stessi. Il primo evento di ogni catena è
  `compliance.audit.chain_started`, con l'id e l'hash di testa della catena
  precedente dello stesso host se noti.
- **Cancellazioni**: quando il job di retention cancella un evento, scrive
  prima una **lapide** in `compliance_audit_tombstones`
  (`chainId`, `seq`, `prevHash`, `hash`, `deletedAt`, `reason`), senza
  contenuto. La catena resta verificabile da un capo all'altro anche con
  buchi in mezzo (classi e tenant diversi scadono in momenti diversi). Le
  lapidi stanno in `compliance_evidence`.
- **Verifica giornaliera** dopo il job: per ogni catena, dal cursore
  `compliance_audit_chain_state.verifiedSeq`, ricalcola gli hash su eventi e
  lapidi in ordine di `seq`. Esito registrato
  (`compliance.audit.chain_verified` o `…chain_broken`), metrica
  `compliance_audit_chain_broken`, allarme.
- **Ancore giornaliere**: l'hash di testa di ogni catena finisce in
  `compliance_audit_anchors` (classe `compliance_evidence`) e nell'export.
- Le regole di accesso a Mongo (utente applicativo senza privilegi di
  amministrazione, utente di amministrazione separato) vanno nel runbook: il
  codice non può imporle.

### 3.5 Emitter collegati

| Evento | Dove |
|---|---|
| `auth.logout` | auth, sulle revoche di sessione volontarie (entrambe le audience) |
| `auth.login.*`, `auth.mfa.*` dei **clienti** | il sink viene collegato anche a `ServiceClientPasswordAuthService` (oggi solo l'istanza operatore lo riceve) e al callback OAuth del client |
| `admin.tenant.impersonate` | `AuthMiddleware.SetAuditSink` collegato |
| ciclo di vita degli utenti | `userService.SetAuditSink` collegato |
| reset password/MFA da admin, scollegamento OAuth, gestione MFA/passkey | `authService.SetAuditSink` collegato |
| account di servizio | `ServiceAccountService.SetAuditSink` collegato |
| `logging.levels.changed`, `logging.diagnostic.*` | modulo logging, tramite `iface.AuditSink` |
| `logging.preview.queried` | anteprima Loki: chi ha consultato quali log (modulo, finestra, filtri, non i risultati) |

Compliance collega i setter nel proprio `Init` tramite il `ServiceRegistry`,
come fa già per `PasswordAuthService` e il servizio tenant.

## 4. Retention degli eventi

### 4.1 Stamp alla scrittura

- Il sink calcola la classe (§1.3), chiede a `PolicyService.RetentionFor`
  la decisione per il tenant dell'evento (la policy assegnata se imposta
  quella classe, altrimenti la piattaforma) e scrive `retentionClass`,
  `policyUuid`, `policyVersion`, `retainUntil = timestamp + giorni`.
- Stesso stamp sugli eventi di sicurezza (classe `authentication_security`) e
  sulle collection di evidenza del motore (classe `compliance_evidence`).
- Allungare una retention vale solo per gli eventi scritti da quel momento:
  nessun trattamento viene esteso a posteriori.

### 4.2 Accorciamento esplicito degli esistenti

- Azione separata «applica agli eventi già scritti» su una classe di una
  policy (per un tenant, o per la piattaforma): ricalcola
  `retainUntil = min(retainUntil, timestamp + giorni)` per gli eventi di
  quella classe e di quel perimetro, **mai sotto il minimo della classe**.
- Richiede motivazione, genera avvisi (e quindi, con quattro occhi, una
  richiesta da approvare), è registrata con il numero di eventi toccati.
- `retainUntil` è un campo di ciclo di vita: modificarlo non rompe la catena
  (§3.4).

### 4.3 Job giornaliero

- Una volta al giorno (UTC) su **una sola replica**: `SETNX` su
  `compliance:event-retention:<AAAA-MM-GG>` (scadenza 25 h), controllo
  ogni ora, primo controllo 10 minuti dopo l'avvio. Senza `SetNX` nel client
  Redis gira una volta al giorno per replica (è idempotente).
- Legge le legal hold attive; **se la lettura fallisce non cancella nulla**
  ed è un allarme.
- Cancella da `compliance_audit_events`, `auth_security_events` e dalle
  collection di evidenza i documenti con `retainUntil < now`, **esclusi**:
  - i soggetti sotto hold, come `actorUserId`/`userUuid` o come risorsa
    `resourceType=user`;
  - **tutto il tenant** di una hold che ha `TenantID` (D22).
- Prima di cancellare un evento dell'audit scrive la sua lapide (§3.4).
- Blocchi da 1000 `_id`, al massimo 50 per ambito per esecuzione; il resto il
  giorno dopo; metrica del backlog.
- Esito registrato (`compliance.retention.purged`, con i conteggi per classe)
  in `compliance_evidence`.
- Il `RetentionService` esistente (lapidi anonimizzate degli utenti) resta
  com'è.

### 4.4 Esecuzione immediata

`POST /v1/admin/compliance/retention/run` (`policy.manage` + step-up) esegue
subito il job, ignorando la chiave del giorno. `restore.sh` lo chiama alla
fine di un ripristino: un backup ripristinato riporta dati già cancellati.

## 5. Sink

### 5.1 Sezione `sinks` della policy di piattaforma

```go
type SinkPolicy struct {
    Loki          struct{ Days, WarnErrorDays int } // 1–3650; WarnErrorDays ≥ Days
    Tempo         struct{ Days int }                // 1–365
    Prometheus    struct{ Days int }                // 1–365
    ContainerLogs struct{ MaxSizeMB, MaxFiles int } // 1–1024; 1–100
    Backups       struct{ Days, MinKeep int }       // 1–3650; 1–100
    DSRExportDays int                               // 1–90
    Spool         struct{ DeadLetterDays int }      // 1–365
    External      []ExternalSink
}

type ExternalSink struct {
    Name                  string
    Kind                  string // otlp | syslog | backup_offsite | other
    Processor             string // responsabile del trattamento
    DPARef                string
    Region                string
    TransferBasis         string // es. "EU", "SCC 2021/914", "adequacy decision"
    DeclaredRetentionDays int
    ReviewDueAt           time.Time
}
```

Default: Loki 14/30, Tempo 3, Prometheus 15, container 50 MB × 5 file,
backup 30 giorni con almeno 3 copie, export DSR 30, dead-letter 30, nessun
sink esterno. `export_retention_days` esce dalla configurazione del modulo e
diventa `sinks.dsrExportDays`.

### 5.2 Manifest e file generati

- Il backend scrive in `COMPLIANCE_RUNTIME_DIR` (default
  `/app/runtime/compliance`, bind mount di `docker/runtime/compliance/`
  sull'host, fuori da git) con scrittura atomica (file temporaneo e rename):
  - `manifest.json`: policy e versione di piattaforma, sezione `sinks`,
    `generatedAt`;
  - `loki-runtime.yaml`: override per il tenant di Loki (`fake`, auth
    disattivata) con `retention_period` e `retention_stream` per
    `{level=~"(?i)warn|error"}` — il filtro case-insensitive corregge il bug
    per cui la retention differenziata non si applicava mai;
  - `tempo-overrides.yaml`: `block_retention` per il tenant di Tempo.
- Riscrittura all'avvio e a ogni cambio di versione della policy di
  piattaforma. Loki ricarica il suo file ogni 10 secondi (`runtime_config`),
  Tempo il suo (`per_tenant_override_config`); la configurazione statica
  resta come ripiego se il file manca.
- Il formato esatto degli override di Loki 3.0 e Tempo 2.4 si fissa in
  implementazione con un test contro i container reali.

### 5.3 Applicazione per sink

| Sink | Come |
|---|---|
| Loki, Tempo | file del §5.2, ricaricati a caldo |
| Trace in uscita | mascherate dal wrapper dell'exporter (§2.5) |
| Log dei container | `orkestra.sh` legge il manifest prima di ogni deploy ed esporta `ORKESTRA_LOG_MAX_SIZE` / `ORKESTRA_LOG_MAX_FILE`; tutti i compose (infra, dev, staging, prod, osservabilità) usano un blocco comune `x-logging` con driver `local`. Con il driver globale `syslog` di questo host i container del progetto smettono di scrivere nel syslog dell'host: va detto nella documentazione |
| Prometheus | `--storage.tsdb.retention.time` da `ORKESTRA_PROMETHEUS_RETENTION`, esportata da `orkestra.sh` dal manifest |
| Backup | `backup-cron.sh` legge giorni e copie minime dal manifest (con `jq`); se il manifest manca usa le variabili d'ambiente attuali e lo segnala nel report |
| Export DSR | valore dichiarato; si applicherà quando l'export su blob sarà implementato |
| Spool e dead-letter | gestiti dal backend (§3.3) |
| Esterni | solo dichiarati; `reviewDueAt` scaduta è un allarme |

Un ripristino crea il file `run-retention-now` nella stessa directory; il
backend lo vede entro un minuto, esegue il job (§4.4) e lo cancella. Così
`restore.sh` non ha bisogno di un token.

### 5.4 Verifica

| Sink | Verifica | Frequenza |
|---|---|---|
| Loki | il backend interroga Loki (`LOKI_QUERY_URL`) sulla finestra fra `days+2` e `days+1` giorni fa (e lo stesso per warn/error): un risultato è una violazione | giornaliera |
| Tempo | ricerca (`TEMPO_QUERY_URL`, nuova variabile) sulla stessa finestra | giornaliera |
| Log dei container | `orkestra.sh retention check` confronta `docker inspect` (driver e opzioni di log) di ogni container dello stack con il manifest e scrive `report-container-logs.json`; lo eseguono il deploy e `backup-cron.sh` | a ogni deploy e ogni notte |
| Backup | `backup-cron.sh` dopo la potatura scrive `report-backups.json` (età del più vecchio, numero, parametri usati) | ogni notte |
| Esterni | `reviewDueAt` | continua |

- Un sink senza URL configurato (es. Loki non installato) risulta **«non
  presente»**, non «in violazione».
- Un report più vecchio di 26 ore è un allarme («verifica mancante»).
- Lo stato di ogni sink (retention richiesta, applicata, ultima verifica,
  violazione) sta in `compliance_sink_status` ed è esposto dall'API e dalla
  console.

### 5.5 Overlay di osservabilità messo in sicurezza

Loki e Tempo contengono dati personali (art. 32):

- porte pubblicate solo su `${INFRA_BIND_ADDRESS}`; Loki, Tempo e il
  collector non pubblicati sull'host (solo rete interna), Grafana sì;
- Grafana: `GF_SECURITY_ADMIN_PASSWORD` obbligatoria (`${VAR:?}`, niente
  default), accesso anonimo disattivato;
- Prometheus senza `--web.enable-lifecycle`;
- il gate CI sulle credenziali di default
  (`docker/tests/credential-fallbacks.test.sh`) include anche questo file.

## 6. Evidenze ed export firmato

- **Contenuto** dell'export per un periodo `[from, to]`:
  - tutte le versioni di policy e le righe di storico delle assegnazioni fino
    a `to` (per ricostruire cosa valeva già a `from`);
  - le richieste di cambio con autore, approvatore, esito e note;
  - gli eventi di audit `compliance.*` del periodo (verifiche delle catene,
    esiti del job, verifiche dei sink, allarmi aperti e chiusi, export);
  - le ancore delle catene del periodo;
  - il catalogo delle classi di retention (finalità, base giuridica, minimi).
- **Firma Ed25519**: la chiave privata è
  `COMPLIANCE_EVIDENCE_SIGNING_KEY` (seed di 32 byte in base64), separata dai
  dati e dalla chiave di cifratura della configurazione. Il file è
  `{ "payload": {...}, "signature": "...", "publicKey": "...", "algorithm": "Ed25519" }`,
  con la firma calcolata sul JSON canonico di `payload`.
  Senza chiave l'export risponde 503 (`compliance.evidence_signing_unavailable`):
  mai un export non firmato.
- La chiave pubblica è mostrata in console e servita da un endpoint; un
  piccolo comando `backend/tools/evidenceverify` permette all'auditor di
  verificare un export da solo.
- **Accesso**: permesso `system.compliance.evidence.export` (distinto da
  `policy.read`), step-up, `Cache-Control: no-store`; ogni export è
  registrato (`compliance.evidence.exported`, con periodo e dimensione).

## 7. API e permessi

Tier-1 operatore, sotto `/v1/admin/compliance/`:

| Metodo e percorso | Permesso | Note |
|---|---|---|
| `GET policies`, `GET policies/{id}`, `GET policies/{id}/versions`, `GET policies/effective?tenantId=`, `POST policies/validate` | `policy.read` | l'elenco include tenant assegnati e campi meno restrittivi della piattaforma |
| `GET retention-classes` | `policy.read` | catalogo di §1.3 |
| `POST policies`, `PUT policies/{id}`, `DELETE policies/{id}` | `policy.manage` + step-up | `PUT` con `expectedVersion`; con avvisi e quattro occhi attivo rispondono **202** con la richiesta creata |
| `GET policy-assignments`, `PUT/DELETE policy-assignments/{tenantId}` | read / manage + step-up | come sopra per i 202 |
| `POST policy-assignments/{tenantId}/validate` | `policy.read` | avvisi di un'assegnazione (o della rimozione) prima di confermarla |
| `POST policies/{id}/retention/shorten` | `policy.manage` + step-up | `{class, tenantId?, reason, acknowledgeWarnings}` (§4.2) |
| `GET change-requests`, `GET change-requests/{id}` | `policy.read` | filtro per stato |
| `POST change-requests/{id}/approve`, `…/reject` | `policy.manage` + step-up | approvatore ≠ autore; nota obbligatoria |
| `POST retention/run` | `policy.manage` + step-up | §4.4 |
| `GET sinks/status` | `policy.read` | §5.4 |
| `GET alarms` | `policy.read` | allarmi aperti e recenti |
| `GET evidence/public-key` | `policy.read` | |
| `GET evidence/export?from=&to=` | `evidence.export` + step-up | §6 |

Tier-2 cliente: `GET /v1/tenants/{tenantId}/compliance/policy` (audience
client, solo il proprio tenant): modalità di IP, UA e identificativi,
giorni per classe impostabili per tenant, data di validità.

Permessi (in `compliance.Permissions()`, regole Cedar solo lato operatore
in `tenant_scope.cedar`):

- `system.compliance.policy.read` — super_admin, administrator, developer;
- `system.compliance.policy.manage` — super_admin, administrator; il
  developer solo in sviluppo e staging (regola già in vigore in authz per i
  suffissi diversi da `.read`/`.view`/`.self`);
- `system.compliance.evidence.export` — super_admin, administrator (stessa
  regola per il developer).

Codici errore (`internal/shared/errcode`, con golden):
`compliance.policy_not_found` (404), `…policy_version_conflict`,
`…policy_platform_protected`, `…policy_in_use`, `…policy_name_taken`,
`…change_request_not_pending`, `…change_request_self_approval` (409),
`…policy_invalid`, `…policy_warnings_unacknowledged`,
`…policy_reason_required`, `…policy_period_invalid` (422),
`…evidence_signing_unavailable` (503), `compliance.change_request_not_found`,
`compliance.tenant_not_found` (404), `compliance.policy_unavailable` (503),
`compliance.policy_persistence_failed` (500).

## 8. Console (frontend-admin)

- **Tab «Policy»** in `/admin/compliance`: catalogo con badge «Piattaforma»,
  «Meno restrittiva», «Revisione scaduta»; «Nuova policy», «Duplica».
- **Tab «Richieste»**: richieste di cambio in attesa (con il numero nel
  titolo del tab), dettaglio con differenze e avvisi, «Approva» e «Rifiuta»
  con nota; l'autore non vede i pulsanti sulle proprie richieste.
- **Tab «Sink»**: stato di ogni sink (retention richiesta, applicata, ultima
  verifica, violazione, «non presente»), «Esegui ora la retention».
- **Esportazione delle evidenze** (visibile con `evidence.export`): periodo,
  download del file firmato, chiave pubblica mostrata.
- **Allarmi**: banner in cima a `/admin/compliance` con gli allarmi aperti.
- **Dettaglio policy** `/admin/compliance/policies/:id` con sezioni
  `?section=`: *Impostazioni* (contenuto dei log; retention per classe con
  minimo, default, finalità e base giuridica; accountability), *Sink* (solo
  piattaforma), *Storico*, *Tenant*. Salvataggio: validazione → modale con
  differenze, avvisi, motivazione obbligatoria, conferma degli avvisi →
  applicato oppure «Richiesta inviata per approvazione». 409 blocca
  l'editor. `useBlocker` scatta sul cambio di pathname o di `?from=`, non di
  `?section=`.
- **Dettaglio tenant** (clients e internal-tenants, Panoramica): card
  «Policy di conformità» con policy effettiva, provenienza, retention per
  classe; «Cambia» con motivazione e avvisi; «Accorcia gli eventi esistenti».
- Tutti i testi in `adminCompliance.*`, en e it.

## 9. Monitoraggio e allarmi

**Metriche Prometheus** (senza tenant né utente nelle label, ADR-0002).
Il collector usa il namespace `orkestra`: il nome esposto è `orkestra_`
seguito dal nome della tabella (es. `orkestra_compliance_log_masking_panics_total`).

| Metrica | Significato |
|---|---|
| `compliance_audit_insert_failures_total` | insert diretti falliti (evento passato allo spool) |
| `compliance_audit_spool_events`, `…_spool_retries_total`, `…_dead_letter_events` | stato dello spool |
| `compliance_audit_metadata_dropped_total{action}`, `…_unknown_action_total{action}` | allowlist |
| `compliance_log_masking_panics_total` | panic recuperati nel mascheramento di log e trace |
| `compliance_policy_snapshot_age_seconds` | età dell'ultima istantanea valida; finché nessuna istantanea è stata caricata, il tempo dall'avvio (l'allarme scatta anche con Mongo giù al boot) |
| `compliance_retention_backlog`, `compliance_retention_run_failures_total` | job di retention |
| `compliance_audit_chain_broken` (0/1) | esito dell'ultima verifica |
| `compliance_sink_violation{sink}` (0/1), `compliance_sink_report_age_seconds{sink}` | verifica dei sink |
| `compliance_clock_skew_seconds` | differenza fra l'orologio del backend e `hello.localTime` di Mongo |
| `compliance_reviews_overdue` | policy e sink esterni con revisione scaduta |

**Allarmi generati dal backend** (valutatore ogni 5 minuti su una sola
replica, stesso `SETNX` del job con chiave per finestra):

| Allarme | Condizione |
|---|---|
| `audit_chain_broken` | ultima verifica fallita |
| `audit_dead_letter` | dead-letter non vuota |
| `audit_spool_backlog` | eventi nello spool da oltre 15 minuti |
| `retention_failed` | job fallito o lettura delle hold fallita |
| `sink_violation` | una sonda o un report segnala dati oltre la retention |
| `sink_report_missing` | report più vecchio di 26 ore |
| `policy_snapshot_stale` | istantanea più vecchia di 5 minuti |
| `clock_skew` | scostamento oltre 2 secondi |
| `review_overdue` | revisione di una policy o di un sink esterno scaduta |
| `no_alarm_recipients` | nessun destinatario configurato |

- Stato in `compliance_alarms` (aperto/chiuso, dal, ultima notifica);
  apertura e chiusura sono eventi di audit (`compliance.alarm.opened` /
  `…closed`).
- Email tramite il modulo notification agli indirizzi della nuova
  impostazione `alarm_recipients` (lista), ripetuta ogni 24 ore finché
  l'allarme resta aperto; banner in console.
- Chi usa l'overlay può comunque costruire alert su Prometheus con le
  metriche sopra; non sono necessari.

## 10. Avvio, migrazioni, errori

### 10.1 Primo avvio

- Crea la policy di piattaforma (default di §1.2) con versione 1, autore
  `system`; operazione idempotente (indice parziale unico).
- Scrive manifest e file dei sink (§5.2).
- Crea la directory dello spool e ne ripete il contenuto.

### 10.2 Migrazioni

- Il registry crea gli indici dichiarati **prima** di ogni `Init` e un
  conflitto è solo un warn (l'indice vecchio resterebbe). Quindi:
  - `compliance_audit_events` dichiara `{timestamp: -1}` (nome diverso dal
    vecchio TTL), `{retainUntil: 1}`, `{chainId: 1, seq: 1}` unico parziale
    sui documenti con `chainId`;
  - il costruttore di `SecurityEventService` smette di creare il TTL e crea
    `{retainUntil: 1}`;
  - l'`Init` di compliance rimuove gli indici `{timestamp: 1}` con
    `expireAfterSeconds` da entrambe le collection (idempotente, ignora le
    collection mancanti).
- **Backfill** una tantum e idempotente dei documenti senza `retainUntil`:
  classe calcolata dall'azione e da `tenantKind` (gli eventi vecchi non hanno
  l'audience: `internal` → operatore, altrimenti cliente),
  `retentionBackfilled: true`, conteggi registrati nell'audit. Gli eventi
  vecchi non hanno posizione di catena: la verifica li ignora e l'export ne
  riporta il numero.
- Si rimuove il campo mai usato `Tenant.RetentionPolicyID`.
- `export_retention_days` esce dal `ConfigSchema` di compliance e passa in
  `sinks.dsrExportDays`.

### 10.3 Errori

| Situazione | Comportamento |
|---|---|
| Mongo irraggiungibile al refresh della policy | ultima istantanea; warn al massimo una volta al minuto; allarme dopo 5 minuti |
| Nessuna istantanea valida | default statici |
| Insert di audit fallito | spool; dopo 20 tentativi o 7 giorni dead-letter e allarme |
| Lettura delle legal hold fallita | nessuna cancellazione, esito `failure`, allarme |
| Panic nel mascheramento | `[REDACTED:error]`, record scritto, metrica |
| Chiave HMAC assente | `hashed` → `omitted` |
| Chiave di firma assente | export 503, mai non firmato |
| Directory di runtime non scrivibile | warn, sink «configurazione non applicata», allarme `sink_violation` |
| Approvazione di una richiesta su una versione cambiata | richiesta `superseded`, nulla applicato |

## 11. Test

- **Unitari**: ordine di restrittività; validazione (ogni errore e avviso,
  minimo rigido di `admin_access`); classificazione degli eventi; mascheratore
  (IP con porta, IPv6, testo libero, gruppi, mappe, errori, panic);
  `PolicyHandler` (tenant da contesto e annotazioni, più restrittiva senza
  tenant, `With` mascherati alla scrittura dopo uno swap); wrapper delle
  trace; allowlist; JSON canonico e hash della catena; firma e verifica
  dell'export; generazione dei file di Loki e Tempo; valutatore degli
  allarmi; benchmark del mascheramento.
- **Integrazione Mongo**: CRUD delle policy con 409 senza scritture;
  quattro occhi (autore ≠ approvatore, `superseded`, scadenza); outbox
  atomico con la modifica; spool con Mongo non raggiungibile e riprova;
  catena con buchi coperti da lapidi e rilevamento di una manomissione;
  job per `retainUntil` con hold di soggetto e di tenant e lettura delle
  hold fallita; accorciamento mai sotto il minimo; backfill; migrazione
  degli indici; export che resta completo dopo la cancellazione degli
  eventi.
- **Catena reale**: una richiesta attraverso `RequestLogger` e `RequireAuth`
  produce `tenant_id`, `user_id` e `route` sulla riga `http_request`.
- **Container reali** (`docker/tests/retention-overrides.test.sh`): Loki e
  Tempo avviati con i file generati espongono la retention attesa
  (`/runtime_config` di Loki, endpoint di stato degli override di Tempo).
- **Script**: `orkestra.sh retention check` e `backup-cron.sh` scrivono report
  corretti con e senza manifest (estensione di
  `scripts/test-orkestra-helpers.sh`).
- **Frontend**: tab Policy, Richieste, Sink; salvataggio con avvisi →
  richiesta; approvazione e rifiuto; 409; `useBlocker`; sola lettura; card del
  tenant; export; parità en/it.
- **Gate CI**: `tenantscope`, `policycoverage`, `errquality`, `piiscan`,
  `logscope` (nuovo), `openapi-check`, credenziali di default dei compose.

## 12. Documentazione

- **ADR-0022** «Configurable compliance policies for logs, audit evidence and
  retention»: supera l'audit «conservato per sempre» dell'ADR-0005 §1.3 e la
  retention fissa dell'ADR-0009.
- `compliance/AGENTS.md`, `logging/AGENTS.md`, `docs/site/modules/core/`
  (compliance, logging): motore, classi, evidenza, sink, allarmi; correzione
  dell'affermazione che `tenant_id` sia su ogni riga.
- `docs/site/operating/logging-policy.mdx`: la policy di logging richiesta da
  ISO A.8.15 (cosa, dove, per quanto, chi accede, come si rivede).
- **Runbook** `docs/site/operating/compliance-runbook.mdx`: NTP sugli host;
  ruoli Mongo separati; access log AdS di SSH e DBMS fuori dall'applicazione;
  procedura di verifica annuale dell'operato degli amministratori
  (export, controllo, esito registrato); ripristino e retention; rotazione
  delle chiavi (HMAC, firma).
- **Modello di voci RoPA** per ogni classe di retention, da adattare dal
  titolare.

## 13. Tappe di consegna

Ogni tappa è rilasciabile da sola e ha il proprio piano.

| Tappa | Contenuto | Dipende da |
|---|---|---|
| **T1 — Log e trace** | tipi `iface`, `redact`, annotazioni e request logger con route template, mascheratore, `PolicyHandler`, wrapper delle trace, linter `logscope` (§2) | — |
| **T2 — Motore di policy** | modelli, classi, validazione, quattro occhi, `PolicyService`, API e console del catalogo (§1, §7, §8 parziale) | T1 |
| **T3 — Evidenza di audit** | sink v2 con nucleo, allowlist, outbox, spool, hash chain e lapidi, eventi di sicurezza, emitter mancanti (§3) | T2 |
| **T4 — Retention** | stamp, job per `retainUntil`, accorciamento esplicito, hold di tenant, backfill, migrazione degli indici, esecuzione immediata (§4, §10.2) | T3 |
| **T5 — Sink** | manifest e file generati, Loki/Tempo, compose e `orkestra.sh`, backup, verifiche, overlay messo in sicurezza (§5) | T2 |
| **T6 — Evidenze, monitoraggio, documentazione** | export firmato e verifica, metriche, allarmi, tab Sink e banner, ADR, runbook, RoPA (§6, §9, §12) | T3, T4, T5 |

Finché T5 e T6 non sono chiuse, **nessuna affermazione di conformità** sui
log operativi va fatta verso clienti o auditor.
