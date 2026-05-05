import { useState } from 'react';
import { Button, Row, Col, Collapse, Spinner, Table } from 'react-bootstrap';
import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import {
  faFileAlt,
  faBullhorn,
  faUsers,
  faShieldAlt,
  faStar,
  faChevronDown,
  faChevronUp,
  faGlobe,
  faCheck,
  faTimes,
} from '@fortawesome/free-solid-svg-icons';
import {
  faFacebook,
  faYoutube,
  faTwitter,
  faInstagram,
  faLinkedin,
  faPinterest,
} from '@fortawesome/free-brands-svg-icons';
import { useLazyEnrichCompanyLookupQuery } from 'store/api/companyApi';
import SubtleBadge from 'components/common/SubtleBadge';
import type {
  EnrichmentType,
  CompanyLookup,
  Manager,
  Shareholder,
  CorporateGroupsData,
  SubsidiaryCompany,
  AffiliateCompany,
} from 'types/company';
import { formatItalianDate, formatCurrency } from 'types/billing';

export const ENRICHMENT_BUTTONS = [
  { type: 'advanced' as EnrichmentType, label: 'Avanzata', icon: faFileAlt },
  { type: 'marketing' as EnrichmentType, label: 'Marketing', icon: faBullhorn },
  { type: 'stakeholders' as EnrichmentType, label: 'Stakeholders', icon: faUsers },
  { type: 'aml' as EnrichmentType, label: 'AML', icon: faShieldAlt },
  { type: 'full' as EnrichmentType, label: 'Completa', icon: faStar },
] as const;

// ========================================
// Shared layout components
// ========================================

export const EnrichmentSection = ({
  title,
  isOpen,
  onToggle,
  children,
}: {
  title: string;
  isOpen: boolean;
  onToggle: () => void;
  children: React.ReactNode;
}) => (
  <div className="border rounded mb-2">
    <div
      className="d-flex align-items-center justify-content-between px-3 py-2 cursor-pointer"
      onClick={onToggle}
      role="button"
    >
      <span className="fw-semibold fs-9">{title}</span>
      <FontAwesomeIcon icon={isOpen ? faChevronUp : faChevronDown} className="text-muted fs-10" />
    </div>
    <Collapse in={isOpen}>
      <div className="px-3 pb-3">{children}</div>
    </Collapse>
  </div>
);

export const FieldRow = ({ label, value }: { label: string; value?: string | number | boolean | null }) => {
  if (value === undefined || value === null) return null;
  return (
    <Col sm={6} md={4}>
      <div className="mb-2">
        <small className="text-muted d-block">{label}</small>
        <span>{typeof value === 'boolean' ? (value ? 'Si' : 'No') : String(value) || '-'}</span>
      </div>
    </Col>
  );
};

const BoolBadge = ({ value }: { value?: boolean }) => {
  if (value === undefined || value === null) return <>-</>;
  return (
    <SubtleBadge bg={value ? 'success' : 'secondary'}>
      <FontAwesomeIcon icon={value ? faCheck : faTimes} className="me-1" />
      {value ? 'Si' : 'No'}
    </SubtleBadge>
  );
};

const SectionLabel = ({ children }: { children: React.ReactNode }) => (
  <h6 className="text-muted fs-10 text-uppercase mt-3 mb-2">{children}</h6>
);

// ========================================
// Shared tables (Managers & Shareholders)
// ========================================

const ManagersTable = ({ managers }: { managers: Manager[] }) => {
  if (!managers.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>Nome</th>
            <th>Codice Fiscale</th>
            <th>Ruolo</th>
            <th>Dal</th>
            <th>Rappr. Legale</th>
          </tr>
        </thead>
        <tbody>
          {managers.map((m, i) => (
            <tr key={i}>
              <td>{m.companyName || [m.name, m.surname].filter(Boolean).join(' ') || '-'}</td>
              <td className="font-monospace">{m.taxCode || '-'}</td>
              <td>
                {m.roles?.map((r, ri) => (
                  <div key={ri}>{r.role?.description || r.role?.code || '-'}</div>
                )) || '-'}
              </td>
              <td>
                {m.roles?.map((r, ri) => (
                  <div key={ri}>{r.roleStartDate ? formatItalianDate(r.roleStartDate) : '-'}</div>
                )) || '-'}
              </td>
              <td><BoolBadge value={m.isLegalRepresentative} /></td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
};

const ShareholdersTable = ({ shareholders }: { shareholders: Shareholder[] }) => {
  if (!shareholders.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>Nome</th>
            <th>Codice Fiscale</th>
            <th>Quota %</th>
            <th>Dal</th>
          </tr>
        </thead>
        <tbody>
          {shareholders.map((s, i) =>
            (s.shareholdersInformation || []).map((info, j) => (
              <tr key={`${i}-${j}`}>
                <td>{info.companyName || [info.name, info.surname].filter(Boolean).join(' ') || '-'}</td>
                <td className="font-monospace">{info.taxCode || '-'}</td>
                <td>{s.percentShare != null ? `${s.percentShare}%` : '-'}</td>
                <td>{info.sinceDate ? formatItalianDate(info.sinceDate) : '-'}</td>
              </tr>
            )),
          )}
        </tbody>
      </Table>
    </div>
  );
};

const CorporateGroupsCard = ({ data }: { data: CorporateGroupsData }) => (
  <Row className="g-2">
    <FieldRow label="Appartiene a un gruppo" value={data.belongsToGroup} />
    <FieldRow label="Nome Gruppo" value={data.groupName} />
    <FieldRow label="Holding" value={data.holdingCompanyName} />
    <FieldRow label="Paese Holding" value={data.holdingCountry?.description} />
    {data.nationalParentCompany && (
      <>
        <FieldRow label="Capogruppo" value={data.nationalParentCompany.companyName} />
        <FieldRow
          label="Sede Capogruppo"
          value={[
            data.nationalParentCompany.streetName,
            data.nationalParentCompany.zipCode,
            data.nationalParentCompany.town,
            data.nationalParentCompany.province?.description,
          ]
            .filter(Boolean)
            .join(', ')}
        />
      </>
    )}
    <FieldRow label="Capogruppo Estera" value={data.hasForeignParentCompany} />
  </Row>
);

const SubsidiariesTable = ({ subsidiaries }: { subsidiaries: SubsidiaryCompany[] }) => {
  if (!subsidiaries.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>Ragione Sociale</th>
            <th>Codice Fiscale</th>
            <th>Sede</th>
          </tr>
        </thead>
        <tbody>
          {subsidiaries.map((s, i) => (
            <tr key={i}>
              <td>{s.companyName || '-'}</td>
              <td className="font-monospace">{s.taxCode || '-'}</td>
              <td>
                {[s.streetName, s.zipCode, s.town, s.province?.description]
                  .filter(Boolean)
                  .join(', ') || '-'}
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
};

const AffiliatesTable = ({ affiliates }: { affiliates: AffiliateCompany[] }) => {
  if (!affiliates.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>Ragione Sociale</th>
            <th>Codice Fiscale</th>
            <th>Quota %</th>
          </tr>
        </thead>
        <tbody>
          {affiliates.map((a, i) => (
            <tr key={i}>
              <td>{a.companyName || '-'}</td>
              <td className="font-monospace">{a.taxCode || '-'}</td>
              <td>{a.percentShare != null ? `${a.percentShare}%` : '-'}</td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
};

// ========================================
// Section components
// ========================================

const SOCIAL_ICONS = [
  { key: 'facebook' as const, icon: faFacebook, label: 'Facebook' },
  { key: 'instagram' as const, icon: faInstagram, label: 'Instagram' },
  { key: 'linkedin' as const, icon: faLinkedin, label: 'LinkedIn' },
  { key: 'twitter' as const, icon: faTwitter, label: 'Twitter' },
  { key: 'youtube' as const, icon: faYoutube, label: 'YouTube' },
  { key: 'pinterest' as const, icon: faPinterest, label: 'Pinterest' },
] as const;

export const AdvancedSection = ({ data }: { data: NonNullable<CompanyLookup['advanced']> }) => (
  <div>
    <Row className="g-2">
      <FieldRow label="PEC" value={data.pec} />
      <FieldRow label="Codice REA" value={data.reaCode} />
      <FieldRow label="CCIAA" value={data.cciaa} />
      {data.atecoClassification?.ateco && (
        <FieldRow
          label="ATECO"
          value={`${data.atecoClassification.ateco.code} - ${data.atecoClassification.ateco.description}`}
        />
      )}
      {data.detailedLegalForm && (
        <FieldRow
          label="Forma Giuridica"
          value={`${data.detailedLegalForm.code} - ${data.detailedLegalForm.description}`}
        />
      )}
      <FieldRow label="Data Inizio" value={data.startDate ? formatItalianDate(data.startDate) : undefined} />
      <FieldRow label="Data Fine" value={data.endDate ? formatItalianDate(data.endDate) : undefined} />
      <FieldRow label="CF Cessato" value={data.taxCodeCeased} />
    </Row>

    {/* VAT Group */}
    {data.vatGroup && (
      <>
        <SectionLabel>Gruppo IVA</SectionLabel>
        <Row className="g-2">
          <FieldRow label="Partecipazione" value={data.vatGroup.vatGroupParticipation} />
          <FieldRow label="Capogruppo IVA" value={data.vatGroup.isVatGroupLeader} />
          <FieldRow label="Registro OK" value={data.vatGroup.registryOk} />
        </Row>
      </>
    )}

    {/* Last Balance Sheet */}
    {data.balanceSheets?.last && (
      <>
        <SectionLabel>Ultimo Bilancio ({data.balanceSheets.last.year})</SectionLabel>
        <Row className="g-2">
          <FieldRow label="Data Bilancio" value={data.balanceSheets.last.balanceSheetDate ? formatItalianDate(data.balanceSheets.last.balanceSheetDate) : undefined} />
          <FieldRow label="Fatturato" value={data.balanceSheets.last.turnover != null ? formatCurrency(data.balanceSheets.last.turnover) : undefined} />
          <FieldRow label="Patrimonio Netto" value={data.balanceSheets.last.netWorth != null ? formatCurrency(data.balanceSheets.last.netWorth) : undefined} />
          <FieldRow label="Capitale Sociale" value={data.balanceSheets.last.shareCapital != null ? formatCurrency(data.balanceSheets.last.shareCapital) : undefined} />
          <FieldRow label="Dipendenti" value={data.balanceSheets.last.employees} />
          <FieldRow label="Totale Attivo" value={data.balanceSheets.last.totalAssets != null ? formatCurrency(data.balanceSheets.last.totalAssets) : undefined} />
          <FieldRow label="Costo Personale" value={data.balanceSheets.last.totalStaffCost != null ? formatCurrency(data.balanceSheets.last.totalStaffCost) : undefined} />
          <FieldRow label="Retribuzione Media" value={data.balanceSheets.last.avgGrossSalary != null ? formatCurrency(data.balanceSheets.last.avgGrossSalary) : undefined} />
        </Row>
      </>
    )}

    {/* Historical Balance Sheets */}
    {data.balanceSheets?.all && data.balanceSheets.all.length > 1 && (
      <>
        <SectionLabel>Storico Bilanci</SectionLabel>
        <div className="table-responsive">
          <Table bordered hover size="sm">
            <thead className="bg-body-tertiary">
              <tr>
                <th>Anno</th>
                <th>Fatturato</th>
                <th>Patrimonio Netto</th>
                <th>Capitale Sociale</th>
                <th>Dipendenti</th>
              </tr>
            </thead>
            <tbody>
              {data.balanceSheets.all.map((bs, i) => (
                <tr key={i}>
                  <td>{bs.year ?? '-'}</td>
                  <td>{bs.turnover != null ? formatCurrency(bs.turnover) : '-'}</td>
                  <td>{bs.netWorth != null ? formatCurrency(bs.netWorth) : '-'}</td>
                  <td>{bs.shareCapital != null ? formatCurrency(bs.shareCapital) : '-'}</td>
                  <td>{bs.employees ?? '-'}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      </>
    )}

    {/* Shareholders */}
    {data.shareHolders && data.shareHolders.length > 0 && (
      <>
        <SectionLabel>Soci</SectionLabel>
        <div className="table-responsive">
          <Table bordered hover size="sm">
            <thead className="bg-body-tertiary">
              <tr>
                <th>Nome</th>
                <th>Codice Fiscale</th>
                <th>Quota %</th>
              </tr>
            </thead>
            <tbody>
              {data.shareHolders.map((s, i) => (
                <tr key={i}>
                  <td>{s.companyName || [s.name, s.surname].filter(Boolean).join(' ') || '-'}</td>
                  <td className="font-monospace">{s.taxCode || '-'}</td>
                  <td>{s.percentShare != null ? `${s.percentShare}%` : '-'}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      </>
    )}
  </div>
);

export const MarketingSection = ({ data }: { data: NonNullable<CompanyLookup['marketing']> }) => (
  <div>
    {/* PEC & Contacts */}
    <Row className="g-2">
      <FieldRow label="PEC" value={data.pec} />
      {data.contacts && (
        <>
          <FieldRow label="Telefono" value={data.contacts.telephoneNumber} />
          <FieldRow label="Fax" value={data.contacts.fax} />
        </>
      )}
    </Row>

    {/* Web & Social */}
    {data.webAndSocial && (
      <>
        <SectionLabel>Web & Social</SectionLabel>
        <Row className="g-2">
          {data.webAndSocial.website && (
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">Sito Web</small>
                <a href={data.webAndSocial.website} target="_blank" rel="noreferrer">
                  <FontAwesomeIcon icon={faGlobe} className="me-1" />
                  {data.webAndSocial.website}
                </a>
              </div>
            </Col>
          )}
          {data.webAndSocial.eCommerce && (
            <Col sm={6} md={4}>
              <div className="mb-2">
                <small className="text-muted d-block">E-Commerce</small>
                <a href={data.webAndSocial.eCommerce} target="_blank" rel="noreferrer">
                  {data.webAndSocial.eCommerce}
                </a>
              </div>
            </Col>
          )}
        </Row>
        <div className="d-flex flex-wrap gap-2 mt-1">
          {SOCIAL_ICONS.map(({ key, icon, label }) => {
            const url = data.webAndSocial?.[key];
            if (!url) return null;
            return (
              <a key={key} href={url} target="_blank" rel="noreferrer" title={label} className="btn btn-sm btn-outline-secondary">
                <FontAwesomeIcon icon={icon} className="me-1" />
                {label}
              </a>
            );
          })}
        </div>
      </>
    )}

    {/* Employees */}
    {data.employees && (
      <>
        <SectionLabel>Dipendenti</SectionLabel>
        <Row className="g-2">
          <FieldRow label="Numero Dipendenti" value={data.employees.employee} />
          <FieldRow label="Fascia" value={data.employees.employeeRange?.description} />
          <FieldRow label="Trend" value={data.employees.employeeTrend} />
        </Row>
      </>
    )}

    {/* Ecofin */}
    {data.ecofin && (
      <>
        <SectionLabel>Eco-Fin</SectionLabel>
        <Row className="g-2">
          <FieldRow label="Data Bilancio" value={data.ecofin.balanceSheetDate ? formatItalianDate(data.ecofin.balanceSheetDate) : undefined} />
          <FieldRow label="Fatturato" value={data.ecofin.turnover != null ? formatCurrency(data.ecofin.turnover) : undefined} />
          <FieldRow label="Fascia Fatturato" value={data.ecofin.turnoverRange?.description} />
          <FieldRow label="Anno Fatturato" value={data.ecofin.turnoverYear} />
          <FieldRow label="Trend Fatturato" value={data.ecofin.turnoverTrend} />
          <FieldRow label="Capitale Sociale" value={data.ecofin.shareCapital != null ? formatCurrency(data.ecofin.shareCapital) : undefined} />
          <FieldRow label="Patrimonio Netto" value={data.ecofin.netWorth != null ? formatCurrency(data.ecofin.netWorth) : undefined} />
          <FieldRow label="Dimensione Impresa" value={data.ecofin.enterpriseSize?.description} />
        </Row>
      </>
    )}

    {/* Branches */}
    {data.branches && (
      <>
        <SectionLabel>Sedi</SectionLabel>
        <Row className="g-2">
          <FieldRow label="Numero Sedi" value={data.branches.numberOfBranches} />
        </Row>
      </>
    )}
  </div>
);

export const StakeholdersSection = ({ data }: { data: NonNullable<CompanyLookup['stakeholders']> }) => (
  <div>
    {data.managers && data.managers.length > 0 && (
      <>
        <SectionLabel>Amministratori</SectionLabel>
        <ManagersTable managers={data.managers} />
      </>
    )}

    {data.shareholders && data.shareholders.length > 0 && (
      <>
        <SectionLabel>Soci</SectionLabel>
        <ShareholdersTable shareholders={data.shareholders} />
      </>
    )}

    {data.corporateGroups && (
      <>
        <SectionLabel>Gruppi Societari</SectionLabel>
        <CorporateGroupsCard data={data.corporateGroups} />
      </>
    )}

    {data.subsidiaries && data.subsidiaries.length > 0 && (
      <>
        <SectionLabel>Controllate</SectionLabel>
        <SubsidiariesTable subsidiaries={data.subsidiaries} />
      </>
    )}

    {data.affiliateCompanies && data.affiliateCompanies.length > 0 && (
      <>
        <SectionLabel>Collegate</SectionLabel>
        <AffiliatesTable affiliates={data.affiliateCompanies} />
      </>
    )}
  </div>
);

export const AMLSection = ({ data }: { data: NonNullable<CompanyLookup['aml']> }) => (
  <div>
    {/* RAE / SAE */}
    <Row className="g-2 mb-2">
      {data.rae && <FieldRow label="RAE" value={`${data.rae.code} - ${data.rae.description}`} />}
      {data.sae && <FieldRow label="SAE" value={`${data.sae.code} - ${data.sae.description}`} />}
    </Row>

    {/* Managers & Shareholders — reuse shared tables */}
    {data.managers && data.managers.length > 0 && (
      <>
        <SectionLabel>Amministratori</SectionLabel>
        <ManagersTable managers={data.managers} />
      </>
    )}

    {data.shareholders && data.shareholders.length > 0 && (
      <>
        <SectionLabel>Soci</SectionLabel>
        <ShareholdersTable shareholders={data.shareholders} />
      </>
    )}

    {/* Corporate Groups */}
    {data.corporateGroups && (
      <>
        <SectionLabel>Gruppi Societari</SectionLabel>
        <CorporateGroupsCard data={data.corporateGroups} />
      </>
    )}

    {/* Foreign Trade */}
    {data.foreignTrade && (
      <>
        <SectionLabel>Commercio Estero</SectionLabel>
        <Row className="g-2">
          <FieldRow label="Importatore" value={data.foreignTrade.isImporter} />
          <FieldRow label="% Import" value={data.foreignTrade.importPercentShare != null ? `${data.foreignTrade.importPercentShare}%` : undefined} />
          <FieldRow label="Paesi Import" value={data.foreignTrade.importCountries} />
          <FieldRow label="Esportatore" value={data.foreignTrade.isExporter} />
          <FieldRow label="% Export" value={data.foreignTrade.exportPercentShare != null ? `${data.foreignTrade.exportPercentShare}%` : undefined} />
          <FieldRow label="Paesi Export" value={data.foreignTrade.exportCountries} />
        </Row>
      </>
    )}

    {/* Public Tenders */}
    {data.publicTenders && data.publicTenders.length > 0 && (
      <>
        <SectionLabel>Appalti Pubblici</SectionLabel>
        <div className="table-responsive">
          <Table bordered hover size="sm">
            <thead className="bg-body-tertiary">
              <tr>
                <th>Anno</th>
                <th>Partecipati</th>
                <th>Vinti</th>
                <th>Valore</th>
              </tr>
            </thead>
            <tbody>
              {data.publicTenders.map((t, i) => (
                <tr key={i}>
                  <td>{t.year || '-'}</td>
                  <td>{t.applied ?? '-'}</td>
                  <td>{t.won ?? '-'}</td>
                  <td>{t.value != null ? formatCurrency(t.value) : '-'}</td>
                </tr>
              ))}
            </tbody>
          </Table>
        </div>
      </>
    )}

    {/* Operating Results */}
    {data.operatingResults && (
      <>
        <SectionLabel>Risultati Operativi</SectionLabel>
        <Row className="g-2">
          <FieldRow label="EBITDA" value={data.operatingResults.ebitda != null ? formatCurrency(data.operatingResults.ebitda) : undefined} />
          <FieldRow label="EBITDA (anno prec.)" value={data.operatingResults.ebitdaL2Y != null ? formatCurrency(data.operatingResults.ebitdaL2Y) : undefined} />
          <FieldRow label="EBIT" value={data.operatingResults.ebit != null ? formatCurrency(data.operatingResults.ebit) : undefined} />
          <FieldRow label="EBIT (anno prec.)" value={data.operatingResults.ebitL2Y != null ? formatCurrency(data.operatingResults.ebitL2Y) : undefined} />
          <FieldRow label="Cash Flow" value={data.operatingResults.cashFlow != null ? formatCurrency(data.operatingResults.cashFlow) : undefined} />
          <FieldRow label="Cash Flow (anno prec.)" value={data.operatingResults.cashFlowL2Y != null ? formatCurrency(data.operatingResults.cashFlowL2Y) : undefined} />
        </Row>
      </>
    )}

    {/* Debts */}
    {data.debts && (
      <>
        <SectionLabel>Debiti</SectionLabel>
        <Row className="g-2">
          <FieldRow label="Codice" value={data.debts.code} />
          <FieldRow label="Valore" value={data.debts.value != null ? formatCurrency(data.debts.value) : undefined} />
        </Row>
      </>
    )}
  </div>
);

/**
 * Self-contained enrichment panel: buttons, fetched-type badges, and collapsible data sections.
 * Manages its own loading/open state internally.
 */
export const EnrichmentPanel = ({
  company,
  onEnriched,
}: {
  company: CompanyLookup;
  onEnriched: (updated: CompanyLookup) => void;
}) => {
  const [activeEnrichment, setActiveEnrichment] = useState<EnrichmentType | null>(null);
  const [openSections, setOpenSections] = useState<Record<string, boolean>>({});
  const [triggerEnrich] = useLazyEnrichCompanyLookupQuery();

  const isEnriched = (type: string) => company.fetchedTypes?.[type] !== undefined;

  const handleEnrich = async (type: EnrichmentType) => {
    if (activeEnrichment) return;
    setActiveEnrichment(type);
    try {
      const enriched = await triggerEnrich({ taxCode: company.taxCode, type }).unwrap();
      onEnriched(enriched);
      if (type === 'full') {
        setOpenSections((prev) => ({ ...prev, advanced: true, marketing: true, stakeholders: true, aml: true }));
      } else {
        setOpenSections((prev) => ({ ...prev, [type]: true }));
      }
    } catch {
      // Error handled by RTK Query
    } finally {
      setActiveEnrichment(null);
    }
  };

  const toggleSection = (key: string) => {
    setOpenSections((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  return (
    <>
      <div className="d-flex flex-wrap gap-2 mb-3">
        {ENRICHMENT_BUTTONS.map(({ type, label, icon }) => {
          const loaded = type === 'full'
            ? ['advanced', 'marketing', 'stakeholders', 'aml'].every(isEnriched)
            : isEnriched(type);
          const isLoading = activeEnrichment === type;

          return (
            <Button
              key={type}
              size="sm"
              variant={loaded ? 'primary' : 'outline-primary'}
              disabled={!!activeEnrichment}
              onClick={() => handleEnrich(type)}
            >
              {isLoading ? (
                <Spinner size="sm" className="me-1" />
              ) : (
                <FontAwesomeIcon icon={icon} className="me-1" />
              )}
              {label}
            </Button>
          );
        })}
      </div>

      {company.fetchedTypes && Object.keys(company.fetchedTypes).length > 0 && (
        <div className="d-flex flex-wrap gap-1 mb-3">
          {Object.entries(company.fetchedTypes).map(([type, timestamp]) => (
            <SubtleBadge key={type} bg="info">
              {type}: {formatItalianDate(timestamp)}
            </SubtleBadge>
          ))}
        </div>
      )}

      {company.advanced && (
        <EnrichmentSection title="Dati Avanzati" isOpen={!!openSections.advanced} onToggle={() => toggleSection('advanced')}>
          <AdvancedSection data={company.advanced} />
        </EnrichmentSection>
      )}

      {company.marketing && (
        <EnrichmentSection title="Marketing" isOpen={!!openSections.marketing} onToggle={() => toggleSection('marketing')}>
          <MarketingSection data={company.marketing} />
        </EnrichmentSection>
      )}

      {company.stakeholders && (
        <EnrichmentSection title="Stakeholders" isOpen={!!openSections.stakeholders} onToggle={() => toggleSection('stakeholders')}>
          <StakeholdersSection data={company.stakeholders} />
        </EnrichmentSection>
      )}

      {company.aml && (
        <EnrichmentSection title="AML (Antiriciclaggio)" isOpen={!!openSections.aml} onToggle={() => toggleSection('aml')}>
          <AMLSection data={company.aml} />
        </EnrichmentSection>
      )}
    </>
  );
};
