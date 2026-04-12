package otelx

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Provider manages the OpenTelemetry trace provider.
type Provider struct {
	tp *sdktrace.TracerProvider
}

// Shutdown gracefully shuts down the trace provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tp == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return p.tp.Shutdown(ctx)
}

// Setup initializes OpenTelemetry.
// The OTLP exporter is configured via standard OTel environment variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_INSECURE, etc.).
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, Setup is a no-op.
func Setup(ctx context.Context) (*Provider, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return &Provider{}, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return &Provider{tp: tp}, nil
}

// TraceResponseHeader is middleware that writes the OTel trace ID to a Traceresponse header.
// It must be wrapped by otelhttp.NewHandler so the span context is available.
func TraceResponseHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := trace.SpanFromContext(r.Context()).SpanContext()
		if sc.HasTraceID() {
			w.Header().Set("Traceresponse", fmt.Sprintf("00-%s-%s-%s", sc.TraceID(), sc.SpanID(), sc.TraceFlags()))
		}
		next.ServeHTTP(w, r)
	})
}
