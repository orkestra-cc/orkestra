//go:build !docker_infra

package container

import "log/slog"

// NewManager returns the no-op manager in the DEFAULT build. The Docker-backed
// manager is compiled only with `-tags docker_infra` (see manager.go), so the
// base binary imports no Docker SDK and carries no Docker-related vulnerability
// surface. Modules that declare InfraContainers() are not actuated in this
// build — operators manage that infrastructure externally.
func NewManager(logger *slog.Logger) Manager {
	logger.Info("Container control compiled out (build without -tags docker_infra); using no-op manager")
	return noopManager{logger: logger, reason: "compiled out"}
}

var _ Manager = noopManager{}
