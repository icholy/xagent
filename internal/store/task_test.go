package store_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pagination"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/store/teststore"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// createArchivedTask inserts a task with the archived flag set. teststore's
// CreateTask helper doesn't expose archived, so the store is driven directly.
func createArchivedTask(t *testing.T, s *store.Store, orgID int64, archived bool) *model.Task {
	t.Helper()
	task := &model.Task{
		Runner:    "r",
		Workspace: "w",
		Status:    model.TaskStatusCompleted,
		OrgID:     orgID,
		Archived:  archived,
	}
	assert.NilError(t, s.CreateTask(t.Context(), nil, task))
	return task
}

// taskIDs extracts the ids of a page of tasks in order.
func taskIDs(tasks []*model.Task) []int64 {
	ids := make([]int64, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	return ids
}

func TestListTasksPage_ActiveOnly(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	// Interleave active and archived; keyset order is newest-created first.
	a := createArchivedTask(t, s, org.OrgID, false)
	_ = createArchivedTask(t, s, org.OrgID, true)
	c := createArchivedTask(t, s, org.OrgID, false)
	_ = createArchivedTask(t, s, org.OrgID, true)

	page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{OrgID: org.OrgID})
	assert.NilError(t, err)
	// Default (archived=false) yields only the active rows, newest first.
	assert.DeepEqual(t, taskIDs(page.Items), []int64{c.ID, a.ID})
	assert.Equal(t, page.NextToken, "")
}

func TestListTasksPage_IncludeArchived(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	a := createArchivedTask(t, s, org.OrgID, false)
	b := createArchivedTask(t, s, org.OrgID, true)
	c := createArchivedTask(t, s, org.OrgID, false)
	d := createArchivedTask(t, s, org.OrgID, true)

	page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
		OrgID:    org.OrgID,
		Archived: true,
	})
	assert.NilError(t, err)
	// Active and archived are interleaved by the same created_at DESC, id DESC
	// keyset ordering — newest-created first, regardless of archived state.
	assert.DeepEqual(t, taskIDs(page.Items), []int64{d.ID, c.ID, b.ID, a.ID})
}

func TestListTasksPage_ArchivedKeysetPaging(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	a := createArchivedTask(t, s, org.OrgID, false)
	b := createArchivedTask(t, s, org.OrgID, true)
	c := createArchivedTask(t, s, org.OrgID, false)

	// Page size 1 forces the keyset cursor to walk archived+active together.
	var got []int64
	token := ""
	for {
		page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
			OrgID:     org.OrgID,
			PageSize:  1,
			PageToken: token,
			Archived:  true,
		})
		assert.NilError(t, err)
		got = append(got, taskIDs(page.Items)...)
		if page.NextToken == "" {
			break
		}
		token = page.NextToken
	}
	assert.DeepEqual(t, got, []int64{c.ID, b.ID, a.ID})
}

func TestListTasksPage_TokenBindsFilter(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	// Two archived-included rows so the first page mints a next token.
	createArchivedTask(t, s, org.OrgID, true)
	createArchivedTask(t, s, org.OrgID, true)

	archivedPage, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
		OrgID:    org.OrgID,
		PageSize: 1,
		Archived: true,
	})
	assert.NilError(t, err)
	assert.Assert(t, archivedPage.NextToken != "")

	// Replaying an archived-minted token under the active-only filter is rejected.
	_, err = s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
		OrgID:     org.OrgID,
		PageSize:  1,
		PageToken: archivedPage.NextToken,
		Archived:  false,
	})
	assert.Assert(t, cmp.ErrorIs(err, pagination.ErrInvalidRequest))

	// And the reverse: an active-minted token replayed under archived=true.
	// Two active rows so the active-only first page also mints a next token.
	createArchivedTask(t, s, org.OrgID, false)
	createArchivedTask(t, s, org.OrgID, false)
	activePage, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
		OrgID:    org.OrgID,
		PageSize: 1,
	})
	assert.NilError(t, err)
	assert.Assert(t, activePage.NextToken != "")

	_, err = s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
		OrgID:     org.OrgID,
		PageSize:  1,
		PageToken: activePage.NextToken,
		Archived:  true,
	})
	assert.Assert(t, cmp.ErrorIs(err, pagination.ErrInvalidRequest))
}

func TestListTasksPage_DefaultTokenWireCompatible(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)

	createArchivedTask(t, s, org.OrgID, false)
	createArchivedTask(t, s, org.OrgID, false)

	page, err := s.ListTasksPage(t.Context(), nil, store.ListTasksPageParams{
		OrgID:    org.OrgID,
		PageSize: 1,
	})
	assert.NilError(t, err)
	assert.Assert(t, page.NextToken != "")

	// The default (archived=false) cursor omits the Archived field entirely
	// (json omitempty), so tokens minted before the archived filter existed stay
	// byte-compatible.
	raw, err := base64.URLEncoding.DecodeString(page.NextToken)
	assert.NilError(t, err)
	assert.Assert(t, !strings.Contains(string(raw), `"a"`), "default token must not carry the archived field: %s", raw)
}

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
