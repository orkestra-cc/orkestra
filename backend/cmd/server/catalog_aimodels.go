package main

import (
	"github.com/orkestra/backend/internal/addons/aimodels"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["aimodels"] = func() module.Module { return aimodels.NewModule() }
}
