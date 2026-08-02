package module

import (
	"fmt"
	"sort"
	"strings"
)

// ValidateConfigDeclarations reports every structural defect in a module's
// config declarations: a field pointing at an undeclared group, a DependsOn
// condition naming a field the module does not have, a duplicate or
// orphaned group key, and cycles in the Parent chain.
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

	fieldKeys := make(map[string]bool, len(schema))
	for _, f := range schema {
		fieldKeys[f.Key] = true
	}

	for _, f := range schema {
		if len(groups) > 0 && f.Group != "" {
			if _, ok := groupByKey[f.Group]; !ok {
				problems = append(problems,
					fmt.Sprintf("field %q references undeclared group %q", f.Key, f.Group))
			}
		}
		for _, c := range f.DependsOn {
			if !fieldKeys[c.Key] {
				problems = append(problems,
					fmt.Sprintf("field %q depends on undeclared field %q", f.Key, c.Key))
			}
			if len(c.In) == 0 {
				problems = append(problems,
					fmt.Sprintf("field %q has a DependsOn on %q with an empty In list", f.Key, c.Key))
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("invalid config declarations: %s", strings.Join(problems, "; "))
}
