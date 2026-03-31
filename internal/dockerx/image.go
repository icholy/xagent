package dockerx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// PullProgress represents a progress update from a Docker image pull.
type PullProgress struct {
	Status   string `json:"status"`
	ID       string `json:"id"`
	Progress string `json:"progress"`
}

// ImageEnsureOptions configures the ImageEnsure function.
type ImageEnsureOptions struct {
	Ref          string
	PullProgress func(PullProgress)
}

// ImageEnsure ensures the image is available locally, pulling it if necessary.
// It returns the image inspect result.
func ImageEnsure(ctx context.Context, docker *client.Client, opts ImageEnsureOptions) (types.ImageInspect, error) {
	info, _, err := docker.ImageInspectWithRaw(ctx, opts.Ref)
	if err == nil {
		return info, nil
	}
	reader, err := docker.ImagePull(ctx, opts.Ref, image.PullOptions{})
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()
	dec := json.NewDecoder(reader)
	for {
		var msg PullProgress
		if err := dec.Decode(&msg); err != nil {
			break
		}
		if opts.PullProgress != nil {
			opts.PullProgress(msg)
		}
	}
	info, _, err = docker.ImageInspectWithRaw(ctx, opts.Ref)
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("failed to inspect image after pull: %w", err)
	}
	return info, nil
}
