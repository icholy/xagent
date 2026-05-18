package command

import (
	"context"
	"testing"

	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
	"gotest.tools/v3/assert"
)

func TestGitCredentialCommand_ServerDefaultAndEnv(t *testing.T) {
	// Without env var, should use DefaultURL
	var gotServer string
	cmd := &cli.Command{
		Name:  "git-credential",
		Flags: GitCredentialCommand.Flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			gotServer = cmd.String("server")
			return nil
		},
	}
	err := cmd.Run(context.Background(), []string{"git-credential"})
	assert.NilError(t, err)
	assert.Equal(t, gotServer, xagentclient.DefaultURL)

	// With env var, should use env value
	t.Setenv("XAGENT_SERVER", "unix:///var/run/xagent.sock")
	cmd2 := &cli.Command{
		Name:  "git-credential",
		Flags: GitCredentialCommand.Flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			gotServer = cmd.String("server")
			return nil
		},
	}
	err = cmd2.Run(context.Background(), []string{"git-credential"})
	assert.NilError(t, err)
	assert.Equal(t, gotServer, "unix:///var/run/xagent.sock")
}
