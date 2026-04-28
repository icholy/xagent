package otelx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"gotest.tools/v3/assert"
)

func TestMuxSetsHTTPRoute(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer("test")

	mux := NewMux("test")
	mux.HandleFunc("GET /foo/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/foo/123", nil)
	ctx, span := tracer.Start(req.Context(), "initial")
	req = req.WithContext(ctx)

	mux.ServeHTTP(httptest.NewRecorder(), req)
	span.End()

	spans := recorder.Ended()
	assert.Equal(t, len(spans), 1)
	s := spans[0]
	assert.Equal(t, s.Name(), "GET /foo/{id}")
	assertAttribute(t, s.Attributes(), "http.route", "GET /foo/{id}")
}

func TestMuxSetsHTTPRouteWithoutVerb(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer("test")

	mux := NewMux("test")
	mux.HandleFunc("/bar/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/bar/456", nil)
	ctx, span := tracer.Start(req.Context(), "initial")
	req = req.WithContext(ctx)

	mux.ServeHTTP(httptest.NewRecorder(), req)
	span.End()

	spans := recorder.Ended()
	assert.Equal(t, len(spans), 1)
	assert.Equal(t, spans[0].Name(), "POST /bar/{id}")
}

func assertAttribute(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			assert.Equal(t, attr.Value.AsString(), want)
			return
		}
	}
	t.Errorf("attribute %q not found in %v", key, attrs)
}
