package authscope

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
			target: MakeTarget("task.read"),
			want:   true,
		},
		{
			name:   "segment count mismatch scope longer",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}},
			target: MakeTarget("task"),
			want:   false,
		},
		{
			name:   "segment count mismatch scope shorter",
			scope:  Scope{Op: [][]string{{"task"}}},
			target: MakeTarget("task.read"),
			want:   false,
		},
		{
			name:   "wildcard matches exactly one segment",
			scope:  Scope{Op: [][]string{{"task"}, {"*"}}},
			target: MakeTarget("task.read"),
			want:   true,
		},
		{
			name:   "wildcard does not span multiple segments",
			scope:  Scope{Op: [][]string{{"task"}, {"*"}}},
			target: MakeTarget("task.a.b"),
			want:   false,
		},
		{
			name:   "single wildcard does not match two segments",
			scope:  Scope{Op: [][]string{{"*"}}},
			target: MakeTarget("task.read"),
			want:   false,
		},
		{
			name:   "alternation member matches",
			scope:  Scope{Op: [][]string{{"task"}, {"create", "update"}}},
			target: MakeTarget("task.update"),
			want:   true,
		},
		{
			name:   "alternation non-member denied",
			scope:  Scope{Op: [][]string{{"task"}, {"create", "update"}}},
			target: MakeTarget("task.delete"),
			want:   false,
		},
		{
			name:   "alternation with star is wildcard (a|b|* == *)",
			scope:  Scope{Op: [][]string{{"task"}, {"a", "b", "*"}}},
			target: MakeTarget("task.zzz"),
			want:   true,
		},
		{
			name:   "empty preds matches any instance",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}},
			target: MakeTarget("task.read", StringAttr("id", "99"), StringAttr("parent", "42")),
			want:   true,
		},
		{
			name:   "absent key in scope is unconstrained",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string]string{"id": "42"}},
			target: MakeTarget("task.read", StringAttr("id", "42"), StringAttr("parent", "7")),
			want:   true,
		},
		{
			name:   "star value is matched literally not as a wildcard",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string]string{"id": "*"}},
			target: MakeTarget("task.read", StringAttr("id", "99")),
			want:   false,
		},
		{
			name:   "star value matches a literal star attribute",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string]string{"id": "*"}},
			target: MakeTarget("task.read", StringAttr("id", "*")),
			want:   true,
		},
		{
			name:   "missing target attribute denies",
			scope:  Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string]string{"id": "42"}},
			target: MakeTarget("task.read"),
			want:   false,
		},
		{
			name:   "value match",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"workspace": "X"}},
			target: MakeTarget("task.create", StringAttr("workspace", "X")),
			want:   true,
		},
		{
			name:   "value mismatch denied",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"workspace": "X"}},
			target: MakeTarget("task.create", StringAttr("workspace", "Z")),
			want:   false,
		},
		{
			name:   "AND across keys all match",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"parent": "42", "workspace": "ws"}},
			target: MakeTarget("task.create", StringAttr("parent", "42"), StringAttr("workspace", "ws")),
			want:   true,
		},
		{
			name:   "AND across keys one fails",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"parent": "42", "workspace": "ws"}},
			target: MakeTarget("task.create", StringAttr("parent", "42"), StringAttr("workspace", "other")),
			want:   false,
		},
		{
			name:   "AND across keys one key missing from target",
			scope:  Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"parent": "42", "workspace": "ws"}},
			target: MakeTarget("task.create", StringAttr("parent", "42")),
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
		mustParse(t, `task.read:{"id":"42"}`),
		mustParse(t, `task.read:{"parent":"42"}`),
	}
	tests := []struct {
		name  string
		attrs []Attr
		want  bool
	}{
		{"own task", []Attr{StringAttr("id", "42"), StringAttr("parent", "7")}, true},
		{"direct child", []Attr{StringAttr("id", "99"), StringAttr("parent", "42")}, true},
		{"unrelated task", []Attr{StringAttr("id", "5"), StringAttr("parent", "7")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := set.Authorize(MakeTarget("task.read", tt.attrs...))
			assert.Equal(t, got, tt.want)
		})
	}
}

// TestAuthorize_CreateConjunction reproduces the §6a create scenario: a single
// fully-constrained scope ANDs all three attributes.
func TestAuthorize_CreateConjunction(t *testing.T) {
	t.Parallel()
	set := Set{mustParse(t, `task.create:{"parent":"42","workspace":"ws","runner":"rn"}`)}

	ok := set.Authorize(MakeTarget("task.create", StringAttr("parent", "42"), StringAttr("workspace", "ws"), StringAttr("runner", "rn")))
	assert.Equal(t, ok, true)

	// Wrong workspace is denied even though parent and runner match.
	denied := set.Authorize(MakeTarget("task.create", StringAttr("parent", "42"), StringAttr("workspace", "evil"), StringAttr("runner", "rn")))
	assert.Equal(t, denied, false)
}

// TestAuthorize_SplitConjunctionIsHole documents the §6a failure mode: splitting
// the create conjunction across separate scopes and ORing them leaves the
// unconstrained attributes as holes. This is a regression guard against ever
// minting create scopes that way.
func TestAuthorize_SplitConjunctionIsHole(t *testing.T) {
	t.Parallel()
	set := Set{
		mustParse(t, `task.create:{"parent":"42"}`),
		mustParse(t, `task.create:{"workspace":"ws"}`),
	}
	// parent matches the first scope, which leaves workspace/runner unconstrained.
	escalated := set.Authorize(MakeTarget("task.create", StringAttr("parent", "42"), StringAttr("workspace", "evil"), StringAttr("runner", "evil")))
	assert.Equal(t, escalated, true)
}

// TestAuthorize_WildcardAdmin reproduces the §6c wildcard scenarios.
func TestAuthorize_WildcardAdmin(t *testing.T) {
	t.Parallel()

	// task.* covers any action on a task instance, including a child.
	taskAdmin := Set{mustParse(t, `task.*`)}
	assert.Equal(t, taskAdmin.Authorize(MakeTarget("task.write", StringAttr("id", "99"), StringAttr("parent", "42"))), true)
	// ...but not a different resource.
	assert.Equal(t, taskAdmin.Authorize(MakeTarget("github_token.create")), false)

	// *.* covers any 2-segment operation with any instance.
	admin := Set{mustParse(t, `*.*`)}
	assert.Equal(t, admin.Authorize(MakeTarget("github_token.create")), true)
	assert.Equal(t, admin.Authorize(MakeTarget("task.read", StringAttr("id", "1"))), true)
	// ...but not a 3-segment operation: * matches exactly one segment.
	assert.Equal(t, admin.Authorize(MakeTarget("task.read.extra")), false)
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
			want: Scope{Op: [][]string{{"github_token"}, {"create"}}},
		},
		{
			name: "empty predicate object",
			in:   "task.read:{}",
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string]string{}},
		},
		{
			name: "string value",
			in:   `task.create:{"workspace":"X"}`,
			want: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"workspace": "X"}},
		},
		{
			name: "numeric id as string",
			in:   `task.read:{"id":"42"}`,
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string]string{"id": "42"}},
		},
		{
			name: "star value parsed as a literal string",
			in:   `task.write:{"id":"*"}`,
			want: Scope{Op: [][]string{{"task"}, {"write"}}, Preds: map[string]string{"id": "*"}},
		},
		{
			name: "multiple predicate keys",
			in:   `task.create:{"parent":"42","workspace":"ws","runner":"rn"}`,
			want: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"parent": "42", "workspace": "ws", "runner": "rn"}},
		},
		{
			name: "path alternation parsed at parse time",
			in:   `task.read|write:{"id":"1"}`,
			want: Scope{Op: [][]string{{"task"}, {"read", "write"}}, Preds: map[string]string{"id": "1"}},
		},
		{
			name: "wildcard segment",
			in:   "task.*",
			want: Scope{Op: [][]string{{"task"}, {"*"}}},
		},
		{
			name: "first colon split with colons inside json",
			in:   `task.read:{"url":"http://example.com:8080/p"}`,
			want: Scope{Op: [][]string{{"task"}, {"read"}}, Preds: map[string]string{"url": "http://example.com:8080/p"}},
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
		{"empty path with predicates", `:{"id":"1"}`},
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
		{"number value", `task.read:{"id":42}`},
		{"bool value", `task.read:{"flag":true}`},
		{"array value", `task.create:{"workspace":["X","Y"]}`},
		{"nested object value", `task.read:{"a":{"b":"c"}}`},
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
			name:  "predicates emitted with sorted keys",
			scope: Scope{Op: [][]string{{"task"}, {"create"}}, Preds: map[string]string{"workspace": "ws", "parent": "42"}},
			want:  `task.create:{"parent":"42","workspace":"ws"}`,
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
		`task.read|write:{"id":"1"}`,
		`task.write:{"id":"*"}`,
		`task.create:{"parent":"42","workspace":"ws","runner":"rn"}`,
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
		`task.create:{"parent":"42","workspace":"ws","runner":"rn"}`,
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
		`task.read:{"id":42}`,
		`task.read:{"workspace":["X","Y"]}`,
		"task.read:null",
	}
	for _, s := range invalid {
		assert.Assert(t, !ValidScope(s), "expected %q to be invalid", s)
	}
}

func TestAdmin(t *testing.T) {
	t.Parallel()
	// Admin authorizes any 2-segment operation, on any instance.
	set := Admin()
	assert.Assert(t, set.Authorize(MakeTarget("task.read", StringAttr("id", "1"))))
	assert.Assert(t, set.Authorize(MakeTarget("github_token.create")))
	// But not operations of a different arity.
	assert.Assert(t, !set.Authorize(MakeTarget("task")))
	assert.Assert(t, !set.Authorize(MakeTarget("task.read.x")))
}

func TestParseSet(t *testing.T) {
	t.Parallel()
	// Empty input yields an empty set.
	set, err := ParseSet(nil)
	assert.NilError(t, err)
	assert.Equal(t, len(set), 0)

	// Each string parses into the set.
	set, err = ParseSet([]string{"task.read", "github_token.create"})
	assert.NilError(t, err)
	assert.Equal(t, len(set), 2)
	assert.Assert(t, set.Authorize(MakeTarget("task.read")))

	// A malformed scope fails the whole parse.
	_, err = ParseSet([]string{"task.read", "task."})
	assert.ErrorContains(t, err, "empty segment")
}
