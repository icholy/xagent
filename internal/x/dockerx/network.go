package dockerx

//go:generate go tool moq -pkg dockerx -out network_repairer_moq_test.go . NetworkRepairer

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// NetworkRepairer is the minimal Docker API surface used by RepairNetworks.
type NetworkRepairer interface {
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
	NetworkInspect(ctx context.Context, networkID string, options network.InspectOptions) (network.Inspect, error)
	NetworkDisconnect(ctx context.Context, networkID, containerID string, force bool) error
	NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error
}

// RepairNetworks reconciles a container's network attachments with the live
// Docker network registry. For each name in networks, it compares the
// container's pinned endpoint ID against the live network's ID, and — if
// they differ, or the container isn't attached at all — force-disconnects
// (if attached) and reconnects by name.
//
// Returns the names of the networks that were actually repaired (may be
// empty). Recovers from stale endpoint pins left behind after
// `docker compose down && up` recreates a network under the same name with
// a fresh ID.
func RepairNetworks(ctx context.Context, c NetworkRepairer, containerID string, networks []string) ([]string, error) {
	if len(networks) == 0 {
		return nil, nil
	}
	info, err := c.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect container: %w", err)
	}
	var repaired []string
	for _, name := range networks {
		live, err := c.NetworkInspect(ctx, name, network.InspectOptions{})
		if err != nil {
			return repaired, fmt.Errorf("inspect network %q: %w", name, err)
		}
		var (
			endpoint *network.EndpointSettings
			attached bool
		)
		if info.NetworkSettings != nil {
			endpoint, attached = info.NetworkSettings.Networks[name]
		}
		if attached && endpoint != nil && endpoint.NetworkID == live.ID {
			continue
		}
		if attached {
			if err := c.NetworkDisconnect(ctx, name, containerID, true); err != nil && !isNotConnectedErr(err) {
				return repaired, fmt.Errorf("disconnect %q: %w", name, err)
			}
		}
		if err := c.NetworkConnect(ctx, name, containerID, nil); err != nil {
			return repaired, fmt.Errorf("connect %q: %w", name, err)
		}
		repaired = append(repaired, name)
	}
	return repaired, nil
}

// isNotConnectedErr reports whether err is the daemon's response for
// disconnecting a container that isn't attached to the given network.
// The daemon returns this as plain text; there is no typed sentinel.
func isNotConnectedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "is not connected")
}
