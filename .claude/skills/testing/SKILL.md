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
