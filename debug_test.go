package pdfmarkdown_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"

	pdfmarkdown "github.com/ivanvanderbyl/pdfmarkdown"
)

func TestDebug_SamplePDF_LineSpacing(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	samplePath := filepath.Join("testdata", "simple.pdf")
	if _, statErr := os.Stat(samplePath); os.IsNotExist(statErr) {
		t.Skip("simple.pdf not found")
	}
	doc, err := instance.OpenDocument(&requests.OpenDocument{
		FilePath: &samplePath,
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

	fmt.Printf("\n=== Page 1 Analysis ===\n")
	fmt.Printf("Total paragraphs detected: %d\n\n", len(page.Paragraphs))

	for pIdx, para := range page.Paragraphs {
		fmt.Printf("Paragraph %d: %d lines\n", pIdx+1, len(para.Lines))

		prevLineBottom := 0.0
		for lIdx, line := range para.Lines {
			if lIdx > 0 {
				gap := line.Box.Y0 - prevLineBottom
				fmt.Printf("  Line %d gap: %.2fpt\n", lIdx+1, gap)
			}

			// Show first few words
			preview := ""
			for i, word := range line.Words {
				if i >= 10 {
					preview += "..."
					break
				}
				preview += word.Text + " "
			}
			fmt.Printf("  Line %d (y:%.1f): %s\n", lIdx+1, line.Box.Y0, preview)

			prevLineBottom = line.Box.Y1
		}
		fmt.Println()
	}
}
