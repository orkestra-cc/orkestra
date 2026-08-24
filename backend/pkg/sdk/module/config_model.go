package module

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// EnvironmentConfig stores config values and encrypted secrets for a single
// named environment (e.g. "production", "sandbox"). Each module document can
// hold multiple environments as a nested map.
type EnvironmentConfig struct {
	ConfigValues    map[string]string `bson:"configValues" json:"configValues"`
	EncryptedValues map[string]string `bson:"encryptedValues" json:"-"`
	UpdatedAt       time.Time         `bson:"updatedAt" json:"updatedAt"`
	// Revision increments on every write to this environment. Absent in
	// documents written before record lists existed; absent and 0 are the
	// same value, so a first mutation there compares against 0 and wins.
	Revision int64 `bson:"revision,omitempty" json:"revision"`
}

// DefaultEnvironments are seeded for every module on first boot.
var DefaultEnvironments = []string{"production", "sandbox"}

// ModuleConfig is the MongoDB document storing a module's runtime configuration.
// Created automatically by the registry from each module's ConfigSchema() on first boot.
type ModuleConfig struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"-"`
	ModuleName      string             `bson:"moduleName" json:"moduleName"`
	DisplayName     string             `bson:"displayName" json:"displayName"`
	Description     string             `bson:"description" json:"description"`
	Category        ModuleCategory     `bson:"category" json:"category"`
	Enabled         bool               `bson:"enabled" json:"enabled"`
	ConfigValues    map[string]string  `bson:"configValues" json:"configValues"` // legacy — migrated to Environments on first access
	EncryptedValues map[string]string  `bson:"encryptedValues" json:"-"`         // legacy — migrated to Environments on first access
	ConfigSchema    []ConfigField      `bson:"configSchema" json:"configSchema"`
	DependsOn       []string           `bson:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	NeedsRestart    bool               `bson:"needsRestart" json:"needsRestart"` // true when config changed post-init
	CreatedAt       time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt       time.Time          `bson:"updatedAt" json:"updatedAt"`

	// Environment profiles — keyed by environment name (e.g. "production", "sandbox").
	ActiveEnvironment string                       `bson:"activeEnvironment,omitempty" json:"activeEnvironment,omitempty"`
	Environments      map[string]EnvironmentConfig `bson:"environments,omitempty" json:"-"`
}

// ActiveEnv returns the effective active environment, defaulting to "production".
func (c *ModuleConfig) ActiveEnv() string {
	if c.ActiveEnvironment != "" {
		return c.ActiveEnvironment
	}
	return "production"
}

// AvailableEnvironments returns sorted environment names from the Environments map.
func (c *ModuleConfig) AvailableEnvironments() []string {
	if len(c.Environments) == 0 {
		return DefaultEnvironments
	}
	names := make([]string, 0, len(c.Environments))
	for name := range c.Environments {
		names = append(names, name)
	}
	// Ensure deterministic order: production first, then alphabetical.
	sorted := make([]string, 0, len(names))
	for _, n := range names {
		if n == "production" {
			sorted = append([]string{n}, sorted...)
		} else {
			sorted = append(sorted, n)
		}
	}
	return sorted
}

// ActiveConfigValues returns the config values for the active environment,
// falling back to the legacy top-level ConfigValues for backward compatibility.
func (c *ModuleConfig) ActiveConfigValues() map[string]string {
	if env, ok := c.Environments[c.ActiveEnv()]; ok {
		return env.ConfigValues
	}
	return c.ConfigValues
}

// ActiveEncryptedValues returns the encrypted values for the active environment,
// falling back to the legacy top-level EncryptedValues for backward compatibility.
func (c *ModuleConfig) ActiveEncryptedValues() map[string]string {
	if env, ok := c.Environments[c.ActiveEnv()]; ok {
		return env.EncryptedValues
	}
	return c.EncryptedValues
}
