package main

import (
	"github.com/orkestra/backend/internal/addons/billing"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["billing"] = func() module.Module { return billing.NewModule() }
}
