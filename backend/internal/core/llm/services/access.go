package services

import (
	"context"
	"sort"

	"github.com/orkestra/backend/internal/core/llm/models"
	"github.com/orkestra/backend/pkg/sdk/iface"
)

// defaultPurpose is the routing key every model should serve and the
// fallback when a requested purpose has no model.
const defaultPurpose = "default"

// Need is what a request requires from a model.
type Need struct {
	Chat             bool
	Tools            bool
	StructuredOutput bool
	Embeddings       bool
}

func (n Need) satisfiedBy(c models.LLMModelCapabilities) bool {
	if n.Chat && !c.Chat {
		return false
	}
	if n.Tools && !c.Tools {
		return false
	}
	if n.StructuredOutput && !c.StructuredOutput {
		return false
	}
	if n.Embeddings && !c.Embeddings {
		return false
	}
	return true
}

// AccessResolver answers "which active models may this user use, for this
// purpose, with these needs", ordered by the purpose priority. Managing a
// model is never a reason to use it: only a grant or Access == everyone is.
// Every call is scoped to the org in ctx by the repositories.
type AccessResolver struct {
	models ModelRepo
	grants GrantRepo
}

func NewAccessResolver(models ModelRepo, grants GrantRepo) *AccessResolver {
	return &AccessResolver{models: models, grants: grants}
}

// grantedSet is the set of model UUIDs userUUID holds a grant for. An empty
// userUUID (a job with no user in context) holds none.
func (a *AccessResolver) grantedSet(ctx context.Context, userUUID string) (map[string]bool, error) {
	set := map[string]bool{}
	if userUUID == "" {
		return set, nil
	}
	gs, err := a.grants.ListByUser(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		set[g.ModelUUID] = true
	}
	return set, nil
}

// usableFrom filters active down to what userUUID may use.
func (a *AccessResolver) usableFrom(ctx context.Context, active []models.Model, userUUID string) ([]models.Model, error) {
	granted, err := a.grantedSet(ctx, userUUID)
	if err != nil {
		return nil, err
	}
	out := make([]models.Model, 0, len(active))
	for _, m := range active {
		if m.Status == models.ModelStatusActive && (m.Access == models.AccessEveryone || granted[m.UUID]) {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Usable lists every active model userUUID may use, in name order. With no
// user it lists only the models open to everyone.
func (a *AccessResolver) Usable(ctx context.Context, userUUID string) ([]models.Model, error) {
	active, err := a.models.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	return a.usableFrom(ctx, active, userUUID)
}

// Candidates returns the usable models that serve purpose and satisfy
// need, ordered by ascending priority then name. The purpose falls back to
// "default" only when no active model of the org declares it at all (spec,
// Routing step 2): if some model declares it but the caller may not use it,
// or lacks a needed capability, the result is empty rather than silently
// moving the request to another purpose. ErrLLMNotConfigured when the org
// has no active model; an empty slice when models exist but none fits, so
// the caller can tell "not configured" from "nothing eligible".
func (a *AccessResolver) Candidates(ctx context.Context, userUUID, purpose string, need Need) ([]models.Model, error) {
	active, err := a.models.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	if len(active) == 0 {
		return nil, iface.ErrLLMNotConfigured
	}
	usable, err := a.usableFrom(ctx, active, userUUID)
	if err != nil {
		return nil, err
	}
	if purpose == "" || !declaresPurpose(active, purpose) {
		purpose = defaultPurpose
	}
	return pickForPurpose(usable, purpose, need), nil
}

// declaresPurpose reports whether any of ms lists purpose.
func declaresPurpose(ms []models.Model, purpose string) bool {
	for _, m := range ms {
		for _, p := range m.Purposes {
			if p.Purpose == purpose {
				return true
			}
		}
	}
	return false
}

// pickForPurpose keeps the models that declare purpose and satisfy need,
// ordered by that purpose's priority (lower wins) then name.
func pickForPurpose(usable []models.Model, purpose string, need Need) []models.Model {
	type ranked struct {
		m    models.Model
		prio int
	}
	var rs []ranked
	for _, m := range usable {
		if !need.satisfiedBy(m.Capabilities) {
			continue
		}
		for _, p := range m.Purposes {
			if p.Purpose == purpose {
				rs = append(rs, ranked{m, p.Priority})
				break
			}
		}
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].prio != rs[j].prio {
			return rs[i].prio < rs[j].prio
		}
		return rs[i].m.Name < rs[j].m.Name
	})
	out := make([]models.Model, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.m)
	}
	return out
}
