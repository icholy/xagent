package agent

import (
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/auth/agentauth"
	"gotest.tools/v3/assert"
)

func TestConfigPrompt_WithoutChildTasksScope(t *testing.T) {
	cfg := &Config{}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(got, "xagent:get_my_task"))
	assert.Assert(t, !strings.Contains(got, "update_child_task"), "child task tools should not be mentioned without scope")
	assert.Assert(t, !strings.Contains(got, "create_child_task"), "child task tools should not be mentioned without scope")
}

func TestConfigPrompt_WithChildTasksScope(t *testing.T) {
	cfg := &Config{Scopes: []string{agentauth.ScopeChildTasks}}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Assert(t, strings.Contains(got, "Use xagent:update_child_task to delegate work to child tasks."))
	assert.Assert(t, strings.Contains(got, "Only use xagent:create_child_task when explicitly instructed to create a new task."))
}

func TestConfigPrompt_Started(t *testing.T) {
	cfg := &Config{Started: true}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Check xagent:get_my_task and continue.")
}

func TestConfigPrompt_WorkspacePromptAppended(t *testing.T) {
	cfg := &Config{Started: true, Prompt: "Custom workspace instructions."}
	got, err := cfg.prompt()
	assert.NilError(t, err)
	assert.Equal(t, got, "The task was updated. Check xagent:get_my_task and continue.\n\nCustom workspace instructions.")
}
