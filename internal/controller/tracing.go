package controller

import (
	"context"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/trace"
)

var Tracer = otel.Tracer("kube-billing")

// InitTracer initializes the OpenTelemetry tracer.
// The OTLP endpoint can be configured via OTEL_EXPORTER_OTLP_ENDPOINT environment variable.
// Default endpoint is localhost:4318 (HTTP, insecure).
func InitTracer() func() {
	ctx := context.Background()

	// Read endpoint from environment variable or use default
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4318"
	}

	log.Printf("Initializing OpenTelemetry tracer with endpoint: %s", endpoint)

	exporter, err := otlptracehttp.New(
		ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		log.Fatal(err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
	)

	otel.SetTracerProvider(tp)

	return func() {
		_ = tp.Shutdown(ctx)
	}
}
