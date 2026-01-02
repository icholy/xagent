package xacp

import (
	"testing"

	"github.com/coder/acp-go-sdk"
	"gotest.tools/v3/assert"
)

func TestCompactUpdates(t *testing.T) {
	tests := []struct {
		name   string
		input  []acp.SessionUpdate
		expect []acp.SessionUpdate
	}{
		{
			name:   "empty",
			input:  nil,
			expect: nil,
		},
		{
			name:   "single chunk",
			input:  []acp.SessionUpdate{textChunk("hello")},
			expect: []acp.SessionUpdate{textChunk("hello")},
		},
		{
			name: "multiple chunks",
			input: []acp.SessionUpdate{
				textChunk("Hello"),
				textChunk(" "),
				textChunk("world"),
				textChunk("!"),
			},
			expect: []acp.SessionUpdate{textChunk("Hello world!")},
		},
		{
			name: "chunks with tool call",
			input: []acp.SessionUpdate{
				textChunk("Before"),
				textChunk(" tool"),
				toolCall("Read file", "running"),
				textChunk("After"),
				textChunk(" tool"),
			},
			expect: []acp.SessionUpdate{
				textChunk("Before tool"),
				toolCall("Read file", "running"),
				textChunk("After tool"),
			},
		},
		{
			name: "thought chunks",
			input: []acp.SessionUpdate{
				thoughtChunk("Thinking"),
				thoughtChunk(" about"),
				thoughtChunk(" this"),
			},
			expect: []acp.SessionUpdate{thoughtChunk("Thinking about this")},
		},
		{
			name: "mixed chunks",
			input: []acp.SessionUpdate{
				textChunk("Agent"),
				textChunk(" message"),
				thoughtChunk("Thought"),
				thoughtChunk(" here"),
				textChunk("More"),
				textChunk(" text"),
			},
			expect: []acp.SessionUpdate{
				textChunk("Agent message"),
				thoughtChunk("Thought here"),
				textChunk("More text"),
			},
		},
		{
			name: "non-chunk passthrough",
			input: []acp.SessionUpdate{
				toolCall("Tool 1", "running"),
				toolCall("Tool 2", "completed"),
			},
			expect: []acp.SessionUpdate{
				toolCall("Tool 1", "running"),
				toolCall("Tool 2", "completed"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompactUpdates(tt.input)
			assert.DeepEqual(t, tt.expect, result)
		})
	}
}

func textChunk(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: text},
			},
		},
	}
}

func thoughtChunk(text string) acp.SessionUpdate {
	return acp.SessionUpdate{
		AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
			Content: acp.ContentBlock{
				Text: &acp.ContentBlockText{Text: text},
			},
		},
	}
}

func toolCall(title, status string) acp.SessionUpdate {
	return acp.SessionUpdate{
		ToolCall: &acp.SessionUpdateToolCall{
			Title:  title,
			Status: acp.ToolCallStatus(status),
		},
	}
}
