package odoo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
)

// fakeOdoo stands up a minimal Odoo 19 JSON-2 mock backed by an
// in-memory partner + category set. The mock only honours the
// fields the adapter actually reads.
type fakeOdoo struct {
	partners   []map[string]any
	categories map[int64]string
	calls      int32
}

func (f *fakeOdoo) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.calls, 1)
		var opts SearchReadOpts
		_ = json.NewDecoder(r.Body).Decode(&opts)
		switch {
		case strings.HasSuffix(r.URL.Path, "/res.partner/search_read"):
			f.servePartners(w, opts)
		case strings.HasSuffix(r.URL.Path, "/res.partner.category/search_read"):
			f.serveCategories(w, opts)
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *fakeOdoo) servePartners(w http.ResponseWriter, opts SearchReadOpts) {
	all := f.partners
	if opts.Domain != nil {
		// honour `["id", "in", [...]]`
		all = filterByIDDomain(all, opts.Domain)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = len(all)
	}
	end := opts.Offset + limit
	if end > len(all) {
		end = len(all)
	}
	page := []map[string]any{}
	if opts.Offset < len(all) {
		page = all[opts.Offset:end]
	}
	_ = json.NewEncoder(w).Encode(SearchReadResult{Records: page, TotalCount: len(all)})
}

func (f *fakeOdoo) serveCategories(w http.ResponseWriter, opts SearchReadOpts) {
	out := []map[string]any{}
	wantedIDs := extractIDsFromDomain(opts.Domain)
	for _, id := range wantedIDs {
		if name, ok := f.categories[id]; ok {
			out = append(out, map[string]any{"id": float64(id), "name": name})
		}
	}
	_ = json.NewEncoder(w).Encode(SearchReadResult{Records: out, TotalCount: len(out)})
}

func filterByIDDomain(all []map[string]any, domain []any) []map[string]any {
	ids := extractIDsFromDomain(domain)
	if len(ids) == 0 {
		return all
	}
	want := make(map[int64]struct{})
	for _, id := range ids {
		want[id] = struct{}{}
	}
	filtered := make([]map[string]any, 0)
	for _, row := range all {
		id := coerceInt64(row["id"])
		if _, ok := want[id]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func extractIDsFromDomain(domain []any) []int64 {
	for _, triple := range domain {
		t, ok := triple.([]any)
		if !ok || len(t) != 3 {
			continue
		}
		field, _ := t[0].(string)
		op, _ := t[1].(string)
		if field != "id" || op != "in" {
			continue
		}
		raw, ok := t[2].([]any)
		if !ok {
			continue
		}
		ids := make([]int64, 0, len(raw))
		for _, v := range raw {
			ids = append(ids, coerceInt64(v))
		}
		return ids
	}
	return nil
}

func newAdapterFor(t *testing.T, f *fakeOdoo, cfg ImportConfig) (importers.Source, func()) {
	t.Helper()
	srv := httptest.NewServer(f.handler(t))
	cfg.BaseURL = srv.URL
	if cfg.Database == "" {
		cfg.Database = "db"
	}
	if cfg.APIKey == "" {
		cfg.APIKey = "k"
	}
	body, _ := json.Marshal(cfg)
	a := New()
	src, err := a.Parse(bytes.NewReader(body), importers.ColumnMapping{})
	if err != nil {
		srv.Close()
		t.Fatalf("Parse: %v", err)
	}
	cleanup := func() {
		_ = src.Close()
		srv.Close()
	}
	return src, cleanup
}

func collect(t *testing.T, src importers.Source) []importers.CanonicalRecord {
	t.Helper()
	got := []importers.CanonicalRecord{}
	for rec := range src.Records() {
		got = append(got, rec)
	}
	if err := src.Err(); err != nil {
		t.Fatalf("Source.Err: %v", err)
	}
	return got
}

func TestAdapter_Name(t *testing.T) {
	if name := New().Name(); name != "odoo" {
		t.Fatalf("Name() = %q", name)
	}
}

func TestAdapter_Capabilities(t *testing.T) {
	c := New().DescribeCapabilities()
	if c.SheetSelection {
		t.Error("SheetSelection should be false for Odoo")
	}
	if !c.ActivityEmission {
		t.Error("ActivityEmission should be true (Phase 4+ may opt-in mail.message)")
	}
}

func TestAdapter_Parse_RejectsBadJSON(t *testing.T) {
	_, err := New().Parse(bytes.NewReader([]byte("not json")), importers.ColumnMapping{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdapter_Parse_MissingCredentials(t *testing.T) {
	body, _ := json.Marshal(ImportConfig{BaseURL: "http://x"})
	_, err := New().Parse(bytes.NewReader(body), importers.ColumnMapping{})
	if err == nil {
		t.Fatal("expected error for missing database+apiKey")
	}
}

func TestAdapter_StreamsOrgAndPerson(t *testing.T) {
	f := &fakeOdoo{
		partners: []map[string]any{
			{"id": float64(1), "name": "ACME S.p.A.", "is_company": true, "vat": "IT01234567890", "email": "info@acme.example", "parent_id": false},
			{"id": float64(2), "name": "Mario Rossi", "is_company": false, "parent_id": []any{float64(1), "ACME S.p.A."}, "email": "mario@acme.example", "function": "CEO"},
			{"id": float64(3), "name": "Standalone Sam", "is_company": false, "parent_id": false, "email": "sam@s.example"},
		},
		categories: map[int64]string{},
	}
	src, cleanup := newAdapterFor(t, f, ImportConfig{PageSize: 50})
	defer cleanup()
	got := collect(t, src)
	if len(got) != 3 {
		t.Fatalf("got %d records, want 3", len(got))
	}
	if got[0].OrgLegalName != "ACME S.p.A." {
		t.Errorf("row0 legalName=%q", got[0].OrgLegalName)
	}
	// Row 1: person under parent → carries both OrgLegalName + PersonFirstName.
	if got[1].OrgLegalName != "ACME S.p.A." {
		t.Errorf("row1 OrgLegalName=%q (parent should resolve from inline name)", got[1].OrgLegalName)
	}
	if got[1].PersonFirstName != "Mario" || got[1].PersonLastName != "Rossi" {
		t.Errorf("row1 name: %q %q", got[1].PersonFirstName, got[1].PersonLastName)
	}
	if got[2].PersonFirstName != "Standalone" || got[2].OrgLegalName != "" {
		t.Errorf("row2 standalone: %+v", got[2])
	}
}

func TestAdapter_PaginatesThroughMultiplePages(t *testing.T) {
	partners := []map[string]any{}
	for i := 1; i <= 7; i++ {
		partners = append(partners, map[string]any{
			"id":         float64(i),
			"name":       "Partner " + strconvI(i),
			"is_company": false,
			"parent_id":  false,
			"email":      "p" + strconvI(i) + "@x.example",
		})
	}
	f := &fakeOdoo{partners: partners}
	src, cleanup := newAdapterFor(t, f, ImportConfig{PageSize: 3})
	defer cleanup()
	got := collect(t, src)
	if len(got) != 7 {
		t.Fatalf("got %d, want 7", len(got))
	}
	// 3 pages: 3, 3, 1. Add ≥1 res.partner.category call only if any
	// row had categories — here none do. So calls = 3.
	if f.calls < 3 {
		t.Errorf("expected at least 3 paged calls, got %d", f.calls)
	}
}

func TestAdapter_ResolvesCategoriesAsTags(t *testing.T) {
	f := &fakeOdoo{
		partners: []map[string]any{
			{"id": float64(1), "name": "Person", "is_company": false, "parent_id": false, "email": "p@x.example", "category_ids": []any{float64(7), float64(8)}},
		},
		categories: map[int64]string{7: "Hot Lead", 8: "VIP"},
	}
	src, cleanup := newAdapterFor(t, f, ImportConfig{PageSize: 50})
	defer cleanup()
	got := collect(t, src)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if len(got[0].TagSlugs) != 2 {
		t.Fatalf("tags: %v", got[0].TagSlugs)
	}
	wantSet := map[string]bool{"hot-lead": true, "vip": true}
	for _, slug := range got[0].TagSlugs {
		if !wantSet[slug] {
			t.Errorf("unexpected slug %q", slug)
		}
	}
}

func TestImportConfig_EngagementWindow(t *testing.T) {
	cfg := ImportConfig{EngagementSinceDays: 30}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if got := cfg.engagementWindow(now); got != "2026-05-02T00:00:00Z" {
		t.Errorf("engagementWindow = %q", got)
	}
}

// strconvI keeps the test fixtures readable without pulling strconv.
func strconvI(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
