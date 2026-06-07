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
func WithTaskID(id int64) Attr { return Int64Attr(AttrTaskID, id) }

// WithTaskParent builds the task.parent attribute. When ignoreZero is set and
// parent is 0, the attribute is marked unset (see Attr.Ignore) rather than the
// literal "0" — callers set it where a 0 parent means "no parent" (a top-level
// task) or to guard a minted scope against an unconstrained predicate.
func WithTaskParent(parent int64, ignoreZero bool) Attr {
	if ignoreZero && parent == 0 {
		return Attr{Name: AttrTaskParent, Ignore: true}
	}
	return Int64Attr(AttrTaskParent, parent)
}

func WithTaskWorkspace(workspace string) Attr { return StringAttr(AttrTaskWorkspace, workspace) }

// WithTaskRunner builds the task.runner attribute. When ignoreZero is set and
// runner is empty, the attribute is marked unset (see Attr.Ignore) rather than
// the literal "" — see WithTaskParent for when callers set it.
func WithTaskRunner(runner string, ignoreZero bool) Attr {
	if ignoreZero && runner == "" {
		return Attr{Name: AttrTaskRunner, Ignore: true}
	}
	return StringAttr(AttrTaskRunner, runner)
}

// WithWorkspaceRunner builds the workspace.runner attribute for a workspace
// register/clear request.
func WithWorkspaceRunner(runner string) Attr { return StringAttr(AttrWorkspaceRunner, runner) }
