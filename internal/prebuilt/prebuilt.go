package prebuilt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v68/github"
)

const DefaultRepo = "icholy/xagent"

var BinaryNames = []string{
	"xagent-linux-amd64",
	"xagent-linux-arm64",
}

// Dir returns the directory where prebuilt binaries are stored.
// It checks XAGENT_PREBUILT_DIR first, then XAGENT_CONFIG_DIR/prebuilt,
// then falls back to os.UserConfigDir()/xagent/prebuilt.
func Dir() (string, error) {
	if dir := os.Getenv("XAGENT_PREBUILT_DIR"); dir != "" {
		return dir, nil
	}
	if dir := os.Getenv("XAGENT_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "prebuilt"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config directory: %w", err)
	}
	return filepath.Join(dir, "xagent", "prebuilt"), nil
}

// BinaryPath returns the path to a prebuilt binary for the given architecture.
func BinaryPath(arch string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("xagent-linux-%s", arch)), nil
}

// ReadBinary reads the prebuilt binary for the given architecture.
func ReadBinary(arch string) ([]byte, error) {
	binPath, err := BinaryPath(arch)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(binPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("prebuilt binary not found: %s\n\nRun 'xagent download' to download prebuilt binaries", binPath)
		}
		return nil, fmt.Errorf("failed to read binary %s: %w", binPath, err)
	}
	return data, nil
}

// Download fetches the latest prebuilt binaries from GitHub Releases
// and writes them to the prebuilt directory.
func Download(ctx context.Context, repo string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	owner, repoName, ok := strings.Cut(repo, "/")
	if !ok {
		return fmt.Errorf("invalid github-repo format, expected owner/repo: %s", repo)
	}
	client := github.NewClient(nil)
	release, _, err := client.Repositories.GetLatestRelease(ctx, owner, repoName)
	if err != nil {
		return fmt.Errorf("failed to get latest release: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	for _, name := range BinaryNames {
		asset := findAsset(release.Assets, name)
		if asset == nil {
			return fmt.Errorf("asset %s not found in release %s", name, release.GetTagName())
		}
		destPath := filepath.Join(dir, name)
		rc, _, err := client.Repositories.DownloadReleaseAsset(ctx, owner, repoName, asset.GetID(), http.DefaultClient)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", name, err)
		}
		if err := writeFile(destPath, rc); err != nil {
			rc.Close()
			return fmt.Errorf("failed to save %s: %w", name, err)
		}
		rc.Close()
	}
	return nil
}

func findAsset(assets []*github.ReleaseAsset, name string) *github.ReleaseAsset {
	for _, a := range assets {
		if a.GetName() == name {
			return a
		}
	}
	return nil
}

func writeFile(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	return f.Chmod(0755)
}
