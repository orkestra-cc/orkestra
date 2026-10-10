package services

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// Fakes for the cross-tenant DSR slices. They hold full rows so the tests
// can assert every personal-data field the producer touches.

type dsrGrants struct {
	rows []models.LLMGrant
	err  error
}

func (d *dsrGrants) ListByUserAllTenants(_ context.Context, user string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for _, g := range d.rows {
		if g.UserUUID == user {
			out = append(out, g)
		}
	}
	return out, d.err
}

func (d *dsrGrants) DeleteByUser(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	kept := d.rows[:0]
	var n int64
	for _, g := range d.rows {
		if g.UserUUID == user {
			n++
			continue
		}
		kept = append(kept, g)
	}
	d.rows = kept
	return n, nil
}

func (d *dsrGrants) ListByGranter(_ context.Context, user string) ([]models.LLMGrant, error) {
	out := []models.LLMGrant{}
	for _, g := range d.rows {
		if g.GrantedBy == user {
			out = append(out, g)
		}
	}
	return out, d.err
}

func (d *dsrGrants) PseudonymizeGranter(_ context.Context, user string) (int64, error) {
	if d.err != nil {
		return 0, d.err
	}
	var n int64
	for i := range d.rows {
		if d.rows[i].GrantedBy == user {
			d.rows[i].GrantedBy = models.ErasedActor
			n++
		}
	}
	return n, nil
}

type dsrCreds struct{ rows []models.Credential }

func (d *dsrCreds) ListByCreator(_ context.Context, user string) ([]models.Credential, error) {
	out := []models.Credential{}
	for _, c := range d.rows {
		if c.CreatedBy == user {
			out = append(out, c)
		}
	}
	return out, nil
}

func (d *dsrCreds) PseudonymizeCreator(_ context.Context, user string) (int64, error) {
	var n int64
	for i := range d.rows {
		if d.rows[i].CreatedBy == user {
			d.rows[i].CreatedBy = models.ErasedActor
			n++
		}
	}
	return n, nil
}

type dsrModels struct{ rows []models.Model }

func (d *dsrModels) ListByCreator(_ context.Context, user string) ([]models.Model, error) {
	out := []models.Model{}
	for _, m := range d.rows {
		if m.CreatedBy == user {
			out = append(out, m)
		}
	}
	return out, nil
}

func (d *dsrModels) PseudonymizeCreator(_ context.Context, user string) (int64, error) {
	var n int64
	for i := range d.rows {
		if d.rows[i].CreatedBy == user {
			d.rows[i].CreatedBy = models.ErasedActor
			n++
		}
	}
	return n, nil
}

var (
	tm0 = time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	tm1 = tm0.Add(time.Hour)
	tm2 = tm0.Add(2 * time.Hour)
	tm3 = tm0.Add(3 * time.Hour)
	tm4 = tm0.Add(4 * time.Hour)
	tm5 = tm0.Add(5 * time.Hour)
	tm6 = tm0.Add(6 * time.Hour)
)

// dsrFixture: subject "u1" is grantee of g1, g2 and g5, granter of g3 and g5,
// creator of cred-1 and model-1. Everything else belongs to other people.
func dsrFixture() (*PIIProducer, *dsrGrants, *dsrCreds, *dsrModels) {
	g := &dsrGrants{rows: []models.LLMGrant{
		{UUID: "g1", TenantID: "t1", ModelUUID: "m1", UserUUID: "u1", GrantedBy: "admin1", CreatedAt: tm0},
		{UUID: "g2", TenantID: "t2", ModelUUID: "m2", UserUUID: "u1", GrantedBy: "u2", CreatedAt: tm1},
		{UUID: "g3", TenantID: "t1", ModelUUID: "m1", UserUUID: "u2", GrantedBy: "u1", CreatedAt: tm2},
		{UUID: "g4", TenantID: "t1", ModelUUID: "m3", UserUUID: "u3", GrantedBy: "admin1", CreatedAt: tm3},
		{UUID: "g5", TenantID: "t2", ModelUUID: "m2", UserUUID: "u1", GrantedBy: "u1", CreatedAt: tm4},
	}}
	c := &dsrCreds{rows: []models.Credential{
		{UUID: "cred-1", TenantID: "t1", Name: "Team OpenAI", Provider: models.ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
			Secret:      models.Envelope{Alg: models.EnvelopeAlgLocal, KeyID: "key-id-xyz", KeyVersion: 1, SchemaVersion: 1, Ciphertext: "CIPHERTEXT-SENTINEL"},
			SecretLast4: "L4ST", Status: models.CredentialStatusActive, CreatedBy: "u1", CreatedAt: tm5},
		{UUID: "cred-2", TenantID: "t1", Name: "Other", Provider: models.ProviderOllama, BaseURL: "http://ollama.internal:11434", Status: models.CredentialStatusActive, CreatedBy: "admin1", CreatedAt: tm0},
	}}
	m := &dsrModels{rows: []models.Model{
		{UUID: "model-1", TenantID: "t1", Name: "Fast", Provider: models.ProviderOpenAI, ModelID: "gpt-x", CreatedBy: "u1", CreatedAt: tm6},
		{UUID: "model-2", TenantID: "t1", Name: "Slow", Provider: models.ProviderOpenAI, ModelID: "gpt-y", CreatedBy: "admin1", CreatedAt: tm0},
	}}
	return NewPIIProducer(c, m, g, slog.Default()), g, c, m
}

func TestPIIProducer_Subject(t *testing.T) {
	p, _, _, _ := dsrFixture()
	if p.Subject() != "llm" {
		t.Fatalf("Subject = %q", p.Subject())
	}
}

func TestPIIProducer_ExportEveryField(t *testing.T) {
	p, _, _, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := out.(*PersonalDataExport)
	if !ok {
		t.Fatalf("export type = %T", out)
	}

	// Grants held as grantee (g1, g2, g5), every projected field.
	want := []ExportedGrant{
		{UUID: "g1", TenantID: "t1", ModelUUID: "m1", CreatedAt: tm0},
		{UUID: "g2", TenantID: "t2", ModelUUID: "m2", CreatedAt: tm1},
		{UUID: "g5", TenantID: "t2", ModelUUID: "m2", CreatedAt: tm4},
	}
	if len(b.Grants) != len(want) {
		t.Fatalf("grants = %+v", b.Grants)
	}
	for i := range want {
		if b.Grants[i] != want[i] {
			t.Errorf("grants[%d] = %+v, want %+v", i, b.Grants[i], want[i])
		}
	}

	// Grants the subject handed out (g3, g5): model + grantee + when.
	wantIssued := []ExportedIssuedGrant{
		{ModelUUID: "m1", UserUUID: "u2", CreatedAt: tm2},
		{ModelUUID: "m2", UserUUID: "u1", CreatedAt: tm4},
	}
	if len(b.GrantsIssued) != len(wantIssued) {
		t.Fatalf("grantsIssued = %+v", b.GrantsIssued)
	}
	for i := range wantIssued {
		if b.GrantsIssued[i] != wantIssued[i] {
			t.Errorf("grantsIssued[%d] = %+v, want %+v", i, b.GrantsIssued[i], wantIssued[i])
		}
	}

	// Credentials created: uuid + name + createdAt only.
	wantCred := ExportedCredentialRef{UUID: "cred-1", Name: "Team OpenAI", CreatedAt: tm5}
	if len(b.CredentialsCreated) != 1 || b.CredentialsCreated[0] != wantCred {
		t.Fatalf("credentialsCreated = %+v, want [%+v]", b.CredentialsCreated, wantCred)
	}
	// Models created.
	wantModel := ExportedModelRef{UUID: "model-1", Name: "Fast", CreatedAt: tm6}
	if len(b.ModelsCreated) != 1 || b.ModelsCreated[0] != wantModel {
		t.Fatalf("modelsCreated = %+v, want [%+v]", b.ModelsCreated, wantModel)
	}
}

// The DSR listing returns whole credentials, envelope included. None of it
// may reach the bundle that is handed to the data subject.
func TestPIIProducer_ExportNeverLeaksCredentialMaterial(t *testing.T) {
	p, _, _, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "u1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, forbidden := range []string{
		"secret", "Secret", "ciphertext", "Ciphertext", "CIPHERTEXT-SENTINEL", "secretLast4", "L4ST",
		"baseUrl", "api.openai.com", "keyId", "key-id-xyz", "keyVersion", "schemaVersion", "alg",
		"provider", "modelId", "gpt-x",
	} {
		if strings.Contains(s, forbidden) {
			t.Errorf("export JSON contains %q: %s", forbidden, s)
		}
	}
	// And the keys it does carry are exactly the intended ones.
	var generic map[string][]map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	wantKeys := map[string][]string{
		"grants":             {"createdAt", "modelUuid", "tenantId", "uuid"},
		"grantsIssued":       {"createdAt", "modelUuid", "userUuid"},
		"credentialsCreated": {"createdAt", "name", "uuid"},
		"modelsCreated":      {"createdAt", "name", "uuid"},
	}
	for section, keys := range wantKeys {
		rows := generic[section]
		if len(rows) == 0 {
			t.Errorf("section %q missing", section)
			continue
		}
		for _, row := range rows {
			if len(row) != len(keys) {
				t.Errorf("%s row has keys %v, want %v", section, row, keys)
			}
			for _, k := range keys {
				if _, ok := row[k]; !ok {
					t.Errorf("%s row missing key %q: %v", section, k, row)
				}
			}
		}
	}
}

func TestPIIProducer_ExportNothingIsNil(t *testing.T) {
	p, _, _, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "nobody")
	if err != nil || out != nil {
		t.Fatalf("no data must export untyped nil, got %#v, %v", out, err)
	}
}

func TestPIIProducer_ExportActorOnly(t *testing.T) {
	// A subject who only created a model (no grants anywhere) still has data.
	p, _, _, _ := dsrFixture()
	out, err := p.ExportPersonalData(context.Background(), "admin1")
	if err != nil || out == nil {
		t.Fatalf("admin1 export = %#v, %v", out, err)
	}
	b := out.(*PersonalDataExport)
	if len(b.Grants) != 0 || len(b.GrantsIssued) != 2 || len(b.CredentialsCreated) != 1 || len(b.ModelsCreated) != 1 {
		t.Fatalf("admin1 bundle = %+v", b)
	}
}

func TestPIIProducer_PurgeBothModes(t *testing.T) {
	for _, mode := range []iface.EraseMode{iface.EraseAnonymize, iface.EraseHardDelete} {
		p, g, c, m := dsrFixture()
		res, err := p.PurgePersonalData(context.Background(), "u1", mode)
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		// Deleted: g1, g2, g5 (grantee). Anonymized: g3 (granter) + cred-1 + model-1.
		// g5 was both grantee and granter; it is deleted, so not counted twice.
		if res.RowsDeleted != 3 || res.RowsAnonymized != 3 {
			t.Errorf("mode %v result = %+v, want 3 deleted / 3 anonymized", mode, res)
		}
		wantColls := map[string]bool{"llm_grants": true, "llm_credentials": true, "llm_models": true}
		if len(res.Collections) != len(wantColls) {
			t.Errorf("mode %v collections = %v", mode, res.Collections)
		}
		for _, coll := range res.Collections {
			if !wantColls[coll] {
				t.Errorf("mode %v unexpected collection %q", mode, coll)
			}
		}

		// llm_grants.userUuid: the subject's grant rows are gone.
		if len(g.rows) != 2 {
			t.Fatalf("mode %v remaining grants = %+v", mode, g.rows)
		}
		for _, row := range g.rows {
			if row.UserUUID == "u1" {
				t.Errorf("mode %v: grantee row survived: %+v", mode, row)
			}
		}
		// llm_grants.grantedBy: g3 rewritten, g4 untouched.
		byUUID := map[string]models.LLMGrant{}
		for _, row := range g.rows {
			byUUID[row.UUID] = row
		}
		if byUUID["g3"].GrantedBy != models.ErasedActor || byUUID["g3"].UserUUID != "u2" {
			t.Errorf("mode %v g3 = %+v", mode, byUUID["g3"])
		}
		if byUUID["g4"].GrantedBy != "admin1" || byUUID["g4"].UserUUID != "u3" {
			t.Errorf("mode %v g4 must be untouched: %+v", mode, byUUID["g4"])
		}
		// llm_credentials.createdBy.
		if c.rows[0].CreatedBy != models.ErasedActor || c.rows[0].Name != "Team OpenAI" {
			t.Errorf("mode %v cred-1 = %+v", mode, c.rows[0])
		}
		if c.rows[1].CreatedBy != "admin1" {
			t.Errorf("mode %v cred-2 must be untouched: %+v", mode, c.rows[1])
		}
		// llm_models.createdBy.
		if m.rows[0].CreatedBy != models.ErasedActor || m.rows[0].Name != "Fast" {
			t.Errorf("mode %v model-1 = %+v", mode, m.rows[0])
		}
		if m.rows[1].CreatedBy != "admin1" {
			t.Errorf("mode %v model-2 must be untouched: %+v", mode, m.rows[1])
		}

		// Nothing is left that names the subject.
		out, err := p.ExportPersonalData(context.Background(), "u1")
		if err != nil || out != nil {
			t.Errorf("mode %v export after purge = %#v, %v", mode, out, err)
		}
	}
}

func TestPIIProducer_PurgeUnknownSubjectIsNoop(t *testing.T) {
	p, g, _, _ := dsrFixture()
	res, err := p.PurgePersonalData(context.Background(), "nobody", iface.EraseHardDelete)
	if err != nil || res.RowsDeleted != 0 || res.RowsAnonymized != 0 || len(g.rows) != 5 {
		t.Fatalf("noop purge = %+v, %v, grants %d", res, err, len(g.rows))
	}
}

func TestPIIProducer_PropagatesRepositoryErrors(t *testing.T) {
	p, g, _, _ := dsrFixture()
	boom := errors.New("db down")
	g.err = boom
	if _, err := p.ExportPersonalData(context.Background(), "u1"); !errors.Is(err, boom) {
		t.Errorf("export err = %v", err)
	}
	if _, err := p.PurgePersonalData(context.Background(), "u1", iface.EraseHardDelete); !errors.Is(err, boom) {
		t.Errorf("purge err = %v", err)
	}
}

// M4: an empty subject would match every row created without a user in
// context, across every org. The producer refuses it before any query.
func TestPIIProducer_RejectsEmptySubject(t *testing.T) {
	p, g, c, m := dsrFixture()
	g.rows = append(g.rows, models.LLMGrant{UUID: "g-anon", TenantID: "t3", ModelUUID: "m9", UserUUID: "u4", GrantedBy: "", CreatedAt: tm0})
	c.rows = append(c.rows, models.Credential{UUID: "cred-anon", TenantID: "t3", Name: "Seeded", CreatedBy: ""})
	m.rows = append(m.rows, models.Model{UUID: "model-anon", TenantID: "t3", Name: "Seeded", CreatedBy: ""})

	if out, err := p.ExportPersonalData(context.Background(), ""); !errors.Is(err, ErrEmptySubject) || out != nil {
		t.Fatalf("export of an empty subject = %#v, %v", out, err)
	}
	res, err := p.PurgePersonalData(context.Background(), "", iface.EraseHardDelete)
	if !errors.Is(err, ErrEmptySubject) || res.RowsDeleted != 0 || res.RowsAnonymized != 0 {
		t.Fatalf("purge of an empty subject = %+v, %v", res, err)
	}
	if g.rows[len(g.rows)-1].GrantedBy != "" || c.rows[len(c.rows)-1].CreatedBy != "" || m.rows[len(m.rows)-1].CreatedBy != "" || len(g.rows) != 6 {
		t.Fatal("a purge of an empty subject rewrote rows")
	}
}
