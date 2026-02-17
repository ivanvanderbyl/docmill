package docmill_test

import (
	"path/filepath"
	"testing"
	"time"

	docmill "github.com/ivanvanderbyl/docmill"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"
)

// TestIssue140_WordExtraction analyzes word extraction to diagnose spacing issues
func TestIssue140_WordExtraction(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-140-example.pdf")
	doc, err := instance.OpenDocument(&requests.OpenDocument{
		FilePath: &pdfPath,
	})
	require.NoError(t, err)
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	pageResp, err := instance.FPDF_LoadPage(&requests.FPDF_LoadPage{
		Document: doc.Document,
		Index:    0,
	})
	require.NoError(t, err)
	defer instance.FPDF_ClosePage(&requests.FPDF_ClosePage{
		Page: pageResp.Page,
	})

	config := docmill.DefaultConfig()
	page, err := docmill.ExtractPage(instance, pageResp.Page, 1, config)
	require.NoError(t, err)

	// Analyze first few paragraphs in detail
	t.Logf("Total paragraphs: %d", len(page.Paragraphs))

	for pi := 0; pi < len(page.Paragraphs) && pi < 5; pi++ {
		para := page.Paragraphs[pi]
		t.Logf("\n=== Paragraph %d ===", pi)
		t.Logf("Lines: %d, IsHeading: %v, IsList: %v, IsCode: %v",
			len(para.Lines), para.IsHeading, para.IsList, para.IsCode)
		t.Logf("BBox: (%.1f, %.1f) → (%.1f, %.1f)",
			para.Box.X0, para.Box.Y0, para.Box.X1, para.Box.Y1)

		for li, line := range para.Lines {
			t.Logf("  Line %d: %d words, Baseline: %.2f", li, len(line.Words), line.Baseline)

			for wi, word := range line.Words {
				if wi < 20 { // Show first 20 words
					t.Logf("    Word %d: %q at (%.1f, %.1f) size:%.1f",
						wi, word.Text, word.Box.X0, word.Box.Y0, word.FontSize)
				}
			}

			if len(line.Words) > 20 {
				t.Logf("    ... and %d more words", len(line.Words)-20)
			}
		}

		// Show paragraph text
		text := para.Text()
		if len(text) > 200 {
			text = text[:200] + "..."
		}
		t.Logf("  Text: %q", text)
	}
}
