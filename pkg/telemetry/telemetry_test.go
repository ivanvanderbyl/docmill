package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ivanvanderbyl/docmill/v2/pkg/telemetry"
)

func TestEnabledReflectsEnv(t *testing.T) {
	for _, k := range []string{
		"DOCMILL_OTEL",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	} {
		t.Setenv(k, "")
	}
	require.False(t, telemetry.Enabled())

	t.Setenv("DOCMILL_OTEL", "1")
	require.True(t, telemetry.Enabled())

	t.Setenv("DOCMILL_OTEL", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://127.0.0.1:8428/opentelemetry/v1/metrics")
	require.True(t, telemetry.Enabled())
}

func TestSetupReturnsCallableShutdown(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://127.0.0.1:4318/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://127.0.0.1:8428/opentelemetry/v1/metrics")

	shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{ServiceVersion: "test"})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// The endpoints are unreachable in the test, so shutdown may return a flush
	// error; it must not panic and must respect the context deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NotPanics(t, func() { _ = shutdown(ctx) })
}
