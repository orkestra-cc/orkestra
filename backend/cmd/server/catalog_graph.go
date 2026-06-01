package main

import (
	"github.com/orkestra/backend/internal/addons/graph"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["graph"] = func() module.Module { return graph.NewModule() }
}
