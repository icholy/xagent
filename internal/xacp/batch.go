package xacp

import (
	acp "github.com/coder/acp-go-sdk"
)

type UpdateBatcher struct {
	BatchSize int
	OnBatch   func(batch []acp.SessionUpdate) error

	partial *acp.SessionUpdate
	pending []acp.SessionUpdate
}

func (b *UpdateBatcher) Update(u acp.SessionUpdate) error {
	switch {
	case b.partial == nil && isChunk(u):
		b.partial = &u
		return nil
	case isMessageChunk(u):
		if isMessageChunk(*b.partial) {
			b.partial.AgentMessageChunk.Content.Text.Text += u.AgentMessageChunk.Content.Text.Text
			return nil
		} else {
			b.pending = append(b.pending, *b.partial)
			b.partial = &u
		}
	case isThoughtChunk(u):
		if isThoughtChunk(*b.partial) {
			b.partial.AgentThoughtChunk.Content.Text.Text += u.AgentThoughtChunk.Content.Text.Text
			return nil
		} else {
			b.pending = append(b.pending, *b.partial)
			b.partial = &u
		}
	default:
		if b.partial != nil {
			b.pending = append(b.pending, *b.partial)
			b.partial = nil
		}
		b.pending = append(b.pending, u)
	}
	if len(b.pending) < b.BatchSize {
		return nil
	}
	return b.flush()
}

func (b *UpdateBatcher) Flush() error {
	if b.partial != nil {
		b.pending = append(b.pending, *b.partial)
		b.partial = nil
	}
	if len(b.pending) > 0 {
		return b.flush()
	}
	return nil
}

func (b *UpdateBatcher) flush() error {
	batch := b.pending
	b.pending = nil
	if b.OnBatch != nil {
		return b.OnBatch(batch)
	}
	return nil
}

func isChunk(u acp.SessionUpdate) bool {
	return isMessageChunk(u) || isThoughtChunk(u)
}

func isMessageChunk(u acp.SessionUpdate) bool {
	return u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil
}

func isThoughtChunk(u acp.SessionUpdate) bool {
	return u.AgentThoughtChunk != nil && u.AgentThoughtChunk.Content.Text != nil
}
