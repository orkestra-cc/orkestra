//go:build !no_addons || addon_company

package main

import (
	"github.com/orkestra/backend/internal/addons/company"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["company"] = func() module.Module { return company.NewModule() }
}
