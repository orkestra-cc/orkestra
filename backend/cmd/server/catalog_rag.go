//go:build !no_addons || addon_rag

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/rag"
)

func init() {
	optionalModules["rag"] = func() module.Module { return rag.NewModule() }
}
