package pdf

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

func TestExtractMarkdownTracesPageStages(t *testing.T) {
	recorder := installTestTracer(t)

	doc := telemetryDocument{page: telemetryPage{}}
	_, err := ExtractMarkdownWithOptions(context.Background(), doc, ExtractionOptions{
		DetectTables:     true,
		ReadingOrder:     true,
		DetectStructure:  true,
		DetectHeadings:   true,
		MaxParallelPages: 1,
	})
	require.NoError(t, err)

	spansByName := make(map[string][]sdktrace.ReadOnlySpan)
	for _, span := range recorder.Ended() {
		spansByName[span.Name()] = append(spansByName[span.Name()], span)
	}

	pageSpans := spansByName["docmill.page"]
	require.Len(t, pageSpans, 1)
	pageSpanID := pageSpans[0].SpanContext().SpanID()
	for _, name := range []string{
		"pipeline.page_open",
		"pipeline.page_size",
		"pipeline.text_cells",
		"pipeline.form_fields",
		"pipeline.ruling_segments",
		"pipeline.word_text_cells",
		"pipeline.heading_detect",
		"pipeline.structure",
		"pipeline.postprocess",
	} {
		stageSpans := spansByName[name]
		require.Len(t, stageSpans, 1, name)
		require.Equal(t, pageSpanID, stageSpans[0].Parent().SpanID(), name)
	}
}

func TestExtractMarkdownEndsPanickingPageStageSpan(t *testing.T) {
	recorder := installTestTracer(t)

	_, err := ExtractMarkdownWithOptions(context.Background(), telemetryDocument{page: panickingTelemetryPage{}}, ExtractionOptions{
		MaxParallelPages: 1,
	})
	require.ErrorContains(t, err, "recovered panic extracting page 0: malformed text")

	var textCellsSpan sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.Name() == "pipeline.text_cells" {
			textCellsSpan = span
			break
		}
	}
	require.NotNil(t, textCellsSpan)
	require.NotEmpty(t, textCellsSpan.Events())
}

func installTestTracer(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previousTracer := tracer
	tracer = provider.Tracer(instrumentationName)
	t.Cleanup(func() {
		tracer = previousTracer
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	return recorder
}

type telemetryDocument struct {
	page Page
}

func (d telemetryDocument) PageCount(context.Context) (int, error) {
	return 1, nil
}

func (d telemetryDocument) Page(context.Context, int) (Page, error) {
	return d.page, nil
}

func (telemetryDocument) Close() error {
	return nil
}

type telemetryPage struct{}

func (telemetryPage) Size(context.Context) (geom.Size, error) {
	return geom.Size{Width: 612, Height: 792}, nil
}

func (telemetryPage) TextCells(context.Context) ([]page.TextCell, error) {
	return nil, nil
}

func (telemetryPage) TextInRect(context.Context, geom.Box) (string, error) {
	return "", nil
}

func (telemetryPage) FormFields(context.Context) ([]page.FormField, error) {
	return nil, nil
}

func (telemetryPage) RulingSegments(context.Context) ([]page.RulingSegment, error) {
	return nil, nil
}

func (telemetryPage) WordTextCells(context.Context) ([]page.TextCell, error) {
	return nil, nil
}

type panickingTelemetryPage struct {
	telemetryPage
}

func (panickingTelemetryPage) TextCells(context.Context) ([]page.TextCell, error) {
	panic("malformed text")
}
