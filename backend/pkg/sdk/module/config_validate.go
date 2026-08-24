package module

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// boolConditionLiterals are the only values a DependsOn may carry against a
// FieldBool target. Matching itself is parseBool-shaped (see FieldCondition),
// but a declaration outside this set — "on", "enabled", "yes please" — is a
// typo that would silently evaluate to false forever.
var boolConditionLiterals = []string{"true", "false", "1", "0", "yes", "no"}

// ValidateConfigDeclarations reports every structural defect in a module's
// config declarations: a field pointing at an undeclared group, a DependsOn
// condition naming a field the module does not have, a duplicate or
// orphaned group key, cycles in the Parent chain, a duplicate field key,
// an uncompilable Pattern, an inverted Min/Max, and DependsOn values that
// can never match the referenced field's type.
//
// These defects are silent at runtime — a mistyped Group renders a rail
// entry pointing at nothing rather than failing — so this function exists to
// turn them into a test failure instead.
//
// Backward compatibility: a module that declares NO groups is using Group as
// a legacy display label, so group references are not checked. Every module
// predates ConfigGroups(), and un-migrated fork addons keep that shape
// indefinitely. DependsOn is checked either way.
//
// All problems are collected and reported together — fixing declarations one
// error per run is needless churn.
func ValidateConfigDeclarations(schema []ConfigField, groups []ConfigGroup) error {
	var problems []string

	groupByKey := make(map[string]ConfigGroup, len(groups))
	for _, g := range groups {
		switch {
		case g.Key == "":
			problems = append(problems, fmt.Sprintf("config group %q has an empty Key", g.Label))
		default:
			if _, dup := groupByKey[g.Key]; dup {
				problems = append(problems, fmt.Sprintf("duplicate config group %q", g.Key))
				continue
			}
			groupByKey[g.Key] = g
		}
	}

	for _, g := range groups {
		if g.Parent == "" {
			continue
		}
		if _, ok := groupByKey[g.Parent]; !ok {
			problems = append(problems, fmt.Sprintf("group %q has undeclared Parent %q", g.Key, g.Parent))
			continue
		}
		// Walk the ancestry; revisiting a key means the chain loops.
		seen := map[string]bool{g.Key: true}
		for cur := g.Parent; cur != ""; {
			if seen[cur] {
				problems = append(problems, fmt.Sprintf("group %q is part of a Parent cycle", g.Key))
				break
			}
			seen[cur] = true
			next, ok := groupByKey[cur]
			if !ok {
				break // reported as an undeclared Parent on its own iteration
			}
			cur = next.Parent
		}
	}

	fieldByKey := make(map[string]ConfigField, len(schema))
	for _, f := range schema {
		if _, dup := fieldByKey[f.Key]; dup {
			// Config values are a flat map keyed by ConfigField.Key, so the
			// second declaration silently wins for storage while both render.
			problems = append(problems, fmt.Sprintf("duplicate config field %q", f.Key))
			continue
		}
		fieldByKey[f.Key] = f
	}

	for _, f := range schema {
		if len(groups) > 0 {
			switch {
			case f.Group == "":
				// The module opted into the sectioned rail, so an ungrouped
				// field has no panel to render in and becomes unreachable.
				problems = append(problems,
					fmt.Sprintf("field %q has an empty Group in a module that declares config groups", f.Key))
			default:
				if _, ok := groupByKey[f.Group]; !ok {
					problems = append(problems,
						fmt.Sprintf("field %q references undeclared group %q", f.Key, f.Group))
				}
			}
		}

		if f.Pattern != "" {
			// The pattern is shipped to the admin UI, which feeds it to
			// new RegExp(); an uncompilable one throws in a render path.
			if _, err := regexp.Compile(f.Pattern); err != nil {
				problems = append(problems,
					fmt.Sprintf("field %q has an invalid Pattern %q: %v", f.Key, f.Pattern, err))
			}
		}

		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			problems = append(problems,
				fmt.Sprintf("field %q has Min %d greater than Max %d", f.Key, *f.Min, *f.Max))
		}

		switch f.DependsOnMatch {
		case "", "all", "any":
		default:
			problems = append(problems,
				fmt.Sprintf("field %q has unknown DependsOnMatch %q (want \"all\" or \"any\")",
					f.Key, f.DependsOnMatch))
		}
		if f.DependsOnMatch != "" && len(f.DependsOn) == 0 {
			problems = append(problems,
				fmt.Sprintf("field %q sets DependsOnMatch %q but declares no DependsOn",
					f.Key, f.DependsOnMatch))
		}

		problems = append(problems, recordListDeclarationProblems(f)...)

		for _, c := range f.DependsOn {
			target, known := fieldByKey[c.Key]
			if !known {
				problems = append(problems,
					fmt.Sprintf("field %q depends on undeclared field %q", f.Key, c.Key))
			}
			if len(c.In) == 0 {
				problems = append(problems,
					fmt.Sprintf("field %q has a DependsOn on %q with an empty In list", f.Key, c.Key))
				continue
			}
			if known {
				problems = append(problems, conditionValueProblems(f.Key, target, c)...)
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("invalid config declarations: %s", strings.Join(problems, "; "))
}

// recordListDeclarationProblems reports every structural defect in one
// field's record-list declaration. Split out of ValidateConfigDeclarations
// because the checks are self-contained — everything they need is the field
// itself, since an element's conditions may only reference its own siblings.
//
// The reserved "__" prefix belongs to the SDK: the roster lives at
// <field>.__items and an element's label at <field>.<slug>.__label, both in
// the same flat value map a sub-field key composes into. A sub-field named
// __label would collide with the element's own label and silently overwrite
// it.
func recordListDeclarationProblems(field ConfigField) []string {
	var problems []string
	isList := field.Type == FieldRecordList

	if isList && len(field.Items) == 0 {
		problems = append(problems, fmt.Sprintf("field %q: type recordList requires items", field.Key))
	}
	if !isList && len(field.Items) > 0 {
		problems = append(problems, fmt.Sprintf("field %q: items is only valid on type recordList", field.Key))
	}
	if !isList {
		return problems
	}

	siblings := make([]string, 0, len(field.Items))
	for _, it := range field.Items {
		siblings = append(siblings, it.Key)
	}

	seen := map[string]bool{}
	for _, it := range field.Items {
		if it.Type == FieldRecordList {
			problems = append(problems, fmt.Sprintf(
				"field %q: sub-field %q may not be a recordList — the schema is not recursive", field.Key, it.Key))
		}
		if strings.HasPrefix(it.Key, "__") {
			problems = append(problems, fmt.Sprintf(
				"field %q: sub-field %q uses the reserved \"__\" prefix", field.Key, it.Key))
		}
		if seen[it.Key] {
			problems = append(problems, fmt.Sprintf("field %q: duplicate sub-field key %q", field.Key, it.Key))
		}
		seen[it.Key] = true
		if it.Min != nil && *it.Min < 0 {
			problems = append(problems, fmt.Sprintf("field %q: sub-field %q has a negative min", field.Key, it.Key))
		}
		if it.Min != nil && it.Max != nil && *it.Min > *it.Max {
			problems = append(problems, fmt.Sprintf("field %q: sub-field %q has min > max", field.Key, it.Key))
		}
		for _, c := range it.DependsOn {
			if !containsFold(siblings, c.Key) {
				problems = append(problems, fmt.Sprintf(
					"field %q: sub-field %q depends on %q, which is not a sibling in the same element",
					field.Key, it.Key, c.Key))
			}
		}
	}

	if field.Min != nil && *field.Min < 0 {
		problems = append(problems, fmt.Sprintf("field %q: negative min arity", field.Key))
	}
	if field.Min != nil && field.Max != nil && *field.Min > *field.Max {
		problems = append(problems, fmt.Sprintf("field %q: min arity exceeds max", field.Key))
	}
	return problems
}

// conditionValueProblems reports In entries that can never match the
// referenced field, given that field's Type. An enum value outside Options
// ("smtps" against ["noop","smtp"]) or a non-boolean literal against a bool
// hides every dependent field forever with no error surfacing anywhere.
// Types other than enum and bool carry no closed value set, so nothing is
// checked for them.
func conditionValueProblems(owner string, target ConfigField, c FieldCondition) []string {
	var problems []string
	switch target.Type {
	case FieldEnum:
		for _, v := range c.In {
			if !containsFold(target.Options, v) {
				problems = append(problems, fmt.Sprintf(
					"field %q has a DependsOn on enum %q with value %q that is not one of its Options",
					owner, target.Key, v))
			}
		}
	case FieldBool:
		for _, v := range c.In {
			if !containsFold(boolConditionLiterals, v) {
				problems = append(problems, fmt.Sprintf(
					"field %q has a DependsOn on bool %q with non-boolean value %q",
					owner, target.Key, v))
			}
		}
	}
	return problems
}

// containsFold reports whether want appears in set, compared the way the
// FieldCondition matching contract compares: case-insensitive and
// whitespace-trimmed.
func containsFold(set []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, s := range set {
		if strings.EqualFold(strings.TrimSpace(s), want) {
			return true
		}
	}
	return false
}
