package main

import (
	marketing "github.com/orkestra/backend/internal/addons/marketing"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["marketing"] = func() module.Module { return marketing.NewModule() }
}
