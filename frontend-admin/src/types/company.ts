// ========================================
// Company Lookup Types
// ========================================

export interface CompanyAddress {
  street: string;
  streetNumber: string;
  town: string;
  province: string;
  zipCode: string;
  region: string;
}

export interface CompanyLookup {
  uuid: string;
  taxCode: string;
  companyName: string;
  vatCode: string;
  activityStatus: string;
  sdiCode: string;
  registrationDate: string;
  address: CompanyAddress;
  sourceId: string;
  createdAt: string;
  updatedAt: string;

  // Enrichment tracking: maps lookup type → ISO timestamp of when it was fetched
  fetchedTypes?: Record<string, string>;

  // Enrichment data (present only when fetched)
  advanced?: AdvancedData;
  marketing?: MarketingData;
  stakeholders?: StakeholdersData;
  aml?: AMLData;
}

// ========================================
// Enrichment Types
// ========================================

export type EnrichmentType = 'advanced' | 'marketing' | 'stakeholders' | 'aml' | 'full';

export interface CodeDescription {
  code: string;
  description: string;
}

// ========================================
// Marketing Sub-Types
// ========================================

export interface ContactsData {
  telephoneNumber?: string;
  fax?: string;
}

export interface WebAndSocialData {
  hasSocial?: boolean;
  website?: string;
  eCommerce?: string;
  facebook?: string;
  youtube?: string;
  twitter?: string;
  instagram?: string;
  linkedin?: string;
  pinterest?: string;
  vimeo?: string;
}

export interface EmployeesData {
  employeeRange?: CodeDescription;
  employee?: number;
  employeeTrend?: number;
}

export interface EcofinData {
  balanceSheetDate?: string;
  turnoverRange?: CodeDescription;
  turnoverYear?: number;
  turnover?: number;
  turnoverTrend?: number;
  shareCapital?: number;
  netWorth?: number;
  enterpriseSize?: CodeDescription;
}

export interface BranchesData {
  numberOfBranches?: number;
}

// ========================================
// Stakeholders Sub-Types
// ========================================

export interface ManagerRole {
  role?: CodeDescription;
  roleStartDate?: string;
}

export interface Manager {
  name?: string;
  surname?: string;
  companyName?: string;
  taxCode?: string;
  roles?: ManagerRole[];
  gender?: CodeDescription;
  birthDate?: string;
  age?: number;
  birthTown?: string;
  isLegalRepresentative?: boolean;
}

export interface ShareholderInfo {
  taxCode?: string;
  name?: string;
  surname?: string;
  companyName?: string;
  sinceDate?: string;
  streetName?: string;
  zipCode?: string;
  town?: string;
}

export interface Shareholder {
  shareholdersInformation?: ShareholderInfo[];
  percentShare?: number;
}

export interface CorporateGroupsData {
  belongsToGroup?: boolean;
  groupName?: string;
  holdingCompanyName?: string;
  holdingCountry?: CodeDescription;
  nationalParentCompany?: {
    companyName?: string;
    streetName?: string;
    town?: string;
    zipCode?: string;
    province?: CodeDescription;
    country?: CodeDescription;
  };
  hasForeignParentCompany?: boolean;
}

export interface SubsidiaryCompany {
  taxCode?: string;
  companyName?: string;
  streetName?: string;
  zipCode?: string;
  town?: string;
  province?: CodeDescription;
}

export interface AffiliateCompany {
  taxCode?: string;
  companyName?: string;
  percentShare?: number;
}

// ========================================
// AML Sub-Types
// ========================================

export interface ForeignTradeData {
  isImporter?: boolean;
  importPercentShare?: number;
  importCountries?: string;
  isExporter?: boolean;
  exportPercentShare?: number;
  exportCountries?: string;
}

export interface PublicTender {
  year?: string;
  applied?: number;
  won?: number;
  value?: number;
}

export interface OperatingResultsData {
  ebitda?: number;
  ebitdaL2Y?: number;
  ebit?: number;
  ebitL2Y?: number;
  cashFlow?: number;
  cashFlowL2Y?: number;
}

export interface DebtsData {
  code?: string;
  value?: number;
}

export interface AtecoClassification {
  ateco?: CodeDescription;
  nace?: CodeDescription;
  sector?: CodeDescription;
  category?: CodeDescription;
  subCategory?: CodeDescription;
}

export interface LegalFormDetail {
  code: string;
  description: string;
}

export interface VATGroupData {
  vatGroupParticipation?: boolean;
  isVatGroupLeader?: boolean;
  registryOk?: boolean;
}

export interface BalanceSheetEntry {
  year?: number;
  employees?: number;
  balanceSheetDate?: string;
  turnover?: number;
  netWorth?: number;
  shareCapital?: number;
  totalStaffCost?: number;
  totalAssets?: number;
  avgGrossSalary?: number;
}

export interface BalanceSheetsData {
  last?: BalanceSheetEntry;
  all?: BalanceSheetEntry[];
}

export interface AdvancedShareholder {
  companyName?: string;
  name?: string;
  surname?: string;
  taxCode?: string;
  percentShare?: number;
}

export interface AdvancedData {
  reaCode?: string;
  cciaa?: string;
  atecoClassification?: AtecoClassification;
  detailedLegalForm?: LegalFormDetail;
  pec?: string;
  startDate?: string;
  endDate?: string;
  taxCodeCeased?: boolean;
  vatGroup?: VATGroupData;
  balanceSheets?: BalanceSheetsData;
  shareHolders?: AdvancedShareholder[];
}

export interface MarketingData {
  contacts?: ContactsData;
  webAndSocial?: WebAndSocialData;
  mail?: unknown;
  pec?: string;
  employees?: EmployeesData;
  ecofin?: EcofinData;
  branches?: BranchesData;
  allOffices?: unknown[];
}

export interface StakeholdersData {
  managers?: Manager[];
  shareholders?: Shareholder[];
  corporateGroups?: CorporateGroupsData;
  subsidiaries?: SubsidiaryCompany[];
  affiliateCompanies?: AffiliateCompany[];
}

export interface AMLData {
  managers?: Manager[];
  shareholders?: Shareholder[];
  corporateGroups?: CorporateGroupsData;
  foreignTrade?: ForeignTradeData;
  publicTenders?: PublicTender[];
  operatingResults?: OperatingResultsData;
  debts?: DebtsData;
  rae?: CodeDescription;
  sae?: CodeDescription;
}

export interface EnrichCompanyParams {
  taxCode: string;
  type: EnrichmentType;
}

export interface CompanyLookupListResponse {
  lookups: CompanyLookup[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
}

export interface CompanyLookupListParams {
  page?: number;
  pageSize?: number;
  [key: string]: string | number | boolean | undefined;
}

export interface CompanyLookupSearchParams {
  q: string;
  page?: number;
  pageSize?: number;
  [key: string]: string | number | boolean | undefined;
}

// ========================================
// Company Search (IT-search API) Types
// ========================================

export interface CompanySearchApiParams {
  companyName?: string;
  autocomplete?: string;
  province?: string;
  townCode?: string;
  atecoCode?: string;
  cciaa?: string;
  reaCode?: string;
  minTurnover?: number;
  maxTurnover?: number;
  minEmployees?: number;
  maxEmployees?: number;
  sdiCode?: string;
  legalFormCode?: string;
  pec?: string;
  shareHolderTaxCode?: string;
  lat?: number;
  long?: number;
  radius?: number;
  activityStatus?: string;
  dataEnrichment?: string;
  dryRun?: number;
  limit?: number;
  skip?: number;
}

export interface CompanySearchResult {
  companies: CompanyLookup[];
  totalResults?: number;
  limit: number;
  skip: number;
  dryRun: boolean;
}

// ========================================
// Constants for UI
// ========================================

export const ACTIVITY_STATUS_COLORS: Record<string, string> = {
  ATTIVA: 'success',
  CESSATA: 'danger',
  SOSPESA: 'warning',
};

export const ACTIVITY_STATUS_LABELS: Record<string, string> = {
  ATTIVA: 'Attiva',
  CESSATA: 'Cessata',
  SOSPESA: 'Sospesa',
};
