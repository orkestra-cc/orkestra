package main

import (
	"github.com/orkestra-cc/orkestra-addon-identity"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["identity"] = func() module.Module { return identity.NewModule() }
}
