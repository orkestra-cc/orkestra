package main

import (
	"github.com/orkestra-cc/orkestra-addon-billing"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["billing"] = func() module.Module { return billing.NewModule() }
}
