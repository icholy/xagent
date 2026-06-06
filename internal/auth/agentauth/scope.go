package agentauth

import (
	"slices"
	"strconv"

	"github.com/icholy/xagent/internal/auth/authscope"
)

// Operation-path segments for the task-caller scope taxonomy. The agnostic
// authscope engine assigns these no meaning; this is the application layer that
// does. They are shared by the runner's token minter (TaskScopes) and the agent
// filter's Target builders so the two cannot drift.
const (
	SegTask        = "task"
	SegRead        = "read"
	SegWrite       = "write"
	SegCreate      = "create"
	SegGitHubToken = "github_token"
)

// Predicate attribute keys for the task-caller scope taxonomy.
const (
	AttrID        = "id"
	AttrParent    = "parent"
	AttrWorkspace = "workspace"
	AttrRunner    = "runner"
)

// TaskScopes builds the grammar scope strings granted to a task token from the
// task's identity and the workspace's enabled capabilities (ScopeChildTasks,
// ScopeGitHubToken). Every task always gets read and write on its own id; the
// child-tasks capability adds read and write on its children plus a
// fully-constrained create scope; the github-token capability adds
// github_token.create.
//
// The create scope is fully constrained here (parent, workspace, and runner all
// present) because an absent predicate key is unconstrained: completeness is the
// minter's responsibility.
func TaskScopes(taskID int64, workspace, runner string, capabilities []string) []string {
	id := strconv.FormatInt(taskID, 10)
	scopes := []string{
		taskScope(SegRead, map[string]string{AttrID: id}),
		taskScope(SegWrite, map[string]string{AttrID: id}),
	}
	if slices.Contains(capabilities, ScopeChildTasks) {
		scopes = append(scopes,
			taskScope(SegRead, map[string]string{AttrParent: id}),
			taskScope(SegWrite, map[string]string{AttrParent: id}),
			taskScope(SegCreate, map[string]string{
				AttrParent:    id,
				AttrWorkspace: workspace,
				AttrRunner:    runner,
			}),
		)
	}
	if slices.Contains(capabilities, ScopeGitHubToken) {
		scopes = append(scopes, authscope.Scope{
			Op: [][]string{{SegGitHubToken}, {SegCreate}},
		}.String())
	}
	return scopes
}

// taskScope builds a task-resource scope string for the given action and
// predicates via the engine's wire form, so call-sites never hand-assemble JSON.
func taskScope(action string, preds map[string]string) string {
	return authscope.Scope{
		Op:    [][]string{{SegTask}, {action}},
		Preds: preds,
	}.String()
}
