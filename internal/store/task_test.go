package store_test

import (
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pagination"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

func TestClearShellSession(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	task := &model.Task{
		Name:         "shell-task",
		Runner:       "r",
		Workspace:    "w",
		Status:       model.TaskStatusCompleted,
		OrgID:        org.OrgID,
		ShellSession: "sess-1",
	}
	assert.NilError(t, s.CreateTask(t.Context(), nil, task))

	// Clearing with the matching session + org empties the field.
	assert.NilError(t, s.ClearShellSession(t.Context(), nil, "sess-1", org.OrgID))
	got, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, got.ShellSession, "")
}

func TestClearShellSession_WrongOrgNoOp(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	other := teststore.CreateOrg(t, s, nil)

	task := &model.Task{
		Name:         "shell-task",
		Runner:       "r",
		Workspace:    "w",
		Status:       model.TaskStatusCompleted,
		OrgID:        org.OrgID,
		ShellSession: "sess-2",
	}
	assert.NilError(t, s.CreateTask(t.Context(), nil, task))

	// Clearing under a different org must not touch the task.
	assert.NilError(t, s.ClearShellSession(t.Context(), nil, "sess-2", other.OrgID))
	got, err := s.GetTask(t.Context(), nil, task.ID, org.OrgID)
	assert.NilError(t, err)
	assert.Equal(t, got.ShellSession, "sess-2")
}

// collectTaskPages walks every page of ListTasksPage with the given page size,
// returning the task IDs in the order they were paged. It fails the test if the
// walk does not terminate within a generous bound (a token that never empties
// would otherwise loop forever).
func collectTaskPages(t *testing.T, s *store.Store, orgID int64, pageSize int32) []int64 {
	t.Helper()
	var ids []int64
	token := ""
	for range 1000 {
		page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
			OrgID:     orgID,
			PageSize:  pageSize,
			PageToken: token,
		})
		assert.NilError(t, err)
		for _, task := range page.Items {
			ids = append(ids, task.ID)
		}
		if page.NextToken == "" {
			return ids
		}
		token = page.NextToken
	}
	t.Fatal("ListTasksPage did not terminate")
	return nil
}

func TestListTasksPage_Keyset(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	// Create tasks in order; created_at increases with each insert, so the
	// newest-first page order is the reverse of creation order.
	var created []int64
	for range 5 {
		task := teststore.CreateTask(t, s, org, nil)
		created = append(created, task.ID)
	}
	var want []int64
	for i := len(created) - 1; i >= 0; i-- {
		want = append(want, created[i])
	}

	// Paging in size-2 chunks must reconstruct the full ordering exactly, with
	// no gaps or duplicates across the page boundaries and its cursor tokens.
	got := collectTaskPages(t, s, org.OrgID, 2)
	assert.DeepEqual(t, got, want)

	// A page large enough to hold everything returns no next token.
	page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 100})
	assert.NilError(t, err)
	assert.Equal(t, page.NextToken, "")
	assert.Equal(t, len(page.Items), len(created))
}

func TestListTasksPage_DefaultPageSize(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	for range 3 {
		teststore.CreateTask(t, s, org, nil)
	}

	// PageSize 0 falls back to the default (50), well above the 3 tasks here, so
	// everything comes back on a single page.
	page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{OrgID: org.OrgID})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 3)
	assert.Equal(t, page.NextToken, "")
}

func TestListTasksPage_OrgScoped(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	other := teststore.CreateOrg(t, s, nil)
	mine := teststore.CreateTask(t, s, org, nil)
	teststore.CreateTask(t, s, other, nil)

	page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 10})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)
	assert.Equal(t, page.Items[0].ID, mine.ID)
}

func TestListTasksPage_ExcludesArchived(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	live := teststore.CreateTask(t, s, org, nil)
	archived := teststore.CreateTask(t, s, org, nil)
	archived.Archived = true
	assert.NilError(t, s.UpdateTask(t.Context(), nil, archived))

	page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 10})
	assert.NilError(t, err)
	assert.Equal(t, len(page.Items), 1)
	assert.Equal(t, page.Items[0].ID, live.ID)
}

func TestListTasksPage_InvalidRequest(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	// A page size beyond the max is a caller mistake, not an internal failure.
	_, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{OrgID: org.OrgID, PageSize: 1000})
	assert.Assert(t, cmp.ErrorIs(err, pagination.ErrInvalidRequest))

	// An undecodable token is likewise a caller mistake.
	_, err = s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{OrgID: org.OrgID, PageToken: "not-a-valid-token!!"})
	assert.Assert(t, cmp.ErrorIs(err, pagination.ErrInvalidRequest))
}
