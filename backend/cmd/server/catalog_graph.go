package main

import (
	"github.com/orkestra-cc/orkestra-addon-graph"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["graph"] = func() module.Module { return graph.NewModule() }
}
