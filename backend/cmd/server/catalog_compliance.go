//go:build !no_addons || addon_compliance

package main

import (
	"github.com/orkestra/backend/internal/addons/compliance"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["compliance"] = func() module.Module { return compliance.NewModule() }
}
