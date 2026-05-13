//go:build !no_addons || addon_subscriptions

package main

import (
	"github.com/orkestra-cc/orkestra-sdk/module"
	"github.com/orkestra/backend/internal/addons/subscriptions"
)

func init() {
	optionalModules["subscriptions"] = func() module.Module { return subscriptions.NewModule() }
}
