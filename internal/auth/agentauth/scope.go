package agentauth

import (
	"slices"
	"strconv"

	"github.com/icholy/xagent/internal/auth/authscope"
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
		taskScope(authscope.OpTaskRead, map[string]string{authscope.AttrTaskID: id}),
		taskScope(authscope.OpTaskWrite, map[string]string{authscope.AttrTaskID: id}),
	}
	if slices.Contains(capabilities, ScopeChildTasks) {
		scopes = append(scopes,
			taskScope(authscope.OpTaskRead, map[string]string{authscope.AttrTaskParent: id}),
			taskScope(authscope.OpTaskWrite, map[string]string{authscope.AttrTaskParent: id}),
			taskScope(authscope.OpTaskCreate, map[string]string{
				authscope.AttrTaskParent:    id,
				authscope.AttrTaskWorkspace: workspace,
				authscope.AttrTaskRunner:    runner,
			}),
		)
	}
	if slices.Contains(capabilities, ScopeGitHubToken) {
		scopes = append(scopes, authscope.Scope{Op: authscope.OpGitHubTokenCreate}.String())
	}
	return scopes
}

// taskScope builds a scope string for the given operation path and predicates
// via the engine's wire form, so call-sites never hand-assemble JSON.
func taskScope(op []string, preds map[string]string) string {
	return authscope.Scope{Op: op, Preds: preds}.String()
}
