//go:build !no_addons || addon_agents

package main

import (
	"github.com/orkestra/backend/internal/addons/agents"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["agents"] = func() module.Module { return agents.NewModule() }
}
