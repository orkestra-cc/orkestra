package models

import (
	"encoding/xml"
)

// FatturaElettronica represents the root element of the Italian electronic invoice
type FatturaElettronica struct {
	XMLName                    xml.Name                   `xml:"http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2 FatturaElettronica"`
	XmlnsXsi                   string                     `xml:"xmlns:xsi,attr,omitempty"`
	SchemaLocation             string                     `xml:"xsi:schemaLocation,attr,omitempty"`
	Versione                   TransmissionFormat         `xml:"versione,attr"`
	SistemaEmittente           string                     `xml:"SistemaEmittente,attr,omitempty"` // Max 10 chars, identifies the issuing system
	FatturaElettronicaHeader   FatturaElettronicaHeader   `xml:"FatturaElettronicaHeader"`
	FatturaElettronicaBody     []FatturaElettronicaBody   `xml:"FatturaElettronicaBody"`
}

// FatturaElettronicaHeader represents the header section of the invoice
type FatturaElettronicaHeader struct {
	Xmlns                  string                 `xml:"xmlns,attr"` // SDI requires xmlns="" on child elements
	DatiTrasmissione       DatiTrasmissione       `xml:"DatiTrasmissione"`
	CedentePrestatore      CedentePrestatore      `xml:"CedentePrestatore"`
	RappresentanteFiscale  *RappresentanteFiscale `xml:"RappresentanteFiscale,omitempty"`
	CessionarioCommittente CessionarioCommittente `xml:"CessionarioCommittente"`
	TerzoIntermediario     *TerzoIntermediario    `xml:"TerzoIntermediarioOSoggettoEmittente,omitempty"`
	SoggettoEmittente      string                 `xml:"SoggettoEmittente,omitempty"` // CC=cessionario/committente, TZ=terzo
}

// DatiTrasmissione represents transmission data
type DatiTrasmissione struct {
	IdTrasmittente     IdFiscale          `xml:"IdTrasmittente"`
	ProgressivoInvio   string             `xml:"ProgressivoInvio"`
	FormatoTrasmissione TransmissionFormat `xml:"FormatoTrasmissione"`
	CodiceDestinatario string             `xml:"CodiceDestinatario"`
	ContattiTrasmit    *ContattiTrasmit   `xml:"ContattiTrasmittente,omitempty"`
	PECDestinatario    string             `xml:"PECDestinatario,omitempty"`
}

// IdFiscale represents a fiscal identifier
type IdFiscale struct {
	IdPaese  string `xml:"IdPaese"`
	IdCodice string `xml:"IdCodice"`
}

// ContattiTrasmit represents transmitter contact information
type ContattiTrasmit struct {
	Telefono string `xml:"Telefono,omitempty"`
	Email    string `xml:"Email,omitempty"`
}

// CedentePrestatore represents the seller/service provider
type CedentePrestatore struct {
	DatiAnagrafici   DatiAnagraficiCedente `xml:"DatiAnagrafici"`
	Sede             Indirizzo             `xml:"Sede"`
	StabileOrganizz  *Indirizzo            `xml:"StabileOrganizzazione,omitempty"`
	IscrizioneREA    *IscrizioneREA        `xml:"IscrizioneREA,omitempty"`
	Contatti         *Contatti             `xml:"Contatti,omitempty"`
	RiferimentoAmm   string                `xml:"RiferimentoAmministrazione,omitempty"`
}

// DatiAnagraficiCedente represents cedente/prestatore identification data
type DatiAnagraficiCedente struct {
	IdFiscaleIVA  IdFiscale     `xml:"IdFiscaleIVA"`
	CodiceFiscale string        `xml:"CodiceFiscale,omitempty"`
	Anagrafica    Anagrafica    `xml:"Anagrafica"`
	AlboProfess   string        `xml:"AlboProfessionale,omitempty"`
	ProvinciaAlbo string        `xml:"ProvinciaAlbo,omitempty"`
	NumIscrizAlbo string        `xml:"NumeroIscrizioneAlbo,omitempty"`
	DataIscrizAlbo string       `xml:"DataIscrizioneAlbo,omitempty"`
	RegimeFiscale RegimeFiscale `xml:"RegimeFiscale"`
}

// Anagrafica represents name/denomination
type Anagrafica struct {
	Denominazione string `xml:"Denominazione,omitempty"`
	Nome          string `xml:"Nome,omitempty"`
	Cognome       string `xml:"Cognome,omitempty"`
	Titolo        string `xml:"Titolo,omitempty"`
	CodEORI       string `xml:"CodEORI,omitempty"`
}

// Indirizzo represents an address
type Indirizzo struct {
	Indirizzo    string `xml:"Indirizzo"`
	NumeroCivico string `xml:"NumeroCivico,omitempty"`
	CAP          string `xml:"CAP"`
	Comune       string `xml:"Comune"`
	Provincia    string `xml:"Provincia,omitempty"`
	Nazione      string `xml:"Nazione"`
}

// IscrizioneREA represents REA registration data
type IscrizioneREA struct {
	Ufficio          string `xml:"Ufficio"`
	NumeroREA        string `xml:"NumeroREA"`
	CapitaleSociale  string `xml:"CapitaleSociale,omitempty"`
	SocioUnico       string `xml:"SocioUnico,omitempty"` // SU=socio unico, SM=più soci
	StatoLiquidazione string `xml:"StatoLiquidazione"`   // LS=in liquidazione, LN=non in liquidazione
}

// Contatti represents contact information
type Contatti struct {
	Telefono string `xml:"Telefono,omitempty"`
	Fax      string `xml:"Fax,omitempty"`
	Email    string `xml:"Email,omitempty"`
}

// RappresentanteFiscale represents a fiscal representative
type RappresentanteFiscale struct {
	DatiAnagrafici DatiAnagraficiRappresentante `xml:"DatiAnagrafici"`
}

// DatiAnagraficiRappresentante represents fiscal representative identification
type DatiAnagraficiRappresentante struct {
	IdFiscaleIVA  IdFiscale  `xml:"IdFiscaleIVA"`
	CodiceFiscale string     `xml:"CodiceFiscale,omitempty"`
	Anagrafica    Anagrafica `xml:"Anagrafica"`
}

// CessionarioCommittente represents the buyer/client
type CessionarioCommittente struct {
	DatiAnagrafici   DatiAnagraficiCessionario `xml:"DatiAnagrafici"`
	Sede             Indirizzo                 `xml:"Sede"`
	StabileOrganizz  *Indirizzo                `xml:"StabileOrganizzazione,omitempty"`
	RappresentanteFisc *RappresentanteFiscale  `xml:"RappresentanteFiscale,omitempty"`
}

// DatiAnagraficiCessionario represents cessionario/committente identification
type DatiAnagraficiCessionario struct {
	IdFiscaleIVA  *IdFiscale `xml:"IdFiscaleIVA,omitempty"`
	CodiceFiscale string     `xml:"CodiceFiscale,omitempty"`
	Anagrafica    Anagrafica `xml:"Anagrafica"`
}

// TerzoIntermediario represents third party intermediary
type TerzoIntermediario struct {
	DatiAnagrafici DatiAnagraficiRappresentante `xml:"DatiAnagrafici"`
}

// FatturaElettronicaBody represents the body section of the invoice
type FatturaElettronicaBody struct {
	Xmlns           string          `xml:"xmlns,attr"` // SDI requires xmlns="" on child elements
	DatiGenerali    DatiGenerali    `xml:"DatiGenerali"`
	DatiBeniServizi DatiBeniServizi `xml:"DatiBeniServizi"`
	DatiVeicoli     *DatiVeicoli    `xml:"DatiVeicoli,omitempty"`
	DatiPagamento   *DatiPagamento  `xml:"DatiPagamento,omitempty"`
	Allegati        []Allegato      `xml:"Allegati,omitempty"`
}

// DatiGenerali represents general document data
type DatiGenerali struct {
	DatiGeneraliDocumento   DatiGeneraliDocumento    `xml:"DatiGeneraliDocumento"`
	DatiOrdineAcquisto      []DatiDocumentoCorrelato `xml:"DatiOrdineAcquisto,omitempty"`
	DatiContratto           []DatiDocumentoCorrelato `xml:"DatiContratto,omitempty"`
	DatiConvenzione         []DatiDocumentoCorrelato `xml:"DatiConvenzione,omitempty"`
	DatiRicezione           []DatiDocumentoCorrelato `xml:"DatiRicezione,omitempty"`
	DatiFattureCollegate    []DatiDocumentoCorrelato `xml:"DatiFattureCollegate,omitempty"`
	DatiSAL                 []DatiSAL                `xml:"DatiSAL,omitempty"`
	DatiDDT                 []DatiDDT                `xml:"DatiDDT,omitempty"`
	DatiTrasporto           *DatiTrasporto           `xml:"DatiTrasporto,omitempty"`
	FatturaPrincipale       *FatturaPrincipale       `xml:"FatturaPrincipale,omitempty"`
}

// DatiGeneraliDocumento represents general document details
type DatiGeneraliDocumento struct {
	TipoDocumento           DocumentType     `xml:"TipoDocumento"`
	Divisa                  string           `xml:"Divisa"`
	Data                    string           `xml:"Data"` // YYYY-MM-DD
	Numero                  string           `xml:"Numero"`
	DatiRitenuta            *DatiRitenuta    `xml:"DatiRitenuta,omitempty"`
	DatiBollo               *DatiBollo       `xml:"DatiBollo,omitempty"`
	DatiCassaPrevidenziale  []DatiCassa      `xml:"DatiCassaPrevidenziale,omitempty"`
	ScontoMaggiorazione     []ScontoMagg     `xml:"ScontoMaggiorazione,omitempty"`
	ImportoTotaleDocumento  string           `xml:"ImportoTotaleDocumento,omitempty"`
	Arrotondamento          string           `xml:"Arrotondamento,omitempty"`
	Causale                 []string         `xml:"Causale,omitempty"`
	Art73                   string           `xml:"Art73,omitempty"` // SI se applicabile
}

// DatiRitenuta represents withholding tax data
type DatiRitenuta struct {
	TipoRitenuta     string `xml:"TipoRitenuta"` // RT01=persone fisiche, RT02=persone giuridiche, RT03=contributo INPS, RT04=contributo ENASARCO, RT05=contributo ENPAM, RT06=altro
	ImportoRitenuta  string `xml:"ImportoRitenuta"`
	AliquotaRitenuta string `xml:"AliquotaRitenuta"`
	CausalePagamento string `xml:"CausalePagamento,omitempty"` // A-Z codici
}

// DatiBollo represents stamp duty data
type DatiBollo struct {
	BolloVirtuale string `xml:"BolloVirtuale"` // SI
	ImportoBollo  string `xml:"ImportoBollo,omitempty"`
}

// DatiCassa represents social security fund data
type DatiCassa struct {
	TipoCassa           string `xml:"TipoCassa"` // TC01-TC22
	AlCassa             string `xml:"AlCassa"`
	ImportoContributoCassa string `xml:"ImportoContributoCassa"`
	ImponibileCassa     string `xml:"ImponibileCassa,omitempty"`
	AliquotaIVA         string `xml:"AliquotaIVA"`
	Ritenuta            string `xml:"Ritenuta,omitempty"` // SI
	Natura              string `xml:"Natura,omitempty"`
	RiferimentoAmm      string `xml:"RiferimentoAmministrazione,omitempty"`
}

// ScontoMagg represents discount/markup
type ScontoMagg struct {
	Tipo        string `xml:"Tipo"` // SC=sconto, MG=maggiorazione
	Percentuale string `xml:"Percentuale,omitempty"`
	Importo     string `xml:"Importo,omitempty"`
}

// DatiDocumentoCorrelato represents related document data
type DatiDocumentoCorrelato struct {
	RiferimentoNumeroLinea []int  `xml:"RiferimentoNumeroLinea,omitempty"`
	IdDocumento            string `xml:"IdDocumento"`
	Data                   string `xml:"Data,omitempty"`
	NumItem                string `xml:"NumItem,omitempty"`
	CodiceCommessaConvenzione string `xml:"CodiceCommessaConvenzione,omitempty"`
	CodiceCUP              string `xml:"CodiceCUP,omitempty"`
	CodiceCIG              string `xml:"CodiceCIG,omitempty"`
}

// DatiSAL represents SAL (stato avanzamento lavori) data
type DatiSAL struct {
	RiferimentoFase int `xml:"RiferimentoFase"`
}

// DatiDDT represents DDT (documento di trasporto) data
type DatiDDT struct {
	NumeroDDT              string `xml:"NumeroDDT"`
	DataDDT                string `xml:"DataDDT"`
	RiferimentoNumeroLinea []int  `xml:"RiferimentoNumeroLinea,omitempty"`
}

// DatiTrasporto represents transport data
type DatiTrasporto struct {
	DatiAnagraficiVettore *DatiAnagraficiRappresentante `xml:"DatiAnagraficiVettore,omitempty"`
	MezzoTrasporto        string                        `xml:"MezzoTrasporto,omitempty"`
	CausaleTrasporto      string                        `xml:"CausaleTrasporto,omitempty"`
	NumeroColli           string                        `xml:"NumeroColli,omitempty"`
	Descrizione           string                        `xml:"Descrizione,omitempty"`
	UnitaMisuraPeso       string                        `xml:"UnitaMisuraPeso,omitempty"`
	PesoLordo             string                        `xml:"PesoLordo,omitempty"`
	PesoNetto             string                        `xml:"PesoNetto,omitempty"`
	DataOraRitiro         string                        `xml:"DataOraRitiro,omitempty"`
	DataInizioTrasporto   string                        `xml:"DataInizioTrasporto,omitempty"`
	TipoResa              string                        `xml:"TipoResa,omitempty"`
	IndirizzoResa         *Indirizzo                    `xml:"IndirizzoResa,omitempty"`
	DataOraConsegna       string                        `xml:"DataOraConsegna,omitempty"`
}

// FatturaPrincipale represents main invoice reference
type FatturaPrincipale struct {
	NumeroFatturaPrincipale string `xml:"NumeroFatturaPrincipale"`
	DataFatturaPrincipale   string `xml:"DataFatturaPrincipale"`
}

// DatiBeniServizi represents goods/services data
type DatiBeniServizi struct {
	DettaglioLinee  []DettaglioLinea `xml:"DettaglioLinee"`
	DatiRiepilogo   []DatiRiepilogo  `xml:"DatiRiepilogo"`
}

// DettaglioLinea represents a single line item
type DettaglioLinea struct {
	NumeroLinea         int          `xml:"NumeroLinea"`
	TipoCessionePrest   string       `xml:"TipoCessionePrestazione,omitempty"` // SC=sconto, PR=premio, AB=abbuono, AC=spesa accessoria
	CodiceArticolo      []CodArticolo `xml:"CodiceArticolo,omitempty"`
	Descrizione         string       `xml:"Descrizione"`
	Quantita            string       `xml:"Quantita,omitempty"`
	UnitaMisura         string       `xml:"UnitaMisura,omitempty"`
	DataInizioPeriodo   string       `xml:"DataInizioPeriodo,omitempty"`
	DataFinePeriodo     string       `xml:"DataFinePeriodo,omitempty"`
	PrezzoUnitario      string       `xml:"PrezzoUnitario"`
	ScontoMaggiorazione []ScontoMagg `xml:"ScontoMaggiorazione,omitempty"`
	PrezzoTotale        string       `xml:"PrezzoTotale"`
	AliquotaIVA         string       `xml:"AliquotaIVA"`
	Ritenuta            string       `xml:"Ritenuta,omitempty"` // SI
	Natura              string       `xml:"Natura,omitempty"`
	RiferimentoAmm      string       `xml:"RiferimentoAmministrazione,omitempty"`
	AltriDatiGestionali []AltriDati  `xml:"AltriDatiGestionali,omitempty"`
}

// CodArticolo represents product code
type CodArticolo struct {
	CodiceTipo   string `xml:"CodiceTipo"`
	CodiceValore string `xml:"CodiceValore"`
}

// AltriDati represents additional management data
type AltriDati struct {
	TipoDato       string `xml:"TipoDato"`
	RiferimentoTesto string `xml:"RiferimentoTesto,omitempty"`
	RiferimentoNumero string `xml:"RiferimentoNumero,omitempty"`
	RiferimentoData string `xml:"RiferimentoData,omitempty"`
}

// DatiRiepilogo represents VAT summary
type DatiRiepilogo struct {
	AliquotaIVA             string `xml:"AliquotaIVA"`
	Natura                  string `xml:"Natura,omitempty"`
	SpeseAccessorie         string `xml:"SpeseAccessorie,omitempty"`
	Arrotondamento          string `xml:"Arrotondamento,omitempty"`
	ImponibileImporto       string `xml:"ImponibileImporto"`
	Imposta                 string `xml:"Imposta"`
	EsigibilitaIVA          string `xml:"EsigibilitaIVA,omitempty"` // I=immediata, D=differita, S=split payment
	RiferimentoNormativo    string `xml:"RiferimentoNormativo,omitempty"`
}

// DatiVeicoli represents vehicle data (for vehicle invoices)
type DatiVeicoli struct {
	Data           string `xml:"Data"`
	TotalePercorso string `xml:"TotalePercorso"`
}

// DatiPagamento represents payment data
type DatiPagamento struct {
	CondizioniPagamento string                `xml:"CondizioniPagamento"` // TP01, TP02, TP03
	DettaglioPagamento  []DettaglioPagamento  `xml:"DettaglioPagamento"`
}

// DettaglioPagamento represents payment details
type DettaglioPagamento struct {
	Beneficiario         string `xml:"Beneficiario,omitempty"`
	ModalitaPagamento    string `xml:"ModalitaPagamento"` // MP01-MP23
	DataRiferimentoTermini string `xml:"DataRiferimentoTerminiPagamento,omitempty"`
	GiorniTerminiPagamento string `xml:"GiorniTerminiPagamento,omitempty"`
	DataScadenzaPagamento string `xml:"DataScadenzaPagamento,omitempty"`
	ImportoPagamento     string `xml:"ImportoPagamento"`
	CodUfficioPostale    string `xml:"CodUfficioPostale,omitempty"`
	CognomeQuietanzante  string `xml:"CognomeQuietanzante,omitempty"`
	NomeQuietanzante     string `xml:"NomeQuietanzante,omitempty"`
	CFQuietanzante       string `xml:"CFQuietanzante,omitempty"`
	TitoloQuietanzante   string `xml:"TitoloQuietanzante,omitempty"`
	IstitutoFinanziario  string `xml:"IstitutoFinanziario,omitempty"`
	IBAN                 string `xml:"IBAN,omitempty"`
	ABI                  string `xml:"ABI,omitempty"`
	CAB                  string `xml:"CAB,omitempty"`
	BIC                  string `xml:"BIC,omitempty"`
	ScontoPagamentoAntic string `xml:"ScontoPagamentoAnticipato,omitempty"`
	DataLimitePagAntic   string `xml:"DataLimitePagamentoAnticipato,omitempty"`
	PenalitaPagamRitard  string `xml:"PenalitaPagamentiRitardati,omitempty"`
	DataDecorrPenale     string `xml:"DataDecorrenzaPenale,omitempty"`
	CodicePagamento      string `xml:"CodicePagamento,omitempty"`
}

// Allegato represents an attachment
type Allegato struct {
	NomeAttachment       string `xml:"NomeAttachment"`
	AlgoritmoCompressione string `xml:"AlgoritmoCompressione,omitempty"`
	FormatoAttachment    string `xml:"FormatoAttachment,omitempty"`
	DescrizioneAttachment string `xml:"DescrizioneAttachment,omitempty"`
	Attachment           string `xml:"Attachment"` // Base64 encoded
}

// NewFatturaElettronica creates a new FatturaPA document
func NewFatturaElettronica(format TransmissionFormat) *FatturaElettronica {
	return &FatturaElettronica{
		XmlnsXsi:       "http://www.w3.org/2001/XMLSchema-instance",
		SchemaLocation: "http://ivaservizi.agenziaentrate.gov.it/docs/xsd/fatture/v1.2 http://www.fatturapa.gov.it/export/fatturazione/sdi/fatturapa/v1.2/Schema_del_file_xml_FatturaPA_versione_1.2.xsd",
		Versione:       format,
	}
}
