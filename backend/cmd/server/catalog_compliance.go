//go:build !no_addons || addon_compliance

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/compliance"
)

func init() {
	optionalModules["compliance"] = func() module.Module { return compliance.NewModule() }
}
