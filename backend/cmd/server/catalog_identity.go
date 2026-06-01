package main

import (
	"github.com/orkestra/backend/internal/addons/identity"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["identity"] = func() module.Module { return identity.NewModule() }
}
