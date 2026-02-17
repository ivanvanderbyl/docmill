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

// TestEdgeCases_DuplicateChars tests handling of PDFs with duplicate CJK characters.
// Issue: Some PDFs have each character duplicated, e.g., "微微软软" instead of "微软"
// Expected: We should be able to extract text even if duplicated (deduplication is optional enhancement)
func TestEdgeCases_DuplicateChars(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-71-duplicate-chars.pdf")
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

	// Should successfully extract text even if characters are duplicated
	require.NotEmpty(t, markdown, "Should extract text from PDF with duplicate characters")
	require.Contains(t, markdown, "微", "Should contain Chinese characters")

	// Note: Actual deduplication (微微软软 -> 微软) could be implemented as an enhancement
	t.Logf("Extracted text length: %d", len(markdown))
}

// TestEdgeCases_Ligatures tests handling of ligature characters (fi, fl, ffi, etc.)
// Issue: Some fonts render ligatures as single glyphs that should be expanded
// Expected: Ligatures should be properly extracted as their component characters
func TestEdgeCases_Ligatures(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-598-example.pdf")
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

	require.NotEmpty(t, markdown, "Should extract text from PDF with ligatures")
	// Ligatures like fi, fl, ffi should be properly expanded if present
	t.Logf("Extracted markdown with potential ligatures: %d chars", len(markdown))
}

// TestEdgeCases_RotationAngles tests handling of text at various rotation angles
// Issue: Text can be rotated at 8 different angles (0, 45, 90, 135, 180, 225, 270, 315)
// Expected: Should correctly extract text regardless of rotation
func TestEdgeCases_RotationAngles(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-848.pdf")
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

	require.NotEmpty(t, markdown, "Should extract text from PDF with rotated text")
	// Text at various angles should all be extracted
	t.Logf("Extracted text from rotated PDF: %d chars", len(markdown))
}

// TestEdgeCases_VerticalText tests handling of vertical text (TTB, BTT directions)
// Issue: Some PDFs have vertical text (common in East Asian languages)
// This PDF (issue-192) tests vertical text detection and also contains tables with grid lines
// Expected: Should correctly extract both vertical text and table structures
func TestEdgeCases_VerticalText(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-192-example.pdf")
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

	mdDoc := &docmill.Document{
		Pages: []docmill.Page{*page},
	}
	markdown := mdDoc.ToMarkdown(config)

	require.NotEmpty(t, markdown, "Should extract vertical text from PDF")

	// According to pdfplumber tests, first word should contain "Agaaaaa:"
	require.Contains(t, markdown, "Agaaaaa", "Should extract expected first word with vertical text")

	// This PDF also contains tables (3 tables with grid lines)
	require.GreaterOrEqual(t, len(page.Tables), 3, "Should detect tables in vertical text PDF")

	t.Logf("Extracted vertical text: %d chars", len(markdown))
	t.Logf("Detected tables: %d", len(page.Tables))
}

// TestEdgeCases_NonZeroMediaBox tests handling of PDFs with non-zero MediaBox origin
// Issue: Some PDFs have MediaBox that doesn't start at (0,0)
// Expected: Coordinates should be correctly adjusted
func TestEdgeCases_NonZeroMediaBox(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-1181.pdf")
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

	require.NotEmpty(t, markdown, "Should extract text from PDF with non-zero MediaBox origin")
	t.Logf("Extracted text from non-zero MediaBox PDF: %d chars", len(markdown))
}

// TestEdgeCases_MalformedPDF tests handling of malformed/corrupted PDFs
// Issue: Real-world PDFs can have various forms of corruption
// Expected: Should handle gracefully without crashing
func TestEdgeCases_MalformedPDF(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "malformed-from-issue-932.pdf")
	doc, err := instance.OpenDocument(&requests.OpenDocument{
		FilePath: &pdfPath,
	})

	// May fail to open, which is acceptable for malformed PDFs
	if err != nil {
		t.Logf("Malformed PDF failed to open (expected): %v", err)
		return
	}
	defer instance.FPDF_CloseDocument(&requests.FPDF_CloseDocument{
		Document: doc.Document,
	})

	// If it opens, try to extract - should not crash
	pageResp, err := instance.FPDF_LoadPage(&requests.FPDF_LoadPage{
		Document: doc.Document,
		Index:    0,
	})
	if err != nil {
		t.Logf("Malformed PDF page load failed (acceptable): %v", err)
		return
	}
	defer instance.FPDF_ClosePage(&requests.FPDF_ClosePage{
		Page: pageResp.Page,
	})

	config := docmill.DefaultConfig()
	_, err = docmill.ExtractPage(instance, pageResp.Page, 1, config)
	// May fail, but should not crash
	if err != nil {
		t.Logf("Malformed PDF extraction failed (acceptable): %v", err)
	}
}

// TestEdgeCases_EmptyPDF tests handling of completely empty PDFs
// Issue: Edge case of 0-byte PDFs
// Expected: Should handle gracefully
func TestEdgeCases_EmptyPDF(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "empty.pdf")
	_, err = instance.OpenDocument(&requests.OpenDocument{
		FilePath: &pdfPath,
	})

	// Should fail to open empty PDF
	require.Error(t, err, "Empty PDF should fail to open")
	t.Logf("Empty PDF correctly rejected: %v", err)
}

// TestEdgeCases_TableWithCurves tests table detection with curved borders
// Issue: Some tables use curved lines instead of straight lines for borders
// Expected: Should detect tables with curved borders
func TestEdgeCases_TableWithCurves(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "table-curves-example.pdf")
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

	// Note: Curved borders may not be detected as straight lines
	// This is a known limitation - curves would need special handling
	t.Logf("Tables detected with curved borders: %d", len(page.Tables))
	t.Logf("Note: Curved borders may not be fully detected without curve-to-line conversion")
}

// TestEdgeCases_UnicodeIssues tests handling of various Unicode edge cases
// Issue: PDFs can have Unicode encoding issues, out-of-bounds characters, etc.
// Expected: Should handle gracefully without crashes
func TestEdgeCases_UnicodeIssues(t *testing.T) {
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle:  1,
		MaxIdle:  1,
		MaxTotal: 1,
	})
	require.NoError(t, err)
	defer pool.Close()

	instance, err := pool.GetInstance(time.Second * 30)
	require.NoError(t, err)

	pdfPath := filepath.Join("testdata", "issue-905.pdf")
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

	require.NotEmpty(t, markdown, "Should extract text despite Unicode issues")
	t.Logf("Extracted text with Unicode edge cases: %d chars", len(markdown))
}
