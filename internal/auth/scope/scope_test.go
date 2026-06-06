package scope

import (
	"testing"

	"gotest.tools/v3/assert"
)

func mustParse(t *testing.T, s string) Scope {
	t.Helper()
	sc, err := Parse(s)
	assert.NilError(t, err)
	return sc
}

func TestMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		scope  Scope
		target Target
		want   bool
	}{
		{
			name:   "exact path no preds",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}},
			target: Target{Op: []string{"task", "read"}},
			want:   true,
		},
		{
			name:   "segment count mismatch scope longer",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}},
			target: Target{Op: []string{"task"}},
			want:   false,
		},
		{
			name:   "segment count mismatch scope shorter",
			scope:  Scope{Op: [][]string{{"task"}}},
			target: Target{Op: []string{"task", "read"}},
			want:   false,
		},
		{
			name:   "wildcard matches exactly one segment",
			scope:  Scope{Op: [][]string{{"task"}, {"*"}}},
			target: Target{Op: []string{"task", "read"}},
			want:   true,
		},
		{
			name:   "wildcard does not span multiple segments",
			scope:  Scope{Op: [][]string{{"task"}, {"*"}}},
			target: Target{Op: []string{"task", "a", "b"}},
			want:   false,
		},
		{
			name:   "single wildcard does not match two segments",
			scope:  Scope{Op: [][]string{{"*"}}},
			target: Target{Op: []string{"task", "read"}},
			want:   false,
		},
		{
			name:   "alternation member matches",
			scope:  Scope{Op: [][]string{{"task"}, {"create", "update"}}},
			target: Target{Op: []string{"task", "update"}},
			want:   true,
		},
		{
			name:   "alternation non-member denied",
			scope:  Scope{Op: [][]string{{"task"}, {"create", "update"}}},
			target: Target{Op: []string{"task", "delete"}},
			want:   false,
		},
		{
			name:   "alternation with star is wildcard (a|b|* == *)",
			scope:  Scope{Op: [][]string{{"task"}, {"a", "b", "*"}}},
			target: Target{Op: []string{"task", "zzz"}},
			want:   true,
		},
		{
			name:   "empty preds matches any instance",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}},
			target: Target{Op: []string{"task", "read"}, Attrs: map[string]string{"id": "99", "parent": "42"}},
			want:   true,
		},
		{
			name:   "absent key in scope is unconstrained",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"id": {"42"}}},
			target: Target{Op: []string{"task", "read"}, Attrs: map[string]string{"id": "42", "parent": "7"}},
			want:   true,
		},
		{
			name:   "star value is unconstrained even when attr present",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"id": {"*"}}},
			target: Target{Op: []string{"task", "read"}, Attrs: map[string]string{"id": "99"}},
			want:   true,
		},
		{
			name:   "star value is unconstrained even when attr absent",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"id": {"*"}}},
			target: Target{Op: []string{"task", "read"}, Attrs: map[string]string{}},
			want:   true,
		},
		{
			name:   "missing target attribute denies",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"id": {"42"}}},
			target: Target{Op: []string{"task", "read"}, Attrs: map[string]string{}},
			want:   false,
		},
		{
			name:   "membership within key matches",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"workspace": {"X", "Y"}}},
			target: Target{Op: []string{"task", "create"}, Attrs: map[string]string{"workspace": "Y"}},
			want:   true,
		},
		{
			name:   "membership within key non-member denied",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"workspace": {"X", "Y"}}},
			target: Target{Op: []string{"task", "create"}, Attrs: map[string]string{"workspace": "Z"}},
			want:   false,
		},
		{
			name:  "AND across keys all match",
			scope: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"parent": {"42"}, "workspace": {"ws"}}},
			target: Target{Op: []string{"task", "create"}, Attrs: map[string]string{
				"parent": "42", "workspace": "ws",
			}},
			want: true,
		},
		{
			name:  "AND across keys one fails",
			scope: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"parent": {"42"}, "workspace": {"ws"}}},
			target: Target{Op: []string{"task", "create"}, Attrs: map[string]string{
				"parent": "42", "workspace": "other",
			}},
			want: false,
		},
		{
			name:   "AND across keys one key missing from target",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"parent": {"42"}, "workspace": {"ws"}}},
			target: Target{Op: []string{"task", "create"}, Attrs: map[string]string{"parent": "42"}},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.scope.Matches(tt.target)
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestAuthorize_OwnTask reproduces the §6b own-task / child-via-parent scenario:
// a caller holding both own-task and child scopes can read its own task and any
// direct child, but not an unrelated task.
func TestAuthorize_OwnTask(t *testing.T) {
	t.Parallel()
	set := Set{
		mustParse(t, `task.read:{"id":42}`),
		mustParse(t, `task.read:{"parent":42}`),
	}
	tests := []struct {
		name  string
		attrs map[string]string
		want  bool
	}{
		{"own task", map[string]string{"id": "42", "parent": "7"}, true},
		{"direct child", map[string]string{"id": "99", "parent": "42"}, true},
		{"unrelated task", map[string]string{"id": "5", "parent": "7"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := set.Authorize(Target{Op: []string{"task", "read"}, Attrs: tt.attrs})
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestAuthorize_CreateConjunction reproduces the §6a create scenario: a single
// fully-constrained scope ANDs all three attributes.
func TestAuthorize_CreateConjunction(t *testing.T) {
	t.Parallel()
	set := Set{mustParse(t, `task.create:{"parent":42,"workspace":"ws","runner":"rn"}`)}

	ok := set.Authorize(Target{Op: []string{"task", "create"}, Attrs: map[string]string{
		"parent": "42", "workspace": "ws", "runner": "rn",
	}})
	assert.Equal(t, ok, true)

	// Wrong workspace is denied even though parent and runner match.
	denied := set.Authorize(Target{Op: []string{"task", "create"}, Attrs: map[string]string{
		"parent": "42", "workspace": "evil", "runner": "rn",
	}})
	assert.Equal(t, denied, false)
}

// TestAuthorize_CreateSetValued reproduces the §6a set-valued create scope:
// "child of 42 in workspace X or Y on runner rn".
func TestAuthorize_CreateSetValued(t *testing.T) {
	t.Parallel()
	set := Set{mustParse(t, `task.create:{"parent":42,"workspace":["X","Y"],"runner":"rn"}`)}

	for _, ws := range []string{"X", "Y"} {
		ok := set.Authorize(Target{Op: []string{"task", "create"}, Attrs: map[string]string{
			"parent": "42", "workspace": ws, "runner": "rn",
		}})
		assert.Equal(t, ok, true)
	}
	denied := set.Authorize(Target{Op: []string{"task", "create"}, Attrs: map[string]string{
		"parent": "42", "workspace": "Z", "runner": "rn",
	}})
	assert.Equal(t, denied, false)
}

// TestAuthorize_SplitConjunctionIsHole documents the §6a failure mode: splitting
// the create conjunction across separate scopes and ORing them leaves the
// unconstrained attributes as holes. This is a regression guard against ever
// minting create scopes that way.
func TestAuthorize_SplitConjunctionIsHole(t *testing.T) {
	t.Parallel()
	set := Set{
		mustParse(t, `task.create:{"parent":42}`),
		mustParse(t, `task.create:{"workspace":"ws"}`),
	}
	// parent matches the first scope, which leaves workspace/runner unconstrained.
	escalated := set.Authorize(Target{Op: []string{"task", "create"}, Attrs: map[string]string{
		"parent": "42", "workspace": "evil", "runner": "evil",
	}})
	assert.Equal(t, escalated, true)
}

// TestAuthorize_WildcardAdmin reproduces the §6c wildcard scenarios.
func TestAuthorize_WildcardAdmin(t *testing.T) {
	t.Parallel()

	// task.* covers any action on a task instance, including a child.
	taskAdmin := Set{mustParse(t, `task.*`)}
	assert.Equal(t, taskAdmin.Authorize(Target{
		Op:    []string{"task", "write"},
		Attrs: map[string]string{"id": "99", "parent": "42"},
	}), true)
	// ...but not a different resource.
	assert.Equal(t, taskAdmin.Authorize(Target{
		Op: []string{"github_token", "create"},
	}), false)

	// *.* covers any 2-segment operation with any instance.
	admin := Set{mustParse(t, `*.*`)}
	assert.Equal(t, admin.Authorize(Target{Op: []string{"github_token", "create"}}), true)
	assert.Equal(t, admin.Authorize(Target{
		Op:    []string{"task", "read"},
		Attrs: map[string]string{"id": "1"},
	}), true)
	// ...but not a 3-segment operation: * matches exactly one segment.
	assert.Equal(t, admin.Authorize(Target{Op: []string{"task", "read", "extra"}}), false)
}

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want Scope
	}{
		{
			name: "no predicates",
			in:   "github_token.create",
			want: Scope{Op: [][]string{{"github_token"}, {"create"}}, Preds: map[string][]string{}},
		},
		{
			name: "empty predicate object",
			in:   "task.read:{}",
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{}},
		},
		{
			name: "number scalar normalizes to singleton",
			in:   `task.read:{"id":42}`,
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"id": {"42"}}},
		},
		{
			name: "string scalar normalizes to singleton",
			in:   `task.create:{"workspace":"X"}`,
			want: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"workspace": {"X"}}},
		},
		{
			name: "bool scalar normalizes to singleton",
			in:   `task.read:{"flag":true}`,
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"flag": {"true"}}},
		},
		{
			name: "string array normalizes to set",
			in:   `task.create:{"workspace":["X","Y"]}`,
			want: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"workspace": {"X", "Y"}}},
		},
		{
			name: "number array stringifies each element",
			in:   `task.read:{"ids":[1,2,3]}`,
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"ids": {"1", "2", "3"}}},
		},
		{
			name: "star value preserved as member",
			in:   `task.write:{"id":"*"}`,
			want: Scope{Op: [][]string{{"task"}, {"write"}}, Preds: map[string][]string{"id": {"*"}}},
		},
		{
			name: "path alternation parsed at parse time",
			in:   `task.read|write:{"id":1}`,
			want: Scope{Op: [][]string{{"task"}, {"read", "write"}}, Preds: map[string][]string{"id": {"1"}}},
		},
		{
			name: "wildcard segment",
			in:   "task.*",
			want: Scope{Op: [][]string{{"task"}, {"*"}}, Preds: map[string][]string{}},
		},
		{
			name: "first colon split with colons inside json",
			in:   `task.read:{"url":"http://example.com:8080/p"}`,
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string][]string{"url": {"http://example.com:8080/p"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tt.in)
			assert.NilError(t, err)
			assert.DeepEqual(t, got, tt.want)
		})
	}
}

func TestParse_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
	}{
		{"empty string", ""},
		{"empty path with predicates", `:{"id":1}`},
		{"empty segment middle", "task..read"},
		{"trailing dot", "task."},
		{"leading dot", ".task"},
		{"empty alternative trailing", "task.create|"},
		{"empty alternative leading", "task.|update"},
		{"empty alternative middle", "task.create||update"},
		{"bad json", `task.read:{nope}`},
		{"trailing colon without json", "task.read:"},
		{"null predicates", "task.read:null"},
		{"array predicates", "task.read:[1,2]"},
		{"scalar predicates", "task.read:42"},
		{"empty value set", `task.read:{"id":[]}`},
		{"nested object value", `task.read:{"a":{"b":1}}`},
		{"nested array value", `task.read:{"a":[[1]]}`},
		{"null value", `task.read:{"a":null}`},
		{"trailing data after object", `task.read:{}garbage`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(tt.in)
			assert.Assert(t, err != nil, "expected error for %q", tt.in)
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		scope Scope
		want  string
	}{
		{
			name:  "no predicates omits colon",
			scope: Scope{Op: [][]string{{"github_token"}, {"create"}}},
			want:  "github_token.create",
		},
		{
			name:  "alternation joined with pipe",
			scope: Scope{Op: [][]string{{"task"}, {"read", "write"}}},
			want:  "task.read|write",
		},
		{
			name:  "predicates emitted as sorted string arrays",
			scope: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string][]string{"workspace": {"X", "Y"}, "parent": {"42"}}},
			want:  `task.create:{"parent":["42"],"workspace":["X","Y"]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.scope.String(), tt.want)
		})
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"task.read",
		"task.*",
		"*.*",
		"github_token.create",
		`task.read|write:{"id":1}`,
		`task.write:{"id":"*"}`,
		`task.create:{"parent":42,"workspace":["X","Y"],"runner":"rn"}`,
		`task.read:{"url":"http://example.com:8080/p"}`,
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			first := mustParse(t, in)
			second := mustParse(t, first.String())
			assert.DeepEqual(t, first, second)
		})
	}
}

func TestValidScope(t *testing.T) {
	t.Parallel()
	valid := []string{
		"task.read",
		"task.*",
		"*.*",
		"task.read|write",
		`task.create:{"parent":42,"workspace":["X","Y"],"runner":"rn"}`,
		"github_token.create",
	}
	for _, s := range valid {
		assert.Assert(t, ValidScope(s), "expected %q to be valid", s)
	}
	invalid := []string{
		"",
		"task.",
		"task..read",
		"task.create|",
		"task.|update",
		`task.read:{nope}`,
		`task.read:{"id":[]}`,
		"task.read:null",
	}
	for _, s := range invalid {
		assert.Assert(t, !ValidScope(s), "expected %q to be invalid", s)
	}
}
