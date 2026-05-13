//go:build !no_addons || addon_billing

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/billing"
)

func init() {
	optionalModules["billing"] = func() module.Module { return billing.NewModule() }
}
