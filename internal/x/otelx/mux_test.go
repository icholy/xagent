package otelx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	s := spans[0]
	if got := s.Name(); got != "GET /foo/{id}" {
		t.Errorf("span name = %q, want %q", got, "GET /foo/{id}")
	}
	var found bool
	for _, attr := range s.Attributes() {
		if attr.Key == "http.route" && attr.Value == attribute.StringValue("GET /foo/{id}") {
			found = true
		}
	}
	if !found {
		t.Errorf("http.route attribute not found, attrs = %v", s.Attributes())
	}
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
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	s := spans[0]
	if got := s.Name(); got != "POST /bar/{id}" {
		t.Errorf("span name = %q, want %q", got, "POST /bar/{id}")
	}
}
