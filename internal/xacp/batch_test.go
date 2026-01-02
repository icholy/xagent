package xacp

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"gotest.tools/v3/assert"
)

func TestUpdateBatcher(t *testing.T) {
	tests := []struct {
		name      string
		batchSize int
		updates   []acp.SessionUpdate
		want      []acp.SessionUpdate
	}{
		{
			name:      "empty",
			batchSize: 1,
			updates:   nil,
			want:      nil,
		},
		{
			name:      "single message",
			batchSize: 1,
			updates:   []acp.SessionUpdate{textChunk("hello")},
			want:      []acp.SessionUpdate{textChunk("hello")},
		},
		{
			name:      "merge consecutive messages",
			batchSize: 1,
			updates:   []acp.SessionUpdate{textChunk("hel"), textChunk("lo")},
			want:      []acp.SessionUpdate{textChunk("hello")},
		},
		{
			name:      "merge three messages",
			batchSize: 1,
			updates:   []acp.SessionUpdate{textChunk("a"), textChunk("b"), textChunk("c")},
			want:      []acp.SessionUpdate{textChunk("abc")},
		},
		{
			name:      "merge consecutive thoughts",
			batchSize: 1,
			updates:   []acp.SessionUpdate{thoughtChunk("think"), thoughtChunk("ing")},
			want:      []acp.SessionUpdate{thoughtChunk("thinking")},
		},
		{
			name:      "message then thought",
			batchSize: 1,
			updates:   []acp.SessionUpdate{textChunk("msg"), thoughtChunk("thought")},
			want:      []acp.SessionUpdate{textChunk("msg"), thoughtChunk("thought")},
		},
		{
			name:      "thought then message",
			batchSize: 1,
			updates:   []acp.SessionUpdate{thoughtChunk("thought"), textChunk("msg")},
			want:      []acp.SessionUpdate{thoughtChunk("thought"), textChunk("msg")},
		},
		{
			name:      "tool call breaks message sequence",
			batchSize: 1,
			updates:   []acp.SessionUpdate{textChunk("a"), toolCall("test", "running"), textChunk("b")},
			want:      []acp.SessionUpdate{textChunk("a"), toolCall("test", "running"), textChunk("b")},
		},
		{
			name:      "messages around tool call",
			batchSize: 1,
			updates:   []acp.SessionUpdate{textChunk("a"), textChunk("b"), toolCall("test", "running"), textChunk("c"), textChunk("d")},
			want:      []acp.SessionUpdate{textChunk("ab"), toolCall("test", "running"), textChunk("cd")},
		},
		{
			name:      "batch size 2",
			batchSize: 2,
			updates:   []acp.SessionUpdate{toolCall("a", "running"), toolCall("b", "running"), toolCall("c", "running")},
			want:      []acp.SessionUpdate{toolCall("a", "running"), toolCall("b", "running"), toolCall("c", "running")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []acp.SessionUpdate
			b := &UpdateBatcher{
				BatchSize: tt.batchSize,
				OnBatch: func(batch []acp.SessionUpdate) error {
					got = append(got, batch...)
					return nil
				},
			}

			for _, u := range tt.updates {
				if err := b.Update(u); err != nil {
					t.Fatalf("Update() error = %v", err)
				}
			}
			if err := b.Flush(); err != nil {
				t.Fatalf("Flush() error = %v", err)
			}

			assert.DeepEqual(t, tt.want, got)
		})
	}
}
