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
  faTimes
} from '@fortawesome/free-solid-svg-icons';
import {
  faFacebook,
  faYoutube,
  faTwitter,
  faInstagram,
  faLinkedin,
  faPinterest
} from '@fortawesome/free-brands-svg-icons';
import { useTranslation } from 'react-i18next';
import { useLazyEnrichCompanyLookupQuery } from 'store/api/companyApi';
import SubtleBadge from 'components/common/SubtleBadge';
import type {
  EnrichmentType,
  CompanyLookup,
  Manager,
  Shareholder,
  CorporateGroupsData,
  SubsidiaryCompany,
  AffiliateCompany
} from 'types/company';
import { formatItalianDate, formatCurrency } from 'types/billing';

// Maps button type → i18n key + icon. Labels resolve via `t()` at render time
// so the toolbar re-renders on language change.
export const ENRICHMENT_BUTTONS = [
  {
    type: 'advanced' as EnrichmentType,
    labelKey: 'company.lookup.enrichment.buttons.advanced',
    icon: faFileAlt
  },
  {
    type: 'marketing' as EnrichmentType,
    labelKey: 'company.lookup.enrichment.buttons.marketing',
    icon: faBullhorn
  },
  {
    type: 'stakeholders' as EnrichmentType,
    labelKey: 'company.lookup.enrichment.buttons.stakeholders',
    icon: faUsers
  },
  {
    type: 'aml' as EnrichmentType,
    labelKey: 'company.lookup.enrichment.buttons.aml',
    icon: faShieldAlt
  },
  {
    type: 'full' as EnrichmentType,
    labelKey: 'company.lookup.enrichment.buttons.full',
    icon: faStar
  }
] as const;

// ========================================
// Shared layout components
// ========================================

export const EnrichmentSection = ({
  title,
  isOpen,
  onToggle,
  children
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
      <FontAwesomeIcon
        icon={isOpen ? faChevronUp : faChevronDown}
        className="text-muted fs-10"
      />
    </div>
    <Collapse in={isOpen}>
      <div className="px-3 pb-3">{children}</div>
    </Collapse>
  </div>
);

export const FieldRow = ({
  label,
  value
}: {
  label: string;
  value?: string | number | boolean | null;
}) => {
  const { t } = useTranslation();
  if (value === undefined || value === null) return null;
  return (
    <Col sm={6} md={4}>
      <div className="mb-2">
        <small className="text-muted d-block">{label}</small>
        <span>
          {typeof value === 'boolean'
            ? value
              ? t('company.lookup.enrichment.yes')
              : t('company.lookup.enrichment.no')
            : String(value) || '-'}
        </span>
      </div>
    </Col>
  );
};

const BoolBadge = ({ value }: { value?: boolean }) => {
  const { t } = useTranslation();
  if (value === undefined || value === null) return <>-</>;
  return (
    <SubtleBadge bg={value ? 'success' : 'secondary'}>
      <FontAwesomeIcon icon={value ? faCheck : faTimes} className="me-1" />
      {value
        ? t('company.lookup.enrichment.yes')
        : t('company.lookup.enrichment.no')}
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
  const { t } = useTranslation();
  if (!managers.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>{t('company.lookup.enrichment.managers.colName')}</th>
            <th>{t('company.lookup.enrichment.managers.colTaxCode')}</th>
            <th>{t('company.lookup.enrichment.managers.colRole')}</th>
            <th>{t('company.lookup.enrichment.managers.colSince')}</th>
            <th>{t('company.lookup.enrichment.managers.colLegalRep')}</th>
          </tr>
        </thead>
        <tbody>
          {managers.map((m, i) => (
            <tr key={i}>
              <td>
                {m.companyName ||
                  [m.name, m.surname].filter(Boolean).join(' ') ||
                  '-'}
              </td>
              <td className="font-monospace">{m.taxCode || '-'}</td>
              <td>
                {m.roles?.map((r, ri) => (
                  <div key={ri}>
                    {r.role?.description || r.role?.code || '-'}
                  </div>
                )) || '-'}
              </td>
              <td>
                {m.roles?.map((r, ri) => (
                  <div key={ri}>
                    {r.roleStartDate ? formatItalianDate(r.roleStartDate) : '-'}
                  </div>
                )) || '-'}
              </td>
              <td>
                <BoolBadge value={m.isLegalRepresentative} />
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </div>
  );
};

const ShareholdersTable = ({
  shareholders
}: {
  shareholders: Shareholder[];
}) => {
  const { t } = useTranslation();
  if (!shareholders.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>{t('company.lookup.enrichment.shareholders.colName')}</th>
            <th>{t('company.lookup.enrichment.shareholders.colTaxCode')}</th>
            <th>{t('company.lookup.enrichment.shareholders.colShare')}</th>
            <th>{t('company.lookup.enrichment.shareholders.colSince')}</th>
          </tr>
        </thead>
        <tbody>
          {shareholders.map((s, i) =>
            (s.shareholdersInformation || []).map((info, j) => (
              <tr key={`${i}-${j}`}>
                <td>
                  {info.companyName ||
                    [info.name, info.surname].filter(Boolean).join(' ') ||
                    '-'}
                </td>
                <td className="font-monospace">{info.taxCode || '-'}</td>
                <td>{s.percentShare != null ? `${s.percentShare}%` : '-'}</td>
                <td>
                  {info.sinceDate ? formatItalianDate(info.sinceDate) : '-'}
                </td>
              </tr>
            ))
          )}
        </tbody>
      </Table>
    </div>
  );
};

const CorporateGroupsCard = ({ data }: { data: CorporateGroupsData }) => {
  const { t } = useTranslation();
  return (
    <Row className="g-2">
      <FieldRow
        label={t('company.lookup.enrichment.corporateGroups.belongsToGroup')}
        value={data.belongsToGroup}
      />
      <FieldRow
        label={t('company.lookup.enrichment.corporateGroups.groupName')}
        value={data.groupName}
      />
      <FieldRow
        label={t(
          'company.lookup.enrichment.corporateGroups.holdingCompanyName'
        )}
        value={data.holdingCompanyName}
      />
      <FieldRow
        label={t('company.lookup.enrichment.corporateGroups.holdingCountry')}
        value={data.holdingCountry?.description}
      />
      {data.nationalParentCompany && (
        <>
          <FieldRow
            label={t('company.lookup.enrichment.corporateGroups.parentCompany')}
            value={data.nationalParentCompany.companyName}
          />
          <FieldRow
            label={t(
              'company.lookup.enrichment.corporateGroups.parentCompanyHQ'
            )}
            value={[
              data.nationalParentCompany.streetName,
              data.nationalParentCompany.zipCode,
              data.nationalParentCompany.town,
              data.nationalParentCompany.province?.description
            ]
              .filter(Boolean)
              .join(', ')}
          />
        </>
      )}
      <FieldRow
        label={t('company.lookup.enrichment.corporateGroups.foreignParent')}
        value={data.hasForeignParentCompany}
      />
    </Row>
  );
};

const SubsidiariesTable = ({
  subsidiaries
}: {
  subsidiaries: SubsidiaryCompany[];
}) => {
  const { t } = useTranslation();
  if (!subsidiaries.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>{t('company.lookup.enrichment.subsidiaries.colName')}</th>
            <th>{t('company.lookup.enrichment.subsidiaries.colTaxCode')}</th>
            <th>{t('company.lookup.enrichment.subsidiaries.colHQ')}</th>
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

const AffiliatesTable = ({
  affiliates
}: {
  affiliates: AffiliateCompany[];
}) => {
  const { t } = useTranslation();
  if (!affiliates.length) return null;
  return (
    <div className="table-responsive">
      <Table bordered hover size="sm">
        <thead className="bg-body-tertiary">
          <tr>
            <th>{t('company.lookup.enrichment.affiliates.colName')}</th>
            <th>{t('company.lookup.enrichment.affiliates.colTaxCode')}</th>
            <th>{t('company.lookup.enrichment.affiliates.colShare')}</th>
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
  { key: 'pinterest' as const, icon: faPinterest, label: 'Pinterest' }
] as const;

export const AdvancedSection = ({
  data
}: {
  data: NonNullable<CompanyLookup['advanced']>;
}) => {
  const { t } = useTranslation();
  return (
    <div>
      <Row className="g-2">
        <FieldRow
          label={t('company.lookup.enrichment.advanced.pec')}
          value={data.pec}
        />
        <FieldRow
          label={t('company.lookup.enrichment.advanced.reaCode')}
          value={data.reaCode}
        />
        <FieldRow
          label={t('company.lookup.enrichment.advanced.cciaa')}
          value={data.cciaa}
        />
        {data.atecoClassification?.ateco && (
          <FieldRow
            label={t('company.lookup.enrichment.advanced.ateco')}
            value={`${data.atecoClassification.ateco.code} - ${data.atecoClassification.ateco.description}`}
          />
        )}
        {data.detailedLegalForm && (
          <FieldRow
            label={t('company.lookup.enrichment.advanced.legalForm')}
            value={`${data.detailedLegalForm.code} - ${data.detailedLegalForm.description}`}
          />
        )}
        <FieldRow
          label={t('company.lookup.enrichment.advanced.startDate')}
          value={data.startDate ? formatItalianDate(data.startDate) : undefined}
        />
        <FieldRow
          label={t('company.lookup.enrichment.advanced.endDate')}
          value={data.endDate ? formatItalianDate(data.endDate) : undefined}
        />
        <FieldRow
          label={t('company.lookup.enrichment.advanced.taxCodeCeased')}
          value={data.taxCodeCeased}
        />
      </Row>

      {/* VAT Group */}
      {data.vatGroup && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.advanced.vatGroupHeader')}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t(
                'company.lookup.enrichment.advanced.vatGroupParticipation'
              )}
              value={data.vatGroup.vatGroupParticipation}
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.vatGroupLeader')}
              value={data.vatGroup.isVatGroupLeader}
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.vatRegistryOk')}
              value={data.vatGroup.registryOk}
            />
          </Row>
        </>
      )}

      {/* Last Balance Sheet */}
      {data.balanceSheets?.last && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.advanced.lastBalanceHeader', {
              year: data.balanceSheets.last.year
            })}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t('company.lookup.enrichment.advanced.balanceDate')}
              value={
                data.balanceSheets.last.balanceSheetDate
                  ? formatItalianDate(data.balanceSheets.last.balanceSheetDate)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.turnover')}
              value={
                data.balanceSheets.last.turnover != null
                  ? formatCurrency(data.balanceSheets.last.turnover)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.netWorth')}
              value={
                data.balanceSheets.last.netWorth != null
                  ? formatCurrency(data.balanceSheets.last.netWorth)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.shareCapital')}
              value={
                data.balanceSheets.last.shareCapital != null
                  ? formatCurrency(data.balanceSheets.last.shareCapital)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.employees')}
              value={data.balanceSheets.last.employees}
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.totalAssets')}
              value={
                data.balanceSheets.last.totalAssets != null
                  ? formatCurrency(data.balanceSheets.last.totalAssets)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.totalStaffCost')}
              value={
                data.balanceSheets.last.totalStaffCost != null
                  ? formatCurrency(data.balanceSheets.last.totalStaffCost)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.advanced.avgGrossSalary')}
              value={
                data.balanceSheets.last.avgGrossSalary != null
                  ? formatCurrency(data.balanceSheets.last.avgGrossSalary)
                  : undefined
              }
            />
          </Row>
        </>
      )}

      {/* Historical Balance Sheets */}
      {data.balanceSheets?.all && data.balanceSheets.all.length > 1 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.advanced.historicalBalanceHeader')}
          </SectionLabel>
          <div className="table-responsive">
            <Table bordered hover size="sm">
              <thead className="bg-body-tertiary">
                <tr>
                  <th>
                    {t('company.lookup.enrichment.advanced.historicalYear')}
                  </th>
                  <th>{t('company.lookup.enrichment.advanced.turnover')}</th>
                  <th>{t('company.lookup.enrichment.advanced.netWorth')}</th>
                  <th>
                    {t('company.lookup.enrichment.advanced.shareCapital')}
                  </th>
                  <th>{t('company.lookup.enrichment.advanced.employees')}</th>
                </tr>
              </thead>
              <tbody>
                {data.balanceSheets.all.map((bs, i) => (
                  <tr key={i}>
                    <td>{bs.year ?? '-'}</td>
                    <td>
                      {bs.turnover != null ? formatCurrency(bs.turnover) : '-'}
                    </td>
                    <td>
                      {bs.netWorth != null ? formatCurrency(bs.netWorth) : '-'}
                    </td>
                    <td>
                      {bs.shareCapital != null
                        ? formatCurrency(bs.shareCapital)
                        : '-'}
                    </td>
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
          <SectionLabel>
            {t('company.lookup.enrichment.advanced.shareHoldersHeader')}
          </SectionLabel>
          <div className="table-responsive">
            <Table bordered hover size="sm">
              <thead className="bg-body-tertiary">
                <tr>
                  <th>{t('company.lookup.enrichment.shareholders.colName')}</th>
                  <th>
                    {t('company.lookup.enrichment.shareholders.colTaxCode')}
                  </th>
                  <th>
                    {t('company.lookup.enrichment.shareholders.colShare')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.shareHolders.map((s, i) => (
                  <tr key={i}>
                    <td>
                      {s.companyName ||
                        [s.name, s.surname].filter(Boolean).join(' ') ||
                        '-'}
                    </td>
                    <td className="font-monospace">{s.taxCode || '-'}</td>
                    <td>
                      {s.percentShare != null ? `${s.percentShare}%` : '-'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          </div>
        </>
      )}
    </div>
  );
};

export const MarketingSection = ({
  data
}: {
  data: NonNullable<CompanyLookup['marketing']>;
}) => {
  const { t } = useTranslation();
  return (
    <div>
      {/* PEC & Contacts */}
      <Row className="g-2">
        <FieldRow
          label={t('company.lookup.enrichment.marketing.pec')}
          value={data.pec}
        />
        {data.contacts && (
          <>
            <FieldRow
              label={t('company.lookup.enrichment.marketing.phone')}
              value={data.contacts.telephoneNumber}
            />
            <FieldRow
              label={t('company.lookup.enrichment.marketing.fax')}
              value={data.contacts.fax}
            />
          </>
        )}
      </Row>

      {/* Web & Social */}
      {data.webAndSocial && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.marketing.webSocialHeader')}
          </SectionLabel>
          <Row className="g-2">
            {data.webAndSocial.website && (
              <Col sm={6} md={4}>
                <div className="mb-2">
                  <small className="text-muted d-block">
                    {t('company.lookup.enrichment.marketing.website')}
                  </small>
                  <a
                    href={data.webAndSocial.website}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <FontAwesomeIcon icon={faGlobe} className="me-1" />
                    {data.webAndSocial.website}
                  </a>
                </div>
              </Col>
            )}
            {data.webAndSocial.eCommerce && (
              <Col sm={6} md={4}>
                <div className="mb-2">
                  <small className="text-muted d-block">
                    {t('company.lookup.enrichment.marketing.eCommerce')}
                  </small>
                  <a
                    href={data.webAndSocial.eCommerce}
                    target="_blank"
                    rel="noreferrer"
                  >
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
                <a
                  key={key}
                  href={url}
                  target="_blank"
                  rel="noreferrer"
                  title={label}
                  className="btn btn-sm btn-outline-secondary"
                >
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
          <SectionLabel>
            {t('company.lookup.enrichment.marketing.employeesHeader')}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t('company.lookup.enrichment.marketing.employeeCount')}
              value={data.employees.employee}
            />
            <FieldRow
              label={t('company.lookup.enrichment.marketing.employeeRange')}
              value={data.employees.employeeRange?.description}
            />
            <FieldRow
              label={t('company.lookup.enrichment.marketing.employeeTrend')}
              value={data.employees.employeeTrend}
            />
          </Row>
        </>
      )}

      {/* Ecofin */}
      {data.ecofin && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.marketing.ecofinHeader')}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t('company.lookup.enrichment.marketing.ecofinBalanceDate')}
              value={
                data.ecofin.balanceSheetDate
                  ? formatItalianDate(data.ecofin.balanceSheetDate)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.marketing.ecofinTurnover')}
              value={
                data.ecofin.turnover != null
                  ? formatCurrency(data.ecofin.turnover)
                  : undefined
              }
            />
            <FieldRow
              label={t(
                'company.lookup.enrichment.marketing.ecofinTurnoverRange'
              )}
              value={data.ecofin.turnoverRange?.description}
            />
            <FieldRow
              label={t(
                'company.lookup.enrichment.marketing.ecofinTurnoverYear'
              )}
              value={data.ecofin.turnoverYear}
            />
            <FieldRow
              label={t(
                'company.lookup.enrichment.marketing.ecofinTurnoverTrend'
              )}
              value={data.ecofin.turnoverTrend}
            />
            <FieldRow
              label={t(
                'company.lookup.enrichment.marketing.ecofinShareCapital'
              )}
              value={
                data.ecofin.shareCapital != null
                  ? formatCurrency(data.ecofin.shareCapital)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.marketing.ecofinNetWorth')}
              value={
                data.ecofin.netWorth != null
                  ? formatCurrency(data.ecofin.netWorth)
                  : undefined
              }
            />
            <FieldRow
              label={t(
                'company.lookup.enrichment.marketing.ecofinEnterpriseSize'
              )}
              value={data.ecofin.enterpriseSize?.description}
            />
          </Row>
        </>
      )}

      {/* Branches */}
      {data.branches && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.marketing.branchesHeader')}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t('company.lookup.enrichment.marketing.numberOfBranches')}
              value={data.branches.numberOfBranches}
            />
          </Row>
        </>
      )}
    </div>
  );
};

export const StakeholdersSection = ({
  data
}: {
  data: NonNullable<CompanyLookup['stakeholders']>;
}) => {
  const { t } = useTranslation();
  return (
    <div>
      {data.managers && data.managers.length > 0 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.managers.header')}
          </SectionLabel>
          <ManagersTable managers={data.managers} />
        </>
      )}

      {data.shareholders && data.shareholders.length > 0 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.shareholders.header')}
          </SectionLabel>
          <ShareholdersTable shareholders={data.shareholders} />
        </>
      )}

      {data.corporateGroups && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.corporateGroups.header')}
          </SectionLabel>
          <CorporateGroupsCard data={data.corporateGroups} />
        </>
      )}

      {data.subsidiaries && data.subsidiaries.length > 0 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.subsidiaries.header')}
          </SectionLabel>
          <SubsidiariesTable subsidiaries={data.subsidiaries} />
        </>
      )}

      {data.affiliateCompanies && data.affiliateCompanies.length > 0 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.affiliates.header')}
          </SectionLabel>
          <AffiliatesTable affiliates={data.affiliateCompanies} />
        </>
      )}
    </div>
  );
};

export const AMLSection = ({
  data
}: {
  data: NonNullable<CompanyLookup['aml']>;
}) => {
  const { t } = useTranslation();
  return (
    <div>
      {/* RAE / SAE */}
      <Row className="g-2 mb-2">
        {data.rae && (
          <FieldRow
            label={t('company.lookup.enrichment.aml.rae')}
            value={`${data.rae.code} - ${data.rae.description}`}
          />
        )}
        {data.sae && (
          <FieldRow
            label={t('company.lookup.enrichment.aml.sae')}
            value={`${data.sae.code} - ${data.sae.description}`}
          />
        )}
      </Row>

      {/* Managers & Shareholders — reuse shared tables */}
      {data.managers && data.managers.length > 0 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.managers.header')}
          </SectionLabel>
          <ManagersTable managers={data.managers} />
        </>
      )}

      {data.shareholders && data.shareholders.length > 0 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.shareholders.header')}
          </SectionLabel>
          <ShareholdersTable shareholders={data.shareholders} />
        </>
      )}

      {/* Corporate Groups */}
      {data.corporateGroups && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.corporateGroups.header')}
          </SectionLabel>
          <CorporateGroupsCard data={data.corporateGroups} />
        </>
      )}

      {/* Foreign Trade */}
      {data.foreignTrade && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.aml.foreignTradeHeader')}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t('company.lookup.enrichment.aml.isImporter')}
              value={data.foreignTrade.isImporter}
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.importPercent')}
              value={
                data.foreignTrade.importPercentShare != null
                  ? `${data.foreignTrade.importPercentShare}%`
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.importCountries')}
              value={data.foreignTrade.importCountries}
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.isExporter')}
              value={data.foreignTrade.isExporter}
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.exportPercent')}
              value={
                data.foreignTrade.exportPercentShare != null
                  ? `${data.foreignTrade.exportPercentShare}%`
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.exportCountries')}
              value={data.foreignTrade.exportCountries}
            />
          </Row>
        </>
      )}

      {/* Public Tenders */}
      {data.publicTenders && data.publicTenders.length > 0 && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.aml.publicTendersHeader')}
          </SectionLabel>
          <div className="table-responsive">
            <Table bordered hover size="sm">
              <thead className="bg-body-tertiary">
                <tr>
                  <th>{t('company.lookup.enrichment.aml.tenderYear')}</th>
                  <th>{t('company.lookup.enrichment.aml.tenderApplied')}</th>
                  <th>{t('company.lookup.enrichment.aml.tenderWon')}</th>
                  <th>{t('company.lookup.enrichment.aml.tenderValue')}</th>
                </tr>
              </thead>
              <tbody>
                {data.publicTenders.map((tender, i) => (
                  <tr key={i}>
                    <td>{tender.year || '-'}</td>
                    <td>{tender.applied ?? '-'}</td>
                    <td>{tender.won ?? '-'}</td>
                    <td>
                      {tender.value != null
                        ? formatCurrency(tender.value)
                        : '-'}
                    </td>
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
          <SectionLabel>
            {t('company.lookup.enrichment.aml.operatingResultsHeader')}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t('company.lookup.enrichment.aml.ebitda')}
              value={
                data.operatingResults.ebitda != null
                  ? formatCurrency(data.operatingResults.ebitda)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.ebitdaPrev')}
              value={
                data.operatingResults.ebitdaL2Y != null
                  ? formatCurrency(data.operatingResults.ebitdaL2Y)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.ebit')}
              value={
                data.operatingResults.ebit != null
                  ? formatCurrency(data.operatingResults.ebit)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.ebitPrev')}
              value={
                data.operatingResults.ebitL2Y != null
                  ? formatCurrency(data.operatingResults.ebitL2Y)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.cashFlow')}
              value={
                data.operatingResults.cashFlow != null
                  ? formatCurrency(data.operatingResults.cashFlow)
                  : undefined
              }
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.cashFlowPrev')}
              value={
                data.operatingResults.cashFlowL2Y != null
                  ? formatCurrency(data.operatingResults.cashFlowL2Y)
                  : undefined
              }
            />
          </Row>
        </>
      )}

      {/* Debts */}
      {data.debts && (
        <>
          <SectionLabel>
            {t('company.lookup.enrichment.aml.debtsHeader')}
          </SectionLabel>
          <Row className="g-2">
            <FieldRow
              label={t('company.lookup.enrichment.aml.debtsCode')}
              value={data.debts.code}
            />
            <FieldRow
              label={t('company.lookup.enrichment.aml.debtsValue')}
              value={
                data.debts.value != null
                  ? formatCurrency(data.debts.value)
                  : undefined
              }
            />
          </Row>
        </>
      )}
    </div>
  );
};

/**
 * Self-contained enrichment panel: buttons, fetched-type badges, and collapsible data sections.
 * Manages its own loading/open state internally.
 */
export const EnrichmentPanel = ({
  company,
  onEnriched
}: {
  company: CompanyLookup;
  onEnriched: (updated: CompanyLookup) => void;
}) => {
  const { t } = useTranslation();
  const [activeEnrichment, setActiveEnrichment] =
    useState<EnrichmentType | null>(null);
  const [openSections, setOpenSections] = useState<Record<string, boolean>>({});
  const [triggerEnrich] = useLazyEnrichCompanyLookupQuery();

  const isEnriched = (type: string) =>
    company.fetchedTypes?.[type] !== undefined;

  const handleEnrich = async (type: EnrichmentType) => {
    if (activeEnrichment) return;
    setActiveEnrichment(type);
    try {
      const enriched = await triggerEnrich({
        taxCode: company.taxCode,
        type
      }).unwrap();
      onEnriched(enriched);
      if (type === 'full') {
        setOpenSections(prev => ({
          ...prev,
          advanced: true,
          marketing: true,
          stakeholders: true,
          aml: true
        }));
      } else {
        setOpenSections(prev => ({ ...prev, [type]: true }));
      }
    } catch {
      // Error handled by RTK Query
    } finally {
      setActiveEnrichment(null);
    }
  };

  const toggleSection = (key: string) => {
    setOpenSections(prev => ({ ...prev, [key]: !prev[key] }));
  };

  return (
    <>
      <div className="d-flex flex-wrap gap-2 mb-3">
        {ENRICHMENT_BUTTONS.map(({ type, labelKey, icon }) => {
          const loaded =
            type === 'full'
              ? ['advanced', 'marketing', 'stakeholders', 'aml'].every(
                  isEnriched
                )
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
              {t(labelKey)}
            </Button>
          );
        })}
      </div>

      {company.fetchedTypes && Object.keys(company.fetchedTypes).length > 0 && (
        <div className="d-flex flex-wrap gap-1 mb-3">
          {Object.entries(company.fetchedTypes).map(([type, timestamp]) => (
            <SubtleBadge key={type} bg="info">
              {t('company.lookup.enrichment.fetchedTypeBadge', {
                type,
                date: formatItalianDate(timestamp)
              })}
            </SubtleBadge>
          ))}
        </div>
      )}

      {company.advanced && (
        <EnrichmentSection
          title={t('company.lookup.enrichment.sections.advanced')}
          isOpen={!!openSections.advanced}
          onToggle={() => toggleSection('advanced')}
        >
          <AdvancedSection data={company.advanced} />
        </EnrichmentSection>
      )}

      {company.marketing && (
        <EnrichmentSection
          title={t('company.lookup.enrichment.sections.marketing')}
          isOpen={!!openSections.marketing}
          onToggle={() => toggleSection('marketing')}
        >
          <MarketingSection data={company.marketing} />
        </EnrichmentSection>
      )}

      {company.stakeholders && (
        <EnrichmentSection
          title={t('company.lookup.enrichment.sections.stakeholders')}
          isOpen={!!openSections.stakeholders}
          onToggle={() => toggleSection('stakeholders')}
        >
          <StakeholdersSection data={company.stakeholders} />
        </EnrichmentSection>
      )}

      {company.aml && (
        <EnrichmentSection
          title={t('company.lookup.enrichment.sections.aml')}
          isOpen={!!openSections.aml}
          onToggle={() => toggleSection('aml')}
        >
          <AMLSection data={company.aml} />
        </EnrichmentSection>
      )}
    </>
  );
};
