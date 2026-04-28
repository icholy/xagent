package otelx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Mux wraps http.ServeMux and annotates the active OpenTelemetry span
// with the matched route pattern after dispatch.
type Mux struct {
	*http.ServeMux
	name string
}

// NewMux returns a new Mux wrapping a fresh http.ServeMux.
// The name is used as the operation name for otelhttp spans.
func NewMux(name string) *Mux {
	return &Mux{ServeMux: http.NewServeMux(), name: name}
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.ServeMux.ServeHTTP(w, r)
	if r.Pattern != "" {
		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(attribute.String("http.route", r.Pattern))
		span.SetName(r.Method + " " + r.Pattern)
	}
}

// Handler returns an http.Handler that wraps the mux with the provided
// middleware, TraceResponseHeader, and otelhttp instrumentation.
func (m *Mux) Handler(middlewares ...func(http.Handler) http.Handler) http.Handler {
	var h http.Handler = m
	for _, mw := range middlewares {
		h = mw(h)
	}
	return otelhttp.NewHandler(TraceResponseHeader(h), m.name)
}
