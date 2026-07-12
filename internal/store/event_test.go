package store_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/pagination"
	"github.com/icholy/xagent/internal/store"
	"github.com/icholy/xagent/internal/store/teststore"
	"github.com/icholy/xagent/internal/x/testx"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
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

func TestListEventsByTaskPage_TokenBindsFilter(t *testing.T) {
	t.Parallel()
	// Arrange - two external + two report events so both a filtered and an
	// all-arms first page mint a next token.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	for i := range 2 {
		assert.NilError(t, s.CreateEvent(t.Context(), nil, &model.Event{
			TaskID:  task.ID,
			OrgID:   org.OrgID,
			Payload: &model.ExternalPayload{Description: fmt.Sprintf("external %d", i+1)},
		}))
		assert.NilError(t, s.CreateEvent(t.Context(), nil, &model.Event{
			TaskID:  task.ID,
			OrgID:   org.OrgID,
			Payload: &model.ReportPayload{Content: fmt.Sprintf("report %d", i+1)},
		}))
	}

	// A token minted under types=[external] ...
	externalPage, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:   task.ID,
		OrgID:    org.OrgID,
		Types:    []string{model.EventTypeExternal},
		PageSize: 1,
	})
	assert.NilError(t, err)
	assert.Assert(t, externalPage.NextToken != "")

	// ... replayed under the all-arms filter is rejected as a cross-filter replay.
	_, err = s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:    task.ID,
		OrgID:     org.OrgID,
		PageSize:  1,
		PageToken: externalPage.NextToken,
	})
	assert.Assert(t, cmp.ErrorIs(err, pagination.ErrInvalidRequest))

	// And the reverse: an all-arms token replayed under types=[external].
	allPage, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:   task.ID,
		OrgID:    org.OrgID,
		PageSize: 1,
	})
	assert.NilError(t, err)
	assert.Assert(t, allPage.NextToken != "")

	_, err = s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:    task.ID,
		OrgID:     org.OrgID,
		Types:     []string{model.EventTypeExternal},
		PageSize:  1,
		PageToken: allPage.NextToken,
	})
	assert.Assert(t, cmp.ErrorIs(err, pagination.ErrInvalidRequest))
}

func TestListEventsByTaskPage_FilteredKeysetPaging(t *testing.T) {
	t.Parallel()
	// Arrange - interleave external and report events; only external ids are the
	// expected walk. Page size 1 forces the keyset cursor to advance under the
	// bound filter.
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	var want []int64
	for i := range 3 {
		external := &model.Event{
			TaskID:  task.ID,
			OrgID:   org.OrgID,
			Payload: &model.ExternalPayload{Description: fmt.Sprintf("external %d", i+1)},
		}
		assert.NilError(t, s.CreateEvent(t.Context(), nil, external))
		want = append(want, external.ID)
		assert.NilError(t, s.CreateEvent(t.Context(), nil, &model.Event{
			TaskID:  task.ID,
			OrgID:   org.OrgID,
			Payload: &model.ReportPayload{Content: fmt.Sprintf("report %d", i+1)},
		}))
	}

	// Act - walk the whole external stream under the same types filter throughout.
	var got []int64
	token := ""
	for {
		page, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
			TaskID:    task.ID,
			OrgID:     org.OrgID,
			Types:     []string{model.EventTypeExternal},
			PageSize:  1,
			PageToken: token,
		})
		assert.NilError(t, err)
		got = append(testx.ExtractField(page.Items, "ID").([]int64), got...)
		if page.NextToken == "" {
			break
		}
		token = page.NextToken
	}

	// Assert - the same-filter forward walk covers every external event, no gaps.
	assert.DeepEqual(t, got, want)
}

func TestListEventsByTaskPage_DefaultTokenWireCompatible(t *testing.T) {
	t.Parallel()
	s := teststore.New(t)
	org := teststore.CreateOrg(t, s, nil)
	task := teststore.CreateTask(t, s, org, nil)
	for i := range 2 {
		assert.NilError(t, s.CreateEvent(t.Context(), nil, &model.Event{
			TaskID:  task.ID,
			OrgID:   org.OrgID,
			Payload: &model.ReportPayload{Content: fmt.Sprintf("report %d", i+1)},
		}))
	}

	page, err := s.ListEventsByTaskPage(t.Context(), nil, store.ListEventsByTaskPageParams{
		TaskID:   task.ID,
		OrgID:    org.OrgID,
		PageSize: 1,
	})
	assert.NilError(t, err)
	assert.Assert(t, page.NextToken != "")

	// The all-arms (nil types) cursor omits the Types field entirely (json
	// omitempty), so tokens minted before the types filter was bound stay
	// byte-compatible.
	raw, err := base64.URLEncoding.DecodeString(page.NextToken)
	assert.NilError(t, err)
	assert.Assert(t, !strings.Contains(string(raw), `"t"`), "default token must not carry the types field: %s", raw)
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
