package otelx

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Mux wraps http.ServeMux and annotates the active OpenTelemetry span
// with the matched route pattern after dispatch.
type Mux struct {
	*http.ServeMux
}

// NewMux returns a new Mux wrapping a fresh http.ServeMux.
func NewMux() *Mux {
	return &Mux{ServeMux: http.NewServeMux()}
}

func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.ServeMux.ServeHTTP(w, r)
	if r.Pattern != "" {
		span := trace.SpanFromContext(r.Context())
		span.SetAttributes(attribute.String("http.route", r.Pattern))
		span.SetName(r.Method + " " + r.Pattern)
	}
}
