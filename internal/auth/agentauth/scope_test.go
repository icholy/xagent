package agentauth

import (
	"testing"

	"github.com/icholy/xagent/internal/auth/authscope"
	"gotest.tools/v3/assert"
)

func TestTaskScopes_OwnTaskOnly(t *testing.T) {
	scopes, err := authscope.ParseSet(TaskScopes(42, "ws", "rn", nil))
	assert.NilError(t, err)

	assert.Assert(t, scopes.Authorize(TaskRead{ID: 42}))
	assert.Assert(t, scopes.Authorize(TaskWrite{ID: 42}))

	// No child, create, or github-token capability without the flags.
	assert.Assert(t, !scopes.Authorize(TaskReadRow{ID: 99, Parent: 42}))
	assert.Assert(t, !scopes.Authorize(TaskWriteRow{ID: 99, Parent: 42}))
	assert.Assert(t, !scopes.Authorize(TaskCreate{Parent: 42, Workspace: "ws", Runner: "rn"}))
	assert.Assert(t, !scopes.Authorize(GitHubTokenCreate{}))
}

func TestTaskScopes_ChildTasks(t *testing.T) {
	scopes, err := authscope.ParseSet(TaskScopes(42, "ws", "rn", []string{ScopeChildTasks}))
	assert.NilError(t, err)

	assert.Assert(t, scopes.Authorize(TaskReadRow{ID: 99, Parent: 42}))
	assert.Assert(t, scopes.Authorize(TaskWriteRow{ID: 99, Parent: 42}))
	assert.Assert(t, scopes.Authorize(TaskCreate{Parent: 42, Workspace: "ws", Runner: "rn"}))

	// Create is fully constrained: a different workspace, runner, or parent is denied.
	assert.Assert(t, !scopes.Authorize(TaskCreate{Parent: 42, Workspace: "other", Runner: "rn"}))
	assert.Assert(t, !scopes.Authorize(TaskCreate{Parent: 42, Workspace: "ws", Runner: "other"}))
	assert.Assert(t, !scopes.Authorize(TaskCreate{Parent: 7, Workspace: "ws", Runner: "rn"}))

	// Unrelated task (neither own nor child) stays denied.
	assert.Assert(t, !scopes.Authorize(TaskReadRow{ID: 7, Parent: 8}))
	assert.Assert(t, !scopes.Authorize(GitHubTokenCreate{}))
}

func TestTaskScopes_GitHubToken(t *testing.T) {
	scopes, err := authscope.ParseSet(TaskScopes(42, "ws", "rn", []string{ScopeGitHubToken}))
	assert.NilError(t, err)

	assert.Assert(t, scopes.Authorize(GitHubTokenCreate{}))
	assert.Assert(t, !scopes.Authorize(TaskCreate{Parent: 42, Workspace: "ws", Runner: "rn"}))
	assert.Assert(t, !scopes.Authorize(TaskReadRow{ID: 99, Parent: 42}))
}
