//go:build !no_addons || addon_subscriptions

package main

import (
	"github.com/orkestra/backend/internal/addons/subscriptions"
	"github.com/orkestra/backend/internal/shared/module"
)

func init() {
	optionalModules["subscriptions"] = func() module.Module { return subscriptions.NewModule() }
}
