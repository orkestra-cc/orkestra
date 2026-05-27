package main

import (
	"github.com/orkestra-cc/orkestra-addon-rag"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["rag"] = func() module.Module { return rag.NewModule() }
}
