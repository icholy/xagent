package containerbuild

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/icholy/xagent/internal/workspace"
)

// File is a file to copy into the container.
type File struct {
	Path    string // absolute path in the container (e.g. /usr/local/bin/xagent)
	Data    []byte
	Mode    int64
	DirMode int64 // if non-zero, create parent directory with this mode
}

// Builder holds the configuration for building a single container.
type Builder struct {
	Docker    *client.Client
	Name      string
	Workspace *workspace.Workspace
	Cmd       []string
	Labels    map[string]string
	Env       []string
	Binds     []string
	Files     []File
}

// Build creates the Docker container and copies all queued files into it.
// Returns the container ID.
func (b *Builder) Build(ctx context.Context) (string, error) {
	wc := &b.Workspace.Container

	resp, err := b.Docker.ContainerCreate(ctx,
		&container.Config{
			Image:      wc.Image,
			User:       wc.User,
			Labels:     b.Labels,
			Cmd:        b.Cmd,
			Env:        append(wc.Environ(), b.Env...),
			WorkingDir: wc.WorkingDir,
		},
		&container.HostConfig{
			Binds:    append(b.Binds, wc.Volumes...),
			GroupAdd: wc.GroupAdd,
			Runtime:  wc.Runtime,
		},
		wc.NetworkingConfig(),
		nil,
		b.Name,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	if err := b.copyFiles(ctx, resp.ID); err != nil {
		return "", fmt.Errorf("failed to copy files: %w", err)
	}

	return resp.ID, nil
}

func (b *Builder) copyFiles(ctx context.Context, containerID string) error {
	if len(b.Files) == 0 {
		return nil
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	dirs := make(map[string]bool)
	for _, f := range b.Files {
		// Create parent directory entry if DirMode is specified.
		if f.DirMode != 0 {
			dir := strings.TrimPrefix(f.Path, "/")
			dir = dir[:strings.LastIndex(dir, "/")]
			if !dirs[dir] {
				dirs[dir] = true
				if err := tw.WriteHeader(&tar.Header{
					Name:     dir + "/",
					Mode:     f.DirMode,
					Typeflag: tar.TypeDir,
				}); err != nil {
					return err
				}
			}
		}
		name := strings.TrimPrefix(f.Path, "/")
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: f.Mode,
			Size: int64(len(f.Data)),
		}); err != nil {
			return err
		}
		if _, err := tw.Write(f.Data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return b.Docker.CopyToContainer(ctx, containerID, "/", &buf, container.CopyToContainerOptions{})
}
