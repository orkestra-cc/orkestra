//go:build !no_addons || addon_sales

package main

import (
	"github.com/orkestra/backend/internal/addons/sales"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["sales"] = func() module.Module { return sales.NewModule() }
}
