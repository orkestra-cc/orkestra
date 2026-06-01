package main

import (
	"github.com/orkestra/backend/internal/addons/sales"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["sales"] = func() module.Module { return sales.NewModule() }
}
