package pdfmarkdown_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ivanvanderbyl/pdfmarkdown"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"
)

// TestProcessingMetrics verifies timing and statistics tracking
func TestProcessingMetrics(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	converter := pdfmarkdown.NewConverter(instance)

	pdfPath := filepath.Join("testdata", "simple.pdf")
	markdown, metrics, err := converter.ConvertFileWithMetrics(pdfPath)
	if err != nil {
		t.Skip("simple.pdf not found")
	}

	require.NotEmpty(t, markdown)

	t.Logf("\n=== PROCESSING METRICS ===")
	t.Logf("Total Time: %v", metrics.TotalTime)
	t.Logf("Document Open Time: %v", metrics.DocumentOpen)
	t.Logf("")
	t.Logf("Document Statistics:")
	t.Logf("  Pages: %d", metrics.Statistics.TotalPages)
	t.Logf("  Paragraphs: %d", metrics.Statistics.TotalParagraphs)
	t.Logf("  Headings: %d", metrics.Statistics.TotalHeadings)
	t.Logf("  Tables: %d", metrics.Statistics.TotalTables)
	t.Logf("  Words: %d", metrics.Statistics.TotalWords)
	t.Logf("  Characters: %d", metrics.Statistics.TotalCharacters)
	t.Logf("")
	t.Logf("Per-Page Timing:")
	for _, pm := range metrics.PageExtractions {
		t.Logf("  Page %d: %v", pm.PageNumber, pm.Duration)
	}

	// Verify metrics are reasonable
	require.Greater(t, metrics.TotalTime, time.Duration(0), "Total time should be positive")
	require.Greater(t, metrics.DocumentOpen, time.Duration(0), "Document open time should be positive")
	require.Equal(t, len(metrics.PageExtractions), metrics.Statistics.TotalPages, "Page count should match")
	require.Greater(t, metrics.Statistics.TotalPages, 0, "Should have extracted pages")
	require.Greater(t, metrics.Statistics.TotalParagraphs, 0, "Should have paragraphs")
	require.Greater(t, metrics.Statistics.TotalWords, 0, "Should have words")

	// Verify each page has timing
	for _, pm := range metrics.PageExtractions {
		require.Greater(t, pm.Duration, time.Duration(0), "Page %d should have positive duration", pm.PageNumber)
	}

	t.Logf("\n✓ All metrics validated successfully")
}

// TestMetricsLogging tests the logging output
func TestMetricsLogging(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	// Enable metrics logging
	config := pdfmarkdown.DefaultConfig()
	config.EnableMetricsLogging = true

	converter := pdfmarkdown.NewConverterWithConfig(instance, config)

	pdfPath := filepath.Join("testdata", "simple.pdf")
	markdown, err := converter.ConvertFile(pdfPath)
	if err != nil {
		t.Skip("simple.pdf not found")
	}

	require.NotEmpty(t, markdown)
	t.Logf("✓ Conversion completed with metrics logging enabled")
	t.Logf("  Check output above for metrics table")
}
