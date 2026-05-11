//go:build !no_addons || addon_billing

package main

import (
	"github.com/orkestra/backend/internal/addons/billing"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["billing"] = func() module.Module { return billing.NewModule() }
}
