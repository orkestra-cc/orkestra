import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
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
  InputGroup,
  Spinner,
} from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faPlus,
  faTrash,
  faSave,
  faPaperPlane,
  faArrowLeft,
  faEdit,
  faEye,
  faFileCode,
  faDownload,
  faFilePdf,
  faRotateLeft,
} from '@fortawesome/free-solid-svg-icons';
import {
  useGetInvoiceQuery,
  useUpdateInvoiceMutation,
  useSendInvoiceMutation,
  useDeleteInvoiceMutation,
  useLazyGetInvoiceXmlQuery,
  useLazyGetInvoiceHtmlQuery,
  useLazyGetInvoicePdfQuery,
  useGetCompaniesQuery,
} from 'store/api/billingApi';
import type {
  CreateInvoiceLineInput,
  CreatePaymentTermsInput,
  UpdateInvoiceInput,
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
} from 'types/billing';
import {
  DOCUMENT_TYPE_LABELS,
  INVOICE_STATUS_LABELS,
  SDI_STATUS_LABELS,
  PAYMENT_METHOD_LABELS,
  PAYMENT_CONDITION_LABELS,
  UNIT_OF_MEASURE_LABELS,
  TIPO_RITENUTA_LABELS,
  TIPO_CASSA_LABELS,
  formatCurrency,
  formatItalianDate,
  getPartyDisplayName,
} from 'types/billing';
import PageHeader from 'components/common/PageHeader';
import FalconCardHeader from 'components/common/FalconCardHeader';

// Payment method options
const PAYMENT_METHODS: { value: PaymentMethod; label: string }[] = [
  { value: 'MP01', label: 'MP01 - Contanti' },
  { value: 'MP02', label: 'MP02 - Assegno' },
  { value: 'MP05', label: 'MP05 - Bonifico' },
  { value: 'MP08', label: 'MP08 - Carta di pagamento' },
  { value: 'MP12', label: 'MP12 - RIBA' },
  { value: 'MP19', label: 'MP19 - SEPA Direct Debit' },
  { value: 'MP23', label: 'MP23 - PagoPA' },
];

// Payment condition options
const PAYMENT_CONDITIONS: { value: PaymentCondition; label: string }[] = [
  { value: 'TP01', label: 'TP01 - Pagamento a rate' },
  { value: 'TP02', label: 'TP02 - Pagamento completo' },
  { value: 'TP03', label: 'TP03 - Anticipo' },
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
  { value: 'MESE', label: 'MESE - Mese' },
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
  { value: 'N6.9', label: 'N6.9 - Reverse charge (altri casi)' },
];

// Withholding tax type options
const TIPO_RITENUTA_OPTIONS: { value: TipoRitenuta; label: string }[] = [
  { value: 'RT01', label: `RT01 - ${TIPO_RITENUTA_LABELS['RT01']}` },
  { value: 'RT02', label: `RT02 - ${TIPO_RITENUTA_LABELS['RT02']}` },
  { value: 'RT03', label: `RT03 - ${TIPO_RITENUTA_LABELS['RT03']}` },
  { value: 'RT04', label: `RT04 - ${TIPO_RITENUTA_LABELS['RT04']}` },
  { value: 'RT05', label: `RT05 - ${TIPO_RITENUTA_LABELS['RT05']}` },
  { value: 'RT06', label: `RT06 - ${TIPO_RITENUTA_LABELS['RT06']}` },
];

// Social security fund type options (common ones)
const TIPO_CASSA_OPTIONS: { value: TipoCassa; label: string }[] = [
  { value: 'TC01', label: `TC01 - ${TIPO_CASSA_LABELS['TC01']}` },
  { value: 'TC02', label: `TC02 - ${TIPO_CASSA_LABELS['TC02']}` },
  { value: 'TC03', label: `TC03 - ${TIPO_CASSA_LABELS['TC03']}` },
  { value: 'TC04', label: `TC04 - ${TIPO_CASSA_LABELS['TC04']}` },
  { value: 'TC05', label: `TC05 - ${TIPO_CASSA_LABELS['TC05']}` },
  { value: 'TC22', label: `TC22 - ${TIPO_CASSA_LABELS['TC22']}` },
];

// Convert date string (YYYY-MM-DD) to RFC 3339 datetime (YYYY-MM-DDTHH:mm:ssZ)
const toRFC3339 = (dateStr: string): string => {
  return `${dateStr}T00:00:00Z`;
};

// Parse RFC 3339 to YYYY-MM-DD for form input
const fromRFC3339 = (dateStr: string): string => {
  if (!dateStr) return '';
  return dateStr.split('T')[0];
};

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
  altriDatiGestionali: [],
});

// Create empty AltriDatiGestionali entry
const createEmptyAltriDati = (): AltriDatiGestionali => ({
  tipoDato: '',
  riferimentoTesto: '',
  riferimentoNumero: undefined,
  riferimentoData: undefined,
});

const IssuedInvoiceDetail: React.FC = () => {
  const { invoiceId } = useParams<{ invoiceId: string }>();
  const navigate = useNavigate();

  // API hooks
  const {
    data: invoice,
    isLoading: isLoadingInvoice,
    error: loadError,
    refetch,
  } = useGetInvoiceQuery(invoiceId!, { skip: !invoiceId });
  const [updateInvoice, { isLoading: isUpdating }] = useUpdateInvoiceMutation();
  const [sendInvoice, { isLoading: isSending }] = useSendInvoiceMutation();
  const [deleteInvoice, { isLoading: isDeleting }] = useDeleteInvoiceMutation();
  const [getInvoiceXml] = useLazyGetInvoiceXmlQuery();
  const [getInvoiceHtml] = useLazyGetInvoiceHtmlQuery();
  const [getInvoicePdf] = useLazyGetInvoicePdfQuery();
  const { data: companiesData } = useGetCompaniesQuery({ pageSize: 100 });

  // UI state
  const [isEditMode, setIsEditMode] = useState(false);
  const [activeTab, setActiveTab] = useState('document');
  const [error, setError] = useState<string>('');
  const [success, setSuccess] = useState<string>('');

  // Form state (initialized from invoice data)
  const [number, setNumber] = useState('');
  const [date, setDate] = useState('');
  const [lines, setLines] = useState<CreateInvoiceLineInput[]>([]);
  const [causale, setCausale] = useState<string[]>(['']);
  const [internalNotes, setInternalNotes] = useState('');

  // Payment terms
  const [paymentCondition, setPaymentCondition] = useState<PaymentCondition>('TP02');
  const [paymentMethod, setPaymentMethod] = useState<PaymentMethod>('MP05');
  const [paymentIban, setPaymentIban] = useState('');
  const [paymentDueDate, setPaymentDueDate] = useState('');
  const [paymentBeneficiario, setPaymentBeneficiario] = useState('');
  const [paymentIstituto, setPaymentIstituto] = useState('');
  const [paymentBic, setPaymentBic] = useState('');
  const [paymentAbi, setPaymentAbi] = useState('');
  const [paymentCab, setPaymentCab] = useState('');
  const [selectedPaymentCompanyId, setSelectedPaymentCompanyId] = useState('');

  // FatturaPA additional fields
  const [enableRitenuta, setEnableRitenuta] = useState(false);
  const [datiRitenuta, setDatiRitenuta] = useState<DatiRitenuta>({
    tipoRitenuta: 'RT01',
    importoRitenuta: 0,
    aliquotaRitenuta: 20,
    causalePagamento: 'A',
  });
  const [enableBollo, setEnableBollo] = useState(false);
  const [datiBollo, setDatiBollo] = useState<DatiBollo>({
    importoBollo: 2.0,
  });
  const [enableCassa, setEnableCassa] = useState(false);
  const [datiCassa, setDatiCassa] = useState<DatiCassa>({
    tipoCassa: 'TC22',
    alCassa: 4,
    importoContributoCassa: 0,
    aliquotaIVA: 22,
  });

  // Check if invoice is editable (only draft status)
  const isDraft = invoice?.status === 'draft';
  const canEdit = isDraft;
  const isLoading = isLoadingInvoice || isUpdating || isSending || isDeleting;

  // Initialize form state from invoice
  useEffect(() => {
    if (invoice) {
      setNumber(invoice.number);
      setDate(fromRFC3339(invoice.date));
      setLines(
        (invoice.lines || []).map((line) => ({
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
        }))
      );
      setCausale(invoice.causale?.length ? invoice.causale : ['']);
      setInternalNotes(invoice.internalNotes || '');

      if (invoice.paymentTerms) {
        setPaymentCondition(invoice.paymentTerms.condition);
        setPaymentMethod(invoice.paymentTerms.paymentMethod);
        setPaymentIban(invoice.paymentTerms.iban || '');
        setPaymentDueDate(fromRFC3339(invoice.paymentTerms.dueDate || ''));
        setPaymentBeneficiario(invoice.paymentTerms.beneficiario || '');
        setPaymentIstituto(invoice.paymentTerms.istitutoFinanziario || '');
        setPaymentBic(invoice.paymentTerms.bic || '');
        setPaymentAbi(invoice.paymentTerms.abi || '');
        setPaymentCab(invoice.paymentTerms.cab || '');
      }

      // FatturaPA additional fields
      if (invoice.datiRitenuta && invoice.datiRitenuta.length > 0) {
        setEnableRitenuta(true);
        setDatiRitenuta(invoice.datiRitenuta[0]);
      } else {
        setEnableRitenuta(false);
      }

      if (invoice.datiBollo) {
        setEnableBollo(true);
        setDatiBollo(invoice.datiBollo);
      } else {
        setEnableBollo(false);
      }

      if (invoice.datiCassaPrevidenziale && invoice.datiCassaPrevidenziale.length > 0) {
        setEnableCassa(true);
        setDatiCassa(invoice.datiCassaPrevidenziale[0]);
      } else {
        setEnableCassa(false);
      }
    }
  }, [invoice]);

  // Calculate totals
  const calculateLineTotals = (line: CreateInvoiceLineInput) => {
    const totalPrice = line.quantity * line.unitPrice;
    const vatAmount = totalPrice * (line.vatRate / 100);
    return { totalPrice, vatAmount };
  };

  const totals = lines.reduce(
    (acc, line) => {
      const { totalPrice, vatAmount } = calculateLineTotals(line);
      return {
        taxable: acc.taxable + totalPrice,
        vat: acc.vat + vatAmount,
        total: acc.total + totalPrice + vatAmount,
      };
    },
    { taxable: 0, vat: 0, total: 0 }
  );

  // Line handlers
  const handleAddLine = () => {
    setLines([...lines, createEmptyLine()]);
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
  const handleAddCausale = () => {
    setCausale([...causale, '']);
  };

  const handleRemoveCausale = (index: number) => {
    if (causale.length > 1) {
      setCausale(causale.filter((_, i) => i !== index));
    }
  };

  const handleCausaleChange = (index: number, value: string) => {
    const newCausale = [...causale];
    newCausale[index] = value;
    setCausale(newCausale);
  };

  // AltriDatiGestionali handlers
  const handleAddAltriDati = (lineIndex: number) => {
    const newLines = [...lines];
    const currentAltriDati = newLines[lineIndex].altriDatiGestionali || [];
    newLines[lineIndex] = {
      ...newLines[lineIndex],
      altriDatiGestionali: [...currentAltriDati, createEmptyAltriDati()],
    };
    setLines(newLines);
  };

  const handleRemoveAltriDati = (lineIndex: number, adgIndex: number) => {
    const newLines = [...lines];
    const currentAltriDati = newLines[lineIndex].altriDatiGestionali || [];
    newLines[lineIndex] = {
      ...newLines[lineIndex],
      altriDatiGestionali: currentAltriDati.filter((_, i) => i !== adgIndex),
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
    const currentAltriDati = [...(newLines[lineIndex].altriDatiGestionali || [])];
    currentAltriDati[adgIndex] = { ...currentAltriDati[adgIndex], [field]: value };
    newLines[lineIndex] = {
      ...newLines[lineIndex],
      altriDatiGestionali: currentAltriDati,
    };
    setLines(newLines);
  };

  // Payment company auto-fill handler
  const handlePaymentCompanySelect = (companyId: string) => {
    setSelectedPaymentCompanyId(companyId);
    if (!companyId) return;

    const company = companiesData?.companies?.find(c => c.id === companyId);
    if (!company) return;

    // Auto-populate payment fields (beneficiario uses denomination as fallback)
    setPaymentBeneficiario(company.beneficiario || company.denomination);
    if (company.istitutoFinanziario) setPaymentIstituto(company.istitutoFinanziario);
    if (company.iban) setPaymentIban(company.iban.toUpperCase());
    if (company.bic) setPaymentBic(company.bic.toUpperCase());
    if (company.abi) setPaymentAbi(company.abi);
    if (company.cab) setPaymentCab(company.cab);
  };

  // Validation
  const validate = (): boolean => {
    if (!number.trim()) {
      setError('Il numero fattura è obbligatorio');
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

  // Build update input
  const buildUpdateInput = (): UpdateInvoiceInput => {
    const paymentTerms: CreatePaymentTermsInput | undefined =
      paymentMethod
        ? {
            condition: paymentCondition,
            paymentMethod: paymentMethod,
            iban: paymentIban || undefined,
            dueDate: paymentDueDate ? toRFC3339(paymentDueDate) : undefined,
            beneficiario: paymentBeneficiario || undefined,
            istitutoFinanziario: paymentIstituto || undefined,
            bic: paymentBic || undefined,
            abi: paymentAbi || undefined,
            cab: paymentCab || undefined,
          }
        : undefined;

    return {
      number,
      date: toRFC3339(date),
      lines: lines.map((line) => ({
        ...line,
        vatNature: line.vatRate === 0 ? line.vatNature : undefined,
      })),
      paymentTerms,
      causale: causale.filter((c) => c.trim()),
      internalNotes: internalNotes || undefined,
      datiRitenuta: enableRitenuta ? [datiRitenuta] : undefined,
      datiBollo: enableBollo ? datiBollo : undefined,
      datiCassaPrevidenziale: enableCassa ? [datiCassa] : undefined,
    };
  };

  // Save changes
  const handleSave = async () => {
    setError('');
    setSuccess('');

    if (!validate()) return;

    try {
      const input = buildUpdateInput();
      await updateInvoice({ id: invoiceId!, data: input }).unwrap();
      setSuccess('Fattura aggiornata con successo');
      setIsEditMode(false);
      refetch();
    } catch (err: unknown) {
      const errorMessage =
        err && typeof err === 'object' && 'data' in err
          ? (err as { data?: { message?: string } }).data?.message
          : undefined;
      setError(errorMessage || 'Errore durante il salvataggio della fattura');
    }
  };

  // Send to SDI
  const handleSendToSDI = async () => {
    setError('');
    setSuccess('');

    if (!window.confirm('Sei sicuro di voler inviare questa fattura al SDI? Una volta inviata non potrà più essere modificata.')) {
      return;
    }

    try {
      await sendInvoice(invoiceId!).unwrap();
      setSuccess('Fattura inviata al SDI con successo');
      refetch();
    } catch (err: unknown) {
      const errorMessage =
        err && typeof err === 'object' && 'data' in err
          ? (err as { data?: { message?: string } }).data?.message
          : undefined;
      setError(errorMessage || 'Errore durante l\'invio della fattura al SDI');
    }
  };

  // Delete invoice
  const handleDelete = async () => {
    setError('');
    setSuccess('');

    if (!window.confirm('Sei sicuro di voler eliminare questa fattura? L\'operazione non può essere annullata.')) {
      return;
    }

    try {
      await deleteInvoice(invoiceId!).unwrap();
      setSuccess('Fattura eliminata con successo');
      setTimeout(() => navigate('/billing/invoices/issued'), 1500);
    } catch (err: unknown) {
      const errorMessage =
        err && typeof err === 'object' && 'data' in err
          ? (err as { data?: { message?: string } }).data?.message
          : undefined;
      setError(errorMessage || 'Errore durante l\'eliminazione della fattura');
    }
  };

  // View XML
  const handleViewXml = async () => {
    try {
      const result = await getInvoiceXml(invoiceId!).unwrap();
      // Use TextEncoder to ensure proper UTF-8 encoding
      const encoder = new TextEncoder();
      const utf8Bytes = encoder.encode(result);
      const blob = new Blob([utf8Bytes], { type: 'application/xml; charset=utf-8' });
      const url = window.URL.createObjectURL(blob);
      window.open(url, '_blank');
    } catch {
      setError('Errore durante il caricamento del file XML');
    }
  };

  // Download XML file
  // Generate FatturaPA compliant filename: {CountryCode}{IdCodice}_{ProgressivoInvio}.xml
  // IdCodice = P.IVA (fiscalIdCode), per FatturaPA specifications
  const generateFatturaFilename = () => {
    const cedente = invoice?.cedentePrestatore;
    if (!cedente) {
      return `fattura_${invoiceId}.xml`;
    }
    // Always use fiscalIdCode (P.IVA) for filename - FatturaPA spec requirement
    const fiscalCode = cedente.fiscalIdCode;
    if (!fiscalCode) {
      return `fattura_${invoiceId}.xml`;
    }
    const countryCode = cedente.fiscalIdCountry || 'IT';
    // Progressive: use last 5 chars of progressivoInvio (filename limit is 5, XML allows 10)
    const rawProgressive = invoice.progressivoInvio ||
      invoiceId?.slice(-5) ||
      String(invoice.number).replace(/\D/g, '').slice(-5).padStart(5, '0') ||
      '00001';
    // FatturaPA filename spec: progressive max 5 alphanumeric chars
    const progressive = rawProgressive.slice(-5);
    return `${countryCode}${fiscalCode}_${progressive}.xml`;
  };

  const handleDownloadXml = async () => {
    try {
      const result = await getInvoiceXml(invoiceId!).unwrap();
      // Use TextEncoder to ensure proper UTF-8 encoding
      const encoder = new TextEncoder();
      const utf8Bytes = encoder.encode(result);
      const blob = new Blob([utf8Bytes], { type: 'application/xml; charset=utf-8' });
      const url = window.URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = generateFatturaFilename();
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      window.URL.revokeObjectURL(url);
    } catch {
      setError('Errore durante il download del file XML');
    }
  };

  // View HTML preview
  const handleViewHtml = async () => {
    try {
      const result = await getInvoiceHtml(invoiceId!).unwrap();
      const blob = new Blob([result], { type: 'text/html' });
      const url = window.URL.createObjectURL(blob);
      window.open(url, '_blank');
    } catch {
      setError('Errore durante il caricamento dell\'anteprima');
    }
  };

  // Generate PDF filename
  const generatePdfFilename = () => {
    const cedente = invoice?.cedentePrestatore;
    const invoiceNumber = invoice?.number || invoiceId;
    if (!cedente) {
      return `fattura_${invoiceNumber}.pdf`;
    }
    const fiscalCode = cedente.codiceFiscale || cedente.fiscalIdCode;
    if (!fiscalCode) {
      return `fattura_${invoiceNumber}.pdf`;
    }
    const countryCode = cedente.fiscalIdCountry || 'IT';
    const progressive = invoice?.progressivoInvio ||
      invoiceId?.slice(-5) ||
      String(invoice?.number).replace(/\D/g, '').slice(-5).padStart(5, '0') ||
      '00001';
    return `${countryCode}${fiscalCode}_${progressive}.pdf`;
  };

  // Download PDF
  const handleDownloadPdf = async () => {
    try {
      await getInvoicePdf({ id: invoiceId!, filename: generatePdfFilename() }).unwrap();
    } catch {
      setError('Errore durante il download del PDF');
    }
  };

  // Cancel edit mode
  const handleCancelEdit = () => {
    setIsEditMode(false);
    // Reset form to original values
    if (invoice) {
      setNumber(invoice.number);
      setDate(fromRFC3339(invoice.date));
      setLines(
        (invoice.lines || []).map((line) => ({
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
        }))
      );
      setCausale(invoice.causale?.length ? invoice.causale : ['']);
      setInternalNotes(invoice.internalNotes || '');

      if (invoice.paymentTerms) {
        setPaymentCondition(invoice.paymentTerms.condition);
        setPaymentMethod(invoice.paymentTerms.paymentMethod);
        setPaymentIban(invoice.paymentTerms.iban || '');
        setPaymentDueDate(fromRFC3339(invoice.paymentTerms.dueDate || ''));
        setPaymentBeneficiario(invoice.paymentTerms.beneficiario || '');
        setPaymentIstituto(invoice.paymentTerms.istitutoFinanziario || '');
        setPaymentBic(invoice.paymentTerms.bic || '');
        setPaymentAbi(invoice.paymentTerms.abi || '');
        setPaymentCab(invoice.paymentTerms.cab || '');
      }

      // Reset FatturaPA fields
      if (invoice.datiRitenuta && invoice.datiRitenuta.length > 0) {
        setEnableRitenuta(true);
        setDatiRitenuta(invoice.datiRitenuta[0]);
      } else {
        setEnableRitenuta(false);
        setDatiRitenuta({ tipoRitenuta: 'RT01', importoRitenuta: 0, aliquotaRitenuta: 20, causalePagamento: 'A' });
      }

      if (invoice.datiBollo) {
        setEnableBollo(true);
        setDatiBollo(invoice.datiBollo);
      } else {
        setEnableBollo(false);
        setDatiBollo({ importoBollo: 2.0 });
      }

      if (invoice.datiCassaPrevidenziale && invoice.datiCassaPrevidenziale.length > 0) {
        setEnableCassa(true);
        setDatiCassa(invoice.datiCassaPrevidenziale[0]);
      } else {
        setEnableCassa(false);
        setDatiCassa({ tipoCassa: 'TC22', alCassa: 4, importoContributoCassa: 0, aliquotaIVA: 22 });
      }
    }
    setError('');
  };

  // Loading state
  if (isLoadingInvoice) {
    return (
      <div className="d-flex justify-content-center align-items-center" style={{ minHeight: '400px' }}>
        <Spinner animation="border" role="status">
          <span className="visually-hidden">Caricamento...</span>
        </Spinner>
      </div>
    );
  }

  // Error state
  if (loadError) {
    return (
      <Alert variant="danger">
        Errore durante il caricamento della fattura. Riprova più tardi.
      </Alert>
    );
  }

  // Not found state
  if (!invoice) {
    return (
      <Alert variant="warning">
        Fattura non trovata.
      </Alert>
    );
  }

  // Debug: Check invoice structure - can be removed after fixing
  if (process.env.NODE_ENV === 'development') {
    console.log('Invoice data:', invoice);
    if (!invoice.lines) {
      console.error('Invoice lines is null/undefined:', invoice);
    }
  }

  // Get customer display name
  const customerName = invoice.cessionarioCommittente
    ? getPartyDisplayName(invoice.cessionarioCommittente)
    : 'N/A';

  return (
    <>
      <PageHeader
        title={`Fattura ${invoice.number}`}
        description={`${DOCUMENT_TYPE_LABELS[invoice.documentType]} - ${INVOICE_STATUS_LABELS[invoice.status]}${invoice.sdiStatus ? ` (${SDI_STATUS_LABELS[invoice.sdiStatus]})` : ''}`}
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
        {!isEditMode && (
          <>
            {invoice?.status && !['draft', 'pending', 'cancelled'].includes(invoice.status) && (
              <Button
                variant="falcon-default"
                size="sm"
                className="me-2"
                onClick={handleViewHtml}
                title="Anteprima"
              >
                <FontAwesomeIcon icon={faEye} className="me-1" />
                Anteprima
              </Button>
            )}
            <Button
              variant="falcon-default"
              size="sm"
              className="me-2"
              onClick={handleViewXml}
              title="Visualizza XML"
            >
              <FontAwesomeIcon icon={faFileCode} className="me-1" />
              XML
            </Button>
            <Button
              variant="falcon-default"
              size="sm"
              className="me-2"
              onClick={handleDownloadXml}
              title="Scarica XML"
            >
              <FontAwesomeIcon icon={faDownload} className="me-1" />
              XML
            </Button>
            <Button
              variant="falcon-primary"
              size="sm"
              className="me-2"
              onClick={handleDownloadPdf}
              title="Scarica PDF"
            >
              <FontAwesomeIcon icon={faFilePdf} className="me-1" />
              PDF
            </Button>
            {invoice.status !== 'draft' && invoice.status !== 'cancelled' &&
              ['TD01','TD02','TD03','TD06','TD24','TD25'].includes(invoice.documentType) && (
              <Button
                variant="falcon-warning"
                size="sm"
                className="me-2"
                onClick={() => navigate(`/billing/invoices/issued/new?fromInvoice=${invoiceId}`)}
                title="Crea Nota di Credito"
              >
                <FontAwesomeIcon icon={faRotateLeft} className="me-1" />
                Nota di Credito
              </Button>
            )}
            {canEdit && (
              <Button
                variant="falcon-primary"
                size="sm"
                onClick={() => setIsEditMode(true)}
              >
                <FontAwesomeIcon icon={faEdit} className="me-1" />
                Modifica
              </Button>
            )}
          </>
        )}
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

      {/* Invoice Info Summary (Read-only) */}
      {!isEditMode && (
        <Card className="mb-3">
          <FalconCardHeader title="Riepilogo Fattura" light={false} />
          <Card.Body>
            <Row>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">Numero</small>
                  <div className="fw-bold">{invoice.number}</div>
                </div>
              </Col>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">Data</small>
                  <div className="fw-bold">{formatItalianDate(invoice.date)}</div>
                </div>
              </Col>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">Tipo Documento</small>
                  <div className="fw-bold">{DOCUMENT_TYPE_LABELS[invoice.documentType]}</div>
                </div>
              </Col>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">Cliente</small>
                  <div className="fw-bold">{customerName}</div>
                </div>
              </Col>
            </Row>
            <Row>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">Imponibile</small>
                  <div className="fw-bold">{formatCurrency(invoice.totalTaxableAmount)}</div>
                </div>
              </Col>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">IVA</small>
                  <div className="fw-bold">{formatCurrency(invoice.totalVatAmount)}</div>
                </div>
              </Col>
              <Col md={3}>
                <div className="mb-3">
                  <small className="text-muted">Totale</small>
                  <div className="fw-bold fs-5 text-primary">{formatCurrency(invoice.totalAmount)}</div>
                </div>
              </Col>
              <Col md={3}>
                {invoice.sdiIdentifier && (
                  <div className="mb-3">
                    <small className="text-muted">ID SDI</small>
                    <div className="fw-bold">{invoice.sdiIdentifier}</div>
                  </div>
                )}
              </Col>
            </Row>
            {invoice.causale && invoice.causale.length > 0 && (
              <Row>
                <Col>
                  <small className="text-muted">Causale</small>
                  {invoice.causale.map((c, idx) => (
                    <div key={idx}>{c}</div>
                  ))}
                </Col>
              </Row>
            )}
          </Card.Body>
        </Card>
      )}

      {/* Invoice Lines (Read-only view or Edit form) */}
      <Card className="mb-3">
        <FalconCardHeader
          title={isEditMode ? 'Modifica Fattura' : 'Dettaglio Righe'}
          light={false}
        />
        <Card.Body>
          {isEditMode ? (
            // Edit Mode - Form
            <Tab.Container activeKey={activeTab} onSelect={(k) => setActiveTab(k || 'document')}>
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
                  <Row>
                    <Col md={4}>
                      <Form.Group className="mb-3">
                        <Form.Label>Tipo Documento</Form.Label>
                        <Form.Control
                          type="text"
                          value={DOCUMENT_TYPE_LABELS[invoice.documentType]}
                          disabled
                        />
                        <Form.Text className="text-muted">
                          Il tipo documento non può essere modificato
                        </Form.Text>
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
                          onChange={(e) => setNumber(e.target.value)}
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
                          onChange={(e) => setDate(e.target.value)}
                        />
                      </Form.Group>
                    </Col>
                  </Row>

                  <Row>
                    <Col md={12}>
                      <Form.Group className="mb-3">
                        <Form.Label>Cliente</Form.Label>
                        <Form.Control type="text" value={customerName} disabled />
                        <Form.Text className="text-muted">
                          Il cliente non può essere modificato
                        </Form.Text>
                      </Form.Group>
                    </Col>
                  </Row>

                  <Form.Group className="mb-3">
                    <Form.Label>Causale / Descrizione</Form.Label>
                    {causale.map((c, index) => (
                      <InputGroup className="mb-2" key={index}>
                        <Form.Control
                          type="text"
                          value={c}
                          onChange={(e) => handleCausaleChange(index, e.target.value)}
                          placeholder="es. Consulenza informatica mese di gennaio 2026"
                          maxLength={200}
                        />
                        {causale.length > 1 && (
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
                                  onChange={(e) =>
                                    handleLineChange(index, 'description', e.target.value)
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
                                  onChange={(e) =>
                                    handleLineChange(index, 'productCode', e.target.value)
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
                                  onChange={(e) =>
                                    handleLineChange(index, 'quantity', parseFloat(e.target.value) || 0)
                                  }
                                />
                              </td>
                              <td>
                                <Form.Select
                                  size="sm"
                                  value={line.unitOfMeasure || ''}
                                  onChange={(e) =>
                                    handleLineChange(index, 'unitOfMeasure', e.target.value as UnitOfMeasure)
                                  }
                                >
                                  <option value="">-</option>
                                  {UNITS_OF_MEASURE.map((um) => (
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
                                  onChange={(e) =>
                                    handleLineChange(index, 'unitPrice', parseFloat(e.target.value) || 0)
                                  }
                                />
                              </td>
                              <td>
                                <Form.Select
                                  size="sm"
                                  value={line.vatRate}
                                  onChange={(e) =>
                                    handleLineChange(index, 'vatRate', parseFloat(e.target.value))
                                  }
                                >
                                  {VAT_RATES.map((rate) => (
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
                                    onChange={(e) =>
                                      handleLineChange(
                                        index,
                                        'vatNature',
                                        (e.target.value as VATNature) || undefined
                                      )
                                    }
                                  >
                                    <option value="">Seleziona...</option>
                                    {VAT_NATURES.map((n) => (
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
                                <strong>{formatCurrency(totalPrice)}</strong>
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
                              <td colSpan={10} className="bg-light p-2">
                                <div className="d-flex align-items-center gap-2 mb-2">
                                  <small className="text-muted fw-bold">Altri Dati Gestionali</small>
                                  <Button
                                    variant="outline-primary"
                                    size="sm"
                                    onClick={() => handleAddAltriDati(index)}
                                  >
                                    <FontAwesomeIcon icon={faPlus} className="me-1" />
                                    Aggiungi
                                  </Button>
                                </div>
                                {(line.altriDatiGestionali || []).length > 0 && (
                                  <div className="ps-2">
                                    {(line.altriDatiGestionali || []).map((adg, adgIndex) => (
                                      <Row key={adgIndex} className="mb-2 align-items-center g-2">
                                        <Col xs={2}>
                                          <Form.Control
                                            size="sm"
                                            type="text"
                                            maxLength={10}
                                            placeholder="Tipo Dato*"
                                            value={adg.tipoDato || ''}
                                            onChange={(e) =>
                                              handleAltriDatiChange(index, adgIndex, 'tipoDato', e.target.value)
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
                                            onChange={(e) =>
                                              handleAltriDatiChange(index, adgIndex, 'riferimentoTesto', e.target.value)
                                            }
                                          />
                                        </Col>
                                        <Col xs={2}>
                                          <Form.Control
                                            size="sm"
                                            type="number"
                                            step="0.01"
                                            placeholder="Rif. Numero"
                                            value={adg.riferimentoNumero ?? ''}
                                            onChange={(e) =>
                                              handleAltriDatiChange(
                                                index,
                                                adgIndex,
                                                'riferimentoNumero',
                                                e.target.value ? parseFloat(e.target.value) : undefined
                                              )
                                            }
                                          />
                                        </Col>
                                        <Col xs={2}>
                                          <Form.Control
                                            size="sm"
                                            type="date"
                                            value={adg.riferimentoData || ''}
                                            onChange={(e) =>
                                              handleAltriDatiChange(index, adgIndex, 'riferimentoData', e.target.value || undefined)
                                            }
                                          />
                                        </Col>
                                        <Col xs={2}>
                                          <Button
                                            variant="outline-danger"
                                            size="sm"
                                            onClick={() => handleRemoveAltriDati(index, adgIndex)}
                                          >
                                            <FontAwesomeIcon icon={faTrash} />
                                          </Button>
                                        </Col>
                                      </Row>
                                    ))}
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
                            <Button variant="falcon-primary" size="sm" onClick={handleAddLine}>
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
                            <td className="text-end">{formatCurrency(totals.taxable)}</td>
                          </tr>
                          <tr>
                            <td>IVA</td>
                            <td className="text-end">{formatCurrency(totals.vat)}</td>
                          </tr>
                          <tr className="fw-bold">
                            <td>Totale</td>
                            <td className="text-end">{formatCurrency(totals.total)}</td>
                          </tr>
                        </tbody>
                      </Table>
                    </Col>
                  </Row>
                </Tab.Pane>

                {/* Ritenute e Contributi Tab */}
                <Tab.Pane eventKey="ritenute">
                  {/* Ritenuta d'Acconto */}
                  <Card className="mb-3">
                    <Card.Header className="bg-body-tertiary d-flex align-items-center">
                      <Form.Check
                        type="switch"
                        id="enableRitenuta"
                        label="Ritenuta d'Acconto"
                        checked={enableRitenuta}
                        onChange={(e) => setEnableRitenuta(e.target.checked)}
                        className="mb-0"
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
                                onChange={(e) =>
                                  setDatiRitenuta({ ...datiRitenuta, tipoRitenuta: e.target.value as TipoRitenuta })
                                }
                              >
                                {TIPO_RITENUTA_OPTIONS.map((opt) => (
                                  <option key={opt.value} value={opt.value}>
                                    {opt.label}
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
                                onChange={(e) =>
                                  setDatiRitenuta({ ...datiRitenuta, aliquotaRitenuta: parseFloat(e.target.value) || 0 })
                                }
                              />
                            </Form.Group>
                          </Col>
                          <Col md={3}>
                            <Form.Group className="mb-3">
                              <Form.Label>Importo</Form.Label>
                              <Form.Control
                                type="number"
                                min="0"
                                step="0.01"
                                value={datiRitenuta.importoRitenuta}
                                onChange={(e) =>
                                  setDatiRitenuta({ ...datiRitenuta, importoRitenuta: parseFloat(e.target.value) || 0 })
                                }
                              />
                            </Form.Group>
                          </Col>
                          <Col md={2}>
                            <Form.Group className="mb-3">
                              <Form.Label>Causale</Form.Label>
                              <Form.Control
                                type="text"
                                value={datiRitenuta.causalePagamento || ''}
                                onChange={(e) =>
                                  setDatiRitenuta({ ...datiRitenuta, causalePagamento: e.target.value.toUpperCase() })
                                }
                                maxLength={2}
                                placeholder="A"
                              />
                            </Form.Group>
                          </Col>
                        </Row>
                      </Card.Body>
                    )}
                  </Card>

                  {/* Bollo Virtuale */}
                  <Card className="mb-3">
                    <Card.Header className="bg-body-tertiary d-flex align-items-center">
                      <Form.Check
                        type="switch"
                        id="enableBollo"
                        label="Bollo Virtuale"
                        checked={enableBollo}
                        onChange={(e) => setEnableBollo(e.target.checked)}
                        className="mb-0"
                      />
                    </Card.Header>
                    {enableBollo && (
                      <Card.Body>
                        <Row>
                          <Col md={4}>
                            <Form.Group className="mb-3">
                              <Form.Label>Importo Bollo</Form.Label>
                              <Form.Control
                                type="number"
                                min="0"
                                step="0.01"
                                value={datiBollo.importoBollo}
                                onChange={(e) =>
                                  setDatiBollo({ ...datiBollo, importoBollo: parseFloat(e.target.value) || 0 })
                                }
                              />
                              <Form.Text className="text-muted">
                                Solitamente €2.00 per fatture esenti IVA &gt; €77.47
                              </Form.Text>
                            </Form.Group>
                          </Col>
                        </Row>
                      </Card.Body>
                    )}
                  </Card>

                  {/* Cassa Previdenziale */}
                  <Card className="mb-3">
                    <Card.Header className="bg-body-tertiary d-flex align-items-center">
                      <Form.Check
                        type="switch"
                        id="enableCassa"
                        label="Cassa Previdenziale"
                        checked={enableCassa}
                        onChange={(e) => setEnableCassa(e.target.checked)}
                        className="mb-0"
                      />
                    </Card.Header>
                    {enableCassa && (
                      <Card.Body>
                        <Row>
                          <Col md={4}>
                            <Form.Group className="mb-3">
                              <Form.Label>Tipo Cassa</Form.Label>
                              <Form.Select
                                value={datiCassa.tipoCassa}
                                onChange={(e) =>
                                  setDatiCassa({ ...datiCassa, tipoCassa: e.target.value as TipoCassa })
                                }
                              >
                                {TIPO_CASSA_OPTIONS.map((opt) => (
                                  <option key={opt.value} value={opt.value}>
                                    {opt.label}
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
                                onChange={(e) =>
                                  setDatiCassa({ ...datiCassa, alCassa: parseFloat(e.target.value) || 0 })
                                }
                              />
                            </Form.Group>
                          </Col>
                          <Col md={3}>
                            <Form.Group className="mb-3">
                              <Form.Label>Importo</Form.Label>
                              <Form.Control
                                type="number"
                                min="0"
                                step="0.01"
                                value={datiCassa.importoContributoCassa}
                                onChange={(e) =>
                                  setDatiCassa({ ...datiCassa, importoContributoCassa: parseFloat(e.target.value) || 0 })
                                }
                              />
                            </Form.Group>
                          </Col>
                          <Col md={3}>
                            <Form.Group className="mb-3">
                              <Form.Label>Aliquota IVA %</Form.Label>
                              <Form.Select
                                value={datiCassa.aliquotaIVA}
                                onChange={(e) =>
                                  setDatiCassa({ ...datiCassa, aliquotaIVA: parseFloat(e.target.value) })
                                }
                              >
                                {VAT_RATES.map((rate) => (
                                  <option key={rate} value={rate}>
                                    {rate}%
                                  </option>
                                ))}
                              </Form.Select>
                            </Form.Group>
                          </Col>
                        </Row>
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
                          onChange={(e) => handlePaymentCompanySelect(e.target.value)}
                        >
                          <option value="">-- Seleziona per precompilare i dati --</option>
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
                          onChange={(e) => setPaymentCondition(e.target.value as PaymentCondition)}
                        >
                          {PAYMENT_CONDITIONS.map((pc) => (
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
                          onChange={(e) => setPaymentMethod(e.target.value as PaymentMethod)}
                        >
                          {PAYMENT_METHODS.map((pm) => (
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
                          onChange={(e) => setPaymentBeneficiario(e.target.value)}
                          placeholder="Nome del beneficiario"
                        />
                      </Form.Group>
                    </Col>
                    <Col md={6}>
                      <Form.Group className="mb-3">
                        <Form.Label>Istituto Finanziario</Form.Label>
                        <Form.Control
                          type="text"
                          value={paymentIstituto}
                          onChange={(e) => setPaymentIstituto(e.target.value)}
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
                          onChange={(e) => setPaymentIban(e.target.value.toUpperCase())}
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
                          onChange={(e) => setPaymentBic(e.target.value.toUpperCase())}
                          placeholder="es. BCITITMM"
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
                          onChange={(e) => setPaymentAbi(e.target.value)}
                          placeholder="es. 03069"
                          maxLength={5}
                        />
                      </Form.Group>
                    </Col>
                    <Col md={3}>
                      <Form.Group className="mb-3">
                        <Form.Label>CAB</Form.Label>
                        <Form.Control
                          type="text"
                          value={paymentCab}
                          onChange={(e) => setPaymentCab(e.target.value)}
                          placeholder="es. 05000"
                          maxLength={5}
                        />
                      </Form.Group>
                    </Col>
                    <Col md={6}>
                      <Form.Group className="mb-3">
                        <Form.Label>Scadenza Pagamento</Form.Label>
                        <Form.Control
                          type="date"
                          value={paymentDueDate}
                          onChange={(e) => setPaymentDueDate(e.target.value)}
                        />
                      </Form.Group>
                    </Col>
                  </Row>
                </Tab.Pane>

                {/* Options Tab */}
                <Tab.Pane eventKey="options">
                  <Form.Group className="mb-3">
                    <Form.Label>Note Interne</Form.Label>
                    <Form.Control
                      as="textarea"
                      rows={3}
                      value={internalNotes}
                      onChange={(e) => setInternalNotes(e.target.value)}
                      placeholder="Note visibili solo internamente (non inviate al SDI)"
                    />
                  </Form.Group>

                  <div className="text-muted">
                    <p>
                      <strong>Firma Digitale:</strong>{' '}
                      {invoice.signatureEnabled ? 'Abilitata' : 'Disabilitata'}
                    </p>
                    <p>
                      <strong>Conservazione Sostitutiva:</strong>{' '}
                      {invoice.legalStorageEnabled ? 'Abilitata' : 'Disabilitata'}
                    </p>
                  </div>
                </Tab.Pane>
              </Tab.Content>
            </Tab.Container>
          ) : (
            // Read-only View - Lines table
            <div className="table-responsive">
              <Table bordered hover size="sm">
                <thead className="bg-body-tertiary">
                  <tr>
                    <th>#</th>
                    <th>Descrizione</th>
                    <th className="text-end">Qtà</th>
                    <th>U.M.</th>
                    <th className="text-end">Prezzo Unit.</th>
                    <th className="text-end">IVA %</th>
                    <th>Natura</th>
                    <th className="text-end">Totale</th>
                  </tr>
                </thead>
                <tbody>
                  {(invoice.lines || []).map((line) => (
                    <tr key={line.lineNumber}>
                      <td>{line.lineNumber}</td>
                      <td>{line.description}</td>
                      <td className="text-end">{line.quantity}</td>
                      <td>{line.unitOfMeasure ? UNIT_OF_MEASURE_LABELS[line.unitOfMeasure] : '-'}</td>
                      <td className="text-end">{formatCurrency(line.unitPrice)}</td>
                      <td className="text-end">{line.vatRate}%</td>
                      <td>{line.vatNature || '-'}</td>
                      <td className="text-end fw-bold">{formatCurrency(line.totalPrice)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot className="bg-body-tertiary">
                  <tr>
                    <td colSpan={7} className="text-end">
                      Imponibile:
                    </td>
                    <td className="text-end">{formatCurrency(invoice.totalTaxableAmount)}</td>
                  </tr>
                  <tr>
                    <td colSpan={7} className="text-end">
                      IVA:
                    </td>
                    <td className="text-end">{formatCurrency(invoice.totalVatAmount)}</td>
                  </tr>
                  <tr className="fw-bold">
                    <td colSpan={7} className="text-end">
                      Totale:
                    </td>
                    <td className="text-end">{formatCurrency(invoice.totalAmount)}</td>
                  </tr>
                </tfoot>
              </Table>
            </div>
          )}
        </Card.Body>
      </Card>

      {/* Ritenute e Contributi (Read-only) */}
      {!isEditMode && (invoice.datiRitenuta?.length || invoice.datiBollo || invoice.datiCassaPrevidenziale?.length) && (
        <Card className="mb-3">
          <FalconCardHeader title="Ritenute e Contributi" light={false} />
          <Card.Body>
            {/* Ritenuta d'Acconto */}
            {invoice.datiRitenuta && invoice.datiRitenuta.length > 0 && (
              <div className="mb-3">
                <h6 className="text-primary mb-2">Ritenuta d'Acconto</h6>
                {invoice.datiRitenuta.map((ritenuta, idx) => (
                  <Row key={idx}>
                    <Col md={3}>
                      <small className="text-muted">Tipo</small>
                      <div>{ritenuta.tipoRitenuta} - {TIPO_RITENUTA_LABELS[ritenuta.tipoRitenuta]}</div>
                    </Col>
                    <Col md={3}>
                      <small className="text-muted">Aliquota</small>
                      <div>{ritenuta.aliquotaRitenuta}%</div>
                    </Col>
                    <Col md={3}>
                      <small className="text-muted">Importo</small>
                      <div>{formatCurrency(ritenuta.importoRitenuta)}</div>
                    </Col>
                    {ritenuta.causalePagamento && (
                      <Col md={3}>
                        <small className="text-muted">Causale</small>
                        <div>{ritenuta.causalePagamento}</div>
                      </Col>
                    )}
                  </Row>
                ))}
              </div>
            )}

            {/* Bollo Virtuale */}
            {invoice.datiBollo && (
              <div className="mb-3">
                <h6 className="text-primary mb-2">Bollo Virtuale</h6>
                <Row>
                  <Col md={3}>
                    <small className="text-muted">Importo</small>
                    <div>{formatCurrency(invoice.datiBollo.importoBollo)}</div>
                  </Col>
                </Row>
              </div>
            )}

            {/* Cassa Previdenziale */}
            {invoice.datiCassaPrevidenziale && invoice.datiCassaPrevidenziale.length > 0 && (
              <div>
                <h6 className="text-primary mb-2">Cassa Previdenziale</h6>
                {invoice.datiCassaPrevidenziale.map((cassa, idx) => (
                  <Row key={idx}>
                    <Col md={3}>
                      <small className="text-muted">Tipo</small>
                      <div>{cassa.tipoCassa} - {TIPO_CASSA_LABELS[cassa.tipoCassa]}</div>
                    </Col>
                    <Col md={2}>
                      <small className="text-muted">Aliquota</small>
                      <div>{cassa.alCassa}%</div>
                    </Col>
                    <Col md={3}>
                      <small className="text-muted">Importo</small>
                      <div>{formatCurrency(cassa.importoContributoCassa)}</div>
                    </Col>
                    <Col md={2}>
                      <small className="text-muted">Aliquota IVA</small>
                      <div>{cassa.aliquotaIVA}%</div>
                    </Col>
                  </Row>
                ))}
              </div>
            )}
          </Card.Body>
        </Card>
      )}

      {/* Payment Terms (Read-only) */}
      {!isEditMode && invoice.paymentTerms && (
        <Card className="mb-3">
          <FalconCardHeader title="Termini di Pagamento" light={false} />
          <Card.Body>
            <Row>
              <Col md={3}>
                <small className="text-muted">Condizione</small>
                <div>{PAYMENT_CONDITION_LABELS[invoice.paymentTerms.condition]}</div>
              </Col>
              <Col md={3}>
                <small className="text-muted">Metodo</small>
                <div>{PAYMENT_METHOD_LABELS[invoice.paymentTerms.paymentMethod]}</div>
              </Col>
              {invoice.paymentTerms.dueDate && (
                <Col md={3}>
                  <small className="text-muted">Scadenza</small>
                  <div>{formatItalianDate(invoice.paymentTerms.dueDate)}</div>
                </Col>
              )}
              {invoice.paymentTerms.beneficiario && (
                <Col md={3}>
                  <small className="text-muted">Beneficiario</small>
                  <div>{invoice.paymentTerms.beneficiario}</div>
                </Col>
              )}
            </Row>
            {(invoice.paymentTerms.iban || invoice.paymentTerms.bic || invoice.paymentTerms.istitutoFinanziario) && (
              <Row className="mt-3">
                {invoice.paymentTerms.istitutoFinanziario && (
                  <Col md={3}>
                    <small className="text-muted">Istituto Finanziario</small>
                    <div>{invoice.paymentTerms.istitutoFinanziario}</div>
                  </Col>
                )}
                {invoice.paymentTerms.iban && (
                  <Col md={4}>
                    <small className="text-muted">IBAN</small>
                    <div>{invoice.paymentTerms.iban}</div>
                  </Col>
                )}
                {invoice.paymentTerms.bic && (
                  <Col md={2}>
                    <small className="text-muted">BIC/SWIFT</small>
                    <div>{invoice.paymentTerms.bic}</div>
                  </Col>
                )}
                {invoice.paymentTerms.abi && (
                  <Col md={1}>
                    <small className="text-muted">ABI</small>
                    <div>{invoice.paymentTerms.abi}</div>
                  </Col>
                )}
                {invoice.paymentTerms.cab && (
                  <Col md={1}>
                    <small className="text-muted">CAB</small>
                    <div>{invoice.paymentTerms.cab}</div>
                  </Col>
                )}
              </Row>
            )}
          </Card.Body>
        </Card>
      )}

      {/* Action Buttons */}
      <Card>
        <Card.Body className="d-flex justify-content-between">
          {isEditMode ? (
            <>
              <Button
                variant="falcon-default"
                onClick={handleCancelEdit}
                disabled={isLoading}
              >
                Annulla
              </Button>
              <div>
                <Button
                  variant="falcon-primary"
                  onClick={handleSave}
                  disabled={isLoading}
                >
                  <FontAwesomeIcon icon={faSave} className="me-1" />
                  {isUpdating ? 'Salvataggio...' : 'Salva Modifiche'}
                </Button>
              </div>
            </>
          ) : (
            <>
              <div>
                {canEdit && (
                  <Button
                    variant="falcon-danger"
                    onClick={handleDelete}
                    disabled={isLoading}
                  >
                    <FontAwesomeIcon icon={faTrash} className="me-1" />
                    Elimina
                  </Button>
                )}
              </div>
              <div>
                {canEdit && (
                  <Button
                    variant="primary"
                    onClick={handleSendToSDI}
                    disabled={isLoading}
                  >
                    <FontAwesomeIcon icon={faPaperPlane} className="me-1" />
                    {isSending ? 'Invio...' : 'Invia al SDI'}
                  </Button>
                )}
              </div>
            </>
          )}
        </Card.Body>
      </Card>
    </>
  );
};

export default IssuedInvoiceDetail;
