package pdfmarkdown_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	pdfmarkdown "github.com/ivanvanderbyl/pdfmarkdown"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"
)

// TestVisualVerification_Issue140 outputs visual coordinates for verification
// This helps verify that word boundaries are being detected correctly
func TestVisualVerification_Issue140(t *testing.T) {
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

	config := pdfmarkdown.DefaultConfig()
	page, err := pdfmarkdown.ExtractPage(instance, pageResp.Page, 1, config)
	require.NoError(t, err)

	t.Logf("\n=== VISUAL VERIFICATION: Issue-140 ===\n")
	t.Logf("This test outputs bounding box coordinates to verify word boundary detection\n")

	if len(page.Paragraphs) == 0 || len(page.Paragraphs[0].Lines) == 0 {
		t.Fatal("No content extracted")
	}

	// Analyze first line (should contain table headers)
	firstLine := page.Paragraphs[0].Lines[0]
	t.Logf("\n--- First Line: %d words ---", len(firstLine.Words))

	for wi, word := range firstLine.Words {
		if wi >= 10 {
			t.Logf("... and %d more words", len(firstLine.Words)-10)
			break
		}

		t.Logf("\nWord %d: %q", wi, word.Text)
		t.Logf("  BBox: X=(%.2f → %.2f) Y=(%.2f → %.2f)",
			word.Box.X0, word.Box.X1, word.Box.Y0, word.Box.Y1)
		t.Logf("  Width: %.2f, Height: %.2f",
			word.Box.Width(), word.Box.Height())
		t.Logf("  FontSize: %.2f", word.FontSize)

		// Calculate gap to next word
		if wi < len(firstLine.Words)-1 {
			nextWord := firstLine.Words[wi+1]
			gap := nextWord.Box.X0 - word.Box.X1
			t.Logf("  Gap to next word: %.2f points", gap)
		}
	}

	// Analyze second line (should contain data with numbers)
	if len(page.Paragraphs[0].Lines) > 1 {
		secondLine := page.Paragraphs[0].Lines[1]
		t.Logf("\n--- Second Line: %d words ---", len(secondLine.Words))

		for wi, word := range secondLine.Words {
			if wi >= 10 {
				t.Logf("... and %d more words", len(secondLine.Words)-10)
				break
			}

			t.Logf("\nWord %d: %q", wi, word.Text)
			t.Logf("  BBox: X=(%.2f → %.2f) Y=(%.2f → %.2f)",
				word.Box.X0, word.Box.X1, word.Box.Y0, word.Box.Y1)

			if wi < len(secondLine.Words)-1 {
				nextWord := secondLine.Words[wi+1]
				gap := nextWord.Box.X0 - word.Box.X1
				t.Logf("  Gap to next: %.2f", gap)
			}
		}
	}

	t.Logf("\n=== Visual Verification Complete ===")
	t.Logf("Review the coordinates above to verify:")
	t.Logf("1. Words have reasonable bounding boxes")
	t.Logf("2. Gaps between words are appropriate")
	t.Logf("3. Number separations (e.g., '0000 .075 .883') are correct")
}



func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		len(s) > len(substr) && s[:len(substr)] == substr ||
		(len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Helper to visualize character-level gaps
func visualizeGaps(t *testing.T, chars []struct {
	text   rune
	x0, x1 float64
}) {
	fmt.Println("\n=== Character-Level Gap Visualization ===")
	for i := 0; i < len(chars)-1; i++ {
		curr := chars[i]
		next := chars[i+1]
		gap := next.x0 - curr.x1

		// Visual representation
		gapChars := int(gap / 2) // Scale gap for display
		if gapChars < 0 {
			gapChars = 0
		}
		if gapChars > 10 {
			gapChars = 10
		}

		fmt.Printf("%c", curr.text)
		for j := 0; j < gapChars; j++ {
			fmt.Printf("·")
		}
	}
	fmt.Println()
}
