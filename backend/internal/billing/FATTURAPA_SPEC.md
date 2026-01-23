# FatturaPA XML Specification Reference

**Specifica Tecnica Fattura Elettronica Italiana / Italian Electronic Invoice Technical Specification**

_Version: 1.2.3 (FPR12) - Valid from April 1, 2025_
_Includes: Specifiche Tecniche v1.9 updates (TD29, RF20)_

---

## Table of Contents

1. [Overview / Panoramica](#1-overview--panoramica)
2. [Document Structure / Struttura Documento](#2-document-structure--struttura-documento)
3. [FatturaElettronicaHeader](#3-fatturaelettronicaheader)
4. [FatturaElettronicaBody](#4-fatturaelettronicabody)
5. [Enumerations / Codifiche](#5-enumerations--codifiche)
6. [Validation Rules / Regole di Validazione](#6-validation-rules--regole-di-validazione)
7. [SDI Error Codes / Codici Errore SDI](#7-sdi-error-codes--codici-errore-sdi)
8. [Official Sources / Fonti Ufficiali](#8-official-sources--fonti-ufficiali)

---

## 1. Overview / Panoramica

### XML Namespaces

```xml
<?xml version="1.0" encoding="UTF-8"?>
<p:FatturaElettronica
    xmlns:p="http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2"
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
    xsi:schemaLocation="http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2
                        http://www.fatturapa.gov.it/export/fatturazione/sdi/fatturapa/v1.2/Schema_del_file_xml_FatturaPA_versione_1.2.xsd"
    versione="FPR12">
```

### Transmission Formats / Formati di Trasmissione

| Code | IT Description | EN Description | CodiceDestinatario Length |
|------|----------------|----------------|---------------------------|
| **FPA12** | Fattura verso Pubblica Amministrazione | Invoice to Public Administration | 6 characters |
| **FPR12** | Fattura verso privati (B2B/B2C) | Invoice to private entities | 7 characters |

### Cardinality Legend / Legenda Cardinalità

| Symbol | Meaning IT | Meaning EN |
|--------|------------|------------|
| `1..1` | Obbligatorio, singolo | Required, single |
| `0..1` | Opzionale, singolo | Optional, single |
| `1..N` | Obbligatorio, multiplo | Required, multiple |
| `0..N` | Opzionale, multiplo | Optional, multiple |

---

## 2. Document Structure / Struttura Documento

### Root Element / Elemento Radice

```
FatturaElettronica
├── @versione (FPA12|FPR12)           [1..1] Formato trasmissione / Transmission format
├── @SistemaEmittente                  [0..1] Sistema che emette / Issuing system (max 10 chars)
├── FatturaElettronicaHeader           [1..1] Intestazione / Header
├── FatturaElettronicaBody             [1..N] Corpo fattura / Invoice body
└── ds:Signature                       [0..1] Firma digitale / Digital signature
```

---

## 3. FatturaElettronicaHeader

### 3.1 DatiTrasmissione / Transmission Data

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **IdTrasmittente** | Transmitter fiscal ID | Complex | 1..1 | |
| └─ IdPaese | Country code | xs:string | 1..1 | ISO 3166-1 alpha-2 (2 chars), e.g., "IT" |
| └─ IdCodice | Fiscal/VAT code | xs:string | 1..1 | 1-28 chars, alphanumeric |
| **ProgressivoInvio** | Sequential transmission number | xs:string | 1..1 | 1-10 chars, alphanumeric |
| **FormatoTrasmissione** | Transmission format | xs:string | 1..1 | Enum: FPA12, FPR12 |
| **CodiceDestinatario** | Recipient code | xs:string | 1..1 | 6 chars (PA) or 7 chars (B2B), or "0000000" |
| **ContattiTrasmittente** | Transmitter contacts | Complex | 0..1 | |
| └─ Telefono | Phone number | xs:string | 0..1 | 5-12 chars |
| └─ Email | Email address | xs:string | 0..1 | 7-256 chars |
| **PECDestinatario** | Recipient PEC email | xs:string | 0..1 | 7-256 chars, required if CodiceDestinatario = "0000000" |

### 3.2 CedentePrestatore / Seller-Provider

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **DatiAnagrafici** | Identification data | Complex | 1..1 | |
| └─ IdFiscaleIVA | VAT number | Complex | 1..1 | |
| └─── IdPaese | Country code | xs:string | 1..1 | ISO 3166-1 alpha-2 |
| └─── IdCodice | VAT number | xs:string | 1..1 | 1-28 chars (IT: 11 digits for P.IVA) |
| └─ CodiceFiscale | Fiscal code | xs:string | 0..1 | 11-16 chars (individuals: 16, companies: 11) |
| └─ Anagrafica | Name/Denomination | Complex | 1..1 | |
| └─── Denominazione | Company name | xs:string | 0..1 | 1-80 chars (use for companies) |
| └─── Nome | First name | xs:string | 0..1 | 1-60 chars (use for individuals) |
| └─── Cognome | Last name | xs:string | 0..1 | 1-60 chars (use for individuals) |
| └─── Titolo | Title | xs:string | 0..1 | 2-10 chars |
| └─── CodEORI | EORI code | xs:string | 0..1 | 13-17 chars |
| └─ AlboProfessionale | Professional register | xs:string | 0..1 | 1-60 chars |
| └─ ProvinciaAlbo | Register province | xs:string | 0..1 | 2 chars (IT province code) |
| └─ NumeroIscrizioneAlbo | Register number | xs:string | 0..1 | 1-60 chars |
| └─ DataIscrizioneAlbo | Register date | xs:date | 0..1 | YYYY-MM-DD |
| └─ RegimeFiscale | Fiscal regime | xs:string | 1..1 | Enum: RF01-RF20 |
| **Sede** | Registered address | Complex | 1..1 | |
| └─ Indirizzo | Street address | xs:string | 1..1 | 1-60 chars |
| └─ NumeroCivico | Street number | xs:string | 0..1 | 1-8 chars |
| └─ CAP | Postal code | xs:string | 1..1 | 5 digits (IT) |
| └─ Comune | City | xs:string | 1..1 | 1-60 chars |
| └─ Provincia | Province | xs:string | 0..1 | 2 chars (IT province code) |
| └─ Nazione | Country | xs:string | 1..1 | ISO 3166-1 alpha-2 |
| **StabileOrganizzazione** | Permanent establishment | Complex | 0..1 | Same structure as Sede |
| **IscrizioneREA** | REA registration | Complex | 0..1 | |
| └─ Ufficio | REA office | xs:string | 1..1 | 2 chars (province) |
| └─ NumeroREA | REA number | xs:string | 1..1 | 1-20 chars |
| └─ CapitaleSociale | Share capital | xs:decimal | 0..1 | 2 decimals |
| └─ SocioUnico | Sole shareholder | xs:string | 0..1 | Enum: SU (sole), SM (multiple) |
| └─ StatoLiquidazione | Liquidation status | xs:string | 1..1 | Enum: LS (in liquidation), LN (not) |
| **Contatti** | Contacts | Complex | 0..1 | |
| └─ Telefono | Phone | xs:string | 0..1 | 5-12 chars |
| └─ Fax | Fax | xs:string | 0..1 | 5-12 chars |
| └─ Email | Email | xs:string | 0..1 | 7-256 chars |
| **RiferimentoAmministrazione** | Admin reference | xs:string | 0..1 | 1-20 chars |

### 3.3 RappresentanteFiscale / Tax Representative (Optional)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **DatiAnagrafici** | Identification data | Complex | 1..1 | |
| └─ IdFiscaleIVA | VAT number | Complex | 1..1 | Same as CedentePrestatore |
| └─ CodiceFiscale | Fiscal code | xs:string | 0..1 | 11-16 chars |
| └─ Anagrafica | Name/Denomination | Complex | 1..1 | Same as CedentePrestatore |

### 3.4 CessionarioCommittente / Buyer-Client

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **DatiAnagrafici** | Identification data | Complex | 1..1 | |
| └─ IdFiscaleIVA | VAT number | Complex | 0..1 | Required for B2B with VAT number |
| └─── IdPaese | Country code | xs:string | 1..1 | ISO 3166-1 alpha-2 |
| └─── IdCodice | VAT number | xs:string | 1..1 | 1-28 chars |
| └─ CodiceFiscale | Fiscal code | xs:string | 0..1 | 11-16 chars, required for individuals |
| └─ Anagrafica | Name/Denomination | Complex | 1..1 | Same as CedentePrestatore |
| **Sede** | Address | Complex | 1..1 | Same structure as CedentePrestatore.Sede |
| **StabileOrganizzazione** | Permanent establishment | Complex | 0..1 | Same structure as Sede |
| **RappresentanteFiscale** | Tax representative | Complex | 0..1 | |
| └─ IdFiscaleIVA | VAT number | Complex | 1..1 | |
| └─ Denominazione/Nome+Cognome | Name | xs:string | 0..1 | |

### 3.5 TerzoIntermediarioOSoggettoEmittente / Third Party Intermediary (Optional)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **DatiAnagrafici** | Identification data | Complex | 1..1 | Same structure as RappresentanteFiscale |

### 3.6 SoggettoEmittente / Invoice Issuer (Optional)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| SoggettoEmittente | Who issued the invoice | xs:string | 0..1 | Enum: CC (buyer issued), TZ (third party issued) |

---

## 4. FatturaElettronicaBody

### 4.1 DatiGenerali / General Data

#### 4.1.1 DatiGeneraliDocumento / General Document Data

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **TipoDocumento** | Document type | xs:string | 1..1 | Enum: TD01-TD29 |
| **Divisa** | Currency | xs:string | 1..1 | ISO 4217 (3 chars), e.g., "EUR" |
| **Data** | Invoice date | xs:date | 1..1 | YYYY-MM-DD, >= 1970-01-01 |
| **Numero** | Invoice number | xs:string | 1..1 | 1-20 chars, alphanumeric |
| **DatiRitenuta** | Withholding tax | Complex | 0..N | |
| └─ TipoRitenuta | Withholding type | xs:string | 1..1 | Enum: RT01-RT06 |
| └─ ImportoRitenuta | Withholding amount | xs:decimal | 1..1 | 2 decimals |
| └─ AliquotaRitenuta | Withholding rate | xs:decimal | 1..1 | 2 decimals (0.00-100.00) |
| └─ CausalePagamento | Payment reason | xs:string | 0..1 | 1-2 chars (A-Z codes) |
| **DatiBollo** | Stamp duty | Complex | 0..1 | |
| └─ BolloVirtuale | Virtual stamp | xs:string | 1..1 | Only "SI" allowed |
| └─ ImportoBollo | Stamp amount | xs:decimal | 0..1 | 2 decimals |
| **DatiCassaPrevidenziale** | Social security fund | Complex | 0..N | |
| └─ TipoCassa | Fund type | xs:string | 1..1 | Enum: TC01-TC22 |
| └─ AlCassa | Fund rate | xs:decimal | 1..1 | 2 decimals |
| └─ ImportoContributoCassa | Contribution amount | xs:decimal | 1..1 | 2 decimals |
| └─ ImponibileCassa | Taxable base | xs:decimal | 0..1 | 2 decimals |
| └─ AliquotaIVA | VAT rate | xs:decimal | 1..1 | 2 decimals (0.00-100.00) |
| └─ Ritenuta | Subject to withholding | xs:string | 0..1 | Only "SI" allowed |
| └─ Natura | VAT nature | xs:string | 0..1 | Enum: N1-N7.x (if AliquotaIVA = 0) |
| └─ RiferimentoAmministrazione | Admin reference | xs:string | 0..1 | 1-20 chars |
| **ScontoMaggiorazione** | Discount/Markup | Complex | 0..N | |
| └─ Tipo | Type | xs:string | 1..1 | Enum: SC (discount), MG (markup) |
| └─ Percentuale | Percentage | xs:decimal | 0..1 | 2 decimals |
| └─ Importo | Amount | xs:decimal | 0..1 | 2 decimals |
| **ImportoTotaleDocumento** | Total document amount | xs:decimal | 0..1 | 2 decimals |
| **Arrotondamento** | Rounding | xs:decimal | 0..1 | 2 decimals |
| **Causale** | Description/Reason | xs:string | 0..N | 1-200 chars each |
| **Art73** | Art. 73 application | xs:string | 0..1 | Only "SI" allowed |

#### 4.1.2 DatiOrdineAcquisto / Purchase Order Data (0..N)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| RiferimentoNumeroLinea | Line number reference | xs:integer | 0..N | 1-9999 |
| IdDocumento | Document ID | xs:string | 1..1 | 1-20 chars |
| Data | Document date | xs:date | 0..1 | YYYY-MM-DD |
| NumItem | Item number | xs:string | 0..1 | 1-20 chars |
| CodiceCommessaConvenzione | Order/Agreement code | xs:string | 0..1 | 1-100 chars |
| CodiceCUP | CUP code | xs:string | 0..1 | 1-15 chars |
| CodiceCIG | CIG code | xs:string | 0..1 | 1-15 chars |

**Note**: Same structure applies to: DatiContratto, DatiConvenzione, DatiRicezione, DatiFattureCollegate

#### 4.1.3 DatiSAL / Work Progress Data (0..N)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| RiferimentoFase | Phase reference | xs:integer | 1..1 | 1-999 |

#### 4.1.4 DatiDDT / Delivery Note Data (0..N)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| NumeroDDT | DDT number | xs:string | 1..1 | 1-20 chars |
| DataDDT | DDT date | xs:date | 1..1 | YYYY-MM-DD |
| RiferimentoNumeroLinea | Line number reference | xs:integer | 0..N | 1-9999 |

#### 4.1.5 DatiTrasporto / Transport Data (0..1)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| DatiAnagraficiVettore | Carrier ID | Complex | 0..1 | Same as RappresentanteFiscale |
| MezzoTrasporto | Transport means | xs:string | 0..1 | 1-80 chars |
| CausaleTrasporto | Transport reason | xs:string | 0..1 | 1-100 chars |
| NumeroColli | Number of packages | xs:integer | 0..1 | 1-9999 |
| Descrizione | Description | xs:string | 0..1 | 1-100 chars |
| UnitaMisuraPeso | Weight unit | xs:string | 0..1 | 1-10 chars |
| PesoLordo | Gross weight | xs:decimal | 0..1 | 4 decimals |
| PesoNetto | Net weight | xs:decimal | 0..1 | 4 decimals |
| DataOraRitiro | Pickup datetime | xs:dateTime | 0..1 | ISO 8601 |
| DataInizioTrasporto | Transport start date | xs:date | 0..1 | YYYY-MM-DD |
| TipoResa | Delivery terms | xs:string | 0..1 | 3 chars (Incoterms) |
| IndirizzoResa | Delivery address | Complex | 0..1 | Same as Sede |
| DataOraConsegna | Delivery datetime | xs:dateTime | 0..1 | ISO 8601 |

#### 4.1.6 FatturaPrincipale / Main Invoice Reference (0..1)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| NumeroFatturaPrincipale | Main invoice number | xs:string | 1..1 | 1-20 chars |
| DataFatturaPrincipale | Main invoice date | xs:date | 1..1 | YYYY-MM-DD |

### 4.2 DatiBeniServizi / Goods and Services Data

#### 4.2.1 DettaglioLinee / Line Items (1..N)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **NumeroLinea** | Line number | xs:integer | 1..1 | 1-9999, sequential |
| **TipoCessionePrestazione** | Line type | xs:string | 0..1 | Enum: SC, PR, AB, AC |
| **CodiceArticolo** | Product codes | Complex | 0..N | |
| └─ CodiceTipo | Code type | xs:string | 1..1 | 1-35 chars (TARIC, CPV, EAN, SSC, etc.) |
| └─ CodiceValore | Code value | xs:string | 1..1 | 1-35 chars |
| **Descrizione** | Description | xs:string | 1..1 | 1-1000 chars (Latin-1) |
| **Quantita** | Quantity | xs:decimal | 0..1 | 2-8 decimals |
| **UnitaMisura** | Unit of measure | xs:string | 0..1 | 1-10 chars |
| **DataInizioPeriodo** | Period start date | xs:date | 0..1 | YYYY-MM-DD |
| **DataFinePeriodo** | Period end date | xs:date | 0..1 | YYYY-MM-DD |
| **PrezzoUnitario** | Unit price | xs:decimal | 1..1 | 2-8 decimals |
| **ScontoMaggiorazione** | Line discount/markup | Complex | 0..N | Same as document-level |
| **PrezzoTotale** | Total price | xs:decimal | 1..1 | 2-8 decimals |
| **AliquotaIVA** | VAT rate | xs:decimal | 1..1 | 2 decimals (0.00-100.00) |
| **Ritenuta** | Subject to withholding | xs:string | 0..1 | Only "SI" allowed |
| **Natura** | VAT nature | xs:string | 0..1 | Enum: N1-N7.x (required if AliquotaIVA = 0) |
| **RiferimentoAmministrazione** | Admin reference | xs:string | 0..1 | 1-20 chars |
| **AltriDatiGestionali** | Other data | Complex | 0..N | |
| └─ TipoDato | Data type | xs:string | 1..1 | 1-10 chars |
| └─ RiferimentoTesto | Text reference | xs:string | 0..1 | 1-60 chars |
| └─ RiferimentoNumero | Number reference | xs:decimal | 0..1 | 2-8 decimals |
| └─ RiferimentoData | Date reference | xs:date | 0..1 | YYYY-MM-DD |

**TipoCessionePrestazione Values:**

| Code | IT Description | EN Description |
|------|----------------|----------------|
| SC | Sconto | Discount |
| PR | Premio | Bonus/Prize |
| AB | Abbuono | Allowance |
| AC | Spesa accessoria | Accessory expense |

#### 4.2.2 DatiRiepilogo / VAT Summary (1..N)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **AliquotaIVA** | VAT rate | xs:decimal | 1..1 | 2 decimals (0.00-100.00) |
| **Natura** | VAT nature | xs:string | 0..1 | Enum: N1-N7.x (required if AliquotaIVA = 0) |
| **SpeseAccessorie** | Accessory expenses | xs:decimal | 0..1 | 2 decimals |
| **Arrotondamento** | Rounding | xs:decimal | 0..1 | 2-8 decimals |
| **ImponibileImporto** | Taxable amount | xs:decimal | 1..1 | 2 decimals |
| **Imposta** | VAT amount | xs:decimal | 1..1 | 2 decimals |
| **EsigibilitaIVA** | VAT exigibility | xs:string | 0..1 | Enum: I, D, S |
| **RiferimentoNormativo** | Normative reference | xs:string | 0..1 | 1-100 chars |

**EsigibilitaIVA Values:**

| Code | IT Description | EN Description |
|------|----------------|----------------|
| I | Esigibilità immediata | Immediate exigibility |
| D | Esigibilità differita | Deferred exigibility |
| S | Scissione dei pagamenti | Split payment (PA only) |

### 4.3 DatiVeicoli / Vehicle Data (0..1)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| Data | First registration date | xs:date | 1..1 | YYYY-MM-DD |
| TotalePercorso | Total kilometers | xs:string | 1..1 | 1-15 chars |

### 4.4 DatiPagamento / Payment Data (0..N)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| **CondizioniPagamento** | Payment conditions | xs:string | 1..1 | Enum: TP01, TP02, TP03 |
| **DettaglioPagamento** | Payment details | Complex | 1..N | |
| └─ Beneficiario | Beneficiary | xs:string | 0..1 | 1-200 chars |
| └─ ModalitaPagamento | Payment method | xs:string | 1..1 | Enum: MP01-MP23 |
| └─ DataRiferimentoTerminiPagamento | Terms reference date | xs:date | 0..1 | YYYY-MM-DD |
| └─ GiorniTerminiPagamento | Payment term days | xs:integer | 0..1 | 1-999 |
| └─ DataScadenzaPagamento | Due date | xs:date | 0..1 | YYYY-MM-DD |
| └─ ImportoPagamento | Payment amount | xs:decimal | 1..1 | 2 decimals |
| └─ CodUfficioPostale | Post office code | xs:string | 0..1 | 1-20 chars |
| └─ CognomeQuietanzante | Signatory surname | xs:string | 0..1 | 1-60 chars |
| └─ NomeQuietanzante | Signatory first name | xs:string | 0..1 | 1-60 chars |
| └─ CFQuietanzante | Signatory fiscal code | xs:string | 0..1 | 16 chars |
| └─ TitoloQuietanzante | Signatory title | xs:string | 0..1 | 2-10 chars |
| └─ IstitutoFinanziario | Financial institution | xs:string | 0..1 | 1-80 chars |
| └─ IBAN | IBAN code | xs:string | 0..1 | 15-34 chars |
| └─ ABI | ABI code | xs:string | 0..1 | 5 chars |
| └─ CAB | CAB code | xs:string | 0..1 | 5 chars |
| └─ BIC | BIC/SWIFT code | xs:string | 0..1 | 8-11 chars |
| └─ ScontoPagamentoAnticipato | Early payment discount | xs:decimal | 0..1 | 2 decimals |
| └─ DataLimitePagamentoAnticipato | Early payment deadline | xs:date | 0..1 | YYYY-MM-DD |
| └─ PenalitaPagamentiRitardati | Late payment penalty | xs:decimal | 0..1 | 2 decimals |
| └─ DataDecorrenzaPenale | Penalty start date | xs:date | 0..1 | YYYY-MM-DD |
| └─ CodicePagamento | Payment code | xs:string | 0..1 | 1-60 chars |

### 4.5 Allegati / Attachments (0..N)

| Element (IT) | Description (EN) | Type | Card. | Constraints |
|--------------|------------------|------|-------|-------------|
| NomeAttachment | Attachment name | xs:string | 1..1 | 1-60 chars |
| AlgoritmoCompressione | Compression algorithm | xs:string | 0..1 | 1-10 chars |
| FormatoAttachment | Attachment format | xs:string | 0..1 | 1-10 chars |
| DescrizioneAttachment | Description | xs:string | 0..1 | 1-100 chars |
| Attachment | Base64 content | xs:base64Binary | 1..1 | |

---

## 5. Enumerations / Codifiche

### 5.1 TipoDocumento / Document Types (TD01-TD29)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| **TD01** | Fattura | Invoice |
| **TD02** | Acconto/Anticipo su fattura | Advance payment on invoice |
| **TD03** | Acconto/Anticipo su parcella | Advance payment on fee note |
| **TD04** | Nota di Credito | Credit note |
| **TD05** | Nota di Debito | Debit note |
| **TD06** | Parcella | Professional fee note |
| **TD07** | Fattura semplificata | Simplified invoice |
| **TD08** | Nota di credito semplificata | Simplified credit note |
| **TD09** | Nota di debito semplificata | Simplified debit note |
| **TD10** | Fattura per acquisto intracomunitario beni | Invoice for intra-EU goods purchase |
| **TD11** | Fattura per acquisto intracomunitario servizi | Invoice for intra-EU services purchase |
| **TD12** | Documento di integrazione e riepilogo | Integration and summary document |
| **TD16** | Integrazione fattura reverse charge interno | Internal reverse charge integration |
| **TD17** | Integrazione/autofattura per acquisto servizi dall'estero | Self-invoice for foreign services purchase |
| **TD18** | Integrazione per acquisto beni intracomunitari | Integration for intra-EU goods purchase |
| **TD19** | Integrazione/autofattura per acquisto di beni ex art.17 c.2 DPR 633/72 | Self-invoice per art.17.2 |
| **TD20** | Autofattura per regolarizzazione e integrazione delle fatture (art. 6 c.9-bis d.lgs. 471/97 o art. 46 c.5 d.l. 331/93) | Self-invoice for regularization (reverse charge, intra-EU) |
| **TD21** | Autofattura per splafonamento | Self-invoice for ceiling breach |
| **TD22** | Estrazione beni da Deposito IVA | Extraction from VAT warehouse |
| **TD23** | Estrazione beni da Deposito IVA con versamento dell'IVA | Extraction from VAT warehouse with VAT payment |
| **TD24** | Fattura differita di cui all'art. 21, comma 4, lett. a) | Deferred invoice (art. 21.4.a) |
| **TD25** | Fattura differita di cui all'art. 21, comma 4, terzo periodo lett. b) | Deferred invoice (art. 21.4.b) |
| **TD26** | Cessione di beni ammortizzabili e per passaggi interni | Sale of depreciable assets / internal transfers |
| **TD27** | Fattura per autoconsumo o per cessioni gratuite senza rivalsa | Self-consumption / free transfers without recourse |
| **TD28** | Acquisti da San Marino con IVA (art. 7 c.1 ter DL 75/2023) | Purchases from San Marino with VAT |
| **TD29** | **(NEW v1.9)** Comunicazione omessa o irregolare fatturazione | Communication of missing/irregular invoicing |

**TD29 Special Rules (from April 1, 2025):**
- Purpose: Report to Agenzia delle Entrate when seller failed to issue invoice or issued irregular invoice
- CedentePrestatore.IdFiscaleIVA.IdPaese must be "IT"
- CedentePrestatore must have P.IVA (not only CodiceFiscale)
- CedentePrestatore must be different from CessionarioCommittente
- Error codes: 00471, 00473, 00475

### 5.2 RegimeFiscale / Fiscal Regimes (RF01-RF20)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| **RF01** | Regime ordinario | Standard regime |
| **RF02** | Contribuenti minimi (art.1, c.96-117, L. 244/07) | Minimum taxpayers |
| **RF04** | Agricoltura e attività connesse e pesca (artt.34 e 34-bis, DPR 633/72) | Agriculture and fishing |
| **RF05** | Vendita sali e tabacchi (art.74, c.1, DPR 633/72) | Sale of salt and tobacco |
| **RF06** | Commercio fiammiferi (art.74, c.1, DPR 633/72) | Match trade |
| **RF07** | Editoria (art.74, c.1, DPR 633/72) | Publishing |
| **RF08** | Gestione servizi telefonia pubblica (art.74, c.1, DPR 633/72) | Public telephony services |
| **RF09** | Rivendita documenti di trasporto pubblico e di sosta (art.74, c.1, DPR 633/72) | Resale of transport documents |
| **RF10** | Intrattenimenti, giochi e altre attività (art.74, c.6, DPR 633/72) | Entertainment and gaming |
| **RF11** | Agenzie viaggi e turismo (art.74-ter, DPR 633/72) | Travel agencies |
| **RF12** | Agriturismo (art.5, c.2, L. 413/91) | Agritourism |
| **RF13** | Vendite a domicilio (art.25-bis, c.6, DPR 600/73) | Door-to-door sales |
| **RF14** | Rivendita beni usati, oggetti d'arte, d'antiquariato o da collezione (art.36, DL 41/95) | Resale of used goods, art, antiques |
| **RF15** | Agenzie di vendite all'asta di oggetti d'arte, antiquariato o da collezione (art.40-bis, DL 41/95) | Auction agencies |
| **RF16** | IVA per cassa P.A. (art.6, c.5, DPR 633/72) | Cash accounting VAT for PA |
| **RF17** | IVA per cassa (art. 32-bis, DL 83/2012) | Cash accounting VAT |
| **RF18** | Altro | Other |
| **RF19** | Regime forfettario (art.1, c.54-89, L. 190/2014) | Flat-rate regime |
| **RF20** | **(NEW v1.9)** Regime transfrontaliero di Franchigia IVA (Direttiva UE 2020/285) | Cross-border VAT exemption regime |

**RF20 Notes (effective January 1, 2025):**
- Implements EU Directive 2020/285 for small business VAT exemption across borders
- Allows simplified invoices without the €400 limit (same as RF19)

### 5.3 Natura / VAT Nature Codes (N1-N7)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| **N1** | Escluse ex art.15 del DPR 633/72 | Excluded per art.15 |
| **N2** | _(obsolete, use N2.1 or N2.2)_ | _(obsolete)_ |
| **N2.1** | Non soggette ad IVA ai sensi degli artt. da 7 a 7-septies del DPR 633/72 | Not subject to VAT (territorial) |
| **N2.2** | Non soggette - altri casi | Not subject - other cases |
| **N3** | _(obsolete, use N3.1-N3.6)_ | _(obsolete)_ |
| **N3.1** | Non imponibili - esportazioni | Non-taxable - exports |
| **N3.2** | Non imponibili - cessioni intracomunitarie | Non-taxable - intra-EU supplies |
| **N3.3** | Non imponibili - cessioni verso San Marino | Non-taxable - supplies to San Marino |
| **N3.4** | Non imponibili - operazioni assimilate alle cessioni all'esportazione | Non-taxable - export-like operations |
| **N3.5** | Non imponibili - a seguito di dichiarazioni d'intento | Non-taxable - intent declarations |
| **N3.6** | Non imponibili - altre operazioni che non concorrono alla formazione del plafond | Non-taxable - other (no ceiling) |
| **N4** | Esenti | Exempt |
| **N5** | Regime del margine / IVA non esposta in fattura | Margin scheme / VAT not shown |
| **N6** | _(obsolete, use N6.1-N6.9)_ | _(obsolete)_ |
| **N6.1** | Inversione contabile - cessione di rottami e altri materiali di recupero | Reverse charge - scrap metal |
| **N6.2** | Inversione contabile - cessione di oro e argento ai sensi della legge 7/2000 nonché di oreficeria usata ad OPO | Reverse charge - gold/silver |
| **N6.3** | Inversione contabile - subappalto nel settore edile | Reverse charge - construction subcontracting |
| **N6.4** | Inversione contabile - cessione di fabbricati | Reverse charge - building sales |
| **N6.5** | Inversione contabile - cessione di telefoni cellulari | Reverse charge - mobile phones |
| **N6.6** | Inversione contabile - cessione di prodotti elettronici | Reverse charge - electronics |
| **N6.7** | Inversione contabile - prestazioni comparto edile e settori connessi | Reverse charge - construction services |
| **N6.8** | Inversione contabile - operazioni settore energetico | Reverse charge - energy sector |
| **N6.9** | Inversione contabile - altri casi | Reverse charge - other cases |
| **N7** | IVA assolta in altro stato UE | VAT paid in another EU state |

### 5.4 ModalitaPagamento / Payment Methods (MP01-MP23)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| **MP01** | Contanti | Cash |
| **MP02** | Assegno | Check |
| **MP03** | Assegno circolare | Bank check |
| **MP04** | Contanti presso Tesoreria | Cash at Treasury |
| **MP05** | Bonifico | Bank transfer |
| **MP06** | Vaglia cambiario | Promissory note |
| **MP07** | Bollettino bancario | Bank slip |
| **MP08** | Carta di pagamento | Payment card |
| **MP09** | RID | Direct debit (RID) |
| **MP10** | RID utenze | Utilities direct debit |
| **MP11** | RID veloce | Fast direct debit |
| **MP12** | RIBA | Bank receipt (RIBA) |
| **MP13** | MAV | MAV payment slip |
| **MP14** | Quietanza erario | Treasury receipt |
| **MP15** | Giroconto su conti di contabilità speciale | Special account transfer |
| **MP16** | Domiciliazione bancaria | Bank domiciliation |
| **MP17** | Domiciliazione postale | Postal domiciliation |
| **MP18** | Bollettino di c/c postale | Postal current account slip |
| **MP19** | SEPA Direct Debit | SEPA Direct Debit |
| **MP20** | SEPA Direct Debit CORE | SEPA DD CORE |
| **MP21** | SEPA Direct Debit B2B | SEPA DD B2B |
| **MP22** | Trattenuta su somme già riscosse | Withholding on collected amounts |
| **MP23** | PagoPA | PagoPA |

### 5.5 CondizioniPagamento / Payment Conditions (TP01-TP03)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| **TP01** | Pagamento a rate | Installment payment |
| **TP02** | Pagamento completo | Full payment |
| **TP03** | Anticipo | Advance payment |

### 5.6 TipoRitenuta / Withholding Tax Types (RT01-RT06)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| **RT01** | Ritenuta persone fisiche | Withholding for individuals |
| **RT02** | Ritenuta persone giuridiche | Withholding for legal entities |
| **RT03** | Contributo INPS | INPS contribution |
| **RT04** | Contributo ENASARCO | ENASARCO contribution |
| **RT05** | Contributo ENPAM | ENPAM contribution |
| **RT06** | Altro contributo previdenziale | Other social security contribution |

### 5.7 TipoCassa / Social Security Fund Types (TC01-TC22)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| **TC01** | Cassa nazionale previdenza e assistenza avvocati e procuratori legali | Lawyers fund |
| **TC02** | Cassa previdenza dottori commercialisti | Accountants fund |
| **TC03** | Cassa previdenza e assistenza geometri | Surveyors fund |
| **TC04** | Cassa nazionale previdenza e assistenza ingegneri e architetti liberi professionisti | Engineers/Architects fund |
| **TC05** | Cassa nazionale del notariato | Notaries fund |
| **TC06** | Cassa nazionale previdenza e assistenza ragionieri e periti commerciali | Bookkeepers fund |
| **TC07** | Ente nazionale assistenza agenti e rappresentanti di commercio (ENASARCO) | Sales agents fund |
| **TC08** | Ente nazionale previdenza e assistenza consulenti del lavoro (ENPACL) | Labor consultants fund |
| **TC09** | Ente nazionale previdenza e assistenza medici (ENPAM) | Doctors fund |
| **TC10** | Ente nazionale previdenza e assistenza farmacisti (ENPAF) | Pharmacists fund |
| **TC11** | Ente nazionale previdenza e assistenza veterinari (ENPAV) | Veterinarians fund |
| **TC12** | Ente nazionale previdenza e assistenza impiegati dell'agricoltura (ENPAIA) | Agricultural employees fund |
| **TC13** | Fondo previdenza impiegati imprese di spedizione e agenzie marittime | Shipping agents fund |
| **TC14** | Istituto nazionale previdenza giornalisti italiani (INPGI) | Journalists fund |
| **TC15** | Opera nazionale assistenza orfani sanitari italiani (ONAOSI) | Healthcare orphans fund |
| **TC16** | Cassa autonoma assistenza integrativa giornalisti italiani (CASAGIT) | Journalists supplementary fund |
| **TC17** | Ente previdenza periti industriali e periti industriali laureati (EPPI) | Industrial technicians fund |
| **TC18** | Ente previdenza e assistenza pluricategoriale (EPAP) | Multi-category fund |
| **TC19** | Ente nazionale previdenza e assistenza biologi (ENPAB) | Biologists fund |
| **TC20** | Ente nazionale previdenza e assistenza professione infermieristica (ENPAPI) | Nurses fund |
| **TC21** | Ente nazionale previdenza e assistenza psicologi (ENPAP) | Psychologists fund |
| **TC22** | INPS | INPS |

---

## 6. Validation Rules / Regole di Validazione

### 6.1 Fiscal Identifier Formats

| Field | Format | Pattern | Example |
|-------|--------|---------|---------|
| P.IVA (IT) | 11 numeric digits | `[0-9]{11}` | 01234567890 |
| Codice Fiscale (individual) | 16 alphanumeric | `[A-Z]{6}[0-9]{2}[A-Z][0-9]{2}[A-Z][0-9]{3}[A-Z]` | RSSMRA80A01H501U |
| Codice Fiscale (company) | 11 numeric digits | `[0-9]{11}` | 01234567890 |
| CodiceDestinatario (PA) | 6 alphanumeric | `[A-Z0-9]{6}` | UFXXXX |
| CodiceDestinatario (B2B) | 7 alphanumeric | `[A-Z0-9]{7}` | JKKZDGR |
| IBAN (IT) | 27 characters | `IT[0-9]{2}[A-Z][0-9]{10}[A-Z0-9]{12}` | IT60X0542811101000000123456 |
| BIC/SWIFT | 8-11 characters | `[A-Z]{6}[A-Z0-9]{2}([A-Z0-9]{3})?` | BCITITMM |
| CAP (IT) | 5 digits | `[0-9]{5}` | 00100 |
| Provincia (IT) | 2 uppercase | `[A-Z]{2}` | RM |

### 6.2 Amount Precision Rules

| Field Type | Decimal Places | Example |
|------------|----------------|---------|
| Amounts (ImportoTotaleDocumento, Imposta, etc.) | 2 | 1234.56 |
| Unit prices (PrezzoUnitario) | 2-8 | 10.12345678 |
| Quantities (Quantita) | 2-8 | 100.50000000 |
| VAT rates (AliquotaIVA) | 2 | 22.00 |
| Total prices (PrezzoTotale) | 2-8 | 1234.56 |

### 6.3 Conditional Requirements

| Condition | Required Field | Notes |
|-----------|----------------|-------|
| AliquotaIVA = 0.00 | Natura | Must specify N1-N7.x code |
| CodiceDestinatario = "0000000" | PECDestinatario | PEC email required for delivery |
| FormatoTrasmissione = "FPA12" | CodiceDestinatario | Must be exactly 6 characters |
| FormatoTrasmissione = "FPR12" | CodiceDestinatario | Must be exactly 7 characters |
| Anagrafica for company | Denominazione | Cannot use Nome/Cognome |
| Anagrafica for individual | Nome + Cognome | Cannot use Denominazione |
| TipoDocumento = "TD29" | CedentePrestatore.IdPaese | Must be "IT" |
| IscrizioneREA present | Ufficio, NumeroREA, StatoLiquidazione | All required (see note below) |
| DatiPagamento present | CondizioniPagamento + DettaglioPagamento | Both required |

**IscrizioneREA All-or-Nothing Rule:**
Per Article 2250 of the Italian Civil Code, companies registered in the Business Registry (Registro delle Imprese) must provide complete REA data. The XML builder enforces an all-or-nothing policy:
- If ALL required fields (Ufficio, NumeroREA, StatoLiquidazione) are present → include `<IscrizioneREA>` element
- If ANY required field is missing → omit `<IscrizioneREA>` element entirely

This prevents the SDI warning: "Non sono stati specificati i valori nei discendenti di `<IscrizioneREA>`"

### 6.4 Cross-Element Validations

1. **VAT Summary Consistency**:
   - Sum of DettaglioLinee.PrezzoTotale per VAT rate must match DatiRiepilogo.ImponibileImporto
   - DatiRiepilogo.Imposta = ImponibileImporto × (AliquotaIVA / 100)

2. **Line Item Totals**:
   - PrezzoTotale = PrezzoUnitario × Quantita (after discounts/markups)
   - If ScontoMaggiorazione present, apply before calculating PrezzoTotale

3. **Document Total**:
   - ImportoTotaleDocumento ≈ Sum of (ImponibileImporto + Imposta) ± Arrotondamento

4. **Date Constraints**:
   - Data >= 1970-01-01
   - DataDDT <= DatiGeneraliDocumento.Data (DDT before invoice)
   - DataScadenzaPagamento >= DatiGeneraliDocumento.Data (usually)

---

## 7. SDI Error Codes / Codici Errore SDI

### 7.1 File/Format Errors (001xx)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| 00100 | Certificato di firma non valido | Invalid signature certificate |
| 00101 | Certificato di firma scaduto | Expired signature certificate |
| 00102 | Certificato di firma revocato | Revoked signature certificate |
| 00103 | Certificato di firma non è di tipo valido | Invalid certificate type |
| 00104 | File non conforme al formato | File not compliant with format |
| 00105 | Contenuto non conforme al formato | Content not compliant with format |

### 7.2 Transmission Errors (002xx)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| 00200 | File non firmato correttamente | File not correctly signed |
| 00201 | Riferimento temporale non valido | Invalid time reference |
| 00202 | Marca temporale non valida | Invalid timestamp |

### 7.3 Header Errors (003xx-004xx)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| 00300 | IdPaese non valido | Invalid country code |
| 00301 | IdCodice non valido | Invalid ID code |
| 00303 | CodiceDestinatario non valido | Invalid recipient code |
| 00305 | FormatoTrasmissione non valido | Invalid transmission format |
| 00306 | CodiceDestinatario non trovato | Recipient code not found |
| 00311 | CodiceFiscale cedente non valido | Invalid seller fiscal code |
| 00312 | IdFiscaleIVA cedente non valido | Invalid seller VAT number |
| 00313 | CodiceFiscale cessionario non valido | Invalid buyer fiscal code |
| 00320 | RegimeFiscale non valido | Invalid fiscal regime |
| 00324 | Codice CAP non valido | Invalid postal code |

### 7.4 Body/Document Errors (004xx)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| 00400 | Numero fattura già presente | Invoice number already exists |
| 00401 | TipoDocumento non valido | Invalid document type |
| 00403 | Data non valida | Invalid date |
| 00411 | Importo totale documento non valido | Invalid document total |
| 00417 | Aliquota IVA non valida | Invalid VAT rate |
| 00418 | Natura non valida | Invalid VAT nature |
| 00419 | Dettaglio linea non valido | Invalid line detail |
| 00420 | DatiRiepilogo incoerente | Inconsistent VAT summary |
| 00421 | Imposta calcolata non corretta | Incorrect calculated VAT |
| 00422 | Totali non coerenti | Inconsistent totals |
| 00423 | AliquotaIVA = 0 senza Natura | VAT rate 0 without Nature code |
| 00424 | AliquotaIVA != 0 con Natura | VAT rate not 0 with Nature code |
| 00425 | Natura non consentita | Nature code not allowed |
| 00427 | Riepilogo IVA assente | VAT summary missing |
| 00428 | Importo pagamento non valido | Invalid payment amount |
| 00429 | ModalitaPagamento non valida | Invalid payment method |
| 00430 | CondizioniPagamento non valido | Invalid payment conditions |

### 7.5 TD29 Specific Errors (New in v1.9)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| 00471 | TD29: Cedente uguale al cessionario | TD29: Seller same as buyer |
| 00473 | TD29: IdPaese cedente diverso da IT | TD29: Seller country not IT |
| 00475 | TD29: Partita IVA cedente assente | TD29: Seller VAT number missing |

### 7.6 Delivery Errors (005xx)

| Code | IT Description | EN Description |
|------|----------------|----------------|
| 00500 | Destinatario non raggiungibile | Recipient unreachable |
| 00501 | Canale non attivo | Channel not active |
| 00502 | Casella PEC piena | PEC mailbox full |
| 00503 | Timeout consegna | Delivery timeout |

---

## 8. Official Sources / Fonti Ufficiali

### Primary Documentation

| Resource | URL | Language |
|----------|-----|----------|
| **XSD Schema v1.2.3 (FPR12)** | https://www.fatturapa.gov.it/export/documenti/fatturapa/v1.4/Schema_VFPR12_v1.2.3.xsd | XML |
| **XSD Schema v1.2.3 (FPA12)** | https://www.fatturapa.gov.it/export/documenti/fatturapa/v1.4/Schema_VFPA12_V1.2.3.xsd | XML |
| **Technical Specs PDF v1.3.2** | https://www.fatturapa.gov.it/export/documenti/Specifiche_tecniche_del_formato_FatturaPA_V1.3.2.pdf | IT |
| **Tabular Reference (Excel)** | https://www.fatturapa.gov.it/export/documenti/fatturapa/v1.4/RappresentazioneTabellareFattOrdinaria.xlsx | IT |
| **Specs v1.9 (Agenzia Entrate)** | https://www.agenziaentrate.gov.it/portale/specifiche-tecniche-versione-1.9 | IT |

### Example Invoices

| Example | Description |
|---------|-------------|
| Single invoice to PA | One line item, FPA12 format |
| Single invoice to private | Multiple line items, FPR12 format |
| Batch invoice | Multiple bodies in single file |

Download from: https://www.fatturapa.gov.it/en/lafatturapa/esempi/

### OpenAPI SDI Integration

| Resource | URL |
|----------|-----|
| OpenAPI SDI Spec | https://console.openapi.com/oas/it/sdi.openapi.json |
| Documentation | https://www.openapi.it/documentazione-sdi/ |
| Sandbox | https://test.sdi.openapi.it |
| Production | https://sdi.openapi.it |

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.2.3 | 2025-04-01 | Added TD29, RF20; simplified invoice limit removal for RF19/RF20 |
| 1.2.2 | 2022-10-01 | Previous stable version |
| 1.2.1 | 2020-10-01 | Added N2.x, N3.x, N6.x subcodes |
| 1.2 | 2017-01-01 | Initial FPR12 format |

---

_Last updated: January 2025_
_Based on: Specifiche Tecniche v1.9, Schema v1.2.3_
