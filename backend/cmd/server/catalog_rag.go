//go:build !no_addons || addon_rag

package main

import (
	"github.com/orkestra/backend/internal/addons/rag"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["rag"] = func() module.Module { return rag.NewModule() }
}
