//go:build !no_addons || addon_aimodels

package main

import (
	"github.com/orkestra/backend/internal/addons/aimodels"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["aimodels"] = func() module.Module { return aimodels.NewModule() }
}
