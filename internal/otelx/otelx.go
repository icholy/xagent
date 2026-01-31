package otelx

import (
	"context"
	"errors"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Provider manages the OpenTelemetry trace provider and exporter.
type Provider struct {
	tp       *sdktrace.TracerProvider
	exporter sdktrace.SpanExporter
}

// Shutdown gracefully shuts down the trace provider and exporter.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tp == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return errors.Join(p.tp.Shutdown(ctx), p.exporter.Shutdown(ctx))
}

// NewProvider initializes OpenTelemetry.
// The OTLP exporter is configured via standard OTel environment variables
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_INSECURE, etc.).
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, NewProvider is a no-op.
func NewProvider(ctx context.Context) (*Provider, error) {
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

	return &Provider{tp: tp, exporter: exporter}, nil
}
