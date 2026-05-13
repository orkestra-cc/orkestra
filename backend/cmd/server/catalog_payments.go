//go:build !no_addons || addon_payments

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/payments"
)

func init() {
	optionalModules["payments"] = func() module.Module { return payments.NewModule() }
}
