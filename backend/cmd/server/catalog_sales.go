//go:build !no_addons || addon_sales

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/sales"
)

func init() {
	optionalModules["sales"] = func() module.Module { return sales.NewModule() }
}
