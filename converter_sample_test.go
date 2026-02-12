package pdfmarkdown_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	pdfmarkdown "github.com/ivanvanderbyl/pdfmarkdown"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"
)

func TestConverter_SamplePDF(t *testing.T) {
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

	// Test with the sample PDF
	samplePath := filepath.Join("testdata", "simple.pdf")
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skip("simple.pdf not found, skipping test")
		return
	}

	markdown, err := converter.ConvertFile(samplePath)
	require.NoError(t, err)
	require.NotEmpty(t, markdown)

	t.Logf("Markdown length: %d chars\n", len(markdown))
	t.Logf("Markdown preview (first 500 chars):\n%s\n", markdown[:min(500, len(markdown))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
