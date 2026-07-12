package pagination_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/icholy/xagent/internal/pagination"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/assert/cmp"
)

// keys 1..10, oldest (1) to newest (10). The forward walk is newest-first
// (descending); the backward walk is the ascending live-follow reverse.
func rows1to10() []int { return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10} }

// TestList_NewestPage covers the open motion: an empty token returns the newest
// page (forward walk from the tail) in one query, with a next-older token and an
// always-populated live-follow token.
func TestList_NewestPage(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 3, Max: 100}
	src := pagination.NewBiSource(rows1to10(), false)

	page, err := pagination.List(context.Background(), cfg, 3, "", src)
	assert.NilError(t, err)

	// Reverse is off, so items read newest-first, exactly like the task list.
	assert.DeepEqual(t, page.Items, []int{10, 9, 8})
	assert.Assert(t, page.ForwardToken != "", "older history remains")
	assert.Assert(t, page.BackwardToken != "", "live-follow token is always set")
	// One query, no probe of the opposite walk.
	assert.Assert(t, cmp.Len(src.QueryCalls(), 1))
	assert.Equal(t, src.QueryCalls()[0].Token.Backward, false)
	assert.Equal(t, src.QueryCalls()[0].Limit, 4) // over-fetch is size+1
}

// TestList_ForwardScrollToExhaustion walks the ForwardToken (older) direction to
// the oldest row and asserts the concatenated pages reproduce every row once,
// newest-first, and that ForwardToken empties at history's end.
func TestList_ForwardScrollToExhaustion(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 3, Max: 100}
	src := pagination.NewBiSource(rows1to10(), false)

	var got []int
	token := ""
	pages := 0
	for {
		page, err := pagination.List(context.Background(), cfg, 3, token, src)
		assert.NilError(t, err)
		got = append(got, page.Items...)
		// Every non-empty page keeps a live-follow token.
		assert.Assert(t, page.BackwardToken != "")
		pages++
		if page.ForwardToken == "" {
			break
		}
		token = page.ForwardToken
	}

	assert.DeepEqual(t, got, []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1})
	assert.Equal(t, pages, 4) // 3 full pages of 3, then a final page of 1
}

// TestList_BackwardFollow covers live-follow: resuming a BackwardToken walks
// toward newer rows, and the BackwardToken is always populated so polling can
// continue past the tail.
func TestList_BackwardFollow(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 3, Max: 100}
	src := pagination.NewBiSource(rows1to10(), false)

	// Grab the newest page, then follow forward-in-time from an older position:
	// take the oldest page's live-follow token to walk newer.
	oldest, err := pagination.List(context.Background(), cfg, 3, "", src)
	assert.NilError(t, err)
	// Walk to the oldest page to obtain a follow token with rows above it.
	page := oldest
	for page.ForwardToken != "" {
		page, err = pagination.List(context.Background(), cfg, 3, page.ForwardToken, src)
		assert.NilError(t, err)
	}
	// page is now [1] (oldest). Follow newer.
	page, err = pagination.List(context.Background(), cfg, 3, page.BackwardToken, src)
	assert.NilError(t, err)
	// Backward page, Reverse off → newest-first: rows above 1 nearest-first
	// ([2,3,4]) reversed for display.
	assert.DeepEqual(t, page.Items, []int{4, 3, 2})
	assert.Assert(t, page.BackwardToken != "", "follow token stays populated")
	assert.Assert(t, page.ForwardToken != "", "backward page exposes a way back")
}

// TestList_EmptyFollowPollEcho covers the tail: following newer when nothing has
// arrived returns an empty page whose BackwardToken echoes the submitted token,
// so the caller keeps its place.
func TestList_EmptyFollowPollEcho(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 3, Max: 100}
	src := pagination.NewBiSource(rows1to10(), false)

	// Newest page's BackwardToken points at the tail (id 10); nothing is newer.
	newest, err := pagination.List(context.Background(), cfg, 3, "", src)
	assert.NilError(t, err)
	follow, err := pagination.List(context.Background(), cfg, 3, newest.BackwardToken, src)
	assert.NilError(t, err)

	assert.Assert(t, cmp.Len(follow.Items, 0))
	assert.Equal(t, follow.BackwardToken, newest.BackwardToken, "empty poll echoes the token")
	assert.Equal(t, follow.ForwardToken, "", "no older boundary on an empty poll")
}

// TestList_OneQueryPerPage asserts a bidirectional page derives both tokens from
// the page it fetched — a single query, no probe of the opposite walk.
func TestList_OneQueryPerPage(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 3, Max: 100}
	src := pagination.NewBiSource(rows1to10(), false)

	page, err := pagination.List(context.Background(), cfg, 3, "", src)
	assert.NilError(t, err)
	assert.Assert(t, page.ForwardToken != "")
	assert.Assert(t, page.BackwardToken != "")
	assert.Assert(t, cmp.Len(src.QueryCalls(), 1))
}

// TestList_ForwardOnlyBackwardToken covers a forward-only source (the task list):
// resubmitting a backward token surfaces ErrUnsupportedDirection, wrapped as
// ErrInvalidRequest so the handler maps it to CodeInvalidArgument.
func TestList_ForwardOnlyBackwardToken(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 3, Max: 100}
	src := pagination.NewBiSource(rows1to10(), true) // forward-only

	// A forward-only source's newest page still yields a BackwardToken; the
	// caller normally never exposes it, but a client that resubmits it trips
	// the guard.
	newest, err := pagination.List(context.Background(), cfg, 3, "", src)
	assert.NilError(t, err)
	_, err = pagination.List(context.Background(), cfg, 3, newest.BackwardToken, src)
	assert.Assert(t, errors.Is(err, pagination.ErrUnsupportedDirection))
	assert.Assert(t, errors.Is(err, pagination.ErrInvalidRequest))
}

// TestList_Reverse covers the Config.Reverse display order: off keeps the
// forward walk's order (newest-first), on flips it (oldest-first) for a
// top-down timeline. Reverse never changes token derivation.
func TestList_Reverse(t *testing.T) {
	t.Parallel()

	off, err := pagination.List(context.Background(),
		pagination.Config{Default: 3, Max: 100, Reverse: false}, 3, "",
		pagination.NewBiSource(rows1to10(), false))
	assert.NilError(t, err)
	assert.DeepEqual(t, off.Items, []int{10, 9, 8})

	on, err := pagination.List(context.Background(),
		pagination.Config{Default: 3, Max: 100, Reverse: true}, 3, "",
		pagination.NewBiSource(rows1to10(), false))
	assert.NilError(t, err)
	assert.DeepEqual(t, on.Items, []int{8, 9, 10})

	// Same window, opposite order; both advertise the same continuations.
	assert.Assert(t, off.ForwardToken != "" && on.ForwardToken != "")
	assert.Assert(t, off.BackwardToken != "" && on.BackwardToken != "")
}

// TestList_ReverseBackwardPage covers Reverse's effect on a backward page: with
// Reverse on, a backward (ascending, nearest-first) page is returned as-is so it
// still reads oldest-first, consistent with a forward page.
func TestList_ReverseBackwardPage(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 3, Max: 100, Reverse: true}
	src := pagination.NewBiSource(rows1to10(), false)

	// Newest page (Reverse on → oldest-first) then follow newer (there is
	// nothing newer than 10, so seed a follow from an older cursor instead).
	first, err := pagination.List(context.Background(), cfg, 3, "", src)
	assert.NilError(t, err)
	assert.DeepEqual(t, first.Items, []int{8, 9, 10})

	// Walk to the oldest page, then follow newer.
	page := first
	for page.ForwardToken != "" {
		page, err = pagination.List(context.Background(), cfg, 3, page.ForwardToken, src)
		assert.NilError(t, err)
	}
	page, err = pagination.List(context.Background(), cfg, 3, page.BackwardToken, src)
	assert.NilError(t, err)
	// Backward page with Reverse on stays as-is: oldest-first.
	assert.DeepEqual(t, page.Items, []int{2, 3, 4})
}

// TestList_SinglePage covers page shaping and next/prev advertisement on a
// newest (forward) page: empty, partial, exactly-full, over-full, and a zero
// page size falling back to the default.
func TestList_SinglePage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		rows             []int
		pageSize         int32
		cfg              pagination.Config
		wantItems        []int
		wantForwardToken bool
	}{
		{"empty", nil, 3, pagination.Config{Default: 50, Max: 100}, nil, false},
		{"partial", []int{9, 10}, 3, pagination.Config{Default: 50, Max: 100}, []int{10, 9}, false},
		{"exactly full", []int{8, 9, 10}, 3, pagination.Config{Default: 50, Max: 100}, []int{10, 9, 8}, false},
		{"over full", []int{7, 8, 9, 10}, 3, pagination.Config{Default: 50, Max: 100}, []int{10, 9, 8}, true},
		{"zero uses default", []int{8, 9, 10}, 0, pagination.Config{Default: 2, Max: 100}, []int{10, 9}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := pagination.NewBiSource(tt.rows, false)
			page, err := pagination.List(context.Background(), tt.cfg, tt.pageSize, "", src)
			assert.NilError(t, err)
			assert.DeepEqual(t, page.Items, tt.wantItems)
			assert.Equal(t, page.ForwardToken != "", tt.wantForwardToken)
			// BackwardToken is set on any non-empty page; empty otherwise.
			assert.Equal(t, page.BackwardToken != "", len(tt.wantItems) > 0)
		})
	}
}

// TestList_Errors covers the ErrInvalidRequest contract: out-of-range page sizes
// and undecodable tokens (bad base64, or base64 that isn't JSON).
func TestList_Errors(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 50, Max: 100}
	tests := []struct {
		name     string
		pageSize int32
		token    string
	}{
		{"negative size", -1, ""},
		{"above max size", 101, ""},
		{"token not base64", 10, "!!!not base64!!!"},
		{"token not json", 10, base64.URLEncoding.EncodeToString([]byte("not json"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := pagination.NewBiSource(rows1to10(), false)
			_, err := pagination.List(context.Background(), cfg, tt.pageSize, tt.token, src)
			assert.Assert(t, errors.Is(err, pagination.ErrInvalidRequest))
		})
	}
}
