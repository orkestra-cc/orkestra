package main

import (
	"github.com/orkestra-cc/orkestra-addon-payments"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["payments"] = func() module.Module { return payments.NewModule() }
}
