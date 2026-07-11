// Package pagination provides generic keyset (cursor) pagination plumbing for
// store list methods. It is storage- and proto-agnostic: it deals only in
// plain ints, strings, and generic type parameters, so it depends on nothing
// but the standard library.
package pagination

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidRequest reports a bad page size or an undecodable page token.
// RPC handlers map it to connect.CodeInvalidArgument.
var ErrInvalidRequest = errors.New("invalid page request")

// Config bounds the page size for a paginated list.
type Config struct {
	Default int // size used when the request omits page_size (0)
	Max     int // largest size a caller may request
}

// Page is one page of results plus the opaque token for the next page.
type Page[T any] struct {
	Items     []T
	NextToken string // empty when there are no more results
}

// Source supplies the query-specific parts of a keyset-paginated list:
// how to fetch a bounded slice of rows after a cursor, and how to derive
// a cursor from a returned row.
//
//go:generate go tool moq -out source_moq_test.go . Source
type Source[T, C any] interface {
	// Query fetches up to limit rows that sort after cursor.
	// A nil cursor means the first page.
	Query(ctx context.Context, cursor *C, limit int32) ([]T, error)
	// Cursor returns the keyset of row; it is what page tokens encode.
	Cursor(row T) C
}

// List runs one page of a keyset-paginated query against src. It validates
// pageSize against cfg, decodes pageToken into a cursor of type C (nil on
// the first page), and calls src.Query with the cursor and a limit of
// size+1. If the extra row came back there is a next page: the row is
// trimmed and NextToken is encoded from src.Cursor(last returned row);
// otherwise NextToken is empty.
func List[T, C any](ctx context.Context, cfg Config, pageSize int32, pageToken string, src Source[T, C]) (*Page[T], error) {
	size := cmp.Or(int(pageSize), cfg.Default)
	if size < 1 || size > cfg.Max {
		return nil, fmt.Errorf("%w: page_size must be between 1 and %d", ErrInvalidRequest, cfg.Max)
	}
	var cursor *C
	if pageToken != "" {
		c, err := decode[C](pageToken)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		cursor = &c
	}
	items, err := src.Query(ctx, cursor, int32(size+1))
	if err != nil {
		return nil, err
	}
	page := &Page[T]{Items: items}
	if len(items) > size {
		page.Items = items[:size]
		token, err := encode(src.Cursor(page.Items[size-1]))
		if err != nil {
			return nil, err
		}
		page.NextToken = token
	}
	return page, nil
}

func encode[C any](c C) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func decode[C any](token string) (C, error) {
	var c C
	b, err := base64.URLEncoding.DecodeString(token)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}
