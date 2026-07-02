// Package telemetry wires OpenTelemetry traces and metrics to OTLP/HTTP
// exporters.
//
// It is inert unless Enabled() reports true, leaving the global no-op providers
// in place so instrumented library code (pkg/pdf) has zero
// overhead when telemetry is off. The exporters honour the standard
// OTEL_EXPORTER_OTLP_* environment variables, so endpoints are configured
// through the environment rather than code. Point metrics at VictoriaMetrics
// and traces at an OTLP trace store (e.g. Tempo):
//
//	OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://localhost:8428/opentelemetry/v1/metrics
//	OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://localhost:4318/v1/traces
package telemetry

import (
	"context"
	"errors"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// DefaultServiceName is the service.name reported on all telemetry.
const DefaultServiceName = "docmill"

// Config controls telemetry setup. Zero values fall back to sensible defaults;
// exporter endpoints come from the standard OTEL_EXPORTER_OTLP_* variables.
type Config struct {
	ServiceName    string
	ServiceVersion string
}

// Enabled reports whether telemetry should be initialised. It is on when
// DOCMILL_OTEL is truthy or any standard OTLP endpoint variable is set, so the
// CLI only pays for telemetry when an operator opts in.
func Enabled() bool {
	if truthy(os.Getenv("DOCMILL_OTEL")) {
		return true
	}
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	} {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// Setup installs global tracer and meter providers backed by OTLP/HTTP
// exporters and returns a shutdown function that flushes pending data. Callers
// must invoke the returned shutdown before the process exits (os.Exit skips
// deferred calls), otherwise buffered spans and metrics are lost.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = DefaultServiceName
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = moduleVersion()
	}

	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
	))
	if err != nil {
		return nil, err
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)

	// docmill is frequently a short-lived CLI: many one-shot processes export
	// once and share identical labels, so cumulative temporality collides into a
	// flat counter and rate() yields nothing. Default to delta temporality, which
	// VictoriaMetrics accumulates correctly. An explicit
	// OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE wins (the SDK honours it),
	// so we only set the default when the operator hasn't.
	var metricOpts []otlpmetrichttp.Option
	if os.Getenv("OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE") == "" {
		metricOpts = append(metricOpts, otlpmetrichttp.WithTemporalitySelector(deltaTemporality))
	}
	metricExp, err := otlpmetrichttp.New(ctx, metricOpts...)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(10*time.Second))),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

// deltaTemporality selects delta aggregation for sums and histograms (so
// short-lived processes accumulate correctly downstream) while leaving
// up/down counters cumulative.
func deltaTemporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	switch kind {
	case sdkmetric.InstrumentKindCounter,
		sdkmetric.InstrumentKindHistogram,
		sdkmetric.InstrumentKindObservableCounter,
		sdkmetric.InstrumentKindGauge,
		sdkmetric.InstrumentKindObservableGauge:
		return metricdata.DeltaTemporality
	default:
		return metricdata.CumulativeTemporality
	}
}

func moduleVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
