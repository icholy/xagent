package store_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pagination"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/store/teststore"
	"github.com/icholy/xagent/internal/x/testx"
	"gotest.tools/v3/assert"
)

func TestListEventsByTaskPage(t *testing.T) {
	t.Parallel()
	// Arrange - seed 10 report events, recording their ids in insertion order.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	var want []int64
	for i := range 10 {
		event := &model.Event{
			TaskID:  task.ID,
			OrgID:   org.OrgID,
			Payload: &model.ReportPayload{Content: fmt.Sprintf("report %d", i+1)},
		}
		assert.NilError(t, s.CreateEvent(t.Context(), nil, event))
		want = append(want, event.ID)
	}

	// Act - open at the tail (empty token), then follow NextToken toward older
	// history, prepending each older page so the whole stream reassembles
	// oldest-first.
	var got []int64
	token := ""
	pages := 0
	for {
		page, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
			TaskID:    task.ID,
			OrgID:     org.OrgID,
			PageSize:  3,
			PageToken: token,
		})
		assert.NilError(t, err)
		// Every page is ascending; older pages are prepended.
		got = append(testx.ExtractField(page.Items, "ID").([]int64), got...)
		pages++
		if page.NextToken == "" {
			break
		}
		token = page.NextToken
	}

	// Assert - the forward walk covers the whole stream ascending, no gaps/dups.
	assert.Equal(t, pages, 4) // 3+3+3+1
	assert.DeepEqual(t, got, want)
}

func TestListEventsByTaskPage_LiveFollow(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	for i := range 3 {
		assert.NilError(t, s.CreateEvent(t.Context(), nil, &model.Event{
			TaskID:  task.ID,
			OrgID:   org.OrgID,
			Payload: &model.ReportPayload{Content: fmt.Sprintf("report %d", i+1)},
		}))
	}

	// The newest page carries a PrevToken (the live-follow cursor at the tail).
	page, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:   task.ID,
		OrgID:    org.OrgID,
		PageSize: 50,
	})
	assert.NilError(t, err)
	assert.Assert(t, page.PrevToken != "")
	follow := page.PrevToken

	// Following the tail cursor now yields nothing new (echoes the cursor).
	empty, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:    task.ID,
		OrgID:     org.OrgID,
		PageSize:  50,
		PageToken: follow,
	})
	assert.NilError(t, err)
	assert.Equal(t, len(empty.Items), 0)
	assert.Assert(t, empty.PrevToken != "") // still resumable

	// Act - a subsequently-inserted event is picked up by the same tail token.
	newEvent := &model.Event{
		TaskID:  task.ID,
		OrgID:   org.OrgID,
		Payload: &model.ReportPayload{Content: "report 4"},
	}
	assert.NilError(t, s.CreateEvent(t.Context(), nil, newEvent))
	page, err = s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:    task.ID,
		OrgID:     org.OrgID,
		PageSize:  50,
		PageToken: follow,
	})
	assert.NilError(t, err)

	// Assert
	assert.DeepEqual(t, testx.ExtractField(page.Items, "ID"), []int64{newEvent.ID})
}

func TestListEventsByTaskPage_TypesFilter(t *testing.T) {
	t.Parallel()
	// Arrange - a mix of report and external events on the same task.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	assert.NilError(t, s.CreateEvent(t.Context(), nil, &model.Event{
		TaskID:  task.ID,
		OrgID:   org.OrgID,
		Payload: &model.ReportPayload{Content: "report"},
	}))
	external := &model.Event{
		TaskID:  task.ID,
		OrgID:   org.OrgID,
		Payload: &model.ExternalPayload{Description: "external"},
	}
	assert.NilError(t, s.CreateEvent(t.Context(), nil, external))

	// Act
	page, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:   task.ID,
		OrgID:    org.OrgID,
		Types:    []string{model.EventTypeExternal},
		PageSize: 50,
	})

	// Assert - only the external event comes back.
	assert.NilError(t, err)
	assert.DeepEqual(t, testx.ExtractField(page.Items, "ID"), []int64{external.ID})
}

func TestListEventsByTaskPage_BadPageSize(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)

	// Act - a page size past the max is rejected.
	_, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:   task.ID,
		OrgID:    org.OrgID,
		PageSize: 201,
	})

	// Assert
	assert.Assert(t, errors.Is(err, pagination.ErrInvalidRequest))
}

func TestListEventsByTaskPage_BadToken(t *testing.T) {
	t.Parallel()
	// Arrange
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)

	// Act - an undecodable token is rejected.
	_, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:    task.ID,
		OrgID:     org.OrgID,
		PageSize:  50,
		PageToken: "!!!not-base64!!!",
	})

	// Assert
	assert.Assert(t, errors.Is(err, pagination.ErrInvalidRequest))
}
