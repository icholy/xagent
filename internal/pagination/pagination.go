// Package pagination provides generic keyset (cursor) pagination plumbing for
// store list methods. It is storage- and proto-agnostic: it deals only in
// plain ints, strings, and generic type parameters, so it depends on nothing
// but the standard library.
//
// Pagination is bidirectional. A page token carries a boundary cursor plus a
// Backward bool (default false = the primary/forward walk). List passes that
// bit straight into Source.Query, and returns two continuation tokens on every
// page: ForwardToken continues the primary walk (toward older rows) and empties
// when that direction is exhausted; BackwardToken reverses it (toward newer
// rows) and, on a non-empty page, stays populated so an append-only stream can
// be followed past its tail. A forward-only caller (the task list) simply
// ignores BackwardToken and behaves exactly as it did before bidirectionality.
package pagination

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// ErrInvalidRequest reports a bad page size or an undecodable page token.
// RPC handlers map it to connect.CodeInvalidArgument.
var ErrInvalidRequest = errors.New("invalid page request")

// ErrUnsupportedDirection is returned by a Source's Query asked to walk a
// direction it does not implement (e.g. the task list with backward=true).
// List never probes for support, so it surfaces only if a client submits a
// page token for a walk the Source lacks — which a forward-only source's caller
// never hands out. List wraps it as ErrInvalidRequest (a bad token).
var ErrUnsupportedDirection = errors.New("unsupported page direction")

// Config bounds the page size and sets the display order of Page.Items via
// Reverse, defined relative to the forward walk's order:
//
//   - Reverse == false (default): forward pages are returned as-is and backward
//     pages reversed, so Items always read in the forward walk's order
//     (newest-first for the task list, unchanged).
//   - Reverse == true: forward pages are reversed and backward pages returned
//     as-is — the opposite order (oldest-first, for a timeline rendered
//     top-down).
//
// Reverse affects only Items ordering, never token derivation. Because the two
// Query walks are mutually opposite, this single flag lands every page — from
// either direction — in one consistent order.
type Config struct {
	Default int  // size used when the request omits page_size (0)
	Max     int  // largest size a caller may request
	Reverse bool // display Items in the reverse of the forward walk's order
}

// Page is one page plus the two continuation tokens, both derived from the
// page's own boundary rows — no extra query. ForwardToken continues the primary
// walk (older) and empties when that direction is exhausted; BackwardToken
// reverses it (newer) and, on a non-empty page, stays populated past the tail
// so an append-only stream can be followed. A forward-only caller (the task
// list) simply ignores BackwardToken.
type Page[T any] struct {
	Items         []T
	ForwardToken  string
	BackwardToken string
}

// Source walks a keyset in either direction and maps a row to a cursor. Query's
// backward argument — the same bit the page token carries — selects the walk:
// false (default) is the primary/forward walk, true its reverse.
//
// Two invariants every Source must satisfy — List relies on these and nothing
// more:
//
//   - Nearest-first, exclusive. Query returns up to limit rows adjacent to the
//     cursor, ordered from the row nearest the cursor outward, and never
//     re-emits the cursor row itself (the cursor is the previous page's
//     boundary row). A nil cursor starts from that walk's far end (the forward
//     walk's first page).
//   - Mutually opposite. Whatever key order the forward walk returns, the
//     backward walk returns the reverse. List needs only that the two are
//     opposite; it never assumes which one is ascending — that is the Source's
//     choice. (By the convention used here, forward walks toward lower keys /
//     older rows and backward toward higher keys / newer rows, but the package
//     does not depend on it.)
//
// A one-directional Source returns ErrUnsupportedDirection for the walk it does
// not implement. List never calls the opposite walk to build a page (both
// tokens come from the page it already fetched), so the error only surfaces if
// a client resubmits a token for it.
//
//go:generate go tool moq -out source_moq_test.go . Source
type Source[T, C any] interface {
	Query(ctx context.Context, cursor *C, backward bool, limit int) ([]T, error)
	// Cursor returns the keyset of row; it is what page tokens encode.
	Cursor(row T) C
}

// keysetToken is what an opaque page token encodes: a cursor plus the direction
// to resume — the bit List passes to Source.Query. Backward defaults to false
// (the forward, primary walk).
type keysetToken[C any] struct {
	Cursor   C    `json:"c"`
	Backward bool `json:"b,omitempty"`
}

// List runs one page of a keyset-paginated query against src.
//
// It validates pageSize against cfg, decodes pageToken into a cursor and a
// direction (an empty token is the newest page: nil cursor, forward), and calls
// src.Query once with a limit of size+1 — the over-fetch reveals whether more
// rows lie further along that walk. Both continuation tokens are derived from
// the returned page's own boundary rows, so a bidirectional page costs a single
// query with no probe of the opposite walk:
//
//   - ForwardToken (older) comes from the lowest-key boundary and is populated
//     only when the over-fetch shows more rows remain that way — it empties at
//     history's end.
//   - BackwardToken (newer) comes from the highest-key boundary and is
//     populated on any non-empty page so an append-only stream can be followed;
//     an empty follow-poll echoes the request token so the caller keeps its
//     place.
//
// Finally Items is oriented per cfg.Reverse. ErrUnsupportedDirection from the
// requested walk surfaces as ErrInvalidRequest (a bad token the client should
// not have had); other query failures surface as-is.
func List[T, C any](ctx context.Context, cfg Config, pageSize int32, pageToken string, src Source[T, C]) (*Page[T], error) {
	size := cmp.Or(int(pageSize), cfg.Default)
	if size < 1 || size > cfg.Max {
		return nil, fmt.Errorf("%w: page_size must be between 1 and %d", ErrInvalidRequest, cfg.Max)
	}
	var cursor *C
	backward := false
	if pageToken != "" {
		tok, err := decode[keysetToken[C]](pageToken)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
		}
		cursor = &tok.Cursor
		backward = tok.Backward
	}
	items, err := src.Query(ctx, cursor, backward, size+1)
	if err != nil {
		if errors.Is(err, ErrUnsupportedDirection) {
			return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
		return nil, err
	}
	// Over-fetch: an extra row means more rows lie further along this walk.
	more := len(items) > size
	if more {
		items = items[:size]
	}
	page := &Page[T]{Items: items}
	switch {
	case len(items) > 0:
		// Query returns rows nearest-first, so items[0] is the boundary nearest
		// the cursor and the last item is the boundary furthest into the walk.
		// Same-direction continuation resumes from the furthest row; the
		// opposite direction resumes from the nearest row (back toward where the
		// cursor came from).
		nearest := src.Cursor(items[0])
		furthest := src.Cursor(items[len(items)-1])
		if backward {
			// Newer/live-follow page: follow onward from the furthest (highest)
			// key — always resumable so the tail can be polled — and expose the
			// nearest (lowest) key as the way back into older history.
			if page.BackwardToken, err = encodeToken(furthest, true); err != nil {
				return nil, err
			}
			if page.ForwardToken, err = encodeToken(nearest, false); err != nil {
				return nil, err
			}
		} else {
			// Older/primary page: the nearest (highest) key resumes the
			// newer/live-follow direction, always populated on a non-empty page;
			// the furthest (lowest) key continues toward older history, but only
			// when the over-fetch shows more remain.
			if page.BackwardToken, err = encodeToken(nearest, true); err != nil {
				return nil, err
			}
			if more {
				if page.ForwardToken, err = encodeToken(furthest, false); err != nil {
					return nil, err
				}
			}
		}
	case backward:
		// Empty follow-poll: no boundary row to derive from, so echo the request
		// token — the same resume cursor — to keep the caller's place at the tail.
		page.BackwardToken = pageToken
	}
	// Orient Items relative to the forward walk's order: reverse a forward page
	// iff Reverse, a backward page iff !Reverse. Because the two walks are
	// mutually opposite, this lands every page in one consistent order.
	if backward != cfg.Reverse {
		slices.Reverse(page.Items)
	}
	return page, nil
}

func encodeToken[C any](cursor C, backward bool) (string, error) {
	return encode(keysetToken[C]{Cursor: cursor, Backward: backward})
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
