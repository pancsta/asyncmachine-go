// Package telemetry provides telemetry exporters: am-dbg, Prometheus,
// OpenTelemetry.
package telemetry

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
	"go.opentelemetry.io/otel/trace"
)

func NewOtelProvider(
	source string, ctx context.Context,
) (trace.Tracer, *sdktrace.TracerProvider, error) {
	//

	// default to local traces
	if os.Getenv(EnvOtelExporterOtlpTracesEndpoint) == "" {
		os.Setenv(EnvOtelExporterOtlpTracesEndpoint, "http://127.0.0.1:4318/v1/traces")
	}

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		))

	exporter, err := otlptrace.New(ctx,
		otlptracehttp.NewClient(
			otlptracehttp.WithInsecure(),
		),
	)
	if err != nil {
		return nil, nil, err
	}

	serviceName := source
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithMaxExportBatchSize(50),
			sdktrace.WithBatchTimeout(100*time.Millisecond),
		),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	// Create a named tracer with package path as its name.
	return otel.Tracer(source), tp, nil
}
