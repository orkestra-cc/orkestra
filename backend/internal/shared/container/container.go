// Package container provides a thin abstraction over the Docker Engine API
// used by the module registry to start/stop containers declared by modules
// via Module.InfraContainers().
//
// The Docker-backed implementation lives in manager.go behind the
// `docker_infra` build tag. The DEFAULT build compiles only the no-op manager
// (manager_default.go), so the base binary carries NO Docker SDK dependency and
// none of its vulnerability surface. A fork that needs managed infra builds
// with `-tags docker_infra` (and re-adds the /var/run/docker.sock mount); with
// the tag absent, modules that declare InfraContainers() are simply not
// actuated and operators manage that infrastructure externally.
package container

import (
	"context"
	"time"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// Manager is the interface consumed by the module registry. It is build-tag
// independent: both the Docker-backed manager and the no-op manager implement
// it, so the registry wiring (module.ContainerManager) is unchanged regardless
// of build tags.
type Manager interface {
	// EnsureStarted creates the container if missing, starts it if stopped,
	// and blocks until the health check passes (or ReadyTimeout elapses).
	// A nil return means the container is ready to serve.
	EnsureStarted(ctx context.Context, spec module.InfraContainerSpec) error

	// EnsureStopped stops the container if it's running. No-op otherwise.
	EnsureStopped(ctx context.Context, name string, timeout time.Duration) error

	// IsRunning returns whether a container with the given name is currently up.
	IsRunning(ctx context.Context, name string) (bool, error)

	// Available reports whether the manager can actually control Docker.
	// Returns false for the no-op manager.
	Available() bool
}
