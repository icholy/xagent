package xacp

import (
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// CompactUpdates merges consecutive chunk updates of the same type into single updates.
// This reduces a stream of small text chunks into complete messages.
func CompactUpdates(updates []acp.SessionUpdate) []acp.SessionUpdate {
	var result []acp.SessionUpdate
	var message strings.Builder
	var thought strings.Builder

	flushMessage := func() {
		if message.Len() > 0 {
			result = append(result, acp.SessionUpdate{
				AgentMessageChunk: &acp.SessionUpdateAgentMessageChunk{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: message.String()},
					},
				},
			})
			message.Reset()
		}
	}

	flushThought := func() {
		if thought.Len() > 0 {
			result = append(result, acp.SessionUpdate{
				AgentThoughtChunk: &acp.SessionUpdateAgentThoughtChunk{
					Content: acp.ContentBlock{
						Text: &acp.ContentBlockText{Text: thought.String()},
					},
				},
			})
			thought.Reset()
		}
	}

	flushAll := func() {
		flushMessage()
		flushThought()
	}

	for _, u := range updates {
		switch {
		case u.AgentMessageChunk != nil:
			flushThought()
			if u.AgentMessageChunk.Content.Text != nil {
				message.WriteString(u.AgentMessageChunk.Content.Text.Text)
			}
		case u.AgentThoughtChunk != nil:
			flushMessage()
			if u.AgentThoughtChunk.Content.Text != nil {
				thought.WriteString(u.AgentThoughtChunk.Content.Text.Text)
			}
		default:
			flushAll()
			result = append(result, u)
		}
	}

	flushAll()
	return result
}
