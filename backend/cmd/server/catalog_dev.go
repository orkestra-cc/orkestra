//go:build !no_addons || addon_dev

package main

import (
	"github.com/orkestra/backend/internal/addons/dev"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["dev"] = func() module.Module { return dev.NewModule() }
}
