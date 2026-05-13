//go:build !no_addons || addon_aimodels

package main

import (
	"github.com/orkestra-cc/orkestra-addon-aimodels"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["aimodels"] = func() module.Module { return aimodels.NewModule() }
}
