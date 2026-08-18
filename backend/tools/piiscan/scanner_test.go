package piiscan

import (
	"go/parser"
	"go/token"
	"testing"
)

// parseToFindings runs the file-level scanner against an inline source
// snippet attributed to a given module. We avoid go/packages so tests stay
// hermetic and don't require the surrounding module to be loadable.
func parseToFindings(t *testing.T, module, src string) *Findings {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "models.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := &Findings{Packages: 1}
	scanFile(fset, file, "internal/core/"+module+"/models/models.go", module, out)
	return out
}

func TestScan_DetectsSubjectFields(t *testing.T) {
	src := `package models

type Membership struct {
	ID        string ` + "`bson:\"_id\"`" + `
	UserUUID  string ` + "`bson:\"userUuid\" json:\"userUuid\"`" + `
	OrgUUID   string ` + "`bson:\"orgUuid\"`" + `
	GrantedBy string ` + "`bson:\"grantedBy\"`" + `
}

type Message struct {
	Recipient string ` + "`bson:\"recipientUserUuid,omitempty\"`" + `
}

// A request DTO with no bson tags must NOT be treated as persisted PII.
type CreateReq struct {
	UserUUID string ` + "`json:\"userUuid\"`" + `
}
`
	f := parseToFindings(t, "tenant", src)
	if len(f.SubjectFields) != 2 {
		t.Fatalf("expected 2 subject fields, got %d: %+v", len(f.SubjectFields), f.SubjectFields)
	}
	byTag := map[string]SubjectField{}
	for _, sf := range f.SubjectFields {
		byTag[sf.Tag] = sf
	}
	if sf, ok := byTag["userUuid"]; !ok || sf.Module != "tenant" || sf.Struct != "Membership" {
		t.Errorf("missing/bad userUuid field: %+v", sf)
	}
	if _, ok := byTag["recipientUserUuid"]; !ok {
		t.Errorf("recipientUserUuid not detected (tag name should be stripped of ,omitempty): %+v", byTag)
	}
	if _, ok := byTag["grantedBy"]; ok {
		t.Errorf("grantedBy (actor ref) must NOT be flagged as subject PII")
	}
	if _, ok := byTag["orgUuid"]; ok {
		t.Errorf("orgUuid must NOT be flagged as subject PII")
	}
}

// A fiscal identifier belonging to a natural person — a sole trader's VAT
// number, a private consumer's codice fiscale — is personal data under GDPR
// just as much as a platform user reference, and a module can persist it
// without ever holding a userUUID. Without these tags in the allow-list the
// gate reports a clean bill of health for a module holding years of fiscal
// personal data.
func TestScan_DetectsFiscalIdentityFields(t *testing.T) {
	src := `package models

type PartyData struct {
	FiscalIDCode  string ` + "`bson:\"fiscalIdCode\" json:\"fiscalIdCode\"`" + `
	CodiceFiscale string ` + "`bson:\"codiceFiscale,omitempty\"`" + `
	VATNumber     string ` + "`bson:\"vatNumber,omitempty\"`" + `
	FiscalCode    string ` + "`bson:\"fiscalCode,omitempty\"`" + `
	Denomination  string ` + "`bson:\"denomination\"`" + `
	SupplierID    string ` + "`bson:\"supplierId\"`" + `
}
`
	f := parseToFindings(t, "invoicing", src)

	byTag := map[string]bool{}
	for _, sf := range f.SubjectFields {
		byTag[sf.Tag] = true
	}
	for _, want := range []string{"fiscalIdCode", "codiceFiscale", "vatNumber", "fiscalCode"} {
		if !byTag[want] {
			t.Errorf("fiscal identifier %q not detected as subject PII: %+v", want, byTag)
		}
	}
	// Non-identifying columns must stay out: a legal name alone is not a
	// stable subject key, and supplierId is an internal reference.
	if byTag["denomination"] {
		t.Errorf("denomination must NOT be flagged — it is not a subject identifier")
	}
	if byTag["supplierId"] {
		t.Errorf("supplierId must NOT be flagged — internal reference, not a subject key")
	}
}

func TestScan_DetectsProducer(t *testing.T) {
	src := `package services

type piiProducer struct{}

func NewPIIProducer(repo any) any { return &piiProducer{} }
`
	f := parseToFindings(t, "notification", src)
	if len(f.Producers) != 1 || f.Producers[0].Module != "notification" {
		t.Fatalf("expected 1 producer for notification, got %+v", f.Producers)
	}
}

func TestModuleFromPkgPath(t *testing.T) {
	cases := map[string]string{
		"github.com/orkestra/backend/internal/core/notification/services": "notification",
		"github.com/orkestra/backend/internal/core/tenant/models":         "tenant",
		"github.com/orkestra/backend/internal/addons/billing/repository":  "billing",
		"github.com/orkestra/backend/internal/core/authz":                 "authz",
		"github.com/orkestra/backend/internal/shared/blob":                "",
		"github.com/orkestra/backend/cmd/server":                          "",
		"github.com/orkestra/backend/pkg/sdk/iface":                       "",
	}
	for in, want := range cases {
		if got := moduleFromPkgPath(in); got != want {
			t.Errorf("moduleFromPkgPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"userUUID":          "useruuid",
		"userUuid":          "useruuid",
		"user_uuid":         "useruuid",
		"recipientUserUuid": "recipientuseruuid",
	}
	for in, want := range cases {
		if got := normalizeTag(in); got != want {
			t.Errorf("normalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReconcile_FlagsModuleWithoutProducer(t *testing.T) {
	f := &Findings{
		SubjectFields: []SubjectField{
			{Module: "withprod", Struct: "S", Field: "UserUUID", Tag: "userUuid", Pos: Position{File: "a.go", Line: 1}},
			{Module: "noprod", Struct: "T", Field: "UserUUID", Tag: "userUuid", Pos: Position{File: "b.go", Line: 2}},
		},
		Producers: []Producer{
			{Module: "withprod", Pos: Position{File: "a.go", Line: 9}},
		},
	}
	report := Reconcile(f, nil)
	if !report.HasErrors() {
		t.Fatalf("expected an error for the module without a producer")
	}
	var flagged []string
	for _, d := range report.Diagnostics {
		flagged = append(flagged, d.Category+":"+d.Key)
	}
	if len(flagged) != 1 || flagged[0] != CategoryNoProducer+":noprod" {
		t.Errorf("expected only noprod flagged, got %+v", flagged)
	}
}

func TestReconcile_BaselineSuppresses(t *testing.T) {
	f := &Findings{
		SubjectFields: []SubjectField{
			{Module: "compliance", Struct: "ErasureRequest", Field: "UserUUID", Tag: "userUuid", Pos: Position{File: "a.go", Line: 1}},
		},
	}
	baseline := map[string]bool{CategoryNoProducer + ":compliance": true}
	report := Reconcile(f, baseline)
	if report.HasErrors() {
		t.Errorf("baseline should suppress the compliance gap, got: %+v", report.Diagnostics)
	}
	if len(report.Diagnostics) != 0 {
		t.Errorf("expected no surviving diagnostics, got %d", len(report.Diagnostics))
	}
}
