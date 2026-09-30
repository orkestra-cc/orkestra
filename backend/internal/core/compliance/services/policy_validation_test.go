package services

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/compliance/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

var testNow = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func platformPolicy() *models.Policy {
	p := models.NewPlatformPolicy("platform", testNow)
	p.Accountability.RoPARef = "RoPA-1"
	p.Accountability.AssessmentRef = "LIA-1"
	return &p
}

// tenantInput is a clean tenant policy: same content as the platform,
// stricter retention, complete accountability, no warnings.
func tenantInput() models.PolicyInput {
	return models.PolicyInput{
		Name:       "Cliente sanità",
		LogContent: iface.DefaultLogContentPolicy(),
		Retention:  map[iface.RetentionClass]int{iface.RetentionPrivilegedChange: 400},
		Accountability: models.Accountability{
			Role: models.RoleProcessor, RoPARef: "RoPA-7", AssessmentRef: "DPIA-3",
			ReviewDueAt: testNow.AddDate(0, 6, 0),
		},
	}
}

func codes(issues []Issue) []string {
	out := []string{}
	for _, i := range issues {
		out = append(out, i.Code+"@"+i.Field)
	}
	return out
}

func TestValidatePolicy_CleanTenantPolicy(t *testing.T) {
	r := ValidatePolicy(PolicyCheck{Input: NormalizePolicyInput(tenantInput(), false), Platform: platformPolicy(), Now: testNow})
	if len(r.Errors) != 0 || len(r.Warnings) != 0 {
		t.Fatalf("errors=%v warnings=%v", codes(r.Errors), codes(r.Warnings))
	}
}

func TestValidatePolicy_Errors(t *testing.T) {
	cases := []struct {
		name     string
		platform bool
		mut      func(*models.PolicyInput)
		want     string
	}{
		{"empty name", false, func(in *models.PolicyInput) { in.Name = "  " }, "name_invalid@name"},
		{"long name", false, func(in *models.PolicyInput) { in.Name = strings.Repeat("n", 81) }, "name_invalid@name"},
		{"bad ip mode", false, func(in *models.PolicyInput) { in.LogContent.IPAddress = "partial" }, "invalid_enum@logContent.ipAddress"},
		{"bad ua mode", false, func(in *models.PolicyInput) { in.LogContent.UserAgent = "" }, "invalid_enum@logContent.userAgent"},
		{"bad subject mode", false, func(in *models.PolicyInput) { in.LogContent.SubjectIDs = "email" }, "invalid_enum@logContent.subjectIds"},
		{"bad role", false, func(in *models.PolicyInput) { in.Accountability.Role = "owner" }, "invalid_enum@accountability.role"},
		{"short pii key", false, func(in *models.PolicyInput) { in.LogContent.PIIKeys = []string{"x"} }, "invalid_pii_key@logContent.piiKeys"},
		{"non ascii pii key", false, func(in *models.PolicyInput) { in.LogContent.PIIKeys = []string{"città"} }, "invalid_pii_key@logContent.piiKeys"},
		{"unknown class", false, func(in *models.PolicyInput) { in.Retention["forever"] = 10 }, "invalid_enum@retention.forever"},
		{"platform-only class on tenant", false, func(in *models.PolicyInput) { in.Retention[iface.RetentionComplianceEvidence] = 100 }, "class_not_tenant_settable@retention.compliance_evidence"},
		{"admin access below the AdS floor", false, func(in *models.PolicyInput) { in.Retention[iface.RetentionAdminAccess] = 183 }, "class_below_minimum@retention.admin_access"},
		{"zero days", false, func(in *models.PolicyInput) { in.Retention[iface.RetentionClientActivity] = 0 }, "retention_out_of_range@retention.client_activity"},
		{"over ten years", false, func(in *models.PolicyInput) { in.Retention[iface.RetentionClientActivity] = 3651 }, "retention_out_of_range@retention.client_activity"},
		{"sinks on tenant", false, func(in *models.PolicyInput) { in.Sinks = models.DefaultSinkPolicy() }, "sinks_not_platform@sinks"},
		{"platform without sinks", true, func(in *models.PolicyInput) { in.Sinks = nil }, "sinks_missing@sinks"},
		{"loki warn below days", true, func(in *models.PolicyInput) { in.Sinks.Loki.WarnErrorDays = 7 }, "sink_invalid@sinks.loki.warnErrorDays"},
		{"tempo over a year", true, func(in *models.PolicyInput) { in.Sinks.Tempo.Days = 366 }, "sink_invalid@sinks.tempo.days"},
		{"dsr export over 90", true, func(in *models.PolicyInput) { in.Sinks.DSRExportDays = 91 }, "sink_invalid@sinks.dsrExportDays"},
		{"external sink without DPA", true, func(in *models.PolicyInput) {
			in.Sinks.External = []models.ExternalSink{{Name: "Grafana Cloud", Kind: models.ExternalKindOTLP, DeclaredRetentionDays: 30}}
		}, "sink_invalid@sinks.external[0]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := tenantInput()
			var platform *models.Policy = platformPolicy()
			if c.platform {
				in = platformPolicy().Input()
				platform = nil
			}
			c.mut(&in)
			r := ValidatePolicy(PolicyCheck{Input: NormalizePolicyInput(in, c.platform), IsPlatform: c.platform, Platform: platform, Now: testNow})
			if !slices.Contains(codes(r.Errors), c.want) {
				t.Fatalf("errors = %v, want %s", codes(r.Errors), c.want)
			}
		})
	}
}

func TestValidatePolicy_TooManyPIIKeys(t *testing.T) {
	in := tenantInput()
	for i := range 101 {
		in.LogContent.PIIKeys = append(in.LogContent.PIIKeys, "key"+strconv.Itoa(i))
	}
	r := ValidatePolicy(PolicyCheck{Input: NormalizePolicyInput(in, false), Platform: platformPolicy(), Now: testNow})
	if !slices.Contains(codes(r.Errors), "invalid_pii_key@logContent.piiKeys") {
		t.Fatalf("errors = %v", codes(r.Errors))
	}
}

func TestValidatePolicy_NameTaken(t *testing.T) {
	r := ValidatePolicy(PolicyCheck{Input: NormalizePolicyInput(tenantInput(), false), Platform: platformPolicy(), NameTaken: true, Now: testNow})
	if !r.HasError(IssueNameTaken) {
		t.Fatalf("errors = %v", codes(r.Errors))
	}
}

func TestValidatePolicy_Warnings(t *testing.T) {
	in := tenantInput()
	in.LogContent.IPAddress = iface.IPAddressFull
	in.LogContent.PIIKeys = []string{"email"}
	in.Retention[iface.RetentionAdminAccess] = 800 // over platform 365 and default 365
	in.Accountability.ReviewDueAt = testNow.AddDate(0, 0, -1)
	in.Accountability.AssessmentRef = ""
	r := ValidatePolicy(PolicyCheck{Input: NormalizePolicyInput(in, false), Platform: platformPolicy(), Now: testNow})
	if len(r.Errors) != 0 {
		t.Fatalf("errors = %v", codes(r.Errors))
	}
	got := r.WarningCodes()
	want := []string{WarnAccountabilityIncomplete, WarnIPFull, WarnLessRestrictiveThanPlatform, WarnRetentionLongerThanDefault, WarnReviewOverdue}
	if !slices.Equal(got, want) {
		t.Fatalf("warnings = %v, want %v", got, want)
	}
	for _, w := range r.Warnings {
		if w.Code != WarnLessRestrictiveThanPlatform {
			continue
		}
		fields, _ := w.Params["fields"].([]string)
		for _, f := range []string{iface.FieldIPAddress, iface.FieldPIIKeys, "retention.admin_access"} {
			if !slices.Contains(fields, f) {
				t.Fatalf("less restrictive fields = %v, missing %s", fields, f)
			}
		}
	}
}

func TestValidatePolicy_ZeroReviewDateIsOverdue(t *testing.T) {
	in := tenantInput()
	in.Accountability.ReviewDueAt = time.Time{}
	r := ValidatePolicy(PolicyCheck{Input: NormalizePolicyInput(in, false), Platform: platformPolicy(), Now: testNow})
	if !slices.Contains(r.WarningCodes(), WarnReviewOverdue) {
		t.Fatalf("warnings = %v", r.WarningCodes())
	}
}

func TestNormalizePolicyInput(t *testing.T) {
	in := tenantInput()
	in.Name = "  Cliente  "
	in.LogContent.PIIKeys = []string{"Phone_Number", "e-mail", "email", "phonenumber"}
	out := NormalizePolicyInput(in, false)
	if out.Name != "Cliente" || !slices.Equal(out.LogContent.PIIKeys, []string{"email", "phonenumber"}) {
		t.Fatalf("normalized = %q %v", out.Name, out.LogContent.PIIKeys)
	}
	if len(out.Retention) != 1 {
		t.Fatalf("tenant retention must not be filled: %v", out.Retention)
	}
	plat := platformPolicy().Input()
	delete(plat.Retention, iface.RetentionComplianceEvidence)
	if got := NormalizePolicyInput(plat, true).Retention[iface.RetentionComplianceEvidence]; got != 1825 {
		t.Fatalf("platform retention not filled with the default: %d", got)
	}
	in.LogContent.PIIKeys[0] = "mutated"
	if out.LogContent.PIIKeys[0] == "mutated" {
		t.Fatal("NormalizePolicyInput shares the PII keys")
	}
}

func TestNormalizeReason(t *testing.T) {
	if r, ok := NormalizeReason("  richiesta del DPO  "); !ok || r != "richiesta del DPO" {
		t.Fatalf("NormalizeReason = %q %v", r, ok)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("r", 501)} {
		if _, ok := NormalizeReason(bad); ok {
			t.Fatalf("reason %q accepted", bad[:min(len(bad), 10)])
		}
	}
}

func TestLessRestrictiveThanCurrent(t *testing.T) {
	platform := platformPolicy()
	strict := &models.Policy{UUID: "strict", LogContent: iface.DefaultLogContentPolicy(),
		Retention: map[iface.RetentionClass]int{iface.RetentionPrivilegedChange: 400}}
	strict.LogContent.IPAddress = iface.IPAddressOmitted
	// Back to the platform: IP truncated instead of omitted, 730 days instead of 400.
	w := LessRestrictiveThanCurrent(platform, strict, platform)
	if len(w) != 1 || w[0].Code != WarnLessRestrictiveThanCurrent {
		t.Fatalf("warnings = %v", codes(w))
	}
	fields, _ := w[0].Params["fields"].([]string)
	if !slices.Contains(fields, iface.FieldIPAddress) || !slices.Contains(fields, "retention.privileged_change") {
		t.Fatalf("fields = %v", fields)
	}
	// Towards a stricter policy: nothing to warn about.
	if w := LessRestrictiveThanCurrent(strict, platform, platform); len(w) != 0 {
		t.Fatalf("stricter move warned: %v", codes(w))
	}
}
