//go:build !no_addons || addon_documents

package main

import (
	"github.com/orkestra/backend/internal/addons/documents"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["documents"] = func() module.Module { return documents.NewModule() }
}
