package main

import (
	"github.com/orkestra/backend/internal/addons/dev"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["dev"] = func() module.Module { return dev.NewModule() }
}
