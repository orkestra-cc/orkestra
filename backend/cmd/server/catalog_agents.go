package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/agents"
)

func init() {
	optionalModules["agents"] = func() module.Module { return agents.NewModule() }
}
