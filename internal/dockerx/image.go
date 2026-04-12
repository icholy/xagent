package dockerx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
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
	RegistryAuth string
	PullProgress func(PullProgress)
}

// ImageEnsure ensures the image is available locally, pulling it if necessary.
// It returns the image inspect result.
func ImageEnsure(ctx context.Context, docker *client.Client, opts ImageEnsureOptions) (types.ImageInspect, error) {
	info, _, err := docker.ImageInspectWithRaw(ctx, opts.Ref)
	if err == nil {
		return info, nil
	}
	reader, err := docker.ImagePull(ctx, opts.Ref, image.PullOptions{RegistryAuth: opts.RegistryAuth})
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

// ResolveRegistryAuth returns the base64-encoded registry auth for the given
// image reference by reading ~/.docker/config.json. It returns an empty string
// if no credentials are configured for the registry.
func ResolveRegistryAuth(imageRef string) string {
	hostname := resolveRegistryHostname(imageRef)
	cfg, err := config.Load("")
	if err != nil {
		return ""
	}
	authConfig, err := cfg.GetAuthConfig(hostname)
	if err != nil {
		return ""
	}
	if authConfig.Username == "" && authConfig.Password == "" && authConfig.IdentityToken == "" {
		return ""
	}
	encoded, err := registry.EncodeAuthConfig(registry.AuthConfig{
		Username:      authConfig.Username,
		Password:      authConfig.Password,
		Auth:          authConfig.Auth,
		ServerAddress: authConfig.ServerAddress,
		IdentityToken: authConfig.IdentityToken,
		RegistryToken: authConfig.RegistryToken,
	})
	if err != nil {
		return ""
	}
	return encoded
}

// resolveRegistryHostname extracts the registry hostname from an image
// reference. For Docker Hub images (no explicit registry), it returns
// the default Docker Hub registry URL.
func resolveRegistryHostname(imageRef string) string {
	// Docker Hub images have no slash or a single slash (library/alpine, alpine)
	// Images with an explicit registry have a dot or colon before the first slash
	for i, c := range imageRef {
		if c == '/' {
			prefix := imageRef[:i]
			if containsAny(prefix, ".:") {
				return prefix
			}
			// No dot or colon means it's a Docker Hub user/repo
			return "https://index.docker.io/v1/"
		}
	}
	// No slash at all means it's a Docker Hub library image (e.g. "alpine")
	return "https://index.docker.io/v1/"
}

func containsAny(s, chars string) bool {
	for _, c := range chars {
		for _, sc := range s {
			if sc == c {
				return true
			}
		}
	}
	return false
}
