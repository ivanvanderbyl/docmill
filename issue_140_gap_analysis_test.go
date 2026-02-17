package docmill_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	docmill "github.com/ivanvanderbyl/docmill"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"
)

// TestIssue140_GapAnalysis analyzes word boundaries in extracted content
func TestIssue140_GapAnalysis(t *testing.T) {
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

	// Focus on the first line which should contain table headers
	if len(page.Paragraphs) == 0 || len(page.Paragraphs[0].Lines) == 0 {
		t.Fatal("No content extracted")
	}

	firstLine := page.Paragraphs[0].Lines[0]
	if len(firstLine.Words) == 0 {
		t.Fatal("No words in first line")
	}

	firstWord := firstLine.Words[0]
	t.Logf("First word: %q", firstWord.Text)
	t.Logf("Word length: %d characters", len(firstWord.Text))

	// Analyze what words we got vs what we should have
	expectedWords := []string{
		"number", "PO", "Rate", "Handling", "Amount", "Accrued", "Amount", "Bill",
		"Quantity", "Item", "Description", "Location", "code", "UPC", "no", "Line",
	}

	t.Logf("\n=== Expected vs Actual ===")
	t.Logf("Expected: %s", strings.Join(expectedWords, " "))

	var actualWords []string
	for _, word := range firstLine.Words {
		actualWords = append(actualWords, word.Text)
	}
	t.Logf("Actual: %s", strings.Join(actualWords, " "))

	// Check if the first word contains the concatenated text
	if strings.Contains(firstWord.Text, "numberPO") {
		t.Logf("\n⚠ Words are still concatenated - gap-based detection needs tuning")

		// Analyze word positions
		t.Logf("\n=== Word Position Analysis ===")
		for i, word := range firstLine.Words {
			if i >= 5 {
				break
			}
			t.Logf("Word %d: %q at X=(%.1f → %.1f) width=%.1f",
				i, word.Text, word.Box.X0, word.Box.X1, word.Box.Width())
		}
	} else {
		t.Logf("\n✓ Words are properly separated")
	}

	// Log recommendation
	totalChars := len(firstWord.Text)
	if totalChars > 50 {
		t.Logf("\n=== Recommendation ===")
		t.Logf("First word has %d characters - likely all headers concatenated", totalChars)
		t.Logf("Need to adjust gap detection threshold or add more heuristics")
	}
}
