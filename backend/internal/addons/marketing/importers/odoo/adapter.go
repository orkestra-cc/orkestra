package odoo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/orkestra-cc/orkestra-addon-marketing/importers"
)

// Adapter implements importers.Importer for Odoo 19.0 res.partner +
// (optionally) mail.message ingestion via the External JSON-2 API.
//
// Unlike CSV/Excel where the payload is the file bytes, the "reader"
// for Odoo carries a JSON-encoded connection config (BaseURL +
// Database + APIKey + optional flags). The wizard's connection form
// serialises this struct; the worker passes it through Parse.
type Adapter struct{}

// New returns a fresh Odoo adapter. Stateless.
func New() *Adapter { return &Adapter{} }

// Name is the canonical identifier persisted on ImportJob.Importer.
func (a *Adapter) Name() string { return "odoo" }

// DescribeCapabilities — Odoo has a single logical "sheet" (the
// res.partner model), so SheetSelection stays false; engagement
// emission is opt-in via the config flag.
func (a *Adapter) DescribeCapabilities() importers.CapabilityFlags {
	return importers.CapabilityFlags{
		SheetSelection:   false,
		ActivityEmission: true,
	}
}

// ImportConfig is the JSON payload the wizard submits. Persisted on
// disk as the "spool file" by the worker; the adapter reads it back
// at Parse time.
//
// The credentials in this struct are sensitive — the wizard pulls
// them from the per-environment ConfigService secrets when the
// operator picks the Odoo importer, so the values are never seen by
// the frontend in plain text after the first time they're entered.
type ImportConfig struct {
	BaseURL  string `json:"baseUrl"`
	Database string `json:"database"`
	APIKey   string `json:"apiKey"`

	// PageSize controls SearchRead.Limit per page. Default 200; cap
	// at 500 to keep memory pressure bounded on enterprise tenants
	// with hundreds of thousands of partners.
	PageSize int `json:"pageSize,omitempty"`

	// IncludeEngagement opts the import into the mail.message pull —
	// per partner, fetch the last EngagementSince days of CRM history
	// and emit one Activity row per message (default false because
	// first-time anagrafica imports usually don't want every
	// historical email in the timeline).
	IncludeEngagement bool `json:"includeEngagement,omitempty"`

	// EngagementSince is the lookback window in days for the
	// mail.message pull. Default 90.
	EngagementSinceDays int `json:"engagementSinceDays,omitempty"`
}

// Parse reads the JSON config from reader, builds a client, and
// returns a streaming Source over res.partner pages. The Source
// drives pagination internally — the pipeline consumes one
// CanonicalRecord per partner without seeing the underlying paging
// loop.
func (a *Adapter) Parse(reader io.Reader, mapping importers.ColumnMapping) (importers.Source, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("odoo: read config: %w", err)
	}
	var cfg ImportConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("odoo: decode config json: %w", err)
	}
	// Mapping options override fields on the config. Useful for
	// re-running the same persisted spool file against a different
	// page size during testing.
	if v := mapping.Options["pageSize"]; v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			cfg.PageSize = n
		}
	}
	if v := mapping.Options["includeEngagement"]; v == "true" {
		cfg.IncludeEngagement = true
	}

	if cfg.PageSize <= 0 {
		cfg.PageSize = 200
	}
	if cfg.PageSize > 500 {
		cfg.PageSize = 500
	}
	if cfg.EngagementSinceDays <= 0 {
		cfg.EngagementSinceDays = 90
	}

	client, err := NewClient(Config{
		BaseURL:  cfg.BaseURL,
		Database: cfg.Database,
		APIKey:   cfg.APIKey,
	})
	if err != nil {
		return nil, err
	}

	src := &source{
		client:  client,
		cfg:     cfg,
		records: make(chan importers.CanonicalRecord, 64),
	}
	go src.run()
	return src, nil
}

// source streams res.partner pages off Odoo. Pagination terminates
// when the page comes back shorter than PageSize (the last page).
type source struct {
	client  *Client
	cfg     ImportConfig
	records chan importers.CanonicalRecord
	err     error
	closed  bool
}

func (s *source) Records() <-chan importers.CanonicalRecord { return s.records }
func (s *source) Err() error                                { return s.err }
func (s *source) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}

func (s *source) run() {
	defer close(s.records)
	offset := 0
	rowIdx := 0
	for {
		res, err := s.client.SearchRead("res.partner", SearchReadOpts{
			Fields: ResPartnerFields,
			Offset: offset,
			Limit:  s.cfg.PageSize,
			Order:  "id asc",
		})
		if err != nil {
			s.err = fmt.Errorf("odoo: search_read res.partner: %w", err)
			return
		}
		if len(res.Records) == 0 {
			return
		}

		parentNames, err := s.fetchParentNames(res.Records)
		if err != nil {
			s.err = err
			return
		}
		categoryNames, err := s.fetchCategoryNames(res.Records)
		if err != nil {
			s.err = err
			return
		}

		for _, raw := range res.Records {
			rec := MapResPartner(raw, parentNames, categoryNames)
			if rec == nil {
				continue
			}
			rec.RowIndex = rowIdx
			rowIdx++
			s.records <- *rec
		}

		if len(res.Records) < s.cfg.PageSize {
			return
		}
		offset += s.cfg.PageSize
	}
}

// fetchParentNames resolves the parent_id references in this page in
// a single follow-up SearchRead. Falls back to the inline display
// name embedded in `parent_id` when the second request fails (e.g.
// network blip mid-import — the Person row still lands).
func (s *source) fetchParentNames(batch []map[string]any) (map[int64]string, error) {
	ids := CollectParentIDs(batch)
	out := make(map[int64]string, len(ids))
	// Always try inline display name first — cheaper than a second
	// roundtrip.
	for _, row := range batch {
		if name := manyOneName(row, "parent_id"); name != "" {
			out[manyOneID(row, "parent_id")] = name
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	missing := make([]int64, 0)
	for _, id := range ids {
		if _, ok := out[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	res, err := s.client.SearchRead("res.partner", SearchReadOpts{
		Fields: []string{"id", "name"},
		Domain: []any{[]any{"id", "in", missing}},
		Limit:  len(missing),
	})
	if err != nil {
		// Non-fatal — leave parent names empty for the missing IDs;
		// the pipeline will skip the org-link half of those rows.
		return out, nil
	}
	for _, row := range res.Records {
		id := coerceInt64(row["id"])
		name := stringField(row, "name")
		if id != 0 && name != "" {
			out[id] = name
		}
	}
	return out, nil
}

// fetchCategoryNames resolves category_ids names. Same fall-through
// behavior as fetchParentNames.
func (s *source) fetchCategoryNames(batch []map[string]any) (map[int64]string, error) {
	ids := CollectCategoryIDs(batch)
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	res, err := s.client.SearchRead("res.partner.category", SearchReadOpts{
		Fields: []string{"id", "name"},
		Domain: []any{[]any{"id", "in", ids}},
		Limit:  len(ids),
	})
	if err != nil {
		if errors.Is(err, ErrOdooNotFound) {
			// Tenant doesn't have the partner.category model — skip
			// tag enrichment for this batch.
			return out, nil
		}
		return out, err
	}
	for _, row := range res.Records {
		id := coerceInt64(row["id"])
		name := stringField(row, "name")
		if id != 0 && name != "" {
			out[id] = name
		}
	}
	return out, nil
}

// engagementWindow returns the RFC3339 cutoff for the mail.message
// pull. Exposed for tests.
func (cfg *ImportConfig) engagementWindow(now time.Time) string {
	cutoff := now.AddDate(0, 0, -cfg.EngagementSinceDays)
	return cutoff.Format(time.RFC3339)
}
