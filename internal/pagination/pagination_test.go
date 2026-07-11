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

// intCursor is the keyset for the in-memory test source: the row's own value.
type intCursor struct {
	V int `json:"v"`
}

// sliceSource is an in-memory pagination.Source over a descending-sorted slice
// of unique ints. A generic interface with two type parameters isn't a good
// fit for moq, so this hand-written double stands in for a store.
type sliceSource struct {
	rows   []int   // sorted descending, unique
	limits []int32 // limits passed to each Query call, in order
}

func (s *sliceSource) Query(_ context.Context, cursor *intCursor, limit int32) ([]int, error) {
	s.limits = append(s.limits, limit)
	start := 0
	if cursor != nil {
		for start < len(s.rows) && s.rows[start] >= cursor.V {
			start++
		}
	}
	end := min(start+int(limit), len(s.rows))
	return s.rows[start:end], nil
}

func (s *sliceSource) Cursor(row int) intCursor {
	return intCursor{V: row}
}

// TestList_RoundTrip walks every page end-to-end, feeding each NextToken back
// in, and asserts the concatenated pages reproduce every row exactly once in
// order. This exercises token encode on one call and decode on the next.
func TestList_RoundTrip(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 50, Max: 100}
	src := &sliceSource{rows: []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}}

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
}

// TestList_FullPageHasNextToken checks that a page whose over-fetch row came
// back is trimmed to size and carries a token anchored to the last kept row.
func TestList_FullPageHasNextToken(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 50, Max: 100}
	src := &sliceSource{rows: []int{10, 9, 8, 7, 6}}

	// Act
	page, err := pagination.List(context.Background(), cfg, 3, "", src)

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, page.Items, []int{10, 9, 8})
	assert.Assert(t, page.NextToken != "")
	// The over-fetch requested size+1 rows.
	assert.DeepEqual(t, src.limits, []int32{4})
}

// TestList_PartialPageNoNextToken checks that fewer rows than the page size
// yields no next token.
func TestList_PartialPageNoNextToken(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 50, Max: 100}
	src := &sliceSource{rows: []int{10, 9}}

	// Act
	page, err := pagination.List(context.Background(), cfg, 3, "", src)

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, page.Items, []int{10, 9})
	assert.Equal(t, page.NextToken, "")
}

// TestList_ExactlyFullNoNextToken checks the boundary where exactly page-size
// rows exist: the over-fetch finds no extra row, so there is no next page.
func TestList_ExactlyFullNoNextToken(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 50, Max: 100}
	src := &sliceSource{rows: []int{10, 9, 8}}

	// Act
	page, err := pagination.List(context.Background(), cfg, 3, "", src)

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, page.Items, []int{10, 9, 8})
	assert.Equal(t, page.NextToken, "")
}

// TestList_EmptyPage checks that no rows yields an empty page and no token.
func TestList_EmptyPage(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 50, Max: 100}
	src := &sliceSource{rows: nil}

	// Act
	page, err := pagination.List(context.Background(), cfg, 3, "", src)

	// Assert
	assert.NilError(t, err)
	assert.Assert(t, cmp.Len(page.Items, 0))
	assert.Equal(t, page.NextToken, "")
}

// TestList_DefaultPageSize checks that a zero page size falls back to the
// configured default (and that the over-fetch is default+1).
func TestList_DefaultPageSize(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 2, Max: 100}
	src := &sliceSource{rows: []int{10, 9, 8, 7}}

	// Act
	page, err := pagination.List(context.Background(), cfg, 0, "", src)

	// Assert
	assert.NilError(t, err)
	assert.DeepEqual(t, page.Items, []int{10, 9})
	assert.Assert(t, page.NextToken != "")
	assert.DeepEqual(t, src.limits, []int32{3})
}

// TestList_PageSizeBounds checks the page-size validation contract: values
// below 1 or above Max are rejected as ErrInvalidRequest; valid sizes pass.
func TestList_PageSizeBounds(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 50, Max: 100}
	tests := []struct {
		name     string
		pageSize int32
		wantErr  bool
	}{
		{"negative", -1, true},
		{"above max", 101, true},
		{"at max", 100, false},
		{"minimum", 1, false},
		{"zero uses default", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := &sliceSource{rows: []int{3, 2, 1}}
			_, err := pagination.List(context.Background(), cfg, tt.pageSize, "", src)
			if tt.wantErr {
				assert.Assert(t, errors.Is(err, pagination.ErrInvalidRequest))
			} else {
				assert.NilError(t, err)
			}
		})
	}
}

// TestList_InvalidToken checks that an undecodable page token surfaces as a
// wrapped ErrInvalidRequest, whether it fails base64 or JSON decoding.
func TestList_InvalidToken(t *testing.T) {
	t.Parallel()
	cfg := pagination.Config{Default: 50, Max: 100}
	tests := []struct {
		name  string
		token string
	}{
		{"not base64", "!!!not base64!!!"},
		{"base64 but not json", base64.URLEncoding.EncodeToString([]byte("not json"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := &sliceSource{rows: []int{3, 2, 1}}
			_, err := pagination.List(context.Background(), cfg, 10, tt.token, src)
			assert.Assert(t, errors.Is(err, pagination.ErrInvalidRequest))
		})
	}
}

// TestList_QueryError checks that an error from the source is returned as-is,
// not wrapped in ErrInvalidRequest.
func TestList_QueryError(t *testing.T) {
	t.Parallel()
	// Arrange
	cfg := pagination.Config{Default: 50, Max: 100}
	boom := errors.New("boom")
	src := errSource{err: boom}

	// Act
	_, err := pagination.List(context.Background(), cfg, 10, "", src)

	// Assert
	assert.Assert(t, errors.Is(err, boom))
	assert.Assert(t, !errors.Is(err, pagination.ErrInvalidRequest))
}

// errSource is a pagination.Source whose Query always fails.
type errSource struct{ err error }

func (s errSource) Query(context.Context, *intCursor, int32) ([]int, error) {
	return nil, s.err
}

func (s errSource) Cursor(row int) intCursor { return intCursor{V: row} }
