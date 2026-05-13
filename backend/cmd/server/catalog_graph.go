//go:build !no_addons || addon_graph

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/graph"
)

func init() {
	optionalModules["graph"] = func() module.Module { return graph.NewModule() }
}
