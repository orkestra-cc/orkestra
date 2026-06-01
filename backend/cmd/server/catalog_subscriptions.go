package main

import (
	"github.com/orkestra/backend/internal/addons/subscriptions"
	"github.com/orkestra/backend/pkg/sdk/module"
)

func init() {
	optionalModules["subscriptions"] = func() module.Module { return subscriptions.NewModule() }
}
