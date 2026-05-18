package command

import (
	"context"
	"testing"

	"github.com/urfave/cli/v3"
	"gotest.tools/v3/assert"
)

func TestGitCredentialCommand_TokenFromEnv(t *testing.T) {
	t.Setenv("XAGENT_AGENT_TOKEN", "test-token-from-env")

	var gotToken string
	cmd := &cli.Command{
		Name: "git-credential",
		Flags: GitCredentialCommand.Flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			gotToken = cmd.String("token")
			return nil
		},
	}
	err := cmd.Run(context.Background(), []string{"git-credential"})
	assert.NilError(t, err)
	assert.Equal(t, gotToken, "test-token-from-env")
}

func TestGitCredentialCommand_TokenFlagOverridesEnv(t *testing.T) {
	t.Setenv("XAGENT_AGENT_TOKEN", "env-token")

	var gotToken string
	cmd := &cli.Command{
		Name: "git-credential",
		Flags: GitCredentialCommand.Flags,
		Action: func(_ context.Context, cmd *cli.Command) error {
			gotToken = cmd.String("token")
			return nil
		},
	}
	err := cmd.Run(context.Background(), []string{"git-credential", "--token", "flag-token"})
	assert.NilError(t, err)
	assert.Equal(t, gotToken, "flag-token")
}
