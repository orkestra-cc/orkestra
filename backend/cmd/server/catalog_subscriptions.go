package main

import (
	"github.com/orkestra-cc/orkestra-addon-subscriptions"
	"github.com/orkestra-cc/orkestra-sdk/module"
)

func init() {
	optionalModules["subscriptions"] = func() module.Module { return subscriptions.NewModule() }
}
