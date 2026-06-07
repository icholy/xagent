package authscope

// This file defines the concrete scope taxonomy that rides on the generic
// matching engine in scope.go: the operation paths, the namespaced attribute
// keys, and the typed attribute constructors that call sites pass to
// Scopes.Allow. It covers both the task-caller surface (the agent path's
// AgentFilter and the runner minter agentauth.Scopes) and the API-caller
// surface (the apiserver handlers); see the per-RPC mapping in
// proposals/draft/scope-based-permissions.md §7.

// Operation paths, as segment slices. The single source of truth for each
// operation: scope creation (the runner's token minter, agentauth.Scopes)
// and Allow lookups share these, so the two can't drift. Treat them as
// immutable — never mutate a returned slice. A wildcard operation would be
// expressed the same way, e.g. var OpTaskAny = []string{"task", "*"}.
var (
	OpTaskRead          = []string{"task", "read"}
	OpTaskWrite         = []string{"task", "write"}
	OpTaskCreate        = []string{"task", "create"}
	OpGitHubTokenCreate = []string{"github_token", "create"}

	// API-caller operation paths (proposal §7). Events and keys are managed
	// coarsely — there is no instance attribute for them — so their RPCs are
	// op-level checks. Lifecycle and sub-resource verbs fold into write.
	OpEventRead      = []string{"event", "read"}
	OpEventWrite     = []string{"event", "write"} // delete + add/remove task fold into write
	OpEventCreate    = []string{"event", "create"}
	OpWorkspaceRead  = []string{"workspace", "read"}
	OpWorkspaceWrite = []string{"workspace", "write"} // register + clear
	OpKeyRead        = []string{"key", "read"}
	OpKeyCreate      = []string{"key", "create"}
	OpKeyWrite       = []string{"key", "write"} // delete folds into write (coarse, no key.id)
	OpOrgRead        = []string{"org", "read"}  // settings, members, routing-rule reads
	OpOrgWrite       = []string{"org", "write"} // members, settings, routing-rules, GH-installation link
	OpOrgCreate      = []string{"org", "create"}
	OpOrgDelete      = []string{"org", "delete"}
	OpAccountWrite   = []string{"account", "write"} // unlink GitHub/Atlassian — user-identity axis
)

// Attribute keys, namespaced by resource ("task.id", not "id") so attribute
// names stay globally unambiguous as the taxonomy grows.
const (
	AttrTaskID        = "task.id"
	AttrTaskParent    = "task.parent"
	AttrTaskWorkspace = "task.workspace"
	AttrTaskRunner    = "task.runner"

	// AttrWorkspaceRunner scopes workspace register/clear to a single runner:
	// a runner registering or clearing only its own workspaces is a genuine
	// isolation boundary on the API surface (proposal §7).
	AttrWorkspaceRunner = "workspace.runner"
)

// WithTaskID, WithTaskParent, WithTaskWorkspace, and WithTaskRunner build the
// attributes for a task-resource request, pairing each namespaced key with its
// value. Call sites pass them straight to Scopes.Allow.
//
// A zero argument (id 0, empty string) yields an ignored attr rather than the
// literal "0"/"": on the Allow side that attribute is treated as unset (e.g. a
// top-level CreateTask with parent 0 doesn't assert task.parent), while New
// panics on it (see Attr.Ignore).
func WithTaskID(id int64) Attr { return int64OrIgnore(AttrTaskID, id) }

func WithTaskParent(parent int64) Attr { return int64OrIgnore(AttrTaskParent, parent) }

func WithTaskWorkspace(workspace string) Attr { return stringOrIgnore(AttrTaskWorkspace, workspace) }

func WithTaskRunner(runner string) Attr { return stringOrIgnore(AttrTaskRunner, runner) }

// WithWorkspaceRunner builds the workspace.runner attribute for a workspace
// register/clear request. An empty runner yields an ignored attr, so a
// runner-less clear falls back to the coarse workspace.write check.
func WithWorkspaceRunner(runner string) Attr { return stringOrIgnore(AttrWorkspaceRunner, runner) }

// int64OrIgnore builds an Int64Attr, or an ignored attr when v is the zero value
// (0) so a zero id/parent reads as unset rather than the literal "0".
func int64OrIgnore(name string, v int64) Attr {
	if v == 0 {
		return Attr{Name: name, Ignore: true}
	}
	return Int64Attr(name, v)
}

// stringOrIgnore builds a StringAttr, or an ignored attr when v is empty so an
// unset string reads as unset rather than the literal "".
func stringOrIgnore(name, v string) Attr {
	if v == "" {
		return Attr{Name: name, Ignore: true}
	}
	return StringAttr(name, v)
}
