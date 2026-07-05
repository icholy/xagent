package logctx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"gotest.tools/v3/assert"
)

// logLine parses the single JSON record written by handling one log call with ctx.
func logLine(t *testing.T, ctx context.Context) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	log := slog.New(NewHandler(slog.NewJSONHandler(&buf, nil)))
	log.InfoContext(ctx, "hello")
	var rec map[string]any
	assert.NilError(t, json.Unmarshal(buf.Bytes(), &rec))
	return rec
}

func TestHandlerAddsOrgAndTaskID(t *testing.T) {
	ctx := WithTaskID(WithOrgID(context.Background(), 7), 42)
	rec := logLine(t, ctx)
	assert.Equal(t, rec["org_id"], float64(7))
	assert.Equal(t, rec["task_id"], float64(42))
}

func TestHandlerOmitsMissingIDs(t *testing.T) {
	rec := logLine(t, context.Background())
	_, hasOrg := rec["org_id"]
	_, hasTask := rec["task_id"]
	_, hasTrace := rec["trace_id"]
	assert.Assert(t, !hasOrg)
	assert.Assert(t, !hasTask)
	assert.Assert(t, !hasTrace)
}

func TestHandlerAddsTraceID(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()
	rec := logLine(t, ctx)
	assert.Equal(t, rec["trace_id"], span.SpanContext().TraceID().String())
}

func TestContextRoundTrip(t *testing.T) {
	ctx := WithOrgID(context.Background(), 3)
	org, ok := OrgID(ctx)
	assert.Assert(t, ok)
	assert.Equal(t, org, int64(3))

	ctx = WithTaskID(ctx, 9)
	task, ok := TaskID(ctx)
	assert.Assert(t, ok)
	assert.Equal(t, task, int64(9))
}
