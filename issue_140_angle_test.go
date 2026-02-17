package docmill_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/stretchr/testify/require"
)

// TestIssue140_AngleAnalysis checks the actual angle values of characters
func TestIssue140_AngleAnalysis(t *testing.T) {
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

	// Extract text page
	textPage, err := instance.FPDFText_LoadPage(&requests.FPDFText_LoadPage{
		Page: requests.Page{
			ByReference: &pageResp.Page,
		},
	})
	require.NoError(t, err)
	defer instance.FPDFText_ClosePage(&requests.FPDFText_ClosePage{
		TextPage: textPage.TextPage,
	})

	// Get character count
	countRes, err := instance.FPDFText_CountChars(&requests.FPDFText_CountChars{
		TextPage: textPage.TextPage,
	})
	require.NoError(t, err)

	t.Logf("Total characters: %d", countRes.Count)

	// Check first 100 character angles
	t.Logf("\n=== First 100 Character Angles ===")

	angleCount := make(map[float32]int)
	for i := 0; i < 100 && i < countRes.Count; i++ {
		unicodeRes, err := instance.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{
			TextPage: textPage.TextPage,
			Index:    i,
		})
		if err != nil || unicodeRes.Unicode == 0 {
			continue
		}

		angle, err := instance.FPDFText_GetCharAngle(&requests.FPDFText_GetCharAngle{
			TextPage: textPage.TextPage,
			Index:    i,
		})
		if err != nil {
			continue
		}

		angleCount[angle.CharAngle]++

		if i < 20 {
			t.Logf("Char %d: '%c' angle=%.2f", i, rune(unicodeRes.Unicode), angle.CharAngle)
		}
	}

	t.Logf("\n=== Angle Distribution (first 100 chars) ===")
	for angle, count := range angleCount {
		t.Logf("Angle %.2f: %d characters (%.1f%%)", angle, count, float64(count)/100.0*100)
	}

	// Check if text is rotated
	maxCount := 0
	var dominantAngle float32
	for angle, count := range angleCount {
		if count > maxCount {
			maxCount = count
			dominantAngle = angle
		}
	}

	t.Logf("\n=== Analysis ===")
	t.Logf("Dominant angle: %.2f (%.1f%% of chars)", dominantAngle, float64(maxCount)/100.0*100)

	if dominantAngle != 0 && dominantAngle != 180 {
		t.Logf("✓ Text is rotated - angle detection should work")
	} else {
		t.Logf("⚠ Text appears horizontal - rotation may not be the issue")
	}
}
