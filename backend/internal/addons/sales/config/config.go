// Package config carries the typed `SalesConfig` DTO that the sales
// addon's service constructors accept. It used to live at
// backend/internal/shared/config (alongside every other module's
// config struct) and was relocated here as part of Phase 5e of the
// SDK split, when the addon was carved into its own Go module.
//
// The shared config package's env-var populator was already dead
// code by the time the relocation happened: `module.go` builds a
// fresh SalesConfig value from its own `Settings` struct (unmarshaled
// via the SDK's ConfigService from the `module_configs` collection),
// so nothing reads `config.Sales` from the kernel-side struct anymore.
package config

import "time"

// SalesConfig holds configuration for the AI Sales Intelligence module.
// Built once in module.Init() from the runtime config snapshot and threaded
// into the service constructors (scraper, orchestrator) by value.
type SalesConfig struct {
	Enabled         bool          // Module enabled flag (SALES_ENABLED)
	MaxConcurrency  int           // Max parallel agent LLM calls per job (SALES_MAX_CONCURRENCY)
	DefaultLocale   string        // Default locale for prompts (SALES_DEFAULT_LOCALE)
	SkillTimeout    time.Duration // Timeout for individual skill calls (SALES_SKILL_TIMEOUT)
	QuickTimeout    time.Duration // Timeout for sync /prospect/quick (SALES_QUICK_TIMEOUT)
	FullTimeout     time.Duration // Timeout for async /prospect pipeline (SALES_FULL_TIMEOUT)
	ScraperTimeout  time.Duration // Timeout per scrape request (SALES_SCRAPER_TIMEOUT)
	ScraperMaxDepth int           // Max subpage depth for scraping (SALES_SCRAPER_MAX_DEPTH)
	MaxTokens       int           // Max output tokens per agent/skill LLM call (SALES_MAX_TOKENS)
}
