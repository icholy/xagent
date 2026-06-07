package agentauth

import (
	"slices"

	"github.com/icholy/xagent/internal/auth/authscope"
)

// ScopeOptions describes the task identity and workspace capabilities a token's
// scopes are minted from.
type ScopeOptions struct {
	TaskID       int64
	Workspace    string
	Runner       string
	Capabilities []string
}

// Scopes builds the scopes granted to a task token from the task's identity and
// the workspace's enabled capabilities (CapabilityChildTasks,
// CapabilityGitHubToken). Every task always gets read and write on its own id;
// the child-tasks capability adds read and write on its children plus a
// fully-constrained create scope; the github-token capability adds
// github_token.create.
//
// The create scope is fully constrained here (parent, workspace, and runner all
// present) because an absent predicate key is unconstrained: completeness is the
// minter's responsibility.
//
// Every task scope additionally constrains task.archived:"false", which is what
// makes archiving a task revoke its token: an archived task's row carries
// task.archived:"true", failing the "false" predicate uniformly for reads and
// writes (see proposals/implemented/eliminate-runner-socket-proxy.md §3). The handlers
// pass the task's real archived state from the loaded row (via Task.ScopeAttr);
// the request-only handlers (CreateTask, ListChildTasks) pass a literal
// task.archived:"false" since they create/list active work and have no row.
func Scopes(opts ScopeOptions) authscope.Scopes {
	scopes := authscope.Scopes{
		authscope.New(authscope.OpTaskRead, authscope.WithTaskID(opts.TaskID), authscope.WithTaskArchived(false)),
		authscope.New(authscope.OpTaskWrite, authscope.WithTaskID(opts.TaskID), authscope.WithTaskArchived(false)),
	}
	if slices.Contains(opts.Capabilities, CapabilityChildTasks) {
		scopes = append(scopes,
			authscope.New(authscope.OpTaskRead, authscope.WithTaskParent(opts.TaskID), authscope.WithTaskArchived(false)),
			authscope.New(authscope.OpTaskWrite, authscope.WithTaskParent(opts.TaskID), authscope.WithTaskArchived(false)),
			authscope.New(authscope.OpTaskCreate,
				authscope.WithTaskParent(opts.TaskID),
				authscope.WithTaskWorkspace(opts.Workspace),
				authscope.WithTaskRunner(opts.Runner),
				authscope.WithTaskArchived(false),
			),
		)
	}
	if slices.Contains(opts.Capabilities, CapabilityGitHubToken) {
		scopes = append(scopes, authscope.New(authscope.OpGitHubTokenCreate))
	}
	return scopes
}
