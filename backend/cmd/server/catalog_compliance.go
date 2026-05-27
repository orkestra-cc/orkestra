package main

import (
	"github.com/orkestra-cc/orkestra-addon-compliance"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["compliance"] = func() module.Module { return compliance.NewModule() }
}
