package agentauth

import (
	"slices"

	"github.com/icholy/xagent/internal/auth/authscope"
)

// TaskScopes builds the scopes granted to a task token from the task's identity
// and the workspace's enabled capabilities (ScopeChildTasks, ScopeGitHubToken).
// Every task always gets read and write on its own id; the child-tasks
// capability adds read and write on its children plus a fully-constrained create
// scope; the github-token capability adds github_token.create.
//
// The create scope is fully constrained here (parent, workspace, and runner all
// present) because an absent predicate key is unconstrained: completeness is the
// minter's responsibility.
func TaskScopes(taskID int64, workspace, runner string, capabilities []string) authscope.Scopes {
	scopes := authscope.Scopes{
		authscope.Make(authscope.OpTaskRead, authscope.WithTaskID(taskID)),
		authscope.Make(authscope.OpTaskWrite, authscope.WithTaskID(taskID)),
	}
	if slices.Contains(capabilities, ScopeChildTasks) {
		scopes = append(scopes,
			authscope.Make(authscope.OpTaskRead, authscope.WithTaskParent(taskID)),
			authscope.Make(authscope.OpTaskWrite, authscope.WithTaskParent(taskID)),
			authscope.Make(authscope.OpTaskCreate,
				authscope.WithTaskParent(taskID),
				authscope.WithTaskWorkspace(workspace),
				authscope.WithTaskRunner(runner),
			),
		)
	}
	if slices.Contains(capabilities, ScopeGitHubToken) {
		scopes = append(scopes, authscope.Make(authscope.OpGitHubTokenCreate))
	}
	return scopes
}
