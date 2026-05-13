//go:build !no_addons || addon_company

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/company"
)

func init() {
	optionalModules["company"] = func() module.Module { return company.NewModule() }
}
