// Package logctx propagates org and task identifiers through the context so
// they can be attached to structured logs and OpenTelemetry traces, making it
// easier to correlate activity while debugging.
package logctx

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type orgIDKey struct{}

type taskIDKey struct{}

// WithOrgID returns a context carrying the org id.
func WithOrgID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, orgIDKey{}, id)
}

// OrgID returns the org id carried by ctx, if any.
func OrgID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(orgIDKey{}).(int64)
	return id, ok
}

// WithTaskID returns a context carrying the task id.
func WithTaskID(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, taskIDKey{}, id)
}

// TaskID returns the task id carried by ctx, if any.
func TaskID(ctx context.Context) (int64, bool) {
	id, ok := ctx.Value(taskIDKey{}).(int64)
	return id, ok
}

// Handler wraps a slog.Handler, adding org_id, task_id and trace_id attributes
// pulled from the context to every record. Use the *Context log methods (e.g.
// InfoContext) so the request context — and thus the identifiers — is available.
type Handler struct {
	slog.Handler
}

// NewHandler wraps h so records are enriched with identifiers from the context.
func NewHandler(h slog.Handler) *Handler {
	return &Handler{Handler: h}
}

// Handle implements slog.Handler.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := OrgID(ctx); ok {
		r.AddAttrs(slog.Int64("org_id", id))
	}
	if id, ok := TaskID(ctx); ok {
		r.AddAttrs(slog.Int64("task_id", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{Handler: h.Handler.WithGroup(name)}
}
