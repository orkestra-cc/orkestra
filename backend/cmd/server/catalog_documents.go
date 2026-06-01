package main

import (
	"github.com/orkestra/backend/internal/addons/documents"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["documents"] = func() module.Module { return documents.NewModule() }
}
