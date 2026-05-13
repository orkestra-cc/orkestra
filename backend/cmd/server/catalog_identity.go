//go:build !no_addons || addon_identity

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/identity"
)

func init() {
	optionalModules["identity"] = func() module.Module { return identity.NewModule() }
}
