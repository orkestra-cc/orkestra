package iface

import "slices"

// IPAddressMode is how a compliance policy treats client IP addresses in
// operational logs (spec §1.1).
type IPAddressMode string

const (
	IPAddressFull      IPAddressMode = "full"
	IPAddressTruncated IPAddressMode = "truncated"
	IPAddressHashed    IPAddressMode = "hashed"
	IPAddressOmitted   IPAddressMode = "omitted"
)

// UserAgentMode is how a compliance policy treats user agents in logs.
type UserAgentMode string

const (
	UserAgentFull    UserAgentMode = "full"
	UserAgentOmitted UserAgentMode = "omitted"
)

// SubjectIDMode is how a compliance policy treats user identifiers in logs.
type SubjectIDMode string

const (
	SubjectIDUUID    SubjectIDMode = "uuid"
	SubjectIDHashed  SubjectIDMode = "hashed"
	SubjectIDOmitted SubjectIDMode = "omitted"
)

// LogContentPolicy decides what reaches the operational logs. PIIKeys are
// normalized attribute keys (lowercase letters and digits only), sorted.
type LogContentPolicy struct {
	IPAddress    IPAddressMode `bson:"ipAddress" json:"ipAddress"`
	UserAgent    UserAgentMode `bson:"userAgent" json:"userAgent"`
	SubjectIDs   SubjectIDMode `bson:"subjectIds" json:"subjectIds"`
	PIIKeys      []string      `bson:"piiKeys" json:"piiKeys"`
	ScanFreeText bool          `bson:"scanFreeText" json:"scanFreeText"`
}

// Field names shared by LessRestrictiveContentFields and the validation issues.
const (
	FieldIPAddress    = "logContent.ipAddress"
	FieldUserAgent    = "logContent.userAgent"
	FieldSubjectIDs   = "logContent.subjectIds"
	FieldPIIKeys      = "logContent.piiKeys"
	FieldScanFreeText = "logContent.scanFreeText"
)

// DefaultPIIKeys is the platform default list. Matching is exact on the
// normalized key, so the list spells out variants instead of relying on a
// "contains" match that would also hit filename, hostname and similar.
func DefaultPIIKeys() []string {
	return []string{
		"address", "birthdate", "codicefiscale", "dateofbirth", "displayname",
		"email", "emailaddress", "firstname", "fiscalcode", "fullname", "iban",
		"lastname", "mobile", "phone", "phonenumber", "streetaddress", "taxcode",
		"username",
	}
}

// DefaultLogContentPolicy is the platform default and the boot-time policy
// used before the compliance module is up.
func DefaultLogContentPolicy() LogContentPolicy {
	return LogContentPolicy{
		IPAddress:    IPAddressTruncated,
		UserAgent:    UserAgentFull,
		SubjectIDs:   SubjectIDUUID,
		PIIKeys:      DefaultPIIKeys(),
		ScanFreeText: true,
	}
}

var (
	ipAddressRank = map[IPAddressMode]int{IPAddressFull: 0, IPAddressTruncated: 1, IPAddressHashed: 2, IPAddressOmitted: 3}
	userAgentRank = map[UserAgentMode]int{UserAgentFull: 0, UserAgentOmitted: 1}
	subjectIDRank = map[SubjectIDMode]int{SubjectIDUUID: 0, SubjectIDHashed: 1, SubjectIDOmitted: 2}
)

func (m IPAddressMode) Valid() bool { _, ok := ipAddressRank[m]; return ok }
func (m UserAgentMode) Valid() bool { _, ok := userAgentRank[m]; return ok }
func (m SubjectIDMode) Valid() bool { _, ok := subjectIDRank[m]; return ok }

// MostRestrictive merges two policies field by field, keeping the more
// restrictive value of each (spec §2.2). It never mutates its inputs.
func MostRestrictive(a, b LogContentPolicy) LogContentPolicy {
	out := a
	if ipAddressRank[b.IPAddress] > ipAddressRank[a.IPAddress] {
		out.IPAddress = b.IPAddress
	}
	if userAgentRank[b.UserAgent] > userAgentRank[a.UserAgent] {
		out.UserAgent = b.UserAgent
	}
	if subjectIDRank[b.SubjectIDs] > subjectIDRank[a.SubjectIDs] {
		out.SubjectIDs = b.SubjectIDs
	}
	keys := make([]string, 0, len(a.PIIKeys)+len(b.PIIKeys))
	keys = append(keys, a.PIIKeys...)
	keys = append(keys, b.PIIKeys...)
	slices.Sort(keys)
	out.PIIKeys = slices.Compact(keys)
	out.ScanFreeText = a.ScanFreeText || b.ScanFreeText
	return out
}

// LessRestrictiveContentFields lists, in a fixed order, the content fields
// on which candidate protects less than baseline (spec §2.2).
func LessRestrictiveContentFields(candidate, baseline LogContentPolicy) []string {
	var out []string
	if ipAddressRank[candidate.IPAddress] < ipAddressRank[baseline.IPAddress] {
		out = append(out, FieldIPAddress)
	}
	if userAgentRank[candidate.UserAgent] < userAgentRank[baseline.UserAgent] {
		out = append(out, FieldUserAgent)
	}
	if subjectIDRank[candidate.SubjectIDs] < subjectIDRank[baseline.SubjectIDs] {
		out = append(out, FieldSubjectIDs)
	}
	for _, k := range baseline.PIIKeys {
		if !slices.Contains(candidate.PIIKeys, k) {
			out = append(out, FieldPIIKeys)
			break
		}
	}
	if baseline.ScanFreeText && !candidate.ScanFreeText {
		out = append(out, FieldScanFreeText)
	}
	return out
}

// RetentionClass groups audit evidence by purpose and legal basis (spec
// §1.3). The catalog with purposes, legal bases and minimums lives in the
// compliance module (internal/core/compliance/retentionclass).
type RetentionClass string

const (
	RetentionAdminAccess            RetentionClass = "admin_access"
	RetentionPrivilegedChange       RetentionClass = "privileged_change"
	RetentionClientActivity         RetentionClass = "client_activity"
	RetentionAuthenticationSecurity RetentionClass = "authentication_security"
	RetentionComplianceEvidence     RetentionClass = "compliance_evidence"
)

// AllRetentionClasses lists the classes in catalog order.
func AllRetentionClasses() []RetentionClass {
	return []RetentionClass{
		RetentionAdminAccess, RetentionPrivilegedChange, RetentionClientActivity,
		RetentionAuthenticationSecurity, RetentionComplianceEvidence,
	}
}

func (c RetentionClass) Valid() bool { return slices.Contains(AllRetentionClasses(), c) }

// RetentionDecision is the retention a sink stamps on an event: the class,
// the policy (and version) that decided it, and the days to keep it.
type RetentionDecision struct {
	Class         RetentionClass `json:"class"`
	PolicyUUID    string         `json:"policyUuid"`
	PolicyVersion int            `json:"policyVersion"`
	Days          int            `json:"days"`
}

// CompliancePolicyProvider resolves the live compliance policy. It is
// published by the compliance module as module.ServiceCompliancePolicy and
// satisfies the slog PolicyHandler's resolver (LogContentFor).
type CompliancePolicyProvider interface {
	// LogContentFor returns the log-content policy of tenantID; "" asks for
	// the strictest policy in force. The pointer is immutable and stays the
	// same while the policy version does not change.
	LogContentFor(tenantID string) *LogContentPolicy
	// RetentionFor returns the retention of class for tenantID: the assigned
	// policy when it sets the class, the platform policy otherwise.
	RetentionFor(tenantID string, class RetentionClass) RetentionDecision
}
