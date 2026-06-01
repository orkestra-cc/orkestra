package main

import (
	"github.com/orkestra/backend/internal/addons/rag"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["rag"] = func() module.Module { return rag.NewModule() }
}
