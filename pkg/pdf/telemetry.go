package pdf

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const instrumentationName = "github.com/ivanvanderbyl/docmill/pkg/pdf"

// latencyBoundsSeconds gives histograms useful resolution from ~50us to a few
// seconds. The default OTel boundaries are tuned for seconds and collapse all of
// this pipeline's sub-millisecond stages into one bucket.
var latencyBoundsSeconds = []float64{
	0.00005, 0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01,
	0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5,
}

// Instruments use the global OTel providers, which are no-ops until
// pkg/telemetry installs real ones (so this is zero-overhead when telemetry is
// off). Creating them at init is safe: the global API returns delegating
// instruments that route to the real provider once installed.
var (
	tracer = otel.Tracer(instrumentationName)
	meter  = otel.Meter(instrumentationName)

	convertDuration  metric.Float64Histogram
	pageDuration     metric.Float64Histogram
	stageDuration    metric.Float64Histogram
	pagesProcessed   metric.Int64Counter
	tablesDetected   metric.Int64Counter
	textCellsPerPage metric.Int64Histogram
	outputBytes      metric.Int64Histogram
)

func init() {
	convertDuration, _ = meter.Float64Histogram("docmill.convert.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(latencyBoundsSeconds...),
		metric.WithDescription("Whole-document PDF to Markdown conversion time"))
	pageDuration, _ = meter.Float64Histogram("docmill.page.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(latencyBoundsSeconds...),
		metric.WithDescription("Per-page processing time"))
	stageDuration, _ = meter.Float64Histogram("docmill.stage.duration",
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(latencyBoundsSeconds...),
		metric.WithDescription("Per-stage pipeline time, labelled by stage"))
	pagesProcessed, _ = meter.Int64Counter("docmill.pages.processed",
		metric.WithDescription("Pages processed"))
	tablesDetected, _ = meter.Int64Counter("docmill.tables.detected",
		metric.WithDescription("Tables detected"))
	textCellsPerPage, _ = meter.Int64Histogram("docmill.page.textcells",
		metric.WithDescription("Text cells per page seen by the pipeline"))
	outputBytes, _ = meter.Int64Histogram("docmill.output.bytes",
		metric.WithUnit("By"),
		metric.WithDescription("Markdown output size in bytes"))
}

// recordStage records the wall time of a named pipeline stage.
func recordStage(ctx context.Context, stage string, start time.Time) {
	stageDuration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(attribute.String("stage", stage)))
}
