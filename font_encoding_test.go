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

// TestFontEncoding_Issue461 tests handling of byte-encoded font names
// Issue: Some PDFs have font names encoded as bytes rather than strings
// Expected: Should handle without crashing
func TestFontEncoding_Issue461(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-461-example.pdf")
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

	mdDoc := &docmill.Document{
		Pages: []docmill.Page{*page},
	}
	markdown := mdDoc.ToMarkdown(config)

	require.NotEmpty(t, markdown, "Should extract text despite font encoding issues")
	t.Logf("Extracted text with byte-encoded fonts: %d chars", len(markdown))
}

// TestFontEncoding_Issue842 tests handling of font attribute issues
// Issue: Some PDFs have unusual font attributes that cause extraction issues
// Expected: Should extract text without errors
func TestFontEncoding_Issue842(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-842-example.pdf")
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

	mdDoc := &docmill.Document{
		Pages: []docmill.Page{*page},
	}
	markdown := mdDoc.ToMarkdown(config)

	require.NotEmpty(t, markdown, "Should extract text despite font attribute issues")
	t.Logf("Extracted text with font attribute edge cases: %d chars", len(markdown))
}

// TestTableDetection_Issue140 tests table detection with sparse tables
// Issue: Tables with many empty cells should still be detected properly
// Expected: Should detect table structure correctly
func TestTableDetection_Issue140(t *testing.T) {
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
	config.DetectTables = true

	page, err := docmill.ExtractPage(instance, pageResp.Page, 1, config)
	require.NoError(t, err)

	// Should detect at least one table
	require.NotEmpty(t, page.Tables, "Should detect table in sparse table PDF")
	t.Logf("Detected %d table(s) in sparse table PDF", len(page.Tables))

	// Verify we can convert to markdown without errors
	mdDoc := &docmill.Document{
		Pages: []docmill.Page{*page},
	}
	markdown := mdDoc.ToMarkdown(config)
	require.Contains(t, markdown, "|", "Markdown should contain table markers")
}
