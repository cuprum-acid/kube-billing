package controller

import (
	"context"
	"log"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

const serviceName = "kube-billing"

var Tracer = otel.Tracer(serviceName)

// InitTracer initializes the OpenTelemetry tracer with a Resource carrying
// service.name=kube-billing so that traces are queryable by service name
// in the Jaeger UI rather than landing under "unknown_service:main".
// The OTLP endpoint is read from OTEL_EXPORTER_OTLP_ENDPOINT (default
// localhost:4318, HTTP, insecure).
func InitTracer() func() {
	ctx := context.Background()

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

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		log.Fatal(err)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)

	otel.SetTracerProvider(tp)

	return func() {
		_ = tp.Shutdown(ctx)
	}
}
