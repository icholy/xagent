package agentauth

import (
	"testing"

	"github.com/icholy/xagent/internal/auth/authscope"
	"gotest.tools/v3/assert"
)

func TestTaskScopes_OwnTaskOnly(t *testing.T) {
	scopes := Scopes(ScopeOptions{TaskID: 42, Workspace: "ws", Runner: "rn"})

	assert.Assert(t, scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(42)))
	assert.Assert(t, scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(42)))

	// No child, create, or github-token capability without the flags.
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(99), authscope.WithTaskParent(42)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(99), authscope.WithTaskParent(42)))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn")))
	assert.Assert(t, !scopes.Allow(authscope.OpGitHubTokenCreate))
}

func TestTaskScopes_ChildTasks(t *testing.T) {
	scopes := Scopes(ScopeOptions{TaskID: 42, Workspace: "ws", Runner: "rn", Capabilities: []string{CapabilityChildTasks}})

	assert.Assert(t, scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(99), authscope.WithTaskParent(42)))
	assert.Assert(t, scopes.Allow(authscope.OpTaskWrite, authscope.WithTaskID(99), authscope.WithTaskParent(42)))
	assert.Assert(t, scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn")))

	// Create is fully constrained: a different workspace, runner, or parent is denied.
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("other"), authscope.WithTaskRunner("rn")))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("other")))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(7), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn")))

	// Unrelated task (neither own nor child) stays denied.
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(7), authscope.WithTaskParent(8)))
	assert.Assert(t, !scopes.Allow(authscope.OpGitHubTokenCreate))
}

func TestTaskScopes_GitHubToken(t *testing.T) {
	scopes := Scopes(ScopeOptions{TaskID: 42, Workspace: "ws", Runner: "rn", Capabilities: []string{CapabilityGitHubToken}})

	assert.Assert(t, scopes.Allow(authscope.OpGitHubTokenCreate))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskCreate, authscope.WithTaskParent(42), authscope.WithTaskWorkspace("ws"), authscope.WithTaskRunner("rn")))
	assert.Assert(t, !scopes.Allow(authscope.OpTaskRead, authscope.WithTaskID(99), authscope.WithTaskParent(42)))
}
