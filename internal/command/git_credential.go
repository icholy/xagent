package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

// GitCredentialCommand implements a git credential helper that fetches
// GitHub App installation tokens from the xagent server.
var GitCredentialCommand = &cli.Command{
	Name:   "git-credential",
	Usage:  "Git credential helper for GitHub App tokens",
	Hidden: true,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "server",
			Usage: "C2 server URL",
			Value: "unix:///var/run/xagent.sock",
		},
		&cli.StringFlag{
			Name:  "token",
			Usage: "Authentication token",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		action := cmd.Args().First()
		client := xagentclient.New(xagentclient.Options{
			BaseURL: cmd.String("server"),
			Token:   cmd.String("token"),
		})
		return runGitCredential(ctx, action, cmd.Root().Reader, cmd.Root().Writer, client)
	},
}

// runGitCredential executes the git credential helper logic.
func runGitCredential(ctx context.Context, action string, r io.Reader, w io.Writer, client xagentclient.Client) error {
	switch action {
	case "get":
		return gitCredentialGet(ctx, r, w, client)
	case "store", "erase":
		// Read and discard stdin, exit 0.
		_, _ = io.Copy(io.Discard, r)
		return nil
	default:
		// Unknown actions exit 0 per git spec.
		return nil
	}
}

// parseGitCredentialInput parses the key=value block from git's credential helper stdin.
func parseGitCredentialInput(r io.Reader) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			result[key] = value
		}
	}
	return result
}

// formatGitCredentialOutput writes credential output in the git key=value format.
func formatGitCredentialOutput(w io.Writer, token string) error {
	_, err := fmt.Fprintf(w, "username=x-access-token\npassword=%s\n\n", token)
	return err
}

func gitCredentialGet(ctx context.Context, r io.Reader, w io.Writer, client xagentclient.Client) error {
	fields := parseGitCredentialInput(r)

	// Only handle github.com over https.
	if fields["host"] != "github.com" {
		return nil
	}
	if protocol, ok := fields["protocol"]; ok && protocol != "https" {
		return nil
	}

	resp, err := client.CreateGitHubToken(ctx, &xagentv1.CreateGitHubTokenRequest{})
	if err != nil {
		return fmt.Errorf("failed to get GitHub token: %w", err)
	}

	return formatGitCredentialOutput(w, resp.Token)
}
