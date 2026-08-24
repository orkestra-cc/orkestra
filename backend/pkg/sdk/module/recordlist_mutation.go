package module

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrRevisionRequired       = errors.New("recordlist: a revision is required to remove elements")
	ErrRevisionStale          = errors.New("recordlist: the environment changed since it was read")
	ErrDuplicateMutationField = errors.New("recordlist: the same field appears twice in one request")
)

// recordListMaxAttempts bounds the retry loop. Only value writes retry, and
// each attempt re-reads a document another writer just moved — five is far
// more than a real admin surface produces and still terminates.
const recordListMaxAttempts = 5

// RecordListMutation is one field's explicit membership intent. Nothing is
// inferred from which keys happen to be present in the accompanying values.
type RecordListMutation struct {
	Field  string
	Create []string
	Remove []string
}

// UpdateEnvironmentConfigWithRecordLists writes an environment's config and
// applies record-list membership changes in one compare-and-swap.
//
// The ordering is the substance: reconcile → validate → persist. The module's
// ValidateConfig hook must judge exactly the map that will be written, so
// removals are applied to the merged map BEFORE validation, not after.
//
// The retry asymmetry is deliberate. A value write may retry: re-merging the
// caller's values onto fresher stored state is the right answer, and it is
// what lets two operators each add an element without either losing. A
// REMOVAL may not: it is a destructive decision the operator made against a
// state they saw on screen, and silently re-applying it to a state they did
// not see could destroy an element — and its secret — that appeared in the
// meantime. A removal that loses the race is reported, not retried.
func (s *ModuleConfigService) UpdateEnvironmentConfigWithRecordLists(
	ctx context.Context, name, envName string,
	values, secrets map[string]string,
	mutations []RecordListMutation, expectedRevision *int64,
) error {
	seen := make(map[string]bool, len(mutations))
	removing := false
	for _, m := range mutations {
		if seen[m.Field] {
			return fmt.Errorf("%w: %q", ErrDuplicateMutationField, m.Field)
		}
		seen[m.Field] = true
		if len(m.Remove) > 0 {
			removing = true
		}
	}
	if removing && expectedRevision == nil {
		return ErrRevisionRequired
	}

	// The roster is SDK-owned. A client that writes it directly would bypass
	// every precondition below, so it never survives the request boundary.
	values = withoutRosterKeys(values)

	encrypted := make(map[string]string, len(secrets))
	for k, v := range secrets {
		enc, err := encryptSecret(v)
		if err != nil {
			return fmt.Errorf("encrypt secret %q: %w", k, err)
		}
		encrypted[k] = enc
	}

	for attempt := 0; attempt < recordListMaxAttempts; attempt++ {
		// GetConfig, not repo.FindByName: it lazily migrates a document that
		// predates environment profiles and lazy-seeds one that has gone
		// missing. Reading the repository directly would 400 on both.
		doc, err := s.GetConfig(ctx, name)
		if err != nil {
			return err
		}
		if doc == nil {
			return fmt.Errorf("module %q not found", name)
		}
		if _, ok := doc.Environments[envName]; !ok {
			return fmt.Errorf("environment %q not found for module %q", envName, name)
		}
		cur := doc.Environments[envName]

		// A removal is decided against a state the caller saw. It never retries.
		if removing && cur.Revision != *expectedRevision {
			return ErrRevisionStale
		}

		next := EnvironmentConfig{
			ConfigValues:    mergeStringMaps(cur.ConfigValues, values),
			EncryptedValues: mergeStringMaps(cur.EncryptedValues, encrypted),
		}

		// Reconcile: preconditions run against the STORED roster, every attempt.
		for _, m := range mutations {
			stored := ParseRoster(cur.ConfigValues, m.Field)
			target, err := ApplyMembership(stored, m.Create, m.Remove)
			if err != nil {
				return err
			}
			for _, slug := range m.Remove {
				for _, k := range KeysUnderElement(keysOf(next.ConfigValues), m.Field, slug) {
					delete(next.ConfigValues, k)
				}
				for _, k := range KeysUnderElement(keysOf(next.EncryptedValues), m.Field, slug) {
					delete(next.EncryptedValues, k)
				}
			}
			if len(target) == 0 {
				delete(next.ConfigValues, RosterKey(m.Field))
			} else {
				next.ConfigValues[RosterKey(m.Field)] = FormatRoster(target)
			}
		}

		// Layer B: the generic value rules the SDK owns. They run against the
		// RECONCILED rosters in next, so an element and its values may be
		// created in one request, and against the SUBMITTED values, so a
		// label already stored is never re-judged by a rule that postdates it.
		byField := make(map[string]RecordListMutation, len(mutations))
		for _, m := range mutations {
			byField[m.Field] = m
		}
		if err := validateRecordListSubmission(doc.ConfigSchema, values, next.ConfigValues, byField); err != nil {
			return err
		}

		// Validate exactly what will be written.
		if err := s.validateModuleConfig(ctx, name, next.ConfigValues); err != nil {
			return err
		}

		won, err := s.repo.CompareAndSwapEnvironment(ctx, name, envName, cur.Revision, next)
		if err != nil {
			return err
		}
		if won {
			return s.InvalidateCache(ctx, name)
		}
		if removing {
			return ErrRevisionStale
		}
		// Value writes retry: re-merging onto fresher state is the right answer.
	}
	return ErrRevisionStale
}

// withoutRosterKeys copies values, dropping any SDK-owned roster key. The
// element label (<field>.<slug>.__label) is deliberately NOT dropped — it is
// operator-editable content, unlike membership.
func withoutRosterKeys(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for k, v := range values {
		if strings.HasSuffix(k, ".__items") {
			continue
		}
		out[k] = v
	}
	return out
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// validateRecordListSubmission enforces the record-list value rules that are
// generic to the SDK: every submitted value must name an element that exists
// once the request is applied, every written label must obey the label bounds,
// and a created element must carry a label whose mint is its slug.
//
// It walks the SCHEMA rather than the mutations, because a request may write
// into a record list without changing its membership at all — renaming an
// element is exactly that, and would otherwise skip every check here.
func validateRecordListSubmission(
	schema []ConfigField,
	submitted, reconciled map[string]string,
	byField map[string]RecordListMutation,
) error {
	for _, field := range schema {
		if field.Type != FieldRecordList {
			continue
		}

		target := ParseRoster(reconciled, field.Key)
		inTarget := make(map[string]bool, len(target))
		for _, slug := range target {
			inTarget[slug] = true
		}

		for key, val := range submitted {
			slug, sub, ok := SplitElementKey(field.Key, key)
			if !ok {
				continue
			}
			if !inTarget[slug] {
				return fmt.Errorf("%w: %q under %q", ErrUnknownSlug, slug, field.Key)
			}
			if sub == labelSuffix {
				if err := ValidateLabel(val); err != nil {
					return fmt.Errorf("%w (element %q under %q)", err, slug, field.Key)
				}
			}
		}

		// The slug/label binding holds at creation and nowhere else: afterwards
		// the label is editable and the slug frozen, so the two are EXPECTED to
		// diverge and nothing may re-mint to restore the equality.
		for _, slug := range byField[field.Key].Create {
			label, ok := submitted[LabelKey(field.Key, slug)]
			if !ok {
				return fmt.Errorf("%w: element %q under %q", ErrLabelRequired, slug, field.Key)
			}
			if err := ValidateLabel(label); err != nil {
				return fmt.Errorf("%w (element %q under %q)", err, slug, field.Key)
			}
			if MintSlug(label) != slug {
				return fmt.Errorf("%w: %q under %q was created from label %q, which mints %q",
					ErrSlugLabelMismatch, slug, field.Key, label, MintSlug(label))
			}
		}
	}
	return nil
}
