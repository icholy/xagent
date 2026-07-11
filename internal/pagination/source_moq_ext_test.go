package pagination

import "context"

// NewMockSource returns a SourceMock backed by an in-memory, pre-ordered slice
// of rows, suitable for driving List in tests. Query returns up to limit rows
// positioned after cursor (a nil cursor starts at the beginning), resuming by
// locating the row equal to the cursor. This treats each row as its own
// keyset, so T and C are instantiated as the same comparable type (e.g. int);
// Cursor returns the row as that keyset.
func NewMockSource[T any, C any](rows []T) *SourceMock[T, C] {
	return &SourceMock[T, C]{
		QueryFunc: func(_ context.Context, cursor *C, limit int32) ([]T, error) {
			start := 0
			if cursor != nil {
				for i, r := range rows {
					if any(r) == any(*cursor) {
						start = i + 1
						break
					}
				}
			}
			end := min(start+int(limit), len(rows))
			return rows[start:end], nil
		},
		CursorFunc: func(row T) C {
			return any(row).(C)
		},
	}
}
