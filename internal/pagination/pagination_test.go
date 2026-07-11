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

// TestList walks every page end-to-end, feeding each NextToken back in, and
// asserts the concatenated pages reproduce every row once in order. This
// covers token encode on one call and decode on the next, the over-fetch
// limit (size+1), full-page trimming, and the tokenless final partial page.
func TestList(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 50, Max: 100}
	src := pagination.NewMockSource[int, int]([]int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1})

	// Act
	var got []int
	token := ""
	pages := 0
	for {
		page, err := pagination.List(context.Background(), cfg, 3, token, src)
		assert.NilError(t, err)
		got = append(got, page.Items...)
		pages++
		if page.NextToken == "" {
			break
		}
		token = page.NextToken
	}

	// Assert
	assert.DeepEqual(t, got, []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1})
	assert.Equal(t, pages, 4) // 3 full pages of 3, then a final page of 1
	calls := src.QueryCalls()
	assert.Assert(t, cmp.Len(calls, 4))
	assert.Equal(t, calls[0].Limit, int32(4)) // over-fetch is size+1
}

// TestList_SinglePage covers how one page is shaped and whether it advertises
// a next page: empty, partial, exactly-full (no extra row), over-full (extra
// row trimmed, token set), and a zero page size falling back to the default.
func TestList_SinglePage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		rows      []int
		pageSize  int32
		cfg       pagination.Config
		wantItems []int
		wantToken bool
	}{
		{"empty", nil, 3, pagination.Config{Default: 50, Max: 100}, nil, false},
		{"partial", []int{10, 9}, 3, pagination.Config{Default: 50, Max: 100}, []int{10, 9}, false},
		{"exactly full", []int{10, 9, 8}, 3, pagination.Config{Default: 50, Max: 100}, []int{10, 9, 8}, false},
		{"over full", []int{10, 9, 8, 7}, 3, pagination.Config{Default: 50, Max: 100}, []int{10, 9, 8}, true},
		{"zero uses default", []int{10, 9, 8}, 0, pagination.Config{Default: 2, Max: 100}, []int{10, 9}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := pagination.NewMockSource[int, int](tt.rows)
			page, err := pagination.List(context.Background(), tt.cfg, tt.pageSize, "", src)
			assert.NilError(t, err)
			assert.DeepEqual(t, page.Items, tt.wantItems)
			assert.Equal(t, page.NextToken != "", tt.wantToken)
		})
	}
}

// TestList_Errors covers the ErrInvalidRequest contract: out-of-range page
// sizes and undecodable tokens (bad base64, or base64 that isn't JSON).
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
			src := pagination.NewMockSource[int, int]([]int{3, 2, 1})
			_, err := pagination.List(context.Background(), cfg, tt.pageSize, tt.token, src)
			assert.Assert(t, errors.Is(err, pagination.ErrInvalidRequest))
		})
	}
}
