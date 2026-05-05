import React, { useState, useEffect, useRef, useMemo } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import {
  Card,
  Form,
  Button,
  Alert,
  Tab,
  Nav,
  Row,
  Col,
  Table,
  InputGroup
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faPlus,
  faTrash,
  faSave,
  faPaperPlane,
  faArrowLeft,
  faInfoCircle
} from '@fortawesome/free-solid-svg-icons';
import {
  useCreateInvoiceMutation,
  useSendInvoiceMutation,
  useGetCustomersQuery,
  useGetCompaniesQuery,
  useGetDefaultCompanyQuery,
  useGetInvoiceQuery
} from 'store/api/billingApi';
import type {
  CreateInvoiceInput,
  CreateInvoiceLineInput,
  CreatePaymentTermsInput,
  DocumentType,
  PaymentMethod,
  PaymentCondition,
  UnitOfMeasure,
  VATNature,
  DatiRitenuta,
  DatiBollo,
  DatiCassa,
  TipoRitenuta,
  TipoCassa,
  AltriDatiGestionali,
  RelatedDocument
} from 'types/billing';
import { formatItalianDate, DOCUMENT_TYPE_LABELS } from 'types/billing';
import PageHeader from 'components/common/PageHeader';
import FalconCardHeader from 'components/common/FalconCardHeader';

// Forfettario (RF19) mandatory causale texts
const FORFETTARIO_CAUSALE =
  "Operazione effettuata in regime forfettario ai sensi dell'articolo 1, commi da 54 a 89, della Legge n. 190/2014 e successive modificazioni";
const PROFESSIONISTA_CAUSALE =
  'Operazione non soggetta a ritenuta alla fonte a titolo di acconto ai sensi dell\'articolo 1, comma 67, Legge n. 190 del 2014 e successive modificazioni';

// Bollo threshold per DPR 642/1972
const BOLLO_THRESHOLD = 77.47;

// Default empty line
const createEmptyLine = (): CreateInvoiceLineInput => ({
  description: '',
  quantity: 1,
  unitOfMeasure: 'PZ' as UnitOfMeasure,
  unitPrice: 0,
  vatRate: 22,
  vatNature: undefined,
  discounts: [],
  productCode: '',
  startDate: undefined,
  endDate: undefined,
  altriDatiGestionali: []
});

// Create empty AltriDatiGestionali entry
const createEmptyAltriDati = (): AltriDatiGestionali => ({
  tipoDato: '',
  riferimentoTesto: '',
  riferimentoNumero: undefined,
  riferimentoData: undefined
});

// Document type options
const DOCUMENT_TYPES: { value: DocumentType; label: string }[] = [
  // Fatture standard
  { value: 'TD01', label: 'TD01 - Fattura' },
  { value: 'TD02', label: 'TD02 - Acconto/Anticipo su fattura' },
  { value: 'TD03', label: 'TD03 - Acconto/Anticipo su parcella' },
  { value: 'TD04', label: 'TD04 - Nota di Credito' },
  { value: 'TD05', label: 'TD05 - Nota di Debito' },
  { value: 'TD06', label: 'TD06 - Parcella' },
  // Fatture semplificate
  { value: 'TD07', label: 'TD07 - Fattura semplificata' },
  { value: 'TD08', label: 'TD08 - Nota di credito semplificata' },
  { value: 'TD09', label: 'TD09 - Nota di debito semplificata' },
  // Autofatture e integrazioni (cedente = cessionario consentito)
  { value: 'TD16', label: 'TD16 - Integrazione reverse charge interno' },
  { value: 'TD17', label: 'TD17 - Autofattura acquisto servizi estero' },
  { value: 'TD18', label: 'TD18 - Integrazione acquisto beni intraUE' },
  { value: 'TD19', label: 'TD19 - Integrazione acquisto beni art.17' },
  { value: 'TD20', label: 'TD20 - Autofattura regolarizzazione' },
  { value: 'TD21', label: 'TD21 - Autofattura splafonamento' },
  // Fatture differite
  { value: 'TD24', label: 'TD24 - Fattura differita (art.21 c.4 lett.a)' },
  { value: 'TD25', label: 'TD25 - Fattura differita (art.21 c.4 lett.b)' },
  // Altri tipi
  { value: 'TD26', label: 'TD26 - Cessione beni ammortizzabili' },
  { value: 'TD27', label: 'TD27 - Autoconsumo/cessioni gratuite' },
  { value: 'TD28', label: 'TD28 - Acquisti da San Marino con IVA' }
];

// Payment method options
const PAYMENT_METHODS: { value: PaymentMethod; label: string }[] = [
  { value: 'MP01', label: 'MP01 - Contanti' },
  { value: 'MP02', label: 'MP02 - Assegno' },
  { value: 'MP05', label: 'MP05 - Bonifico' },
  { value: 'MP08', label: 'MP08 - Carta di pagamento' },
  { value: 'MP12', label: 'MP12 - RIBA' },
  { value: 'MP19', label: 'MP19 - SEPA Direct Debit' },
  { value: 'MP23', label: 'MP23 - PagoPA' }
];

// Payment condition options
const PAYMENT_CONDITIONS: { value: PaymentCondition; label: string }[] = [
  { value: 'TP01', label: 'TP01 - Pagamento a rate' },
  { value: 'TP02', label: 'TP02 - Pagamento completo' },
  { value: 'TP03', label: 'TP03 - Anticipo' }
];

// Unit of measure options
const UNITS_OF_MEASURE: { value: UnitOfMeasure; label: string }[] = [
  { value: 'PZ', label: 'PZ - Pezzo' },
  { value: 'KG', label: 'KG - Chilogrammo' },
  { value: 'LT', label: 'LT - Litro' },
  { value: 'MT', label: 'MT - Metro' },
  { value: 'MQ', label: 'MQ - Metro quadrato' },
  { value: 'H', label: 'H - Ora' },
  { value: 'GG', label: 'GG - Giorno' },
  { value: 'MESE', label: 'MESE - Mese' }
];

// VAT rates
const VAT_RATES = [0, 4, 5, 10, 22];

// VAT Nature options (for 0% VAT)
const VAT_NATURES: { value: VATNature; label: string }[] = [
  { value: 'N1', label: 'N1 - Escluse ex art.15' },
  { value: 'N2.1', label: 'N2.1 - Non soggette (artt. 7-7septies)' },
  { value: 'N2.2', label: 'N2.2 - Non soggette (altri casi)' },
  { value: 'N3.1', label: 'N3.1 - Non imponibili (esportazioni)' },
  { value: 'N3.5', label: 'N3.5 - Non imponibili (dichiarazioni intento)' },
  { value: 'N3.6', label: 'N3.6 - Non imponibili (altre)' },
  { value: 'N4', label: 'N4 - Esenti' },
  { value: 'N5', label: 'N5 - Regime del margine' },
  { value: 'N6.1', label: 'N6.1 - Reverse charge (rottami)' },
  { value: 'N6.9', label: 'N6.9 - Reverse charge (altri casi)' }
];

// Withholding tax types (Ritenuta d'acconto)
const TIPO_RITENUTA_OPTIONS: { value: TipoRitenuta; label: string }[] = [
  { value: 'RT01', label: 'RT01 - Ritenuta persone fisiche' },
  { value: 'RT02', label: 'RT02 - Ritenuta persone giuridiche' },
  { value: 'RT03', label: 'RT03 - Contributo INPS' },
  { value: 'RT04', label: 'RT04 - Contributo ENASARCO' },
  { value: 'RT05', label: 'RT05 - Contributo ENPAM' },
  { value: 'RT06', label: 'RT06 - Altro contributo previdenziale' }
];

// Social security fund types (Cassa previdenziale)
const TIPO_CASSA_OPTIONS: { value: TipoCassa; label: string }[] = [
  { value: 'TC01', label: 'TC01 - Cassa Avvocati' },
  { value: 'TC02', label: 'TC02 - Cassa Commercialisti' },
  { value: 'TC03', label: 'TC03 - Cassa Geometri' },
  { value: 'TC04', label: 'TC04 - Cassa Ingegneri/Architetti' },
  { value: 'TC05', label: 'TC05 - Cassa Notariato' },
  { value: 'TC06', label: 'TC06 - Cassa Ragionieri' },
  { value: 'TC07', label: 'TC07 - ENASARCO' },
  { value: 'TC08', label: 'TC08 - ENPACL' },
  { value: 'TC09', label: 'TC09 - ENPAM' },
  { value: 'TC10', label: 'TC10 - ENPAF' },
  { value: 'TC22', label: 'TC22 - INPS' }
];

const NewIssuedInvoice: React.FC = () => {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const fromInvoiceId = searchParams.get('fromInvoice');

  const [createInvoice, { isLoading: isCreating }] = useCreateInvoiceMutation();
  const [sendInvoice, { isLoading: isSending }] = useSendInvoiceMutation();
  const { data: customersData } = useGetCustomersQuery({ pageSize: 100 });
  const { data: companiesData } = useGetCompaniesQuery({ pageSize: 100 });
  const { data: defaultCompany } = useGetDefaultCompanyQuery();

  // Fetch source invoice for credit note pre-population
  const { data: sourceInvoice } = useGetInvoiceQuery(fromInvoiceId!, {
    skip: !fromInvoiceId
  });

  const activeTab = searchParams.get('tab') || 'document';
  const setActiveTab = (tab: string) => {
    setSearchParams((prev) => { prev.set('tab', tab); return prev; }, { replace: true });
  };
  const [error, setError] = useState<string>('');
  const [success, setSuccess] = useState<string>('');

  // Form state
  const [documentType, setDocumentType] = useState<DocumentType>('TD01');
  const [number, setNumber] = useState('');
  const [date, setDate] = useState(new Date().toISOString().split('T')[0]);
  const [companyId, setCompanyId] = useState('');
  const [customerId, setCustomerId] = useState('');
  const [lines, setLines] = useState<CreateInvoiceLineInput[]>([
    createEmptyLine()
  ]);
  const [causale, setCausale] = useState<string[]>(['']);
  const [internalNotes, setInternalNotes] = useState('');
  const [legalStorageEnabled, setLegalStorageEnabled] = useState(true);
  const [signatureEnabled, setSignatureEnabled] = useState(true);
  const [relatedDocuments, setRelatedDocuments] = useState<RelatedDocument[]>(
    []
  );

  // Forfettario (RF19) detection
  const selectedCompany = companiesData?.companies?.find(
    c => c.id === companyId
  );
  const isForfettario = selectedCompany?.regimeFiscale === 'RF19';
  const isProfessional =
    isForfettario && (selectedCompany?.isProfessional ?? false);

  // Track whether bollo was auto-activated by forfettario logic
  const bolloAutoActivated = useRef(false);
  // Track number of auto-causale lines for forfettario
  const autoCausaleCount = useMemo(() => {
    if (!isForfettario) return 0;
    return isProfessional ? 2 : 1;
  }, [isForfettario, isProfessional]);

  // Payment terms
  const [paymentCondition, setPaymentCondition] =
    useState<PaymentCondition | ''>('');
  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod | ''>('');
  const [paymentBeneficiario, setPaymentBeneficiario] = useState('');
  const [paymentIstituto, setPaymentIstituto] = useState('');
  const [paymentIban, setPaymentIban] = useState('');
  const [paymentAbi, setPaymentAbi] = useState('');
  const [paymentCab, setPaymentCab] = useState('');
  const [paymentBic, setPaymentBic] = useState('');
  const [paymentDueDate, setPaymentDueDate] = useState('');
  const [selectedPaymentCompanyId, setSelectedPaymentCompanyId] = useState('');

  // Withholding tax (Ritenuta d'acconto)
  const [enableRitenuta, setEnableRitenuta] = useState(false);
  const [datiRitenuta, setDatiRitenuta] = useState<DatiRitenuta>({
    tipoRitenuta: 'RT01',
    importoRitenuta: 0,
    aliquotaRitenuta: 20,
    causalePagamento: 'A'
  });

  // Stamp duty (Bollo virtuale)
  const [enableBollo, setEnableBollo] = useState(false);
  const [datiBollo, setDatiBollo] = useState<DatiBollo>({
    importoBollo: 2.0
  });

  // Social security fund (Cassa previdenziale)
  const [enableCassa, setEnableCassa] = useState(false);
  const [datiCassa, setDatiCassa] = useState<DatiCassa>({
    tipoCassa: 'TC22',
    alCassa: 4,
    importoContributoCassa: 0,
    aliquotaIVA: 22
  });

  const isLoading = isCreating || isSending;
  const isCreditNote = !!fromInvoiceId;

  // Set default company when loaded
  useEffect(() => {
    if (defaultCompany && !companyId) {
      setCompanyId(defaultCompany.id);
    }
  }, [defaultCompany, companyId]);

  // Pre-populate form from source invoice (credit note)
  useEffect(() => {
    if (!sourceInvoice) return;

    setDocumentType('TD04');
    setDate(new Date().toISOString().split('T')[0]);
    setNumber('');

    if (sourceInvoice.customerId) {
      setCustomerId(sourceInvoice.customerId);
    }

    // Copy lines
    if (sourceInvoice.lines?.length) {
      setLines(
        sourceInvoice.lines.map(line => ({
          description: line.description,
          quantity: line.quantity,
          unitOfMeasure: line.unitOfMeasure,
          unitPrice: line.unitPrice,
          vatRate: line.vatRate,
          vatNature: line.vatNature,
          discounts: line.discounts || [],
          productCode: line.productCode,
          startDate: line.startDate,
          endDate: line.endDate,
          altriDatiGestionali: line.altriDatiGestionali || []
        }))
      );
    }

    // Set causale with reference to original invoice
    const sourceDate = formatItalianDate(sourceInvoice.date);
    setCausale([
      `Nota di credito rif. fattura n. ${sourceInvoice.number} del ${sourceDate}`
    ]);

    // Copy payment terms
    if (sourceInvoice.paymentTerms) {
      setPaymentCondition(sourceInvoice.paymentTerms.condition);
      setPaymentMethod(sourceInvoice.paymentTerms.paymentMethod);
      setPaymentIban(sourceInvoice.paymentTerms.iban || '');
      setPaymentDueDate('');
      setPaymentBeneficiario(sourceInvoice.paymentTerms.beneficiario || '');
      setPaymentIstituto(sourceInvoice.paymentTerms.istitutoFinanziario || '');
      setPaymentBic(sourceInvoice.paymentTerms.bic || '');
      setPaymentAbi(sourceInvoice.paymentTerms.abi || '');
      setPaymentCab(sourceInvoice.paymentTerms.cab || '');
    }

    // Copy ritenuta
    if (sourceInvoice.datiRitenuta?.length) {
      setEnableRitenuta(true);
      setDatiRitenuta(sourceInvoice.datiRitenuta[0]);
    }

    // Copy bollo
    if (sourceInvoice.datiBollo) {
      setEnableBollo(true);
      setDatiBollo(sourceInvoice.datiBollo);
    }

    // Copy cassa previdenziale
    if (sourceInvoice.datiCassaPrevidenziale?.length) {
      setEnableCassa(true);
      setDatiCassa(sourceInvoice.datiCassaPrevidenziale[0]);
    }

    // Copy options
    setLegalStorageEnabled(sourceInvoice.legalStorageEnabled);
    setSignatureEnabled(sourceInvoice.signatureEnabled);

    // Set related document reference
    setRelatedDocuments([
      {
        type: 'fattura',
        number: sourceInvoice.number,
        date: sourceInvoice.date
      }
    ]);

    // Set company from source invoice (find by cedentePrestatore)
    // The companyId is not directly on the invoice, but we can match by fiscal code
    // For simplicity, keep the default company (it should be the same)
  }, [sourceInvoice]);

  // Forfettario: force lines to IVA 0% / N2.2
  useEffect(() => {
    if (isForfettario) {
      setLines(prev =>
        prev.map(line => ({
          ...line,
          vatRate: 0,
          vatNature: 'N2.2' as VATNature
        }))
      );
    } else {
      // Restore defaults when switching away from forfettario
      setLines(prev =>
        prev.map(line =>
          line.vatRate === 0 && line.vatNature === 'N2.2'
            ? { ...line, vatRate: 22, vatNature: undefined }
            : line
        )
      );
    }
  }, [isForfettario]);

  // Forfettario: manage automatic causale lines
  useEffect(() => {
    if (isForfettario) {
      const autoCausali = isProfessional
        ? [FORFETTARIO_CAUSALE, PROFESSIONISTA_CAUSALE]
        : [FORFETTARIO_CAUSALE];
      setCausale(prev => {
        // Remove any existing auto-causale, then prepend fresh ones
        const userCausali = prev.filter(
          c => c !== FORFETTARIO_CAUSALE && c !== PROFESSIONISTA_CAUSALE
        );
        return [...autoCausali, ...userCausali];
      });
    } else {
      // Remove auto-causale when disabling forfettario
      setCausale(prev =>
        prev.filter(
          c => c !== FORFETTARIO_CAUSALE && c !== PROFESSIONISTA_CAUSALE
        )
      );
    }
  }, [isForfettario, isProfessional]);

  // Forfettario professional: auto-enable cassa previdenziale with IVA 0%
  useEffect(() => {
    if (isProfessional) {
      setEnableCassa(true);
      setDatiCassa(prev => ({
        ...prev,
        aliquotaIVA: 0,
        natura: 'N2.2' as VATNature
      }));
    } else if (!isProfessional && isForfettario === false) {
      // Only reset cassa if we're fully leaving forfettario mode
      setEnableCassa(false);
    }
  }, [isProfessional, isForfettario]);

  // Calculate totals
  const calculateLineTotals = (line: CreateInvoiceLineInput) => {
    const totalPrice = line.quantity * line.unitPrice;
    const vatAmount = totalPrice * (line.vatRate / 100);
    return { totalPrice, vatAmount };
  };

  const totalsBeforeBollo = lines.reduce(
    (acc, line) => {
      const { totalPrice, vatAmount } = calculateLineTotals(line);
      return {
        taxable: acc.taxable + totalPrice,
        vat: acc.vat + vatAmount,
        total: acc.total + totalPrice + vatAmount
      };
    },
    { taxable: 0, vat: 0, total: 0 }
  );

  // Include bollo in display total (per FatturaPA spec)
  const bolloAmount = enableBollo ? datiBollo.importoBollo || 2 : 0;
  const totals = {
    ...totalsBeforeBollo,
    total: totalsBeforeBollo.total + bolloAmount
  };

  // Forfettario: auto-enable bollo when total exceeds threshold
  useEffect(() => {
    if (isForfettario && totalsBeforeBollo.total > BOLLO_THRESHOLD) {
      if (!enableBollo) {
        setEnableBollo(true);
        setDatiBollo({ importoBollo: 2.0 });
        bolloAutoActivated.current = true;
      }
    } else if (bolloAutoActivated.current) {
      // Auto-disable only if it was auto-activated
      setEnableBollo(false);
      bolloAutoActivated.current = false;
    }
  }, [isForfettario, totalsBeforeBollo.total, enableBollo]);

  // Line handlers
  const handleAddLine = () => {
    const newLine = createEmptyLine();
    if (isForfettario) {
      newLine.vatRate = 0;
      newLine.vatNature = 'N2.2' as VATNature;
    }
    setLines([...lines, newLine]);
  };

  const handleRemoveLine = (index: number) => {
    if (lines.length > 1) {
      setLines(lines.filter((_, i) => i !== index));
    }
  };

  const handleLineChange = (
    index: number,
    field: keyof CreateInvoiceLineInput,
    value: string | number | undefined
  ) => {
    const newLines = [...lines];
    newLines[index] = { ...newLines[index], [field]: value };
    setLines(newLines);
  };

  // Causale handlers
  const isAutoCausale = (index: number) => {
    if (!isForfettario) return false;
    return index < autoCausaleCount;
  };

  const handleAddCausale = () => {
    setCausale([...causale, '']);
  };

  const handleRemoveCausale = (index: number) => {
    if (isAutoCausale(index)) return; // Cannot remove auto-causale
    if (causale.length > 1) {
      setCausale(causale.filter((_, i) => i !== index));
    }
  };

  const handleCausaleChange = (index: number, value: string) => {
    if (isAutoCausale(index)) return; // Cannot edit auto-causale
    const newCausale = [...causale];
    newCausale[index] = value;
    setCausale(newCausale);
  };

  // Payment company auto-fill handler
  const handlePaymentCompanySelect = (companyId: string) => {
    setSelectedPaymentCompanyId(companyId);
    if (!companyId) return;

    const company = companiesData?.companies?.find(c => c.id === companyId);
    if (!company) return;

    // Auto-populate payment fields (beneficiario uses denomination as fallback)
    setPaymentBeneficiario(company.beneficiario || company.denomination);
    if (company.istitutoFinanziario)
      setPaymentIstituto(company.istitutoFinanziario);
    if (company.iban) setPaymentIban(company.iban.toUpperCase());
    if (company.bic) setPaymentBic(company.bic.toUpperCase());
    if (company.abi) setPaymentAbi(company.abi);
    if (company.cab) setPaymentCab(company.cab);
  };

  // AltriDatiGestionali handlers
  const handleAddAltriDati = (lineIndex: number) => {
    const newLines = [...lines];
    const currentAltriDati = newLines[lineIndex].altriDatiGestionali || [];
    newLines[lineIndex] = {
      ...newLines[lineIndex],
      altriDatiGestionali: [...currentAltriDati, createEmptyAltriDati()]
    };
    setLines(newLines);
  };

  const handleRemoveAltriDati = (lineIndex: number, adgIndex: number) => {
    const newLines = [...lines];
    const currentAltriDati = newLines[lineIndex].altriDatiGestionali || [];
    newLines[lineIndex] = {
      ...newLines[lineIndex],
      altriDatiGestionali: currentAltriDati.filter((_, i) => i !== adgIndex)
    };
    setLines(newLines);
  };

  const handleAltriDatiChange = (
    lineIndex: number,
    adgIndex: number,
    field: keyof AltriDatiGestionali,
    value: string | number | undefined
  ) => {
    const newLines = [...lines];
    const currentAltriDati = [
      ...(newLines[lineIndex].altriDatiGestionali || [])
    ];
    currentAltriDati[adgIndex] = {
      ...currentAltriDati[adgIndex],
      [field]: value
    };
    newLines[lineIndex] = {
      ...newLines[lineIndex],
      altriDatiGestionali: currentAltriDati
    };
    setLines(newLines);
  };

  // Validation
  const validate = (): boolean => {
    if (!companyId) {
      setError("Selezionare un'azienda emittente");
      setActiveTab('document');
      return false;
    }

    if (!number.trim()) {
      setError('Il numero fattura è obbligatorio');
      setActiveTab('document');
      return false;
    }

    if (!customerId) {
      setError('Selezionare un cliente');
      setActiveTab('document');
      return false;
    }

    if (lines.length === 0) {
      setError('Aggiungere almeno una riga');
      setActiveTab('lines');
      return false;
    }

    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (!line.description.trim()) {
        setError(`Riga ${i + 1}: la descrizione è obbligatoria`);
        setActiveTab('lines');
        return false;
      }
      if (line.quantity <= 0) {
        setError(`Riga ${i + 1}: la quantità deve essere maggiore di zero`);
        setActiveTab('lines');
        return false;
      }
      if (line.vatRate === 0 && !line.vatNature) {
        setError(`Riga ${i + 1}: selezionare la natura IVA per aliquota 0%`);
        setActiveTab('lines');
        return false;
      }
    }

    return true;
  };

  // Convert date string (YYYY-MM-DD) to RFC 3339 datetime (YYYY-MM-DDTHH:mm:ssZ)
  const toRFC3339 = (dateStr: string): string => {
    return `${dateStr}T00:00:00Z`;
  };

  // Build invoice input
  const buildInvoiceInput = (): CreateInvoiceInput => {
    const paymentTerms: CreatePaymentTermsInput | undefined =
      paymentMethod && paymentCondition
        ? {
            condition: paymentCondition,
            paymentMethod: paymentMethod,
            beneficiario: paymentBeneficiario || undefined,
            istitutoFinanziario: paymentIstituto || undefined,
            iban: paymentIban || undefined,
            abi: paymentAbi || undefined,
            cab: paymentCab || undefined,
            bic: paymentBic || undefined,
            dueDate: paymentDueDate ? toRFC3339(paymentDueDate) : undefined
          }
        : undefined;

    return {
      documentType,
      number,
      date: toRFC3339(date),
      currency: 'EUR',
      companyId,
      customerId,
      // FatturaPA specific data
      datiRitenuta: enableRitenuta ? [datiRitenuta] : undefined,
      datiBollo: enableBollo ? datiBollo : undefined,
      datiCassaPrevidenziale: enableCassa ? [datiCassa] : undefined,
      lines: lines.map(line => ({
        ...line,
        vatNature: line.vatRate === 0 ? line.vatNature : undefined
      })),
      paymentTerms,
      relatedDocuments:
        relatedDocuments.length > 0 ? relatedDocuments : undefined,
      causale: causale.filter(c => c.trim()),
      internalNotes: internalNotes || undefined,
      legalStorageEnabled,
      signatureEnabled
    };
  };

  // Save as draft
  const handleSaveDraft = async () => {
    setError('');
    setSuccess('');

    if (!validate()) return;

    try {
      const input = buildInvoiceInput();
      await createInvoice(input).unwrap();
      setSuccess('Fattura salvata come bozza');
      setTimeout(() => navigate('/billing/invoices/issued'), 1500);
    } catch (err: unknown) {
      const errorMessage =
        err && typeof err === 'object' && 'data' in err
          ? (err as { data?: { message?: string } }).data?.message
          : undefined;
      setError(errorMessage || 'Errore durante il salvataggio della fattura');
    }
  };

  // Save and send to SDI
  const handleSaveAndSend = async () => {
    setError('');
    setSuccess('');

    if (!validate()) return;

    try {
      const input = buildInvoiceInput();
      const invoice = await createInvoice(input).unwrap();

      // Now send to SDI
      await sendInvoice(invoice.id).unwrap();
      setSuccess('Fattura creata e inviata al SDI');
      setTimeout(() => navigate('/billing/invoices/issued'), 1500);
    } catch (err: unknown) {
      const errorMessage =
        err && typeof err === 'object' && 'data' in err
          ? (err as { data?: { message?: string } }).data?.message
          : undefined;
      setError(
        errorMessage || 'Errore durante la creazione/invio della fattura'
      );
    }
  };

  const selectedCustomer = customersData?.customers?.find(
    c => c.id === customerId
  );

  return (
    <>
      <PageHeader
        title={isCreditNote ? 'Nuova Nota di Credito' : 'Nuova Fattura'}
        description={
          isCreditNote
            ? `${DOCUMENT_TYPE_LABELS['TD04']} - da fattura n. ${sourceInvoice?.number || '...'}`
            : 'Crea una nuova fattura elettronica'
        }
        className="mb-3"
      >
        <Button
          variant="falcon-default"
          size="sm"
          className="me-2"
          onClick={() => navigate('/billing/invoices/issued')}
        >
          <FontAwesomeIcon icon={faArrowLeft} className="me-1" />
          Torna alla lista
        </Button>
      </PageHeader>

      {error && (
        <Alert variant="danger" dismissible onClose={() => setError('')}>
          {error}
        </Alert>
      )}

      {success && (
        <Alert variant="success" dismissible onClose={() => setSuccess('')}>
          {success}
        </Alert>
      )}

      {isCreditNote && sourceInvoice && (
        <Alert variant="info">
          Stai creando una <strong>Nota di Credito (TD04)</strong> per la
          fattura n. <strong>{sourceInvoice.number}</strong> del{' '}
          {formatItalianDate(sourceInvoice.date)}. Il tipo documento è impostato
          su TD04 e il riferimento alla fattura originale verrà incluso
          automaticamente.
        </Alert>
      )}

      {isForfettario && (
        <Alert variant="info">
          <FontAwesomeIcon icon={faInfoCircle} className="me-2" />
          <strong>Regime forfettario (RF19)</strong> — Le righe sono impostate
          automaticamente a IVA 0% con natura N2.2. La causale normativa è
          inserita automaticamente.
          {isProfessional &&
            ' Il soggetto è un professionista: causale ritenuta e cassa previdenziale pre-compilate.'}
        </Alert>
      )}

      <Card className="mb-3">
        <FalconCardHeader
          title={isCreditNote ? 'Dati Nota di Credito' : 'Dati Fattura'}
          light={false}
        />
        <Card.Body>
          <Tab.Container
            activeKey={activeTab}
            onSelect={k => { if (k) setActiveTab(k); }}
          >
            <Nav variant="tabs" className="mb-3">
              <Nav.Item>
                <Nav.Link eventKey="document">Documento</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="lines">Righe ({lines.length})</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="ritenute">Ritenute e Contributi</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="payment">Pagamento</Nav.Link>
              </Nav.Item>
              <Nav.Item>
                <Nav.Link eventKey="options">Opzioni</Nav.Link>
              </Nav.Item>
            </Nav>

            <Tab.Content>
              {/* Document Tab */}
              <Tab.Pane eventKey="document">
                {/* Company Selector */}
                <Row className="mb-3">
                  <Col md={12}>
                    <Form.Group>
                      <Form.Label>
                        Azienda Emittente <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Select
                        value={companyId}
                        onChange={e => setCompanyId(e.target.value)}
                      >
                        <option value="">Seleziona azienda...</option>
                        {companiesData?.companies
                          ?.filter(c => c.isActive)
                          .map(company => (
                            <option key={company.id} value={company.id}>
                              {company.denomination} - P.IVA{' '}
                              {company.fiscalIdCode}
                              {company.isDefault && ' (Default)'}
                            </option>
                          ))}
                      </Form.Select>
                      <Form.Text className="text-muted">
                        L'azienda selezionata verrà utilizzata come
                        cedente/prestatore nella fattura
                      </Form.Text>
                    </Form.Group>
                  </Col>
                </Row>

                <hr className="my-3" />

                <Row>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        Tipo Documento <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Select
                        value={documentType}
                        onChange={e =>
                          setDocumentType(e.target.value as DocumentType)
                        }
                        disabled={isCreditNote}
                      >
                        {DOCUMENT_TYPES.map(dt => (
                          <option key={dt.value} value={dt.value}>
                            {dt.label}
                          </option>
                        ))}
                      </Form.Select>
                      {isCreditNote && (
                        <Form.Text className="text-muted">
                          Tipo documento fissato a TD04 per la nota di credito
                        </Form.Text>
                      )}
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        Numero Fattura <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="text"
                        value={number}
                        onChange={e => setNumber(e.target.value)}
                        placeholder="es. 2026/001"
                      />
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        Data <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Control
                        type="date"
                        value={date}
                        onChange={e => setDate(e.target.value)}
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={8}>
                    <Form.Group className="mb-3">
                      <Form.Label>
                        Cliente <span className="text-danger">*</span>
                      </Form.Label>
                      <Form.Select
                        value={customerId}
                        onChange={e => setCustomerId(e.target.value)}
                      >
                        <option value="">Seleziona cliente...</option>
                        {customersData?.customers?.map(customer => (
                          <option key={customer.id} value={customer.id}>
                            {customer.isCompany
                              ? customer.denomination
                              : `${customer.name} ${customer.surname}`}{' '}
                            - {customer.fiscalIdCode}
                          </option>
                        ))}
                      </Form.Select>
                    </Form.Group>
                  </Col>
                  <Col md={4}>
                    {selectedCustomer && (
                      <div className="mt-4 text-muted small">
                        <div>
                          <strong>SDI:</strong>{' '}
                          {selectedCustomer.codiceDestinatario ||
                            selectedCustomer.pecDestinatario ||
                            'N/A'}
                        </div>
                        <div>
                          <strong>P.IVA:</strong>{' '}
                          {selectedCustomer.fiscalIdCode}
                        </div>
                      </div>
                    )}
                  </Col>
                </Row>

                <Form.Group className="mb-3">
                  <Form.Label>Causale / Descrizione</Form.Label>
                  {causale.map((c, index) => (
                    <InputGroup className="mb-2" key={index}>
                      <Form.Control
                        type="text"
                        value={c}
                        onChange={e =>
                          handleCausaleChange(index, e.target.value)
                        }
                        placeholder="es. Consulenza informatica mese di gennaio 2026"
                        maxLength={200}
                        readOnly={isAutoCausale(index)}
                        className={
                          isAutoCausale(index) ? 'bg-light text-muted' : ''
                        }
                      />
                      {causale.length > 1 && !isAutoCausale(index) && (
                        <Button
                          variant="outline-danger"
                          onClick={() => handleRemoveCausale(index)}
                        >
                          <FontAwesomeIcon icon={faTrash} />
                        </Button>
                      )}
                    </InputGroup>
                  ))}
                  <Button variant="link" size="sm" onClick={handleAddCausale}>
                    <FontAwesomeIcon icon={faPlus} className="me-1" />
                    Aggiungi riga causale
                  </Button>
                </Form.Group>
              </Tab.Pane>

              {/* Lines Tab */}
              <Tab.Pane eventKey="lines">
                <div className="table-responsive">
                  <Table bordered hover size="sm">
                    <thead className="bg-body-tertiary">
                      <tr>
                        <th style={{ width: '25%' }}>Descrizione *</th>
                        <th style={{ width: '10%' }}>Codice</th>
                        <th style={{ width: '7%' }}>Qtà *</th>
                        <th style={{ width: '8%' }}>U.M.</th>
                        <th style={{ width: '10%' }}>Prezzo Unit.</th>
                        <th style={{ width: '7%' }}>IVA %</th>
                        <th style={{ width: '13%' }}>Natura</th>
                        <th style={{ width: '10%' }}>Totale</th>
                        <th style={{ width: '5%' }}></th>
                      </tr>
                    </thead>
                    <tbody>
                      {lines.map((line, index) => {
                        const { totalPrice } = calculateLineTotals(line);
                        return (
                          <React.Fragment key={index}>
                            <tr>
                              <td>
                                <Form.Control
                                  size="sm"
                                  type="text"
                                  value={line.description}
                                  onChange={e =>
                                    handleLineChange(
                                      index,
                                      'description',
                                      e.target.value
                                    )
                                  }
                                  placeholder="Descrizione"
                                />
                              </td>
                              <td>
                                <Form.Control
                                  size="sm"
                                  type="text"
                                  maxLength={35}
                                  value={line.productCode || ''}
                                  onChange={e =>
                                    handleLineChange(
                                      index,
                                      'productCode',
                                      e.target.value
                                    )
                                  }
                                  placeholder="Codice"
                                />
                              </td>
                              <td>
                                <Form.Control
                                  size="sm"
                                  type="number"
                                  min="0"
                                  step="0.01"
                                  value={line.quantity}
                                  onChange={e =>
                                    handleLineChange(
                                      index,
                                      'quantity',
                                      parseFloat(e.target.value) || 0
                                    )
                                  }
                                />
                              </td>
                              <td>
                                <Form.Select
                                  size="sm"
                                  value={line.unitOfMeasure || ''}
                                  onChange={e =>
                                    handleLineChange(
                                      index,
                                      'unitOfMeasure',
                                      e.target.value as UnitOfMeasure
                                    )
                                  }
                                >
                                  <option value="">-</option>
                                  {UNITS_OF_MEASURE.map(um => (
                                    <option key={um.value} value={um.value}>
                                      {um.value}
                                    </option>
                                  ))}
                                </Form.Select>
                              </td>
                              <td>
                                <Form.Control
                                  size="sm"
                                  type="number"
                                  min="0"
                                  step="0.01"
                                  value={line.unitPrice}
                                  onChange={e =>
                                    handleLineChange(
                                      index,
                                      'unitPrice',
                                      parseFloat(e.target.value) || 0
                                    )
                                  }
                                />
                              </td>
                              <td>
                                <Form.Select
                                  size="sm"
                                  value={line.vatRate}
                                  onChange={e =>
                                    handleLineChange(
                                      index,
                                      'vatRate',
                                      parseFloat(e.target.value)
                                    )
                                  }
                                  disabled={isForfettario}
                                >
                                  {VAT_RATES.map(rate => (
                                    <option key={rate} value={rate}>
                                      {rate}%
                                    </option>
                                  ))}
                                </Form.Select>
                              </td>
                              <td>
                                {line.vatRate === 0 ? (
                                  <Form.Select
                                    size="sm"
                                    value={line.vatNature || ''}
                                    onChange={e =>
                                      handleLineChange(
                                        index,
                                        'vatNature',
                                        (e.target.value as VATNature) ||
                                          undefined
                                      )
                                    }
                                    disabled={isForfettario}
                                  >
                                    <option value="">Seleziona...</option>
                                    {VAT_NATURES.map(n => (
                                      <option key={n.value} value={n.value}>
                                        {n.value}
                                      </option>
                                    ))}
                                  </Form.Select>
                                ) : (
                                  <span className="text-muted">-</span>
                                )}
                              </td>
                              <td className="text-end">
                                <strong>
                                  {totalPrice.toLocaleString('it-IT', {
                                    style: 'currency',
                                    currency: 'EUR'
                                  })}
                                </strong>
                              </td>
                              <td>
                                <Button
                                  variant="outline-danger"
                                  size="sm"
                                  onClick={() => handleRemoveLine(index)}
                                  disabled={lines.length === 1}
                                >
                                  <FontAwesomeIcon icon={faTrash} />
                                </Button>
                              </td>
                            </tr>
                            {/* AltriDatiGestionali row */}
                            <tr>
                              <td colSpan={9} className="bg-body-tertiary p-2">
                                <div className="d-flex align-items-center gap-2 mb-2">
                                  <small className="text-muted fw-bold ms-2">
                                    Altri Dati Gestionali
                                  </small>
                                  <Button
                                    variant="outline-primary"
                                    size="sm"
                                    onClick={() => handleAddAltriDati(index)}
                                  >
                                    <FontAwesomeIcon
                                      icon={faPlus}
                                      className="me-1"
                                    />
                                    Aggiungi
                                  </Button>
                                </div>
                                {(line.altriDatiGestionali || []).length >
                                  0 && (
                                  <div className="ps-2 ms-1">
                                    {(line.altriDatiGestionali || []).map(
                                      (adg, adgIndex) => (
                                        <Row
                                          key={adgIndex}
                                          className="mb-2 align-items-center g-2"
                                        >
                                          <Col xs={2}>
                                            <Form.Control
                                              size="sm"
                                              type="text"
                                              maxLength={10}
                                              placeholder="Tipo Dato*"
                                              value={adg.tipoDato || ''}
                                              onChange={e =>
                                                handleAltriDatiChange(
                                                  index,
                                                  adgIndex,
                                                  'tipoDato',
                                                  e.target.value
                                                )
                                              }
                                            />
                                          </Col>
                                          <Col xs={4}>
                                            <Form.Control
                                              size="sm"
                                              type="text"
                                              maxLength={60}
                                              placeholder="Rif. Testo"
                                              value={adg.riferimentoTesto || ''}
                                              onChange={e =>
                                                handleAltriDatiChange(
                                                  index,
                                                  adgIndex,
                                                  'riferimentoTesto',
                                                  e.target.value
                                                )
                                              }
                                            />
                                          </Col>
                                          <Col xs={2}>
                                            <Form.Control
                                              size="sm"
                                              type="number"
                                              step="0.01"
                                              placeholder="Rif. Numero"
                                              value={
                                                adg.riferimentoNumero ?? ''
                                              }
                                              onChange={e =>
                                                handleAltriDatiChange(
                                                  index,
                                                  adgIndex,
                                                  'riferimentoNumero',
                                                  e.target.value
                                                    ? parseFloat(e.target.value)
                                                    : undefined
                                                )
                                              }
                                            />
                                          </Col>
                                          <Col xs={2}>
                                            <Form.Control
                                              size="sm"
                                              type="date"
                                              value={adg.riferimentoData || ''}
                                              onChange={e =>
                                                handleAltriDatiChange(
                                                  index,
                                                  adgIndex,
                                                  'riferimentoData',
                                                  e.target.value || undefined
                                                )
                                              }
                                            />
                                          </Col>
                                          <Col xs={2}>
                                            <Button
                                              variant="outline-danger"
                                              size="sm"
                                              onClick={() =>
                                                handleRemoveAltriDati(
                                                  index,
                                                  adgIndex
                                                )
                                              }
                                            >
                                              <FontAwesomeIcon icon={faTrash} />
                                            </Button>
                                          </Col>
                                        </Row>
                                      )
                                    )}
                                  </div>
                                )}
                              </td>
                            </tr>
                          </React.Fragment>
                        );
                      })}
                    </tbody>
                    <tfoot>
                      <tr>
                        <td colSpan={9}>
                          <Button
                            variant="falcon-primary"
                            size="sm"
                            onClick={handleAddLine}
                          >
                            <FontAwesomeIcon icon={faPlus} className="me-1" />
                            Aggiungi Riga
                          </Button>
                        </td>
                      </tr>
                    </tfoot>
                  </Table>
                </div>

                {/* Totals */}
                <Row className="justify-content-end mt-3">
                  <Col md={4}>
                    <Table size="sm" className="border">
                      <tbody>
                        <tr>
                          <td>Imponibile</td>
                          <td className="text-end">
                            {totals.taxable.toLocaleString('it-IT', {
                              style: 'currency',
                              currency: 'EUR'
                            })}
                          </td>
                        </tr>
                        <tr>
                          <td>IVA</td>
                          <td className="text-end">
                            {totals.vat.toLocaleString('it-IT', {
                              style: 'currency',
                              currency: 'EUR'
                            })}
                          </td>
                        </tr>
                        {enableBollo && bolloAmount > 0 && (
                          <tr>
                            <td>
                              Bollo virtuale
                              {isForfettario && (
                                <small className="text-muted d-block">
                                  DPR 642/1972
                                </small>
                              )}
                            </td>
                            <td className="text-end">
                              {bolloAmount.toLocaleString('it-IT', {
                                style: 'currency',
                                currency: 'EUR'
                              })}
                            </td>
                          </tr>
                        )}
                        <tr className="fw-bold">
                          <td>Totale</td>
                          <td className="text-end">
                            {totals.total.toLocaleString('it-IT', {
                              style: 'currency',
                              currency: 'EUR'
                            })}
                          </td>
                        </tr>
                      </tbody>
                    </Table>
                  </Col>
                </Row>
              </Tab.Pane>

              {/* Ritenute e Contributi Tab */}
              <Tab.Pane eventKey="ritenute">
                {/* Withholding Tax Section */}
                <Card className="mb-3">
                  <Card.Header className="bg-body-tertiary">
                    <Form.Check
                      type="switch"
                      id="enableRitenuta"
                      label={<strong>Ritenuta d'Acconto</strong>}
                      checked={enableRitenuta}
                      onChange={e => setEnableRitenuta(e.target.checked)}
                    />
                  </Card.Header>
                  {enableRitenuta && (
                    <Card.Body>
                      <Row>
                        <Col md={4}>
                          <Form.Group className="mb-3">
                            <Form.Label>Tipo Ritenuta</Form.Label>
                            <Form.Select
                              value={datiRitenuta.tipoRitenuta}
                              onChange={e =>
                                setDatiRitenuta({
                                  ...datiRitenuta,
                                  tipoRitenuta: e.target.value as TipoRitenuta
                                })
                              }
                            >
                              {TIPO_RITENUTA_OPTIONS.map(tr => (
                                <option key={tr.value} value={tr.value}>
                                  {tr.label}
                                </option>
                              ))}
                            </Form.Select>
                          </Form.Group>
                        </Col>
                        <Col md={3}>
                          <Form.Group className="mb-3">
                            <Form.Label>Aliquota %</Form.Label>
                            <Form.Control
                              type="number"
                              min="0"
                              max="100"
                              step="0.01"
                              value={datiRitenuta.aliquotaRitenuta}
                              onChange={e =>
                                setDatiRitenuta({
                                  ...datiRitenuta,
                                  aliquotaRitenuta:
                                    parseFloat(e.target.value) || 0
                                })
                              }
                            />
                          </Form.Group>
                        </Col>
                        <Col md={3}>
                          <Form.Group className="mb-3">
                            <Form.Label>Importo €</Form.Label>
                            <Form.Control
                              type="number"
                              min="0"
                              step="0.01"
                              value={datiRitenuta.importoRitenuta}
                              onChange={e =>
                                setDatiRitenuta({
                                  ...datiRitenuta,
                                  importoRitenuta:
                                    parseFloat(e.target.value) || 0
                                })
                              }
                            />
                          </Form.Group>
                        </Col>
                        <Col md={2}>
                          <Form.Group className="mb-3">
                            <Form.Label>Causale</Form.Label>
                            <Form.Control
                              type="text"
                              maxLength={2}
                              value={datiRitenuta.causalePagamento || ''}
                              onChange={e =>
                                setDatiRitenuta({
                                  ...datiRitenuta,
                                  causalePagamento: e.target.value.toUpperCase()
                                })
                              }
                              placeholder="A"
                            />
                          </Form.Group>
                        </Col>
                      </Row>
                    </Card.Body>
                  )}
                </Card>

                {/* Stamp Duty Section */}
                <Card className="mb-3">
                  <Card.Header className="bg-body-tertiary">
                    <Form.Check
                      type="switch"
                      id="enableBollo"
                      label={<strong>Bollo Virtuale</strong>}
                      checked={enableBollo}
                      onChange={e => setEnableBollo(e.target.checked)}
                    />
                    <Form.Text className="text-muted d-block mt-1">
                      Obbligatorio per fatture esenti/escluse IVA superiori a
                      €77,47
                    </Form.Text>
                  </Card.Header>
                  {enableBollo && (
                    <Card.Body>
                      <Row>
                        <Col md={4}>
                          <Form.Group className="mb-3">
                            <Form.Label>Importo Bollo €</Form.Label>
                            <Form.Control
                              type="number"
                              min="0"
                              step="0.01"
                              value={datiBollo.importoBollo}
                              onChange={e =>
                                setDatiBollo({
                                  importoBollo:
                                    parseFloat(e.target.value) || 2.0
                                })
                              }
                            />
                          </Form.Group>
                        </Col>
                      </Row>
                    </Card.Body>
                  )}
                </Card>

                {/* Social Security Fund Section */}
                <Card className="mb-3">
                  <Card.Header className="bg-body-tertiary">
                    <Form.Check
                      type="switch"
                      id="enableCassa"
                      label={<strong>Cassa Previdenziale</strong>}
                      checked={enableCassa}
                      onChange={e => setEnableCassa(e.target.checked)}
                    />
                    <Form.Text className="text-muted d-block mt-1">
                      Contributo cassa previdenza per professionisti
                    </Form.Text>
                  </Card.Header>
                  {enableCassa && (
                    <Card.Body>
                      <Row>
                        <Col md={4}>
                          <Form.Group className="mb-3">
                            <Form.Label>Tipo Cassa</Form.Label>
                            <Form.Select
                              value={datiCassa.tipoCassa}
                              onChange={e =>
                                setDatiCassa({
                                  ...datiCassa,
                                  tipoCassa: e.target.value as TipoCassa
                                })
                              }
                            >
                              {TIPO_CASSA_OPTIONS.map(tc => (
                                <option key={tc.value} value={tc.value}>
                                  {tc.label}
                                </option>
                              ))}
                            </Form.Select>
                          </Form.Group>
                        </Col>
                        <Col md={2}>
                          <Form.Group className="mb-3">
                            <Form.Label>Aliquota %</Form.Label>
                            <Form.Control
                              type="number"
                              min="0"
                              max="100"
                              step="0.01"
                              value={datiCassa.alCassa}
                              onChange={e =>
                                setDatiCassa({
                                  ...datiCassa,
                                  alCassa: parseFloat(e.target.value) || 0
                                })
                              }
                            />
                          </Form.Group>
                        </Col>
                        <Col md={3}>
                          <Form.Group className="mb-3">
                            <Form.Label>Importo €</Form.Label>
                            <Form.Control
                              type="number"
                              min="0"
                              step="0.01"
                              value={datiCassa.importoContributoCassa}
                              onChange={e =>
                                setDatiCassa({
                                  ...datiCassa,
                                  importoContributoCassa:
                                    parseFloat(e.target.value) || 0
                                })
                              }
                            />
                          </Form.Group>
                        </Col>
                        <Col md={3}>
                          <Form.Group className="mb-3">
                            <Form.Label>Aliquota IVA %</Form.Label>
                            <Form.Select
                              value={datiCassa.aliquotaIVA}
                              onChange={e =>
                                setDatiCassa({
                                  ...datiCassa,
                                  aliquotaIVA: parseFloat(e.target.value)
                                })
                              }
                              disabled={isForfettario}
                            >
                              {VAT_RATES.map(rate => (
                                <option key={rate} value={rate}>
                                  {rate}%
                                </option>
                              ))}
                            </Form.Select>
                          </Form.Group>
                        </Col>
                      </Row>
                      {datiCassa.aliquotaIVA === 0 && (
                        <Row>
                          <Col md={4}>
                            <Form.Group className="mb-3">
                              <Form.Label>Natura IVA</Form.Label>
                              <Form.Select
                                value={datiCassa.natura || ''}
                                onChange={e =>
                                  setDatiCassa({
                                    ...datiCassa,
                                    natura:
                                      (e.target.value as VATNature) || undefined
                                  })
                                }
                                disabled={isForfettario}
                              >
                                <option value="">Seleziona...</option>
                                {VAT_NATURES.map(n => (
                                  <option key={n.value} value={n.value}>
                                    {n.label}
                                  </option>
                                ))}
                              </Form.Select>
                            </Form.Group>
                          </Col>
                        </Row>
                      )}
                    </Card.Body>
                  )}
                </Card>
              </Tab.Pane>

              {/* Payment Tab */}
              <Tab.Pane eventKey="payment">
                {/* Company payment data auto-fill */}
                <Card className="mb-3 border-info">
                  <Card.Header className="bg-body-tertiary">
                    <strong>Precompila dati bancari</strong>
                  </Card.Header>
                  <Card.Body>
                    <Form.Group>
                      <Form.Label>Seleziona azienda per precompilare</Form.Label>
                      <Form.Select
                        value={selectedPaymentCompanyId}
                        onChange={e =>
                          handlePaymentCompanySelect(e.target.value)
                        }
                      >
                        <option value="">
                          -- Seleziona per precompilare i dati --
                        </option>
                        {companiesData?.companies
                          ?.filter(c => c.isActive)
                          .map(company => (
                            <option key={company.id} value={company.id}>
                              {company.denomination}
                              {company.iban
                                ? ` - ${company.iban.slice(0, 4)}...${company.iban.slice(-4)}`
                                : ' (Dati bancari non configurati)'}
                            </option>
                          ))}
                      </Form.Select>
                      <Form.Text className="text-muted">
                        I campi sottostanti verranno compilati automaticamente.
                      </Form.Text>
                    </Form.Group>
                  </Card.Body>
                </Card>

                <Row>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>Condizione di Pagamento</Form.Label>
                      <Form.Select
                        value={paymentCondition}
                        onChange={e =>
                          setPaymentCondition(
                            e.target.value as PaymentCondition | ''
                          )
                        }
                      >
                        <option value="">Seleziona...</option>
                        {PAYMENT_CONDITIONS.map(pc => (
                          <option key={pc.value} value={pc.value}>
                            {pc.label}
                          </option>
                        ))}
                      </Form.Select>
                    </Form.Group>
                  </Col>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>Metodo di Pagamento</Form.Label>
                      <Form.Select
                        value={paymentMethod}
                        onChange={e =>
                          setPaymentMethod(
                            e.target.value as PaymentMethod | ''
                          )
                        }
                      >
                        <option value="">Seleziona...</option>
                        {PAYMENT_METHODS.map(pm => (
                          <option key={pm.value} value={pm.value}>
                            {pm.label}
                          </option>
                        ))}
                      </Form.Select>
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>Beneficiario</Form.Label>
                      <Form.Control
                        type="text"
                        value={paymentBeneficiario}
                        onChange={e => setPaymentBeneficiario(e.target.value)}
                        placeholder="Nome del beneficiario del pagamento"
                      />
                    </Form.Group>
                  </Col>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>Istituto Finanziario</Form.Label>
                      <Form.Control
                        type="text"
                        value={paymentIstituto}
                        onChange={e => setPaymentIstituto(e.target.value)}
                        placeholder="Nome della banca"
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>IBAN</Form.Label>
                      <Form.Control
                        type="text"
                        value={paymentIban}
                        onChange={e =>
                          setPaymentIban(e.target.value.toUpperCase())
                        }
                        placeholder="es. IT60X0542811101000000123456"
                        maxLength={34}
                      />
                    </Form.Group>
                  </Col>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>BIC/SWIFT</Form.Label>
                      <Form.Control
                        type="text"
                        value={paymentBic}
                        onChange={e =>
                          setPaymentBic(e.target.value.toUpperCase())
                        }
                        placeholder="es. UNCRITM1XXX"
                        maxLength={11}
                      />
                    </Form.Group>
                  </Col>
                </Row>

                <Row>
                  <Col md={3}>
                    <Form.Group className="mb-3">
                      <Form.Label>ABI</Form.Label>
                      <Form.Control
                        type="text"
                        value={paymentAbi}
                        onChange={e =>
                          setPaymentAbi(
                            e.target.value.replace(/\D/g, '').slice(0, 5)
                          )
                        }
                        placeholder="12345"
                        maxLength={5}
                      />
                      <Form.Text className="text-muted">
                        Codice banca (5 cifre)
                      </Form.Text>
                    </Form.Group>
                  </Col>
                  <Col md={3}>
                    <Form.Group className="mb-3">
                      <Form.Label>CAB</Form.Label>
                      <Form.Control
                        type="text"
                        value={paymentCab}
                        onChange={e =>
                          setPaymentCab(
                            e.target.value.replace(/\D/g, '').slice(0, 5)
                          )
                        }
                        placeholder="67890"
                        maxLength={5}
                      />
                      <Form.Text className="text-muted">
                        Codice filiale (5 cifre)
                      </Form.Text>
                    </Form.Group>
                  </Col>
                  <Col md={6}>
                    <Form.Group className="mb-3">
                      <Form.Label>Scadenza Pagamento</Form.Label>
                      <Form.Control
                        type="date"
                        value={paymentDueDate}
                        onChange={e => setPaymentDueDate(e.target.value)}
                      />
                    </Form.Group>
                  </Col>
                </Row>
              </Tab.Pane>

              {/* Options Tab */}
              <Tab.Pane eventKey="options">
                <Form.Group className="mb-3">
                  <Form.Check
                    type="switch"
                    id="signatureEnabled"
                    label="Applica Firma Digitale"
                    checked={signatureEnabled}
                    onChange={e => setSignatureEnabled(e.target.checked)}
                  />
                  <Form.Text className="text-muted">
                    La fattura verrà firmata digitalmente prima dell'invio al
                    SDI
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Check
                    type="switch"
                    id="legalStorageEnabled"
                    label="Conservazione Sostitutiva"
                    checked={legalStorageEnabled}
                    onChange={e => setLegalStorageEnabled(e.target.checked)}
                  />
                  <Form.Text className="text-muted">
                    La fattura verrà conservata a norma di legge per 10 anni
                  </Form.Text>
                </Form.Group>

                <Form.Group className="mb-3">
                  <Form.Label>Note Interne</Form.Label>
                  <Form.Control
                    as="textarea"
                    rows={3}
                    value={internalNotes}
                    onChange={e => setInternalNotes(e.target.value)}
                    placeholder="Note visibili solo internamente (non inviate al SDI)"
                  />
                </Form.Group>
              </Tab.Pane>
            </Tab.Content>
          </Tab.Container>
        </Card.Body>
      </Card>

      {/* Action Buttons */}
      <Card>
        <Card.Body className="d-flex justify-content-between">
          <Button
            variant="falcon-default"
            onClick={() => navigate('/billing/invoices/issued')}
            disabled={isLoading}
          >
            Annulla
          </Button>
          <div>
            <Button
              variant="falcon-primary"
              className="me-2"
              onClick={handleSaveDraft}
              disabled={isLoading}
            >
              <FontAwesomeIcon icon={faSave} className="me-1" />
              {isCreating ? 'Salvataggio...' : 'Salva Bozza'}
            </Button>
            <Button
              variant="primary"
              onClick={handleSaveAndSend}
              disabled={isLoading}
            >
              <FontAwesomeIcon icon={faPaperPlane} className="me-1" />
              {isSending ? 'Invio...' : 'Salva e Invia al SDI'}
            </Button>
          </div>
        </Card.Body>
      </Card>
    </>
  );
};

export default NewIssuedInvoice;
