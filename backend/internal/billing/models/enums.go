package models

// InvoiceDirection indicates whether the invoice is issued (active) or received (passive)
type InvoiceDirection string

const (
	DirectionIssued   InvoiceDirection = "issued"   // Fatture attive (emesse)
	DirectionReceived InvoiceDirection = "received" // Fatture passive (ricevute)
)

// InvoiceStatus represents the workflow status of an invoice
type InvoiceStatus string

const (
	StatusDraft     InvoiceStatus = "draft"     // Bozza
	StatusPending   InvoiceStatus = "pending"   // In attesa di invio
	StatusSent      InvoiceStatus = "sent"      // Inviata a SDI
	StatusDelivered InvoiceStatus = "delivered" // Consegnata al destinatario
	StatusRejected  InvoiceStatus = "rejected"  // Scartata da SDI
	StatusAccepted  InvoiceStatus = "accepted"  // Accettata (PA)
	StatusPaid      InvoiceStatus = "paid"      // Pagata
	StatusCancelled InvoiceStatus = "cancelled" // Annullata
)

// SDIStatus represents the notification status from SDI
type SDIStatus string

const (
	SDIStatusNone SDIStatus = ""     // Nessuna notifica
	SDIStatusRC   SDIStatus = "RC"   // Ricevuta di Consegna
	SDIStatusNS   SDIStatus = "NS"   // Notifica di Scarto
	SDIStatusMC   SDIStatus = "MC"   // Mancata Consegna
	SDIStatusNE   SDIStatus = "NE"   // Notifica Esito (PA)
	SDIStatusDT   SDIStatus = "DT"   // Decorrenza Termini
	SDIStatusAT   SDIStatus = "AT"   // Attestazione di avvenuta trasmissione
)

// DocumentType represents the tipo documento as per FatturaPA specification
type DocumentType string

const (
	DocTypeFattura                  DocumentType = "TD01" // Fattura
	DocTypeAccontoFattura           DocumentType = "TD02" // Acconto/Anticipo su fattura
	DocTypeAccontoParcella          DocumentType = "TD03" // Acconto/Anticipo su parcella
	DocTypeNotaCredito              DocumentType = "TD04" // Nota di Credito
	DocTypeNotaDebito               DocumentType = "TD05" // Nota di Debito
	DocTypeParcella                 DocumentType = "TD06" // Parcella
	DocTypeFatturaSimplificata      DocumentType = "TD07" // Fattura semplificata
	DocTypeNotaCreditoSemplificata  DocumentType = "TD08" // Nota di credito semplificata
	DocTypeNotaDebitoSemplificata   DocumentType = "TD09" // Nota di debito semplificata
	DocTypeFatturaAcquisto          DocumentType = "TD10" // Fattura per acquisto intracomunitario beni
	DocTypeFatturaServizi           DocumentType = "TD11" // Fattura per acquisto intracomunitario servizi
	DocTypeDocumentoRiepilogo       DocumentType = "TD12" // Documento riepilogo fatture acquisto
	DocTypeFatturaAutoconsumo       DocumentType = "TD16" // Integrazione fattura reverse charge interno
	DocTypeIntegrazioneServizi      DocumentType = "TD17" // Integrazione/autofattura per acquisto servizi dall'estero
	DocTypeIntegrazioneBeni         DocumentType = "TD18" // Integrazione per acquisto beni intracomunitari
	DocTypeIntegrazioneBeniArt17    DocumentType = "TD19" // Integrazione/autofattura per acquisto di beni ex art.17 c.2 DPR 633/72
	DocTypeAutofatturaRegolarizzare DocumentType = "TD20" // Autofattura per regolarizzazione e integrazione delle fatture
	DocTypeAutofatturaSplafonamento DocumentType = "TD21" // Autofattura per splafonamento
	DocTypeEstrazioneBeniDeposito   DocumentType = "TD22" // Estrazione beni da Deposito IVA
	DocTypeEstrazioneBeniDepositoPA DocumentType = "TD23" // Estrazione beni da Deposito IVA con versamento dell'IVA
	DocTypeFatturaDifferita         DocumentType = "TD24" // Fattura differita di cui all'art. 21, comma 4, lett. a)
	DocTypeFatturaDifferitaTrian    DocumentType = "TD25" // Fattura differita di cui all'art. 21, comma 4, terzo periodo lett. b)
	DocTypeCessioneBeniAmmortizz    DocumentType = "TD26" // Cessione di beni ammortizzabili e per passaggi interni
	DocTypeFatturaAutoconsumoOmaggi DocumentType = "TD27" // Fattura per autoconsumo o per cessioni gratuite senza rivalsa
	DocTypeAcquistoSanMarino        DocumentType = "TD28" // Acquisti da San Marino con IVA (art. 7 c.1 ter DL 75/2023)
	DocTypeComunicazioneOmessa      DocumentType = "TD29" // Comunicazione omessa o irregolare fatturazione (v1.9 - April 2025)
)

// RegimeFiscale represents the fiscal regime as per FatturaPA specification
type RegimeFiscale string

const (
	RegimeOrdinario                  RegimeFiscale = "RF01" // Ordinario
	RegimeContributtiMinimi          RegimeFiscale = "RF02" // Contribuenti minimi (art.1, c.96-117, L. 244/07)
	RegimeAgevolato                  RegimeFiscale = "RF04" // Agricoltura e attività connesse e pesca (artt.34 e 34-bis, DPR 633/72)
	RegimeVenditaSaliTabacchi        RegimeFiscale = "RF05" // Vendita sali e tabacchi (art.74, c.1, DPR. 633/72)
	RegimeCommercioFiammiferi        RegimeFiscale = "RF06" // Commercio fiammiferi (art.74, c.1, DPR 633/72)
	RegimeEditoria                   RegimeFiscale = "RF07" // Editoria (art.74, c.1, DPR 633/72)
	RegimeTelefonia                  RegimeFiscale = "RF08" // Gestione servizi telefonia pubblica (art.74, c.1, DPR 633/72)
	RegimeRivenditaDocumenti         RegimeFiscale = "RF09" // Rivendita documenti di trasporto pubblico e di sosta (art.74, c.1, DPR 633/72)
	RegimeIntrattenimenti            RegimeFiscale = "RF10" // Intrattenimenti, giochi e altre attività (art.74, c.6, DPR 633/72)
	RegimeAgenzieViaggio             RegimeFiscale = "RF11" // Agenzie viaggi e turismo (art.74-ter, DPR 633/72)
	RegimeAgroalimentare             RegimeFiscale = "RF12" // Agriturismo (art.5, c.2, L. 413/91)
	RegimeVenditePortaPorta          RegimeFiscale = "RF13" // Vendite a domicilio (art.25-bis, c.6, DPR 600/73)
	RegimeRivenditaBeniUsati         RegimeFiscale = "RF14" // Rivendita beni usati, oggetti d'arte, d'antiquariato o da collezione (art.36, DL 41/95)
	RegimeAgenzieVenditeAste         RegimeFiscale = "RF15" // Agenzie di vendite all'asta di oggetti d'arte, antiquariato o da collezione (art.40-bis, DL 41/95)
	RegimeIVAPerCassa                RegimeFiscale = "RF16" // IVA per cassa P.A. (art.6, c.5, DPR 633/72)
	RegimeIVAPerCassaGenerale        RegimeFiscale = "RF17" // IVA per cassa (art. 32-bis, DL 83/2012)
	RegimeAltro                      RegimeFiscale = "RF18" // Altro
	RegimeForfettario                RegimeFiscale = "RF19" // Regime forfettario (art.1, c.54-89, L. 190/2014)
	RegimeFranchigiaIVA              RegimeFiscale = "RF20" // Regime transfrontaliero di Franchigia IVA (Direttiva UE 2020/285 - v1.9 Jan 2025)
)

// PaymentMethod represents the modalità di pagamento as per FatturaPA specification
type PaymentMethod string

const (
	PaymentContanti              PaymentMethod = "MP01" // Contanti
	PaymentAssegno               PaymentMethod = "MP02" // Assegno
	PaymentAssegnoCircolare      PaymentMethod = "MP03" // Assegno circolare
	PaymentContantiPressoTesor   PaymentMethod = "MP04" // Contanti presso Tesoreria
	PaymentBonificoSepa          PaymentMethod = "MP05" // Bonifico
	PaymentVagliaPostale         PaymentMethod = "MP06" // Vaglia cambiario
	PaymentBollettinoBancario    PaymentMethod = "MP07" // Bollettino bancario
	PaymentCartaPagamento        PaymentMethod = "MP08" // Carta di pagamento
	PaymentRID                   PaymentMethod = "MP09" // RID
	PaymentRIDUtenze             PaymentMethod = "MP10" // RID utenze
	PaymentRIDVeloce             PaymentMethod = "MP11" // RID veloce
	PaymentRIBA                  PaymentMethod = "MP12" // Riba
	PaymentMAV                   PaymentMethod = "MP13" // MAV
	PaymentQuietanzaErario       PaymentMethod = "MP14" // Quietanza erario stato
	PaymentGirocontoBancario     PaymentMethod = "MP15" // Giroconto su conti di contabilità speciale
	PaymentDomiciliazioneBancar  PaymentMethod = "MP16" // Domiciliazione bancaria
	PaymentDomiciliazionePostale PaymentMethod = "MP17" // Domiciliazione postale
	PaymentBollettinoCCP         PaymentMethod = "MP18" // Bollettino di c/c postale
	PaymentSEPADirectDebit       PaymentMethod = "MP19" // SEPA Direct Debit
	PaymentSEPADirectDebitCore   PaymentMethod = "MP20" // SEPA Direct Debit CORE
	PaymentSEPADirectDebitB2B    PaymentMethod = "MP21" // SEPA Direct Debit B2B
	PaymentTrattenutaSuSomme     PaymentMethod = "MP22" // Trattenuta su somme già riscosse
	PaymentPagoPA                PaymentMethod = "MP23" // PagoPA
)

// PaymentCondition represents the condizioni di pagamento as per FatturaPA specification
type PaymentCondition string

const (
	PaymentConditionRata      PaymentCondition = "TP01" // Pagamento a rate
	PaymentConditionCompleto  PaymentCondition = "TP02" // Pagamento completo
	PaymentConditionAnticipo  PaymentCondition = "TP03" // Anticipo
)

// VATNature represents the natura IVA for zero-rate or exempt transactions
type VATNature string

const (
	VATNatureN1   VATNature = "N1"   // Escluse ex art.15
	VATNatureN2   VATNature = "N2"   // Non soggette (obsoleto)
	VATNatureN2_1 VATNature = "N2.1" // Non soggette ad IVA - artt. da 7 a 7-septies DPR 633/72
	VATNatureN2_2 VATNature = "N2.2" // Non soggette - altri casi
	VATNatureN3   VATNature = "N3"   // Non imponibili (obsoleto)
	VATNatureN3_1 VATNature = "N3.1" // Non imponibili - esportazioni
	VATNatureN3_2 VATNature = "N3.2" // Non imponibili - cessioni intracomunitarie
	VATNatureN3_3 VATNature = "N3.3" // Non imponibili - cessioni verso San Marino
	VATNatureN3_4 VATNature = "N3.4" // Non imponibili - operazioni assimilate alle cessioni all'esportazione
	VATNatureN3_5 VATNature = "N3.5" // Non imponibili - dichiarazioni d'intento
	VATNatureN3_6 VATNature = "N3.6" // Non imponibili - altre operazioni non imponibili
	VATNatureN4   VATNature = "N4"   // Esenti
	VATNatureN5   VATNature = "N5"   // Regime del margine / IVA non esposta
	VATNatureN6   VATNature = "N6"   // Inversione contabile (obsoleto)
	VATNatureN6_1 VATNature = "N6.1" // Inversione contabile - cessione rottami
	VATNatureN6_2 VATNature = "N6.2" // Inversione contabile - cessione oro e argento
	VATNatureN6_3 VATNature = "N6.3" // Inversione contabile - subappalto edilizia
	VATNatureN6_4 VATNature = "N6.4" // Inversione contabile - cessione fabbricati
	VATNatureN6_5 VATNature = "N6.5" // Inversione contabile - cessione telefoni cellulari
	VATNatureN6_6 VATNature = "N6.6" // Inversione contabile - cessione prodotti elettronici
	VATNatureN6_7 VATNature = "N6.7" // Inversione contabile - prestazioni comparto edile e settori connessi
	VATNatureN6_8 VATNature = "N6.8" // Inversione contabile - operazioni settore energetico
	VATNatureN6_9 VATNature = "N6.9" // Inversione contabile - altri casi
	VATNatureN7   VATNature = "N7"   // IVA assolta in altro stato UE
)

// NotificationType represents the type of SDI notification
type NotificationType string

const (
	NotificationRC NotificationType = "RC" // Ricevuta di Consegna
	NotificationNS NotificationType = "NS" // Notifica di Scarto
	NotificationMC NotificationType = "MC" // Mancata Consegna
	NotificationNE NotificationType = "NE" // Notifica Esito (solo PA)
	NotificationDT NotificationType = "DT" // Decorrenza Termini
	NotificationAT NotificationType = "AT" // Attestazione di avvenuta trasmissione
)

// NEOutcome represents the outcome of a Notifica Esito (NE) for PA invoices
type NEOutcome string

const (
	OutcomeAccepted NEOutcome = "EC01" // Accettata
	OutcomeRejected NEOutcome = "EC02" // Rifiutata
)

// TransmissionFormat represents the formato di trasmissione
type TransmissionFormat string

const (
	FormatFPA12 TransmissionFormat = "FPA12" // Fattura verso PA
	FormatFPR12 TransmissionFormat = "FPR12" // Fattura verso privati
)

// UnitOfMeasure represents common units of measure
type UnitOfMeasure string

const (
	UnitPiece   UnitOfMeasure = "PZ"  // Pezzo
	UnitKg      UnitOfMeasure = "KG"  // Chilogrammo
	UnitLt      UnitOfMeasure = "LT"  // Litro
	UnitMt      UnitOfMeasure = "MT"  // Metro
	UnitMq      UnitOfMeasure = "MQ"  // Metro quadrato
	UnitMc      UnitOfMeasure = "MC"  // Metro cubo
	UnitHour    UnitOfMeasure = "H"   // Ora
	UnitDay     UnitOfMeasure = "GG"  // Giorno
	UnitMonth   UnitOfMeasure = "MESE"// Mese
	UnitYear    UnitOfMeasure = "ANNO"// Anno
	UnitPercent UnitOfMeasure = "%"   // Percentuale
)
