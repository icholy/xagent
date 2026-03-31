package dockerx

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// ImageEnsure ensures the image is available locally, pulling it if necessary.
// It returns the image inspect result.
func ImageEnsure(ctx context.Context, docker *client.Client, ref string, log *slog.Logger) (types.ImageInspect, error) {
	info, _, err := docker.ImageInspectWithRaw(ctx, ref)
	if err == nil {
		return info, nil
	}
	log.Info("pulling image", "image", ref)
	reader, err := docker.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()
	dec := json.NewDecoder(reader)
	for {
		var msg pullProgress
		if err := dec.Decode(&msg); err != nil {
			break
		}
		if msg.Status != "" {
			if msg.ID != "" {
				log.Info("pull", "status", msg.Status, "id", msg.ID, "progress", msg.Progress)
			} else {
				log.Info("pull", "status", msg.Status)
			}
		}
	}
	info, _, err = docker.ImageInspectWithRaw(ctx, ref)
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("failed to inspect image after pull: %w", err)
	}
	return info, nil
}

type pullProgress struct {
	Status   string `json:"status"`
	ID       string `json:"id"`
	Progress string `json:"progress"`
}
