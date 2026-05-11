//go:build !no_addons || addon_graph

package main

import (
	"github.com/orkestra/backend/internal/addons/graph"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["graph"] = func() module.Module { return graph.NewModule() }
}
