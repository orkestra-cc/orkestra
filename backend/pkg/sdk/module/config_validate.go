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
