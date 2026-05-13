//go:build !no_addons || addon_aimodels

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/aimodels"
)

func init() {
	optionalModules["aimodels"] = func() module.Module { return aimodels.NewModule() }
}
