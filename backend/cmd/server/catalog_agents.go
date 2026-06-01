package main

import (
	"github.com/orkestra/backend/internal/addons/agents"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["agents"] = func() module.Module { return agents.NewModule() }
}
