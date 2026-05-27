package main

import (
	marketing "github.com/orkestra-cc/orkestra-addon-marketing"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["marketing"] = func() module.Module { return marketing.NewModule() }
}
