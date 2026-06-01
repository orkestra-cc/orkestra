package main

import (
	"github.com/orkestra/backend/internal/addons/compliance"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["compliance"] = func() module.Module { return compliance.NewModule() }
}
