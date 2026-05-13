//go:build !no_addons || addon_dev

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/dev"
)

func init() {
	optionalModules["dev"] = func() module.Module { return dev.NewModule() }
}
