package container

import (
	"context"
	"log/slog"
	"time"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// noopManager satisfies Manager without touching Docker. Returned by
// NewManager when the socket is unreachable or CONTAINER_CONTROL_ENABLED is
// false so that module toggling still works; operators are expected to
// manage external infrastructure themselves in that case.
type noopManager struct {
	logger *slog.Logger
	reason string
}

func (n noopManager) Available() bool { return false }

func (n noopManager) EnsureStarted(_ context.Context, spec module.InfraContainerSpec) error {
	n.logger.Debug("Container control disabled, skipping start",
		slog.String("name", spec.Name),
		slog.String("reason", n.reason))
	return nil
}

func (n noopManager) EnsureStopped(_ context.Context, name string, _ time.Duration) error {
	n.logger.Debug("Container control disabled, skipping stop",
		slog.String("name", name),
		slog.String("reason", n.reason))
	return nil
}

func (n noopManager) IsRunning(_ context.Context, _ string) (bool, error) {
	return false, nil
}
