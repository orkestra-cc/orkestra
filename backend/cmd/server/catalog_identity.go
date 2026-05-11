//go:build !no_addons || addon_identity

package main

import (
	"github.com/orkestra/backend/internal/addons/identity"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["identity"] = func() module.Module { return identity.NewModule() }
}
