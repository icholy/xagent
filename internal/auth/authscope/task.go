package authscope

// This file defines the concrete task-caller scope taxonomy that rides on the
// generic matching engine in scope.go: the operation paths, the namespaced
// attribute keys, and the typed attribute constructors that call sites pass to
// Scopes.Allow.

// Operation paths, as segment slices. The single source of truth for each
// operation: scope creation (the runner's token minter, agentauth.TaskScopes)
// and Allow lookups share these, so the two can't drift. Treat them as
// immutable — never mutate a returned slice. A wildcard operation would be
// expressed the same way, e.g. var OpTaskAny = []string{"task", "*"}.
var (
	OpTaskRead          = []string{"task", "read"}
	OpTaskWrite         = []string{"task", "write"}
	OpTaskCreate        = []string{"task", "create"}
	OpGitHubTokenCreate = []string{"github_token", "create"}
)

// Attribute keys, namespaced by resource ("task.id", not "id") so attribute
// names stay globally unambiguous as the taxonomy grows.
const (
	AttrTaskID        = "task.id"
	AttrTaskParent    = "task.parent"
	AttrTaskWorkspace = "task.workspace"
	AttrTaskRunner    = "task.runner"
)

// WithTaskID, WithTaskParent, WithTaskWorkspace, and WithTaskRunner build the
// attributes for a task-resource request, pairing each namespaced key with its
// value. Call sites pass them straight to Scopes.Allow.
func WithTaskID(id int64) Attr { return Int64Attr(AttrTaskID, id) }

func WithTaskParent(parent int64) Attr { return Int64Attr(AttrTaskParent, parent) }

func WithTaskWorkspace(workspace string) Attr { return StringAttr(AttrTaskWorkspace, workspace) }

func WithTaskRunner(runner string) Attr { return StringAttr(AttrTaskRunner, runner) }
