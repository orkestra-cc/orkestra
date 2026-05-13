//go:build !no_addons || addon_documents

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/documents"
)

func init() {
	optionalModules["documents"] = func() module.Module { return documents.NewModule() }
}
