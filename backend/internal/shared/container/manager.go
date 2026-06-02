// Package container provides a thin abstraction over the Docker Engine API
// used by the module registry to start/stop containers declared by modules
// via Module.InfraContainers(). Construction is fail-soft: if the Docker
// socket is unreachable or CONTAINER_CONTROL_ENABLED is false, NewManager
// returns a no-op implementation so module toggling still works (operators
// are expected to manage infrastructure externally in that case).
package container

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"

	"github.com/orkestra/backend/pkg/sdk/module"
)

// Manager is the interface consumed by the module registry.
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

type dockerManager struct {
	cli    *client.Client
	logger *slog.Logger
}

// NewManager constructs a Manager. It honors CONTAINER_CONTROL_ENABLED=false
// and any docker-client construction failure by returning a no-op manager.
func NewManager(logger *slog.Logger) Manager {
	if v := strings.ToLower(os.Getenv("CONTAINER_CONTROL_ENABLED")); v == "false" || v == "0" || v == "no" {
		logger.Info("Container control disabled via CONTAINER_CONTROL_ENABLED")
		return noopManager{logger: logger, reason: "disabled"}
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Warn("Docker client unavailable, container control disabled", slog.String("error", err.Error()))
		return noopManager{logger: logger, reason: "unreachable"}
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(pingCtx); err != nil {
		logger.Warn("Docker socket unreachable, container control disabled", slog.String("error", err.Error()))
		_ = cli.Close()
		return noopManager{logger: logger, reason: "ping failed"}
	}

	logger.Info("Docker socket reachable, container manager active")
	return &dockerManager{cli: cli, logger: logger}
}

func (m *dockerManager) Available() bool { return true }

func (m *dockerManager) IsRunning(ctx context.Context, name string) (bool, error) {
	info, err := m.cli.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return info.State != nil && info.State.Running, nil
}

func (m *dockerManager) EnsureStarted(ctx context.Context, spec module.InfraContainerSpec) error {
	if spec.Name == "" || spec.Image == "" {
		return fmt.Errorf("container spec requires Name and Image")
	}

	info, err := m.cli.ContainerInspect(ctx, spec.Name)
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("inspect %s: %w", spec.Name, err)
	}

	switch {
	case errdefs.IsNotFound(err):
		m.logger.Info("Creating infra container", slog.String("name", spec.Name), slog.String("image", spec.Image))
		if err := m.pullIfMissing(ctx, spec.Image); err != nil {
			return fmt.Errorf("pull %s: %w", spec.Image, err)
		}
		if err := m.createContainer(ctx, spec); err != nil {
			return fmt.Errorf("create %s: %w", spec.Name, err)
		}

	case info.State != nil && info.State.Running && !info.State.Restarting:
		m.logger.Debug("Infra container already running", slog.String("name", spec.Name))
		return m.waitHealthy(ctx, spec)

	default:
		// Container exists but is stopped/exited/restarting. Remove and
		// recreate so admin-edited env vars and image overrides take
		// effect on the next start — otherwise ContainerStart would
		// reuse the stale spec baked into the existing container.
		m.logger.Info("Recreating infra container with current spec", slog.String("name", spec.Name))
		if err := m.cli.ContainerRemove(ctx, spec.Name, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("remove stale %s: %w", spec.Name, err)
		}
		if err := m.pullIfMissing(ctx, spec.Image); err != nil {
			return fmt.Errorf("pull %s: %w", spec.Image, err)
		}
		if err := m.createContainer(ctx, spec); err != nil {
			return fmt.Errorf("create %s: %w", spec.Name, err)
		}
	}

	m.logger.Info("Starting infra container", slog.String("name", spec.Name))
	if err := m.cli.ContainerStart(ctx, spec.Name, container.StartOptions{}); err != nil {
		return fmt.Errorf("start %s: %w", spec.Name, err)
	}
	return m.waitHealthy(ctx, spec)
}

func (m *dockerManager) EnsureStopped(ctx context.Context, name string, timeout time.Duration) error {
	info, err := m.cli.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("inspect %s: %w", name, err)
	}
	if info.State == nil || !info.State.Running {
		return nil
	}

	m.logger.Info("Stopping infra container", slog.String("name", name))
	seconds := int(timeout.Seconds())
	if seconds <= 0 {
		seconds = 10
	}
	return m.cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &seconds})
}

func (m *dockerManager) pullIfMissing(ctx context.Context, ref string) error {
	if _, _, err := m.cli.ImageInspectWithRaw(ctx, ref); err == nil {
		return nil
	}
	m.logger.Info("Pulling image", slog.String("image", ref))
	rc, err := m.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	// Drain the pull stream; without this the pull may be cancelled early.
	_, err = io.Copy(io.Discard, rc)
	return err
}

func (m *dockerManager) createContainer(ctx context.Context, spec module.InfraContainerSpec) error {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}

	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, p := range spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		port, err := nat.NewPort(proto, strconv.Itoa(p.ContainerPort))
		if err != nil {
			return fmt.Errorf("invalid container port: %w", err)
		}
		exposed[port] = struct{}{}
		if p.HostPort > 0 {
			bindings[port] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: strconv.Itoa(p.HostPort)}}
		}
	}

	binds := make([]string, 0, len(spec.Volumes))
	for _, v := range spec.Volumes {
		if v.Name == "" || v.Target == "" {
			continue
		}
		binds = append(binds, v.Name+":"+v.Target)
	}

	cfg := &container.Config{
		Image:        spec.Image,
		Env:          env,
		ExposedPorts: exposed,
		Labels:       spec.Labels,
	}
	hostCfg := &container.HostConfig{
		Binds:         binds,
		PortBindings:  bindings,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}

	netCfg := &network.NetworkingConfig{}
	if spec.Network != "" {
		netCfg.EndpointsConfig = map[string]*network.EndpointSettings{
			spec.Network: {},
		}
	}

	_, err := m.cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, spec.Name)
	return err
}

func (m *dockerManager) waitHealthy(ctx context.Context, spec module.InfraContainerSpec) error {
	if spec.HealthCheck == nil {
		return nil
	}
	hc := spec.HealthCheck
	interval := hc.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	retries := hc.Retries
	if retries <= 0 {
		retries = 30
	}
	timeout := hc.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	readyTimeout := spec.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = 60 * time.Second
	}

	// Pick TCP dial vs HTTP GET based on which probe field is set. TCPPort
	// wins when both are specified (unusual) so databases with an optional
	// HTTP admin endpoint still get probed on their primary protocol port.
	var probe func(ctx context.Context) error
	var probeDesc string
	switch {
	case hc.TCPPort > 0:
		addr := fmt.Sprintf("%s:%d", spec.Name, hc.TCPPort)
		dialer := &net.Dialer{Timeout: timeout}
		probe = func(ctx context.Context) error {
			conn, err := dialer.DialContext(ctx, "tcp", addr)
			if err != nil {
				return err
			}
			_ = conn.Close()
			return nil
		}
		probeDesc = "tcp://" + addr
	case hc.HTTPPath != "":
		httpClient := &http.Client{Timeout: timeout}
		probeURL := fmt.Sprintf("http://%s:%d%s", spec.Name, hc.Port, hc.HTTPPath)
		probe = func(ctx context.Context) error {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
			if err != nil {
				return err
			}
			resp, err := httpClient.Do(req)
			if err != nil {
				return err
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 400 {
				return fmt.Errorf("http status %d", resp.StatusCode)
			}
			return nil
		}
		probeDesc = probeURL
	default:
		return fmt.Errorf("container %s health check has neither HTTPPath nor TCPPort set", spec.Name)
	}

	deadline := time.Now().Add(readyTimeout)
	for attempt := 1; attempt <= retries; attempt++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("container %s did not become healthy within %s", spec.Name, readyTimeout)
		}

		if err := probe(ctx); err == nil {
			m.logger.Info("Infra container healthy",
				slog.String("name", spec.Name),
				slog.Int("attempt", attempt))
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	return fmt.Errorf("container %s failed health check at %s after %d attempts", spec.Name, probeDesc, retries)
}
