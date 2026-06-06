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

// The types below are typed authscope.Targeter values for the task-caller
// taxonomy. They give the filter call-sites a domain seam instead of inline
// Target/map literals, and reuse the same Seg*/Attr* constants as the minter so
// the two cannot drift. Each emits exactly the attributes its RPC requires (per
// proposal §7): the own-task request cases are id-only, the loaded-row cases
// carry {id, parent}, and ChildList carries {parent} only.

// TaskRead authorizes reading a task by id alone (own-task fast path).
type TaskRead struct{ ID int64 }

func (t TaskRead) Target() authscope.Target {
	return authscope.Target{
		Op:    []string{SegTask, SegRead},
		Attrs: map[string]string{AttrID: strconv.FormatInt(t.ID, 10)},
	}
}

// TaskReadRow authorizes reading a loaded task row, against both its id (own)
// and its parent (child).
type TaskReadRow struct{ ID, Parent int64 }

func (t TaskReadRow) Target() authscope.Target {
	return authscope.Target{
		Op: []string{SegTask, SegRead},
		Attrs: map[string]string{
			AttrID:     strconv.FormatInt(t.ID, 10),
			AttrParent: strconv.FormatInt(t.Parent, 10),
		},
	}
}

// TaskWrite authorizes writing a task by id alone (own-task request cases).
type TaskWrite struct{ ID int64 }

func (t TaskWrite) Target() authscope.Target {
	return authscope.Target{
		Op:    []string{SegTask, SegWrite},
		Attrs: map[string]string{AttrID: strconv.FormatInt(t.ID, 10)},
	}
}

// TaskWriteRow authorizes writing a loaded task row, against both its id (own)
// and its parent (child).
type TaskWriteRow struct{ ID, Parent int64 }

func (t TaskWriteRow) Target() authscope.Target {
	return authscope.Target{
		Op: []string{SegTask, SegWrite},
		Attrs: map[string]string{
			AttrID:     strconv.FormatInt(t.ID, 10),
			AttrParent: strconv.FormatInt(t.Parent, 10),
		},
	}
}

// TaskCreate authorizes creating a task; the predicate set is fully constrained
// (parent, workspace, runner) to match the minter's create scope.
type TaskCreate struct {
	Parent    int64
	Workspace string
	Runner    string
}

func (t TaskCreate) Target() authscope.Target {
	return authscope.Target{
		Op: []string{SegTask, SegCreate},
		Attrs: map[string]string{
			AttrParent:    strconv.FormatInt(t.Parent, 10),
			AttrWorkspace: t.Workspace,
			AttrRunner:    t.Runner,
		},
	}
}

// ChildList authorizes listing the children of a parent task ({parent} only).
type ChildList struct{ Parent int64 }

func (t ChildList) Target() authscope.Target {
	return authscope.Target{
		Op:    []string{SegTask, SegRead},
		Attrs: map[string]string{AttrParent: strconv.FormatInt(t.Parent, 10)},
	}
}

// GitHubTokenCreate authorizes issuing a GitHub token (no instance).
type GitHubTokenCreate struct{}

func (GitHubTokenCreate) Target() authscope.Target {
	return authscope.Target{Op: []string{SegGitHubToken, SegCreate}}
}
