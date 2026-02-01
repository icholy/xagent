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

type copyEntry struct {
	path    string
	content []byte
	mode    int64
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

	files []copyEntry
}

// CopyFile queues a file to be copied into the container when Build is called.
// The path should be absolute (e.g. /usr/local/bin/xagent).
func (b *Builder) CopyFile(path string, content []byte, mode int64) {
	b.files = append(b.files, copyEntry{
		path:    path,
		content: content,
		mode:    mode,
	})
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
	if len(b.files) == 0 {
		return nil
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range b.files {
		name := strings.TrimPrefix(f.path, "/")
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: f.mode,
			Size: int64(len(f.content)),
		}); err != nil {
			return err
		}
		if _, err := tw.Write(f.content); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return b.Docker.CopyToContainer(ctx, containerID, "/", &buf, container.CopyToContainerOptions{})
}
