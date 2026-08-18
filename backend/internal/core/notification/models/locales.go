package models

// SupportedLocales is the single source of truth for which languages this
// deployment ships templates in. The parity test cross-products it with
// every declared template id; adding a locale here without adding the texts
// turns the build red rather than silently failing sends at runtime.
var SupportedLocales = []string{"en"}
