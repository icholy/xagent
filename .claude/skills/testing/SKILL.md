---
name: testing
description: Guidelines for writing Go tests. Apply when creating or modifying test files.
---

# Testing Guidelines

## Test Structure

Use the **Arrange, Act, Assert** pattern. Separate each phase with a blank line or comment.

```go
func TestGetTask_Permissions(t *testing.T) {
    t.Parallel()
    // Arrange
    srv := setupTestServer(t)
    ctxA, _ := createTestOrg(t, srv, testOrgOptions{Workspaces: true})
    ctxB, _ := createTestOrg(t, srv, testOrgOptions{Workspaces: true})
    resp, err := srv.CreateTask(ctxA, &xagentv1.CreateTaskRequest{
        Name: "User A's Task", Runner: "test-runner", Workspace: "test-workspace",
    })
    assert.NilError(t, err)

    // Act - User B tries to get User A's task
    _, err = srv.GetTask(ctxB, &xagentv1.GetTaskRequest{Id: resp.Task.Id})

    // Assert
    assert.ErrorContains(t, err, "not found")
}
```

## Test Naming

- Use `TestFunctionName` for the happy path.
- Use `TestFunctionName_Scenario` for specific cases (e.g. `TestCreateTask_BadRunner`).
- Keep names concise and descriptive.

## Subtests

Only use `t.Run` for table-driven tests where you're iterating over a slice of test cases. Do NOT use `t.Run` to group individual cases -- write separate top-level test functions instead.

```go
// Good: table-driven
func TestValidate(t *testing.T) {
    tests := []struct {
        name  string
        input string
        err   string
    }{
        {"empty", "", "required"},
        {"too long", strings.Repeat("x", 256), "too long"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := validate(tt.input)
            assert.ErrorContains(t, err, tt.err)
        })
    }
}

// Bad: don't wrap individual cases in t.Run
func TestCreateTask(t *testing.T) {
    t.Run("bad runner", func(t *testing.T) { ... })    // NO
    t.Run("bad workspace", func(t *testing.T) { ... }) // NO
}
```

## Server Test Helpers

- `setupTestServer(t)` creates a server with a clean database connection.
- `createTestOrg(t, srv, testOrgOptions{})` returns `(context.Context, *testOrg)`:
  - `testOrgOptions{Workspaces: true}` registers default workspaces (`test-runner`, `runner-1`, `runner-2` with `test-workspace`, `workspace-1`, `workspace-2`, `default`).
  - `testOrgOptions{}` creates only the user and org, no workspaces.
- `testOrg` has `UserID` and `OrgID` fields.

## Interface Test Doubles

When a test needs a stand-in for an interface, **generate a mock with moq** -- do NOT hand-write a `fakeX` struct that implements the interface. This is a near-absolute default; a hand-written fake is only acceptable in genuinely exceptional cases.

Add a `//go:generate` directive next to the interface and run `go generate ./...`. Generated mocks live beside the interface as `*_moq.go` (importable from production code) or `*_moq_test.go` (test-only). For example, `internal/server/notifyserver/notifyserver.go` declares:

```go
//go:generate go tool moq -out org_resolver_moq_test.go . OrgResolver
```

which generates `org_resolver_moq_test.go` containing `OrgResolverMock`. A test sets the mock's `...Func` fields to control behavior and asserts against the generated `...Calls()` accessor:

```go
// Good: configure behavior via the generated Func field
mockedOrgResolver := &OrgResolverMock{
    ResolveOrgFunc: func(ctx context.Context, userID string, orgID int64) (int64, error) {
        return orgID, nil
    },
}

srv := New(Options{OrgResolver: mockedOrgResolver})
// ... exercise srv ...

// assert the method was called exactly once
assert.Equal(t, len(mockedOrgResolver.ResolveOrgCalls()), 1)

// Bad: hand-written fake implementing the interface
type fakeOrgResolver struct{}
func (fakeOrgResolver) ResolveOrg(ctx context.Context, userID string, orgID int64) (int64, error) {
    return orgID, nil
}
```

See `internal/xagentclient/client_moq.go` (`ClientMock`) for a checked-in `*_moq.go` example.

## Prefer Duplication Over Helpers

Do NOT create little factory/helper functions that build a struct for a couple of call sites. Inline and duplicate the values in each test case -- duplication in tests is preferred over this kind of indirection.

Only extract a helper for genuinely large shared setup (e.g. `setupTestServer` / `createTestOrg` above).

```go
// Bad: don't wrap a small struct literal in a helper
func taskNotification(id int64, msg string) model.Notification {
    return model.Notification{
        Type:           "change",
        Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: id}},
        ChannelMessage: msg,
    }
}

// Good: write the literal inline in each test case
want := model.Notification{
    Type:           "change",
    Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 1}},
    ChannelMessage: "first",
}
// ... and again in the next case ...
want := model.Notification{
    Type:           "change",
    Resources:      []model.NotificationResource{{Action: "updated", Type: "task", ID: 2}},
    ChannelMessage: "second",
}
```

## Assertions

Use `gotest.tools/v3/assert` -- not the standard library `testing` package for assertions.

```go
assert.NilError(t, err)
assert.Equal(t, got, want)
assert.ErrorContains(t, err, "not found")
assert.DeepEqual(t, got, want, protocmp.Transform())
```

## Running Tests

```bash
mise run build    # Create bins & webui (required)
mise run up:test  # Setup dependencies (required)
mise run test     # Run all tests
mise run test -- -run=TestFoo -v  # Run specific tests
```
