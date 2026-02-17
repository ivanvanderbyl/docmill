package docmill_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	docmill "github.com/ivanvanderbyl/docmill"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"
)

// TestIssue140_Analysis performs detailed analysis of the table structure
func TestIssue140_Analysis(t *testing.T) {
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

	// Extract with both detection methods enabled
	config := docmill.DefaultConfig()
	config.DetectTables = true
	config.UseSegmentBasedTables = true

	page, err := docmill.ExtractPage(instance, pageResp.Page, 1, config)
	require.NoError(t, err)

	// Log detailed information
	t.Logf("Page dimensions: %.2f × %.2f", page.Width, page.Height)
	t.Logf("Detected %d tables", len(page.Tables))
	t.Logf("Detected %d columns", len(page.Columns))
	t.Logf("Number of paragraphs: %d", len(page.Paragraphs))

	// Analyze each detected table
	for i, table := range page.Tables {
		t.Logf("\n=== Table %d ===", i+1)
		t.Logf("Dimensions: %d rows × %d columns", table.NumRows, table.NumCols)
		t.Logf("BBox: (%.1f, %.1f) → (%.1f, %.1f)",
			table.BBox.X0, table.BBox.Top, table.BBox.X1, table.BBox.Bottom)

		// Print table structure
		for r, row := range table.Rows {
			t.Logf("Row %d (%d cells):", r, len(row.Cells))
			for c, cell := range row.Cells {
				content := cell.Content
				if len(content) > 50 {
					content = content[:50] + "..."
				}
				t.Logf("  [%d,%d]: %q", r, c, content)
			}
		}

		// Write table to JSON for inspection
		tableJSON, _ := json.MarshalIndent(table, "", "  ")
		outputPath := filepath.Join("testdata", "issue-140-table-analysis.json")
		_ = os.WriteFile(outputPath, tableJSON, 0644)
		t.Logf("Table JSON written to: %s", outputPath)
	}

	// Convert to markdown and inspect
	mdDoc := &docmill.Document{
		Pages: []docmill.Page{*page},
	}
	markdown := mdDoc.ToMarkdown(config)

	outputPath := filepath.Join("testdata", "issue-140-output.md")
	err = os.WriteFile(outputPath, []byte(markdown), 0644)
	require.NoError(t, err)
	t.Logf("Markdown output written to: %s", outputPath)

	t.Logf("\n=== Markdown Output ===\n%s", markdown)
}
