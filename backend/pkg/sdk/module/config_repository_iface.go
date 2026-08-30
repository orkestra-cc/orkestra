package module

import "context"

// ConfigRepository abstracts the module_configs collection so the service can
// be unit-tested with a fake. The fake lives beside its consumers in
// recordlist_fake_repo_test.go. *ModuleConfigRepository satisfies it.
//
// The surface is exactly what ModuleConfigService calls — no more. Every
// method added here is a method every substitute implementation must carry,
// so repository helpers with no service consumer stay off it.
type ConfigRepository interface {
	FindByName(ctx context.Context, name string) (*ModuleConfig, error)
	FindAll(ctx context.Context) ([]ModuleConfig, error)
	Upsert(ctx context.Context, config *ModuleConfig) error
	UpdateEnabled(ctx context.Context, name string, enabled bool) error
	UpdateConfigValues(ctx context.Context, name string, values, encrypted map[string]string) error
	UpdateEnvironmentConfig(ctx context.Context, name, envName string, values, encrypted map[string]string) error
	SetActiveEnvironment(ctx context.Context, name, envName string) error
	ActivateEnvironment(ctx context.Context, name, envName string) error
	MigrateToEnvironments(ctx context.Context, name string, configValues, encryptedValues map[string]string) error
	ClearNeedsRestart(ctx context.Context, name string) error
	RefreshMetadata(ctx context.Context, m Module) error
	CompareAndSwapEnvironment(ctx context.Context, name, envName string, expectedRevision int64, next EnvironmentConfig, needsRestart bool) (bool, error)
	CompareAndSwapConfig(ctx context.Context, name string, m ConfigMutation) (bool, error)
}

// ConfigRepository is provided TO ModuleConfigService by the host, never
// implemented BY a module — so, like RedisClient, it is outside the SDK's
// additive-only rule for consumer interfaces (see pkg/sdk/CLAUDE.md,
// "Versioning policy"). A fork that substitutes its own repository (a test
// double, typically) tracks it.
var _ ConfigRepository = (*ModuleConfigRepository)(nil)
