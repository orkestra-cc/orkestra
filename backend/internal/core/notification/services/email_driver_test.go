package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/pkg/sdk/module"
)

type reqDriver struct {
	name string
	reqs []ProfileRequirement
}

func (d *reqDriver) Name() string                   { return d.name }
func (d *reqDriver) Requires() []ProfileRequirement { return d.reqs }
func (d *reqDriver) Send(context.Context, SenderProfile, EmailMessage) error {
	return nil
}

func TestValidateProfile_ViewsAndMissingFields(t *testing.T) {
	d := &reqDriver{name: "x", reqs: []ProfileRequirement{
		{Key: SubFromAddress}, {Key: SubSMTPHost}, {Key: SubSMTPPassword, Secret: true},
	}}
	p := SenderProfile{FromAddress: "a@b", SMTPHost: "h"}

	if err := ValidateProfile(d, p, SaveTimeView); err != nil {
		t.Fatalf("save-time view must ignore the missing secret, got %v", err)
	}
	err := ValidateProfile(d, p, RuntimeView)
	var inc *ProfileIncompleteError
	if !errors.As(err, &inc) || !errors.Is(err, ErrSenderNotConfigured) {
		t.Fatalf("want *ProfileIncompleteError wrapping ErrSenderNotConfigured, got %v", err)
	}
	if len(inc.Missing) != 1 || inc.Missing[0] != SubSMTPPassword || inc.Driver != "x" {
		t.Fatalf("Missing = %v Driver = %q", inc.Missing, inc.Driver)
	}
	if err := ValidateProfile(d, SenderProfile{}, RuntimeView); err == nil {
		t.Fatal("empty profile must fail")
	}
}

func TestSenderProfile_FieldRoundTrip(t *testing.T) {
	var p SenderProfile
	for _, key := range []string{SubProvider, SubFromAddress, SubFromName, SubReplyTo, SubSMTPHost, SubSMTPTLSMode, SubSMTPUsername, SubSMTPPassword, SubMailUpUser, SubMailUpSecret} {
		p.setField(key, "v-"+key)
		if got := p.Field(key); got != "v-"+key {
			t.Errorf("Field(%q) = %q", key, got)
		}
	}
	p.setField(SubSMTPPort, "2525")
	if p.SMTPPort != 2525 || p.Field(SubSMTPPort) != "2525" {
		t.Fatalf("port round trip: %d %q", p.SMTPPort, p.Field(SubSMTPPort))
	}
	if (SenderProfile{}).Field(SubSMTPPort) != "" {
		t.Fatal("zero port must read as unset")
	}
	if (SenderProfile{}).Field("nope") != "" {
		t.Fatal("unknown key must read empty")
	}
}

func TestLegacyProfile_SynthesizesDefault(t *testing.T) {
	p := LegacyProfile(SenderProfile{Provider: "", SMTPHost: "h"})
	if p.Slug != LegacySlug || p.Provider != "noop" || len(p.Categories) != 1 || p.Categories[0] != "*" {
		t.Fatalf("unexpected legacy profile %+v", p)
	}
	// Reserved by construction: the console can never mint this slug.
	if module.ValidSlug(LegacySlug) {
		t.Fatalf("LegacySlug %q must be outside the record-list slug grammar", LegacySlug)
	}
	if p.SMTPHost != "h" {
		t.Fatal("transport fields must be preserved")
	}
	if LegacyProfile(SenderProfile{Provider: "smtp"}).Provider != "smtp" {
		t.Fatal("a set provider must be kept")
	}
}

func TestDriverRegistry(t *testing.T) {
	r := NewDriverRegistry(&reqDriver{name: "b"}, &reqDriver{name: "a"})
	if _, ok := r.Get("a"); !ok {
		t.Fatal("registered driver not found")
	}
	if _, ok := r.Get("ses"); ok {
		t.Fatal("unknown driver must not resolve")
	}
	if n := r.Names(); len(n) != 2 || n[0] != "a" || n[1] != "b" {
		t.Fatalf("Names() = %v, want sorted", n)
	}
}
