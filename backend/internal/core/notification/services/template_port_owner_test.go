package services

import (
	"context"
	"errors"
	"testing"

	"github.com/orkestra/backend/internal/core/notification/models"
	"github.com/orkestra/backend/pkg/sdk/ctxauth"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

func tenantCtx(id string) context.Context {
	return context.WithValue(context.Background(), ctxauth.KeyTenantID, id)
}

func TestPortRequiresOwnerFromCtx(t *testing.T) {
	svc, _, _, _ := newHeaderKit(t, "https://app.example")
	err := svc.CreateTemplate(context.Background(), "digest:0f0e0d0c-0b0a-4908-8706-050403020100", "it", "s", "<b>h</b>", "t")
	if !errors.Is(err, iface.ErrTemplateOwnerRequired) {
		t.Fatalf("want ErrTemplateOwnerRequired, got %v", err)
	}
	if err := svc.UpsertTemplate(context.Background(), "digest:0f0e0d0c-0b0a-4908-8706-050403020100", "it", "s", "<b>h</b>", "t"); !errors.Is(err, iface.ErrTemplateOwnerRequired) {
		t.Fatalf("upsert without ctx tenant: %v", err)
	}
	if _, err := svc.DeleteTemplatesByPrefix(context.Background(), "digest:0f0e0d0c-0b0a-4908-8706-050403020100"); !errors.Is(err, iface.ErrTemplateOwnerRequired) {
		t.Fatalf("delete without ctx tenant: %v", err)
	}
}

func TestPortCreateIsInsertOnlyAndOwnerBound(t *testing.T) {
	svc, _, _, tmpl := newHeaderKit(t, "https://app.example")
	const id = "digest:0f0e0d0c-0b0a-4908-8706-050403020100"
	if err := svc.CreateTemplate(tenantCtx("A"), id, "it", "s", "h", "t"); err != nil {
		t.Fatal(err)
	}
	// The port stamps the ctx tenant as the owner and never writes through the
	// SYSTEM path: a snapshot that landed in the system store would be visible
	// to every tenant and to the operator template admin.
	doc, ok := tmpl.owned["A/"+id+"/it"]
	if !ok {
		t.Fatalf("no owned document for A: %v", tmpl.owned)
	}
	if doc.OwnerTenantID != "A" || doc.TemplateID != id || doc.Locale != "it" ||
		doc.Channel != models.ChannelEmail || doc.IsSystem || doc.Subject != "s" {
		t.Fatalf("owned document is not the ctx tenant's non-system email snapshot: %+v", doc)
	}
	if len(tmpl.store) != 0 {
		t.Fatalf("the port must not write a system template: %v", tmpl.store)
	}
	if err := svc.CreateTemplate(tenantCtx("A"), id, "it", "s2", "h2", "t2"); !errors.Is(err, iface.ErrTemplateExists) {
		t.Fatalf("second create: %v", err)
	}
	if _, err := svc.GetTemplate(tenantCtx("B"), id, "it"); !errors.Is(err, iface.ErrTemplateNotFound) {
		t.Fatalf("tenant B must not read A's snapshot: %v", err)
	}
	v, err := svc.GetTemplate(tenantCtx("A"), id, "it")
	if err != nil || v.Subject != "s" {
		t.Fatalf("owner read: %v %+v", err, v)
	}
	if n, err := svc.DeleteTemplatesByPrefix(tenantCtx("B"), id); err != nil || n != 0 {
		t.Fatalf("B deletes nothing: %d %v", n, err)
	}
	if n, err := svc.DeleteTemplatesByPrefix(tenantCtx("A"), id); err != nil || n != 1 {
		t.Fatalf("A deletes its own: %d %v", n, err)
	}
	if len(tmpl.owned) != 0 {
		t.Fatalf("A's snapshot survived its own delete: %v", tmpl.owned)
	}
	if _, err := svc.DeleteTemplatesByPrefix(tenantCtx("A"), "digest:"); !errors.Is(err, iface.ErrTemplateIDInvalid) {
		t.Fatalf("prefix without uuid must be refused: %v", err)
	}
}

// TestPortWritesRefuseAnUndeletableID pins the write-side half of the id
// grammar. DeleteTemplatesByPrefix is the ONLY way to remove an owned
// template — the admin surface is system-only and there is no single-document
// owned delete — so an id the delete grammar cannot name would be a row no
// surface can ever remove: a retention and erasure hazard. Both writes refuse
// it at write time instead.
func TestPortWritesRefuseAnUndeletableID(t *testing.T) {
	svc, _, _, tmpl := newHeaderKit(t, "https://app.example")
	for _, id := range []string{
		"welcome",           // no segments at all
		"digest:",           // empty tail
		"digest:not-a-uuid", // tail is not 36 chars
		"Digest:0f0e0d0c-0b0a-4908-8706-050403020100", // uppercase head
		"digest:0f0e0d0c-0b0a-4908-8706-05040302010z", // tail not hex
	} {
		if err := svc.CreateTemplate(tenantCtx("A"), id, "it", "s", "h", "t"); !errors.Is(err, iface.ErrTemplateIDInvalid) {
			t.Fatalf("CreateTemplate(%q): want ErrTemplateIDInvalid, got %v", id, err)
		}
		if err := svc.UpsertTemplate(tenantCtx("A"), id, "it", "s", "h", "t"); !errors.Is(err, iface.ErrTemplateIDInvalid) {
			t.Fatalf("UpsertTemplate(%q): want ErrTemplateIDInvalid, got %v", id, err)
		}
	}
	if len(tmpl.owned) != 0 {
		t.Fatalf("a refused write must leave nothing behind: %v", tmpl.owned)
	}

	// Realistic owned ids are accepted, and each is nameable by the delete
	// grammar.
	for _, id := range []string{
		"digest:0f0e0d0c-0b0a-4908-8706-050403020100",
		"digest:0f0e0d0c-0b0a-4908-8706-050403020100:run:0f0e0d0c-0b0a-4908-8706-050403020101",
		"preview:test:0f0e0d0c-0b0a-4908-8706-050403020100:0f0e0d0c-0b0a-4908-8706-050403020102",
	} {
		if err := svc.UpsertTemplate(tenantCtx("A"), id, "it", "s", "h", "t"); err != nil {
			t.Fatalf("UpsertTemplate(%q): %v", id, err)
		}
		if n, err := svc.DeleteTemplatesByPrefix(tenantCtx("A"), id); err != nil || n != 1 {
			t.Fatalf("DeleteTemplatesByPrefix(%q): %d %v", id, n, err)
		}
	}
}

func TestGetTemplateIsOwnerOnlyWhileSendResolvesSystemToo(t *testing.T) {
	svc, _, _, tmpl := newHeaderKit(t, "https://app.example")
	tmpl.system["auth.verify_email/en"] = &models.TemplateDoc{TemplateID: "auth.verify_email", Locale: "en", Subject: "sys"}
	if _, err := svc.GetTemplate(tenantCtx("A"), "auth.verify_email", "en"); !errors.Is(err, iface.ErrTemplateNotFound) {
		t.Fatalf("GetTemplate reads only the owner's documents: a system template is not returned, got %v", err)
	}
	if _, err := svc.GetTemplate(context.Background(), "auth.verify_email", "en"); !errors.Is(err, iface.ErrTemplateOwnerRequired) {
		t.Fatalf("no tenant in ctx: %v", err)
	}
	doc, err := tmpl.Resolve(tenantCtx("A"), "A", "auth.verify_email", "en")
	if err != nil || doc.Subject != "sys" {
		t.Fatalf("SendTemplated's resolution keeps the system fallback: %v", err)
	}
}

// TestSendTemplatedResolvesOwnerThenSystem exercises the cascade through the
// real service rather than the double: SendTemplated must hand the ctx tenant
// to Resolve, so an owned snapshot shadows the system template of the same id
// and a send with no tenant in ctx still finds the system one.
func TestSendTemplatedResolvesOwnerThenSystem(t *testing.T) {
	svc, driver, _, tmpl := newHeaderKit(t, "https://api.example")
	tmpl.system["auth.verify_email/en"] = &models.TemplateDoc{TemplateID: "auth.verify_email", Locale: "en", Subject: "sys"}
	tmpl.owned = map[string]*models.TemplateDoc{
		"A/auth.verify_email/en": {OwnerTenantID: "A", TemplateID: "auth.verify_email", Locale: "en", Subject: "owned"},
	}

	req := iface.TemplatedNotificationRequest{
		TemplateID: "auth.verify_email",
		Locale:     "en",
		Type:       models.TypeTransactional,
		Recipients: []iface.Recipient{{Address: "ada@example.test"}},
	}

	tmpl.renderedFor = nil
	if _, err := svc.SendTemplated(tenantCtx("A"), req); err != nil {
		t.Fatalf("SendTemplated (owner): %v", err)
	}
	if tmpl.renderedFor == nil || tmpl.renderedFor.Subject != "owned" {
		t.Fatalf("SendTemplated must render the ctx tenant's own snapshot, rendered %+v", tmpl.renderedFor)
	}

	tmpl.renderedFor = nil
	if _, err := svc.SendTemplated(context.Background(), req); err != nil {
		t.Fatalf("SendTemplated (system): %v", err)
	}
	if tmpl.renderedFor == nil || tmpl.renderedFor.Subject != "sys" {
		t.Fatalf("a send with no tenant in ctx must still resolve the system template, rendered %+v", tmpl.renderedFor)
	}
	if driver.sends != 2 {
		t.Fatalf("both sends must reach the driver, got %d", driver.sends)
	}
}
