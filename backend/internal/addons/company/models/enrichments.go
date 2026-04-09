package models

// AdvancedData holds data from the IT-advanced endpoint
type AdvancedData struct {
	REACode             string               `bson:"reaCode,omitempty" json:"reaCode,omitempty"`
	CCIAA               string               `bson:"cciaa,omitempty" json:"cciaa,omitempty"`
	AtecoClassification *AtecoClassification `bson:"atecoClassification,omitempty" json:"atecoClassification,omitempty"`
	DetailedLegalForm   *LegalFormDetail     `bson:"detailedLegalForm,omitempty" json:"detailedLegalForm,omitempty"`
	PEC                 string               `bson:"pec,omitempty" json:"pec,omitempty"`
	StartDate           string               `bson:"startDate,omitempty" json:"startDate,omitempty"`
	EndDate             string               `bson:"endDate,omitempty" json:"endDate,omitempty"`
	TaxCodeCeased       *bool                `bson:"taxCodeCeased,omitempty" json:"taxCodeCeased,omitempty"`
	VATGroup            *VATGroupData        `bson:"vatGroup,omitempty" json:"vatGroup,omitempty"`
	BalanceSheets       *BalanceSheetsData   `bson:"balanceSheets,omitempty" json:"balanceSheets,omitempty"`
	ShareHolders        []AdvancedShareholder `bson:"shareHolders,omitempty" json:"shareHolders,omitempty"`
}

// AtecoClassification represents ATECO industry classification codes
type AtecoClassification struct {
	Ateco       *CodeDescription `bson:"ateco,omitempty" json:"ateco,omitempty"`
	Nace        *CodeDescription `bson:"nace,omitempty" json:"nace,omitempty"`
	Sector      *CodeDescription `bson:"sector,omitempty" json:"sector,omitempty"`
	Category    *CodeDescription `bson:"category,omitempty" json:"category,omitempty"`
	SubCategory *CodeDescription `bson:"subCategory,omitempty" json:"subCategory,omitempty"`
}

// VATGroupData holds VAT group participation info
type VATGroupData struct {
	VATGroupParticipation *bool `bson:"vatGroupParticipation,omitempty" json:"vatGroupParticipation,omitempty"`
	IsVATGroupLeader      *bool `bson:"isVatGroupLeader,omitempty" json:"isVatGroupLeader,omitempty"`
	RegistryOk            *bool `bson:"registryOk,omitempty" json:"registryOk,omitempty"`
}

// BalanceSheetsData holds balance sheet information
type BalanceSheetsData struct {
	Last *BalanceSheetEntry   `bson:"last,omitempty" json:"last,omitempty"`
	All  []BalanceSheetEntry  `bson:"all,omitempty" json:"all,omitempty"`
}

// BalanceSheetEntry represents a single balance sheet year
type BalanceSheetEntry struct {
	Year           *int     `bson:"year,omitempty" json:"year,omitempty"`
	Employees      *int     `bson:"employees,omitempty" json:"employees,omitempty"`
	BalanceSheetDate string `bson:"balanceSheetDate,omitempty" json:"balanceSheetDate,omitempty"`
	Turnover       *float64 `bson:"turnover,omitempty" json:"turnover,omitempty"`
	NetWorth       *float64 `bson:"netWorth,omitempty" json:"netWorth,omitempty"`
	ShareCapital   *float64 `bson:"shareCapital,omitempty" json:"shareCapital,omitempty"`
	TotalStaffCost *float64 `bson:"totalStaffCost,omitempty" json:"totalStaffCost,omitempty"`
	TotalAssets    *float64 `bson:"totalAssets,omitempty" json:"totalAssets,omitempty"`
	AvgGrossSalary *float64 `bson:"avgGrossSalary,omitempty" json:"avgGrossSalary,omitempty"`
}

// AdvancedShareholder represents a shareholder from the advanced endpoint
type AdvancedShareholder struct {
	CompanyName  string   `bson:"companyName,omitempty" json:"companyName,omitempty"`
	Name         string   `bson:"name,omitempty" json:"name,omitempty"`
	Surname      string   `bson:"surname,omitempty" json:"surname,omitempty"`
	TaxCode      string   `bson:"taxCode,omitempty" json:"taxCode,omitempty"`
	PercentShare *float64 `bson:"percentShare,omitempty" json:"percentShare,omitempty"`
}

// CodeDescription is a reusable code + description pair
type CodeDescription struct {
	Code        string `bson:"code,omitempty" json:"code,omitempty"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
}

// LegalFormDetail represents detailed legal form information
type LegalFormDetail struct {
	Code        string `bson:"code,omitempty" json:"code,omitempty"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
}

// ========================================
// Marketing sub-types
// ========================================

// ContactsData holds contact information
type ContactsData struct {
	TelephoneNumber string `bson:"telephoneNumber,omitempty" json:"telephoneNumber,omitempty"`
	Fax             string `bson:"fax,omitempty" json:"fax,omitempty"`
}

// WebAndSocialData holds web and social media presence
type WebAndSocialData struct {
	HasSocial *bool  `bson:"hasSocial,omitempty" json:"hasSocial,omitempty"`
	Website   string `bson:"website,omitempty" json:"website,omitempty"`
	ECommerce string `bson:"eCommerce,omitempty" json:"eCommerce,omitempty"`
	Facebook  string `bson:"facebook,omitempty" json:"facebook,omitempty"`
	Youtube   string `bson:"youtube,omitempty" json:"youtube,omitempty"`
	Twitter   string `bson:"twitter,omitempty" json:"twitter,omitempty"`
	Instagram string `bson:"instagram,omitempty" json:"instagram,omitempty"`
	Linkedin  string `bson:"linkedin,omitempty" json:"linkedin,omitempty"`
	Pinterest string `bson:"pinterest,omitempty" json:"pinterest,omitempty"`
	Vimeo     string `bson:"vimeo,omitempty" json:"vimeo,omitempty"`
}

// EmployeesData holds employee information
type EmployeesData struct {
	EmployeeRange *CodeDescription `bson:"employeeRange,omitempty" json:"employeeRange,omitempty"`
	Employee      *int             `bson:"employee,omitempty" json:"employee,omitempty"`
	EmployeeTrend *float64         `bson:"employeeTrend,omitempty" json:"employeeTrend,omitempty"`
}

// EcofinData holds economic/financial information
type EcofinData struct {
	BalanceSheetDate string           `bson:"balanceSheetDate,omitempty" json:"balanceSheetDate,omitempty"`
	TurnoverRange    *CodeDescription `bson:"turnoverRange,omitempty" json:"turnoverRange,omitempty"`
	TurnoverYear     *int             `bson:"turnoverYear,omitempty" json:"turnoverYear,omitempty"`
	Turnover         *float64         `bson:"turnover,omitempty" json:"turnover,omitempty"`
	TurnoverTrend    *float64         `bson:"turnoverTrend,omitempty" json:"turnoverTrend,omitempty"`
	ShareCapital     *float64         `bson:"shareCapital,omitempty" json:"shareCapital,omitempty"`
	NetWorth         *float64         `bson:"netWorth,omitempty" json:"netWorth,omitempty"`
	EnterpriseSize   *CodeDescription `bson:"enterpriseSize,omitempty" json:"enterpriseSize,omitempty"`
}

// BranchesData holds branch information
type BranchesData struct {
	NumberOfBranches *int `bson:"numberOfBranches,omitempty" json:"numberOfBranches,omitempty"`
}

// MarketingData holds data from the IT-marketing endpoint
type MarketingData struct {
	Contacts     *ContactsData     `bson:"contacts,omitempty" json:"contacts,omitempty"`
	WebAndSocial *WebAndSocialData `bson:"webAndSocial,omitempty" json:"webAndSocial,omitempty"`
	Mail         *MailData         `bson:"mail,omitempty" json:"mail,omitempty"`
	PEC          string            `bson:"pec,omitempty" json:"pec,omitempty"`
	Employees    *EmployeesData    `bson:"employees,omitempty" json:"employees,omitempty"`
	Ecofin       *EcofinData       `bson:"ecofin,omitempty" json:"ecofin,omitempty"`
	Branches     *BranchesData     `bson:"branches,omitempty" json:"branches,omitempty"`
	AllOffices   []OfficeEntry     `bson:"allOffices,omitempty" json:"allOffices,omitempty"`
}

// MailData holds email contact information
type MailData struct {
	Email string `bson:"email,omitempty" json:"email,omitempty"`
}

// OfficeEntry represents a single company office/branch
type OfficeEntry struct {
	CompanyDetails *OfficeCompanyDetails `bson:"companyDetails,omitempty" json:"companyDetails,omitempty"`
	CompanyStatus  *OfficeCompanyStatus  `bson:"companyStatus,omitempty" json:"companyStatus,omitempty"`
	Address        *OfficeAddress        `bson:"address,omitempty" json:"address,omitempty"`
}

// OfficeCompanyDetails holds office company details
type OfficeCompanyDetails struct {
	VATCode        string           `bson:"vatCode,omitempty" json:"vatCode,omitempty"`
	TaxCode        string           `bson:"taxCode,omitempty" json:"taxCode,omitempty"`
	LastUpdateDate string           `bson:"lastUpdateDate,omitempty" json:"lastUpdateDate,omitempty"`
	CCIAA          string           `bson:"cciaa,omitempty" json:"cciaa,omitempty"`
	REACode        string           `bson:"reaCode,omitempty" json:"reaCode,omitempty"`
	CompanyName    string           `bson:"companyName,omitempty" json:"companyName,omitempty"`
	OfficeType     *CodeDescription `bson:"officeType,omitempty" json:"officeType,omitempty"`
	OpenapiNumber  string           `bson:"openapiNumber,omitempty" json:"openapiNumber,omitempty"`
}

// OfficeCompanyStatus holds office activity status
type OfficeCompanyStatus struct {
	ActivityStatus *CodeDescription `bson:"activityStatus,omitempty" json:"activityStatus,omitempty"`
}

// OfficeAddress holds office address details
type OfficeAddress struct {
	StreetName string           `bson:"streetName,omitempty" json:"streetName,omitempty"`
	Hamlet     string           `bson:"hamlet,omitempty" json:"hamlet,omitempty"`
	ZipCode    string           `bson:"zipCode,omitempty" json:"zipCode,omitempty"`
	Town       string           `bson:"town,omitempty" json:"town,omitempty"`
	Province   *CodeDescription `bson:"province,omitempty" json:"province,omitempty"`
	Region     *CodeDescription `bson:"region,omitempty" json:"region,omitempty"`
	Country    *CodeDescription `bson:"country,omitempty" json:"country,omitempty"`
}

// ========================================
// Stakeholders / AML shared sub-types
// ========================================

// ManagerRole represents a role held by a manager
type ManagerRole struct {
	Role          *CodeDescription `bson:"role,omitempty" json:"role,omitempty"`
	RoleStartDate string           `bson:"roleStartDate,omitempty" json:"roleStartDate,omitempty"`
}

// Manager represents a company manager or director
type Manager struct {
	Name                  string           `bson:"name,omitempty" json:"name,omitempty"`
	Surname               string           `bson:"surname,omitempty" json:"surname,omitempty"`
	CompanyName           string           `bson:"companyName,omitempty" json:"companyName,omitempty"`
	TaxCode               string           `bson:"taxCode,omitempty" json:"taxCode,omitempty"`
	Roles                 []ManagerRole    `bson:"roles,omitempty" json:"roles,omitempty"`
	Gender                *CodeDescription `bson:"gender,omitempty" json:"gender,omitempty"`
	BirthDate             string           `bson:"birthDate,omitempty" json:"birthDate,omitempty"`
	Age                   *int             `bson:"age,omitempty" json:"age,omitempty"`
	BirthTown             string           `bson:"birthTown,omitempty" json:"birthTown,omitempty"`
	IsLegalRepresentative *bool            `bson:"isLegalRepresentative,omitempty" json:"isLegalRepresentative,omitempty"`
}

// ShareholderInfo represents information about a shareholder
type ShareholderInfo struct {
	TaxCode     string `bson:"taxCode,omitempty" json:"taxCode,omitempty"`
	Name        string `bson:"name,omitempty" json:"name,omitempty"`
	Surname     string `bson:"surname,omitempty" json:"surname,omitempty"`
	CompanyName string `bson:"companyName,omitempty" json:"companyName,omitempty"`
	SinceDate   string `bson:"sinceDate,omitempty" json:"sinceDate,omitempty"`
	StreetName  string `bson:"streetName,omitempty" json:"streetName,omitempty"`
	ZipCode     string `bson:"zipCode,omitempty" json:"zipCode,omitempty"`
	Town        string `bson:"town,omitempty" json:"town,omitempty"`
}

// Shareholder represents a company shareholder with ownership percentage
type Shareholder struct {
	ShareholdersInformation []ShareholderInfo `bson:"shareholdersInformation,omitempty" json:"shareholdersInformation,omitempty"`
	PercentShare            *float64          `bson:"percentShare,omitempty" json:"percentShare,omitempty"`
}

// NationalParentCompany represents the national parent company in a corporate group
type NationalParentCompany struct {
	CompanyName string           `bson:"companyName,omitempty" json:"companyName,omitempty"`
	StreetName  string           `bson:"streetName,omitempty" json:"streetName,omitempty"`
	Town        string           `bson:"town,omitempty" json:"town,omitempty"`
	ZipCode     string           `bson:"zipCode,omitempty" json:"zipCode,omitempty"`
	Province    *CodeDescription `bson:"province,omitempty" json:"province,omitempty"`
	Country     *CodeDescription `bson:"country,omitempty" json:"country,omitempty"`
}

// CorporateGroupsData holds corporate group information
type CorporateGroupsData struct {
	BelongsToGroup         *bool                  `bson:"belongsToGroup,omitempty" json:"belongsToGroup,omitempty"`
	GroupName              string                 `bson:"groupName,omitempty" json:"groupName,omitempty"`
	HoldingCompanyName     string                 `bson:"holdingCompanyName,omitempty" json:"holdingCompanyName,omitempty"`
	HoldingCountry         *CodeDescription       `bson:"holdingCountry,omitempty" json:"holdingCountry,omitempty"`
	NationalParentCompany  *NationalParentCompany `bson:"nationalParentCompany,omitempty" json:"nationalParentCompany,omitempty"`
	HasForeignParentCompany *bool                 `bson:"hasForeignParentCompany,omitempty" json:"hasForeignParentCompany,omitempty"`
}

// SubsidiaryCompany represents a subsidiary company
type SubsidiaryCompany struct {
	TaxCode     string           `bson:"taxCode,omitempty" json:"taxCode,omitempty"`
	CompanyName string           `bson:"companyName,omitempty" json:"companyName,omitempty"`
	StreetName  string           `bson:"streetName,omitempty" json:"streetName,omitempty"`
	ZipCode     string           `bson:"zipCode,omitempty" json:"zipCode,omitempty"`
	Town        string           `bson:"town,omitempty" json:"town,omitempty"`
	Province    *CodeDescription `bson:"province,omitempty" json:"province,omitempty"`
}

// AffiliateCompany represents an affiliate company
type AffiliateCompany struct {
	TaxCode      string   `bson:"taxCode,omitempty" json:"taxCode,omitempty"`
	CompanyName  string   `bson:"companyName,omitempty" json:"companyName,omitempty"`
	PercentShare *float64 `bson:"percentShare,omitempty" json:"percentShare,omitempty"`
}

// StakeholdersData holds data from the IT-stakeholders endpoint
type StakeholdersData struct {
	Managers           []Manager            `bson:"managers,omitempty" json:"managers,omitempty"`
	Shareholders       []Shareholder        `bson:"shareholders,omitempty" json:"shareholders,omitempty"`
	CorporateGroups    *CorporateGroupsData `bson:"corporateGroups,omitempty" json:"corporateGroups,omitempty"`
	Subsidiaries       []SubsidiaryCompany  `bson:"subsidiaries,omitempty" json:"subsidiaries,omitempty"`
	AffiliateCompanies []AffiliateCompany   `bson:"affiliateCompanies,omitempty" json:"affiliateCompanies,omitempty"`
}

// ========================================
// AML-specific sub-types
// ========================================

// ForeignTradeData holds import/export trade information
type ForeignTradeData struct {
	IsImporter        *bool    `bson:"isImporter,omitempty" json:"isImporter,omitempty"`
	ImportPercentShare *float64 `bson:"importPercentShare,omitempty" json:"importPercentShare,omitempty"`
	ImportCountries   string   `bson:"importCountries,omitempty" json:"importCountries,omitempty"`
	IsExporter        *bool    `bson:"isExporter,omitempty" json:"isExporter,omitempty"`
	ExportPercentShare *float64 `bson:"exportPercentShare,omitempty" json:"exportPercentShare,omitempty"`
	ExportCountries   string   `bson:"exportCountries,omitempty" json:"exportCountries,omitempty"`
}

// PublicTender represents a public tender record
type PublicTender struct {
	Year    string   `bson:"year,omitempty" json:"year,omitempty"`
	Applied *int     `bson:"applied,omitempty" json:"applied,omitempty"`
	Won     *int     `bson:"won,omitempty" json:"won,omitempty"`
	Value   *float64 `bson:"value,omitempty" json:"value,omitempty"`
}

// OperatingResultsData holds operating financial results
type OperatingResultsData struct {
	Ebitda      *float64 `bson:"ebitda,omitempty" json:"ebitda,omitempty"`
	EbitdaL2Y   *float64 `bson:"ebitdaL2Y,omitempty" json:"ebitdaL2Y,omitempty"`
	Ebit        *float64 `bson:"ebit,omitempty" json:"ebit,omitempty"`
	EbitL2Y     *float64 `bson:"ebitL2Y,omitempty" json:"ebitL2Y,omitempty"`
	CashFlow    *float64 `bson:"cashFlow,omitempty" json:"cashFlow,omitempty"`
	CashFlowL2Y *float64 `bson:"cashFlowL2Y,omitempty" json:"cashFlowL2Y,omitempty"`
}

// DebtsData holds debt information
type DebtsData struct {
	Code  string   `bson:"code,omitempty" json:"code,omitempty"`
	Value *float64 `bson:"value,omitempty" json:"value,omitempty"`
}

// AMLData holds data from the IT-aml endpoint (anti-money laundering)
type AMLData struct {
	Managers         []Manager             `bson:"managers,omitempty" json:"managers,omitempty"`
	Shareholders     []Shareholder         `bson:"shareholders,omitempty" json:"shareholders,omitempty"`
	CorporateGroups  *CorporateGroupsData  `bson:"corporateGroups,omitempty" json:"corporateGroups,omitempty"`
	ForeignTrade     *ForeignTradeData     `bson:"foreignTrade,omitempty" json:"foreignTrade,omitempty"`
	PublicTenders    []PublicTender        `bson:"publicTenders,omitempty" json:"publicTenders,omitempty"`
	OperatingResults *OperatingResultsData `bson:"operatingResults,omitempty" json:"operatingResults,omitempty"`
	Debts            []DebtsData           `bson:"debts,omitempty" json:"debts,omitempty"`
	RAE              *CodeDescription      `bson:"rae,omitempty" json:"rae,omitempty"`
	SAE              *CodeDescription      `bson:"sae,omitempty" json:"sae,omitempty"`
}
