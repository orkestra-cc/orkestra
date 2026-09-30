package iface

import (
	"slices"
	"testing"
)

func TestModesValid(t *testing.T) {
	if !IPAddressHashed.Valid() || IPAddressMode("partial").Valid() {
		t.Fatal("IPAddressMode.Valid")
	}
	if !UserAgentOmitted.Valid() || UserAgentMode("").Valid() {
		t.Fatal("UserAgentMode.Valid")
	}
	if !SubjectIDHashed.Valid() || SubjectIDMode("email").Valid() {
		t.Fatal("SubjectIDMode.Valid")
	}
}

func TestDefaultLogContentPolicy(t *testing.T) {
	p := DefaultLogContentPolicy()
	if p.IPAddress != IPAddressTruncated || p.UserAgent != UserAgentFull ||
		p.SubjectIDs != SubjectIDUUID || !p.ScanFreeText {
		t.Fatalf("unexpected defaults: %+v", p)
	}
	if !slices.IsSorted(p.PIIKeys) || !slices.Contains(p.PIIKeys, "email") ||
		slices.Contains(p.PIIKeys, "name") {
		t.Fatalf("pii keys must be sorted, include email, exclude bare name: %v", p.PIIKeys)
	}
	// The slice must be a fresh copy: mutating it cannot leak into the next call.
	p.PIIKeys[0] = "zzz"
	if DefaultLogContentPolicy().PIIKeys[0] == "zzz" {
		t.Fatal("DefaultLogContentPolicy shares its PIIKeys slice")
	}
}

func TestMostRestrictive(t *testing.T) {
	a := LogContentPolicy{IPAddress: IPAddressHashed, UserAgent: UserAgentFull, SubjectIDs: SubjectIDUUID, PIIKeys: []string{"email", "phone"}, ScanFreeText: false}
	b := LogContentPolicy{IPAddress: IPAddressTruncated, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDOmitted, PIIKeys: []string{"iban", "email"}, ScanFreeText: true}
	got := MostRestrictive(a, b)
	want := LogContentPolicy{IPAddress: IPAddressHashed, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDOmitted, PIIKeys: []string{"email", "iban", "phone"}, ScanFreeText: true}
	if got.IPAddress != want.IPAddress || got.UserAgent != want.UserAgent ||
		got.SubjectIDs != want.SubjectIDs || got.ScanFreeText != want.ScanFreeText ||
		!slices.Equal(got.PIIKeys, want.PIIKeys) {
		t.Fatalf("MostRestrictive = %+v, want %+v", got, want)
	}
	if !slices.Equal(a.PIIKeys, []string{"email", "phone"}) {
		t.Fatal("MostRestrictive mutated its input")
	}
}

func TestLessRestrictiveContentFields(t *testing.T) {
	base := LogContentPolicy{IPAddress: IPAddressTruncated, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDHashed, PIIKeys: []string{"email", "iban"}, ScanFreeText: true}
	same := base
	if got := LessRestrictiveContentFields(same, base); len(got) != 0 {
		t.Fatalf("identical policy reported %v", got)
	}
	looser := LogContentPolicy{IPAddress: IPAddressFull, UserAgent: UserAgentFull, SubjectIDs: SubjectIDUUID, PIIKeys: []string{"email"}, ScanFreeText: false}
	got := LessRestrictiveContentFields(looser, base)
	want := []string{FieldIPAddress, FieldUserAgent, FieldSubjectIDs, FieldPIIKeys, FieldScanFreeText}
	if !slices.Equal(got, want) {
		t.Fatalf("LessRestrictiveContentFields = %v, want %v", got, want)
	}
	stricter := LogContentPolicy{IPAddress: IPAddressOmitted, UserAgent: UserAgentOmitted, SubjectIDs: SubjectIDOmitted, PIIKeys: []string{"email", "iban", "phone"}, ScanFreeText: true}
	if got := LessRestrictiveContentFields(stricter, base); len(got) != 0 {
		t.Fatalf("stricter policy reported %v", got)
	}
}

func TestRetentionClassValid(t *testing.T) {
	all := AllRetentionClasses()
	if len(all) != 5 {
		t.Fatalf("AllRetentionClasses = %v, want 5 classes", all)
	}
	for _, c := range all {
		if !c.Valid() {
			t.Fatalf("%q not valid", c)
		}
	}
	if RetentionClass("forever").Valid() || RetentionClass("").Valid() {
		t.Fatal("unknown class accepted")
	}
}
