package agentauth

import (
	"testing"

	"github.com/icholy/xagent/internal/auth/authscope"
	"gotest.tools/v3/assert"
)

func mustSet(t *testing.T, scopes []string) authscope.Set {
	t.Helper()
	set, err := authscope.ParseSet(scopes)
	assert.NilError(t, err)
	return set
}

func readTarget(id, parent string) authscope.Target {
	attrs := map[string]string{AttrID: id}
	if parent != "" {
		attrs[AttrParent] = parent
	}
	return authscope.Target{Op: []string{SegTask, SegRead}, Attrs: attrs}
}

func writeTarget(id, parent string) authscope.Target {
	attrs := map[string]string{AttrID: id}
	if parent != "" {
		attrs[AttrParent] = parent
	}
	return authscope.Target{Op: []string{SegTask, SegWrite}, Attrs: attrs}
}

func createTarget(parent, workspace, runner string) authscope.Target {
	return authscope.Target{
		Op: []string{SegTask, SegCreate},
		Attrs: map[string]string{
			AttrParent:    parent,
			AttrWorkspace: workspace,
			AttrRunner:    runner,
		},
	}
}

func githubTokenTarget() authscope.Target {
	return authscope.Target{Op: []string{SegGitHubToken, SegCreate}}
}

func TestTaskScopes_OwnTaskOnly(t *testing.T) {
	set := mustSet(t, TaskScopes(42, "ws", "rn", nil))

	assert.Assert(t, set.Authorize(readTarget("42", "0")))
	assert.Assert(t, set.Authorize(writeTarget("42", "0")))

	// No child, create, or github-token capability without the flags.
	assert.Assert(t, !set.Authorize(readTarget("99", "42")))
	assert.Assert(t, !set.Authorize(writeTarget("99", "42")))
	assert.Assert(t, !set.Authorize(createTarget("42", "ws", "rn")))
	assert.Assert(t, !set.Authorize(githubTokenTarget()))
}

func TestTaskScopes_ChildTasks(t *testing.T) {
	set := mustSet(t, TaskScopes(42, "ws", "rn", []string{ScopeChildTasks}))

	assert.Assert(t, set.Authorize(readTarget("99", "42")))
	assert.Assert(t, set.Authorize(writeTarget("99", "42")))
	assert.Assert(t, set.Authorize(createTarget("42", "ws", "rn")))

	// Create is fully constrained: a different workspace or runner is denied.
	assert.Assert(t, !set.Authorize(createTarget("42", "other", "rn")))
	assert.Assert(t, !set.Authorize(createTarget("42", "ws", "other")))
	assert.Assert(t, !set.Authorize(createTarget("7", "ws", "rn")))

	// Unrelated task (neither own nor child) stays denied.
	assert.Assert(t, !set.Authorize(readTarget("7", "8")))
	assert.Assert(t, !set.Authorize(githubTokenTarget()))
}

func TestTaskScopes_GitHubToken(t *testing.T) {
	set := mustSet(t, TaskScopes(42, "ws", "rn", []string{ScopeGitHubToken}))

	assert.Assert(t, set.Authorize(githubTokenTarget()))
	assert.Assert(t, !set.Authorize(createTarget("42", "ws", "rn")))
	assert.Assert(t, !set.Authorize(readTarget("99", "42")))
}
