package xacp

import (
	"log/slog"

	acp "github.com/coder/acp-go-sdk"
)

type UpdateLoggerOptions struct {
	Log       *slog.Logger
	BatchSize int
}

type UpdateLogger struct {
	logger  *slog.Logger
	batcher UpdateBatcher
}

func NewUpdateLogger(opts UpdateLoggerOptions) *UpdateLogger {
	l := &UpdateLogger{logger: opts.Log}
	l.batcher.BatchSize = opts.BatchSize
	l.batcher.OnBatch = func(batch []acp.SessionUpdate) error {
		for _, u := range batch {
			l.log(u)
		}
		return nil
	}
	return l
}

func (l *UpdateLogger) Update(u acp.SessionUpdate) {
	_ = l.batcher.Update(u)
}

func (l *UpdateLogger) Flush() {
	_ = l.batcher.Flush()
}

func (l *UpdateLogger) log(u acp.SessionUpdate) {
	switch {
	case u.AgentMessageChunk != nil && u.AgentMessageChunk.Content.Text != nil:
		l.logger.Info("agent", "text", u.AgentMessageChunk.Content.Text.Text)
	case u.AgentThoughtChunk != nil && u.AgentThoughtChunk.Content.Text != nil:
		l.logger.Info("thought", "text", u.AgentThoughtChunk.Content.Text.Text)
	case u.ToolCall != nil:
		l.logger.Info("tool", "title", u.ToolCall.Title, "status", string(u.ToolCall.Status))
	case u.ToolCallUpdate != nil:
		// Skip tool call updates - they're noisy and not useful
	case u.Plan != nil:
		l.logger.Info("plan", "entries", len(u.Plan.Entries))
	}
}
