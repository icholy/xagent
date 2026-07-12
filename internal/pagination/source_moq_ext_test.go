package pagination

import (
	"context"
	"slices"
)

// NewMockSource returns a SourceMock backed by an in-memory slice of int keys,
// treating each row as its own keyset (T and C are both int). Rows are held in
// ascending key order internally; the argument order is irrelevant. It walks
// both directions per the Source contract:
//
//   - forward (backward=false): descending, nearest-first — keys below the
//     cursor, highest first; a nil cursor starts at the newest (highest) key.
//   - backward (backward=true): ascending, nearest-first — keys above the
//     cursor, lowest first.
//
// If forwardOnly is set, the backward walk returns ErrUnsupportedDirection,
// mirroring the task list.
func NewMockSource(rows []int, forwardOnly bool) *SourceMock[int, int] {
	sorted := slices.Clone(rows)
	slices.Sort(sorted)
	return &SourceMock[int, int]{
		QueryFunc: func(_ context.Context, token Token[int], limit int) ([]int, error) {
			cursor := token.Cursor
			if token.Backward {
				if forwardOnly {
					return nil, ErrUnsupportedDirection
				}
				// ascending, keys strictly above the cursor (a nil cursor would
				// start below the lowest key, but backward always has a cursor).
				var out []int
				for _, r := range sorted {
					if cursor != nil && r <= *cursor {
						continue
					}
					out = append(out, r)
					if len(out) == limit {
						break
					}
				}
				return out, nil
			}
			// descending, keys strictly below the cursor (nil cursor = newest).
			var out []int
			for i := len(sorted) - 1; i >= 0; i-- {
				r := sorted[i]
				if cursor != nil && r >= *cursor {
					continue
				}
				out = append(out, r)
				if len(out) == limit {
					break
				}
			}
			return out, nil
		},
		CursorFunc: func(row int) int {
			return row
		},
	}
}
