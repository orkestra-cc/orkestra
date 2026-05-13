//go:build !no_addons || addon_company

package main

import (
	"github.com/orkestra-cc/orkestra-addon-company"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["company"] = func() module.Module { return company.NewModule() }
}
