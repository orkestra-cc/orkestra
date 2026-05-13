//go:build !no_addons || addon_sales

package main

import (
	"github.com/orkestra-cc/orkestra-addon-sales"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["sales"] = func() module.Module { return sales.NewModule() }
}
