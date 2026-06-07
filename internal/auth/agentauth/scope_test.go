package agentauth

import (
	"testing"

	"github.com/icholy/xagent/internal/auth/authscope"
	"gotest.tools/v3/assert"
)

func TestScopes_OwnTaskOnly(t *testing.T) {
	scopes := Scopes(ScopeOptions{
		TaskID:    42,
		Workspace: "ws",
		Runner:    "rn",
	})

	assert.Assert(t, scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(42), authscope.WithTaskArchived(false)))
	assert.Assert(t, scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(42), authscope.WithTaskArchived(false)))

	// An archived own task is denied: the scope constrains task.archived:"false".
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(42), authscope.WithTaskArchived(true)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(42), authscope.WithTaskArchived(true)))

	// No child, create, or github-token capability without the flags.
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(99), authscope.WithTaskParent(42), authscope.WithTaskArchived(false)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(99), authscope.WithTaskParent(42), authscope.WithTaskArchived(false)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn"), authscope.WithTaskArchived(false)))
	assert.Assert(t, !scopes.Allow(authscope.OpGitHubTokenCreate))
}

func TestScopes_ChildTasks(t *testing.T) {
	scopes := Scopes(ScopeOptions{
		TaskID:       42,
		Workspace:    "ws",
		Runner:       "rn",
		Capabilities: []string{CapabilityChildTasks},
	})

	assert.Assert(t, scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(99), authscope.WithTaskParent(42), authscope.WithTaskArchived(false)))
	assert.Assert(t, scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(99), authscope.WithTaskParent(42), authscope.WithTaskArchived(false)))
	assert.Assert(t, scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn"), authscope.WithTaskArchived(false)))

	// An archived child is denied: the child scopes constrain task.archived:"false".
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(99), authscope.WithTaskParent(42), authscope.WithTaskArchived(true)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(99), authscope.WithTaskParent(42), authscope.WithTaskArchived(true)))

	// Create is fully constrained: a different workspace, runner, or parent is denied.
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("other"), authscope.WithTaskRunner("rn"), authscope.WithTaskArchived(false)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("other"), authscope.WithTaskArchived(false)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(7), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn"), authscope.WithTaskArchived(false)))

	// Unrelated task (neither own nor child) stays denied.
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(7), authscope.WithTaskParent(8), authscope.WithTaskArchived(false)))
	assert.Assert(t, !scopes.Allow(authscope.OpGitHubTokenCreate))
}

func TestScopes_GitHubToken(t *testing.T) {
	scopes := Scopes(ScopeOptions{
		TaskID:       42,
		Workspace:    "ws",
		Runner:       "rn",
		Capabilities: []string{CapabilityGitHubToken},
	})

	assert.Assert(t, scopes.Allow(authscope.OpGitHubTokenCreate))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn"), authscope.WithTaskArchived(false)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(99), authscope.WithTaskParent(42), authscope.WithTaskArchived(false)))
}
