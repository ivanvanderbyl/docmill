package pdf_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"github.com/stretchr/testify/require"
)

func TestExtractMarkdownWalksPagesAndTextCellsInNativeOrder(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					{Index: 2, Text: "Body", Box: geom.Box{L: 0, T: 20, R: 40, B: 30, Origin: geom.TopLeft}},
					{Index: 1, Text: "Title", Box: geom.Box{L: 0, T: 0, R: 40, B: 10, Origin: geom.TopLeft}},
				},
			},
			{
				cells: []page.TextCell{
					{Index: 1, Text: "Second page", Box: geom.Box{L: 0, T: 0, R: 40, B: 10, Origin: geom.TopLeft}},
					{Index: 2, Text: "   ", Box: geom.Box{L: 0, T: 20, R: 40, B: 30, Origin: geom.TopLeft}},
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "Title\n\nBody\n\nSecond page", got)
}

func TestExtractMarkdownWithOptionsRendersDetectedTablesInPageFlow(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					pdfTextCell(1, "Intro", 0, 0, 40, 10),
					pdfTextCell(2, "Name", 0, 40, 35, 50),
					pdfTextCell(3, "Value", 100, 40, 140, 50),
					pdfTextCell(4, "Ada", 0, 62, 28, 72),
					pdfTextCell(5, "1", 100, 62, 108, 72),
					pdfTextCell(6, "Bob", 0, 84, 28, 94),
					pdfTextCell(7, "2", 100, 84, 108, 94),
					pdfTextCell(8, "Outro", 0, 130, 42, 140),
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              3,
			MinCols:              2,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	})

	require.NoError(t, err)
	require.Equal(t, "Intro\n\n| Name | Value |\n| ---- | ----: |\n| Ada  |     1 |\n| Bob  |     2 |\n\nOutro", got)
}

func TestExtractMarkdownRendersFilledFormFieldsInline(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					pdfTextCell(1, "Intro", 0, 20, 40, 30),
					pdfTextCell(2, "Outro", 0, 150, 40, 160),
				},
				formFields: []page.FormField{
					{
						Name:  "Applicant name",
						Value: "Ada Lovelace",
						Box:   geom.Box{L: 120, T: 70, R: 260, B: 90, Origin: geom.TopLeft},
					},
					{
						Name:  "Accept terms",
						Value: "Yes",
						Box:   geom.Box{L: 120, T: 100, R: 140, B: 120, Origin: geom.TopLeft},
					},
					{
						Name:  "Empty field",
						Value: "   ",
						Box:   geom.Box{L: 120, T: 125, R: 260, B: 145, Origin: geom.TopLeft},
					},
					{
						Name:  "Unchecked",
						Value: "Off",
						Box:   geom.Box{L: 120, T: 130, R: 140, B: 150, Origin: geom.TopLeft},
					},
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "Intro\n\nApplicant name: Ada Lovelace\n\nAccept terms: Yes\n\nOutro", got)
}

func TestExtractMarkdownUsesWordCellsForTableDetectionAndLineCellsForProse(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					pdfTextCell(1, "Intro paragraph", 0, 0, 120, 10),
					pdfTextCell(2, "Name Year Score", 0, 40, 230, 50),
					pdfTextCell(3, "Ada", 0, 62, 28, 72),
					pdfTextCell(4, "2024", 100, 62, 135, 72),
					pdfTextCell(5, "98", 200, 62, 218, 72),
					pdfTextCell(6, "Bob", 0, 84, 28, 94),
					pdfTextCell(7, "2025", 100, 84, 135, 94),
					pdfTextCell(8, "99", 200, 84, 218, 94),
					pdfTextCell(9, "Cy", 0, 106, 20, 116),
					pdfTextCell(10, "2026", 100, 106, 135, 116),
					pdfTextCell(11, "97", 200, 106, 218, 116),
					pdfTextCell(12, "Table 1: Results", 0, 132, 120, 142),
					pdfTextCell(13, "Outro paragraph", 0, 170, 120, 180),
				},
				wordCells: []page.TextCell{
					pdfTextCell(1, "Name", 0, 40, 35, 50),
					pdfTextCell(2, "Year", 100, 40, 132, 50),
					pdfTextCell(3, "Score", 200, 40, 242, 50),
					pdfTextCell(4, "Ada", 0, 62, 28, 72),
					pdfTextCell(5, "2024", 100, 62, 135, 72),
					pdfTextCell(6, "98", 200, 62, 218, 72),
					pdfTextCell(7, "Bob", 0, 84, 28, 94),
					pdfTextCell(8, "2025", 100, 84, 135, 94),
					pdfTextCell(9, "99", 200, 84, 218, 94),
					pdfTextCell(10, "Cy", 0, 106, 20, 116),
					pdfTextCell(11, "2026", 100, 106, 135, 116),
					pdfTextCell(12, "97", 200, 106, 218, 116),
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              4,
			MinCols:              3,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	})

	require.NoError(t, err)
	require.Equal(t, "Intro paragraph\n\n| Name | Year | Score |\n| ---- | ---: | ----: |\n| Ada  | 2024 |    98 |\n| Bob  | 2025 |    99 |\n| Cy   | 2026 |    97 |\n\nTable 1: Results\n\nOutro paragraph", got)
}

func TestExtractMarkdownAssignsDetectedTableTextFromWordCells(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					pdfTextCell(1, "Intro paragraph", 0, 0, 120, 10),
					pdfTextCell(2, "Name", 0, 40, 35, 50),
					pdfTextCell(3, "Score", 100, 40, 138, 50),
					pdfTextCell(4, "Flag", 200, 40, 230, 50),
					pdfTextCell(5, "Alpha", 0, 62, 38, 72),
					pdfTextCell(6, "99", 100, 63, 112, 72),
					pdfTextCell(7, ".", 113, 69, 116, 74),
					pdfTextCell(8, "58%", 118, 62, 140, 72),
					pdfTextCell(9, "ok", 200, 62, 214, 72),
					pdfTextCell(10, "Beta", 0, 84, 30, 94),
					pdfTextCell(11, "98.00%", 100, 84, 140, 94),
					pdfTextCell(12, "ok", 200, 84, 214, 94),
					pdfTextCell(13, "Outro paragraph", 0, 130, 120, 140),
				},
				wordCells: []page.TextCell{
					pdfTextCell(1, "Name", 0, 40, 35, 50),
					pdfTextCell(2, "Score", 100, 40, 138, 50),
					pdfTextCell(3, "Flag", 200, 40, 230, 50),
					pdfTextCell(4, "Alpha", 0, 62, 38, 72),
					pdfTextCell(5, "99.58%", 100, 62, 140, 72),
					pdfTextCell(6, "ok", 200, 62, 214, 72),
					pdfTextCell(7, "Beta", 0, 84, 30, 94),
					pdfTextCell(8, "98.00%", 100, 84, 140, 94),
					pdfTextCell(9, "ok", 200, 84, 214, 94),
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              3,
			MinCols:              3,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	})

	require.NoError(t, err)
	require.Contains(t, got, "| Alpha | 99.58% | ok   |")
	require.NotContains(t, got, "58% 99")
	require.Contains(t, got, "Outro paragraph")
}

func TestExtractMarkdownFindsAnchoredTableAfterEarlierDetectedTable(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					pdfTextCell(1, "Intro paragraph", 0, 0, 120, 10),
					pdfTextCell(2, "Name", 0, 40, 35, 50),
					pdfTextCell(3, "Value", 100, 40, 140, 50),
					pdfTextCell(4, "Ada", 0, 62, 28, 72),
					pdfTextCell(5, "1", 100, 62, 108, 72),
					pdfTextCell(6, "Bob", 0, 84, 28, 94),
					pdfTextCell(7, "2", 100, 84, 108, 94),
					pdfTextCell(8, "Cal", 0, 106, 24, 116),
					pdfTextCell(9, "3", 100, 106, 108, 116),
					pdfTextCell(10, "Name Year Score", 0, 152, 230, 162),
					pdfTextCell(11, "Cy", 0, 174, 20, 184),
					pdfTextCell(12, "2024", 100, 174, 135, 184),
					pdfTextCell(13, "98", 200, 174, 218, 184),
					pdfTextCell(14, "Dee", 0, 196, 28, 206),
					pdfTextCell(15, "2025", 100, 196, 135, 206),
					pdfTextCell(16, "99", 200, 196, 218, 206),
					pdfTextCell(17, "Eve", 0, 218, 28, 228),
					pdfTextCell(18, "2026", 100, 218, 135, 228),
					pdfTextCell(19, "97", 200, 218, 218, 228),
					pdfTextCell(20, "Table 2: Results", 0, 254, 120, 264),
				},
				wordCells: []page.TextCell{
					pdfTextCell(1, "Name", 0, 152, 35, 162),
					pdfTextCell(2, "Year", 100, 152, 132, 162),
					pdfTextCell(3, "Score", 200, 152, 242, 162),
					pdfTextCell(4, "Cy", 0, 174, 20, 184),
					pdfTextCell(5, "2024", 100, 174, 135, 184),
					pdfTextCell(6, "98", 200, 174, 218, 184),
					pdfTextCell(7, "Dee", 0, 196, 28, 206),
					pdfTextCell(8, "2025", 100, 196, 135, 206),
					pdfTextCell(9, "99", 200, 196, 218, 206),
					pdfTextCell(10, "Eve", 0, 218, 28, 228),
					pdfTextCell(11, "2026", 100, 218, 135, 228),
					pdfTextCell(12, "97", 200, 218, 218, 228),
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              4,
			MinCols:              2,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	})

	require.NoError(t, err)
	require.Contains(t, got, "| Name | Value |")
	require.Contains(t, got, "| Name | Year | Score |")
	require.NotContains(t, got, "Name Year Score\n\nCy 2024 98")
}

func TestExtractMarkdownKeepsCaptionlessMultilineGridAvailableForTableDetection(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Version History", 70.87, 62.03, 225.47, 82.46, 21.00),
				pdfTextCellWithFont(2, "This page provides a record of edits and changes made to this book since its initial publication.", 71.31, 139.01, 543.39, 148.62, 10.50),
				pdfTextCellWithFont(3, "Whenever edits or updates are made in the text, we provide a record and description of those", 71.36, 154.76, 543.39, 164.27, 10.50),
				pdfTextCellWithFont(4, "changes here. If the change is minor, the version number increases by 0.1. If the edits involve", 71.35, 170.40, 543.39, 180.12, 10.50),
				pdfTextCellWithFont(5, "substantial updates, the edition number increases to the next whole number.", 71.13, 186.26, 440.07, 195.77, 10.50),
				pdfTextCellWithFont(6, "r.", 440.00, 192.86, 443.91, 194.05, 10.50),
				pdfTextCellWithFont(7, "The files posted alongside this book always reflect the most recent version. If you find an error in", 71.31, 212.51, 543.39, 222.12, 10.50),
				pdfTextCellWithFont(8, "this book, pleaselet us know in the Rebus Community forum, where reported errors will be visible", 71.12, 228.15, 543.39, 237.77, 10.50),
				pdfTextCellWithFont(9, "to others.", 71.12, 244.01, 119.71, 251.89, 10.50),
				pdfTextCellWithFont(10, "We will contact the author, make the necessary changes, and replace all file types as soon as", 71.36, 270.26, 543.39, 279.87, 10.50),
				pdfTextCellWithFont(11, "possible. Once we receive the updated files, this Version History page will be updated to reflect", 71.67, 285.90, 543.39, 295.62, 10.50),
				pdfTextCellWithFont(12, "the edits made.", 71.12, 301.76, 148.13, 309.64, 10.50),
				pdfTextCellWithFont(13, "Version History", 70.87, 337.33, 186.82, 352.65, 15.75),
				pdfTextCellWithFont(14, "Version History", 271.27, 369.57, 340.86, 378.13, 9.45),
				pdfTextCellWithFont(15, "Version", 76.17, 391.49, 109.95, 398.59, 9.45),
				pdfTextCellWithFont(16, "Date", 135.76, 391.49, 156.72, 398.59, 9.45),
				pdfTextCellWithFont(17, "Change", 197.37, 391.40, 231.46, 400.15, 9.45),
				pdfTextCellWithFont(18, "Affected Sections", 374.96, 391.40, 457.01, 398.59, 9.45),
				pdfTextCellWithFont(19, "1.0", 76.25, 418.03, 89.33, 425.23, 9.45),
				pdfTextCellWithFont(20, "April 30,", 135.51, 412.85, 171.84, 421.50, 9.45),
				pdfTextCellWithFont(21, "2022", 135.57, 423.24, 158.05, 430.42, 9.45),
				pdfTextCellWithFont(22, "Original", 197.56, 418.04, 230.22, 426.79, 9.45),
				pdfTextCellWithFont(23, "1.0", 76.25, 450.68, 89.33, 457.87, 9.45),
				pdfTextCellWithFont(24, "June 3,", 135.47, 445.49, 165.13, 453.94, 9.45),
				pdfTextCellWithFont(25, "2022", 135.57, 455.89, 158.05, 463.07, 9.45),
				pdfTextCellWithFont(26, "Small edits for clarity on Creative", 197.40, 445.49, 341.02, 454.15, 9.45),
				pdfTextCellWithFont(27, "Commons licensing and attribution.", 197.56, 455.89, 351.90, 464.64, 9.45),
				pdfTextCellWithFont(28, "1. Introduction to Open Educational", 375.12, 445.49, 527.81, 454.15, 9.45),
				pdfTextCellWithFont(29, "Resources", 375.57, 455.98, 421.20, 463.08, 9.45),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Regexp(t, `(?m)^\|\s*Version\s+\|\s*Date\s+\|\s*Change\s+\|\s*Affected Sections\s+\|$`, got)
	require.Regexp(t, `(?m)^\|\s*1\.0\s+\|\s*April 30, 2022\s+\|\s*Original\s+\|\s*\|$`, got)
	require.Contains(t, got, "Small edits for clarity on Creative Commons licensing and attribution.")
	require.Regexp(t, `(?m)^\|\s*1\.0\s+\|\s*June 3, 2022\s+\|\s*Small edits for clarity on Creative Commons licensing and attribution\.\s+\|\s*1\. Introduction to Open Educational Resources\s+\|$`, got)
	require.NotContains(t, got, "# 1.0 Original")
	require.NotContains(t, got, "# 1. Introduction to Open Educational Resources")
}

func TestExtractMarkdownKeepsCaptionBeforeMultilineTableAvailableForDetection(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "election integrity paragraph", 54, 58, 375, 67, 10),
		pdfTextCellWithFont(2, "Table: The number of accredited observers as of 28 April", 54.23, 116.36, 353.53, 126.52, 11),
		pdfTextCellWithFont(3, "202215", 54.28, 129.29, 85.41, 137.68, 11),
		pdfTextCellWithFont(4, "No.", 62.44, 159.67, 74.98, 166.22, 9),
		pdfTextCellWithFont(5, "Name of organization", 88.20, 159.56, 172.43, 168.00, 9),
		pdfTextCellWithFont(6, "Number of accredited", 280.76, 159.56, 365.46, 166.22, 9),
		pdfTextCellWithFont(7, "observers", 303.64, 170.47, 342.50, 177.02, 9),
		pdfTextCellWithFont(8, "1", 67.24, 186.00, 69.61, 192.47, 9),
		pdfTextCellWithFont(9, "Union of Youth Federations of Cambodia", 88.23, 185.92, 249.24, 192.57, 9),
		pdfTextCellWithFont(10, "17,266", 310.32, 186.00, 336.44, 193.74, 9),
		pdfTextCellWithFont(11, "(UYFC)", 88.07, 196.72, 117.69, 205.15, 9),
		pdfTextCellWithFont(12, "2", 66.52, 212.35, 70.78, 218.82, 9),
		pdfTextCellWithFont(13, "Cambodian Women for Peace and", 87.97, 212.27, 224.78, 218.92, 9),
		pdfTextCellWithFont(14, "9,835", 312.22, 212.35, 334.00, 220.09, 9),
		pdfTextCellWithFont(15, "Development", 88.21, 223.18, 140.48, 231.40, 9),
		pdfTextCellWithFont(16, "3", 66.64, 238.70, 70.86, 245.28, 9),
		pdfTextCellWithFont(17, "Association of Democratic Students of", 87.52, 238.62, 239.90, 245.27, 9),
		pdfTextCellWithFont(18, "711", 316.35, 238.70, 328.63, 245.17, 9),
		pdfTextCellWithFont(19, "Cambodia", 87.97, 249.42, 128.15, 256.07, 9),
		pdfTextCellWithFont(20, "4", 66.37, 265.08, 70.83, 271.53, 9),
		pdfTextCellWithFont(21, "Association of Intellectual and Youth", 87.52, 264.97, 231.01, 271.62, 9),
		pdfTextCellWithFont(22, "46", 318.21, 265.05, 327.69, 271.62, 9),
		pdfTextCellWithFont(23, "Volunteer", 87.56, 275.88, 125.67, 282.42, 9),
		pdfTextCellWithFont(24, "5", 66.64, 291.52, 70.90, 297.98, 9),
		pdfTextCellWithFont(25, "Our Friends Association", 87.95, 291.32, 182.43, 297.98, 9),
		pdfTextCellWithFont(26, "27", 318.36, 291.41, 327.70, 297.88, 9),
		pdfTextCellWithFont(27, "6", 66.60, 307.96, 70.85, 314.53, 9),
		pdfTextCellWithFont(28, "COMFREL", 87.97, 307.87, 131.21, 314.53, 9),
		pdfTextCellWithFont(29, "26", 318.36, 307.96, 327.69, 314.53, 9),
		pdfTextCellWithFont(30, "7", 66.68, 323.62, 70.86, 329.98, 9),
		pdfTextCellWithFont(31, "Traditional and Modern Mental Health", 87.73, 323.54, 237.13, 330.08, 9),
		pdfTextCellWithFont(32, "15", 319.08, 323.51, 327.74, 330.08, 9),
		pdfTextCellWithFont(33, "Organization", 87.95, 334.22, 137.92, 342.66, 9),
		pdfTextCellWithFont(34, "Total", 87.71, 349.89, 107.73, 356.43, 9),
		pdfTextCellWithFont(35, "27,926", 309.56, 349.86, 336.54, 357.77, 9),
		pdfTextCellWithFont(36, "15 https://www.nec.gov.kh/khmer/content/5524", 54.65, 536.91, 185.00, 542.53, 6),
	}
	doc := fakeDocument{pages: []fakePage{{
		size:      geom.Size{Width: 420, Height: 580},
		cells:     cells,
		wordCells: cells,
	}}}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables:   true,
		DetectHeadings: true,
	})

	require.NoError(t, err)
	require.Contains(t, got, "| No.")
	require.Contains(t, got, "Name of organization")
	require.Contains(t, got, "Number of accredited observers |")
	require.Contains(t, got, "|   6 | COMFREL")
	require.Contains(t, got, "|     | Total")
	require.NotContains(t, got, "# Table: The number")
	require.NotContains(t, got, "# 6 COMFREL")
}

func TestExtractMarkdownKeepsLabelValueHeaderAvailableForTableDetection(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(3, "6.", 86.15, 132.40, 97.18, 144.03, 18),
		pdfTextCellWithFont(4, "ECO CIRCLE COMPETENCE FRAMEWORK", 122.76, 132.38, 410.85, 144.03, 18),
		pdfTextCellWithFont(5, "Competence Area", 76.65, 194.53, 183.17, 206.01, 14),
		pdfTextCellWithFont(6, "#1 THE 3 RS: RECYCLE", 213.41, 195.46, 344.48, 206.59, 17),
		pdfTextCellWithFont(7, "-", 345.56, 201.39, 349.79, 203.10, 17),
		pdfTextCellWithFont(8, "REUSE", 351.53, 195.65, 387.68, 206.56, 17),
		pdfTextCellWithFont(9, "-", 388.64, 201.39, 392.87, 203.10, 17),
		pdfTextCellWithFont(10, "REDUCE", 394.61, 195.65, 443.64, 206.56, 17),
		pdfTextCellWithFont(11, "Competence Statement", 76.57, 236.86, 193.51, 246.69, 12),
		pdfTextCellWithFont(12, "To know the basics of the 3 Rs and their importance and", 210.12, 236.04, 452.14, 245.13, 10.6),
		pdfTextCellWithFont(13, "implementation into daily life in relation to green entrepreneurship", 210.74, 249.96, 500.23, 259.05, 10.6),
		pdfTextCellWithFont(14, "and circular economy.", 210.53, 263.83, 303.82, 272.88, 10.6),
		pdfTextCellWithFont(15, "Learning Outcomes", 76.93, 325.69, 172.06, 335.77, 12),
		pdfTextCellWithFont(16, "Knowledge", 76.93, 358.37, 131.18, 368.65, 12),
		pdfTextCellWithFont(17, "●", 229.09, 360.69, 234.25, 365.85, 12),
		pdfTextCellWithFont(18, "To understand the meaning of reducing, reusing and recycling", 246.12, 359.43, 512.26, 368.52, 10.6),
		pdfTextCellWithFont(19, "and how they connect", 246.53, 373.27, 340.68, 382.32, 10.6),
		pdfTextCellWithFont(20, "●", 229.09, 389.73, 234.25, 394.89, 12),
		pdfTextCellWithFont(21, "To understand the importance of the 3 Rs as waste", 246.12, 388.47, 465.80, 397.56, 10.6),
		pdfTextCellWithFont(22, "management", 246.84, 403.32, 302.52, 411.36, 10.6),
		pdfTextCellWithFont(23, "●", 228.97, 418.16, 233.51, 422.70, 10.6),
		pdfTextCellWithFont(24, "To be familiar with the expansion of the 3 Rs - the 7 Rs", 246.12, 416.19, 477.89, 425.28, 10.6),
		pdfTextCellWithFont(25, "Skills", 76.48, 450.43, 100.90, 458.71, 12),
		pdfTextCellWithFont(26, "●", 229.09, 452.75, 234.25, 457.91, 12),
		pdfTextCellWithFont(27, "To implement different ways of waste management into daily", 246.12, 451.49, 511.54, 460.58, 10.6),
		pdfTextCellWithFont(28, "life", 246.84, 465.29, 258.83, 472.59, 10.6),
		pdfTextCellWithFont(29, "●", 228.97, 481.06, 233.51, 485.60, 10.6),
		pdfTextCellWithFont(30, "To properly implement recycling in day-to-day activities", 246.12, 479.13, 483.17, 488.18, 10.6),
		pdfTextCellWithFont(31, "●", 228.97, 494.86, 233.51, 499.40, 10.6),
		pdfTextCellWithFont(32, "To promote reducing and reusing before recycling", 246.12, 492.89, 459.06, 501.98, 10.6),
		pdfTextCellWithFont(33, "Attitudes and Values", 76.24, 527.23, 179.53, 535.51, 12),
		pdfTextCellWithFont(34, "●", 229.09, 529.55, 234.25, 534.71, 12),
		pdfTextCellWithFont(35, "To acquire a proactive approach to implementing the 3 Rs into", 246.12, 528.33, 514.84, 537.38, 10.6),
		pdfTextCellWithFont(36, "daily personal life", 246.55, 542.21, 320.76, 551.30, 10.6),
		pdfTextCellWithFont(37, "●", 228.97, 557.98, 233.51, 562.52, 10.6),
		pdfTextCellWithFont(38, "To educate others on the importance of sustainable waste", 246.12, 556.01, 497.17, 565.10, 10.6),
		pdfTextCellWithFont(39, "management", 246.84, 570.86, 302.52, 578.90, 10.6),
	}
	doc := fakeDocument{pages: []fakePage{{
		size:  geom.Size{Width: 595, Height: 842},
		cells: cells,
	}}}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables:   true,
		DetectHeadings: true,
		ReadingOrder:   true,
	})

	require.NoError(t, err)
	require.Contains(t, got, "| Competence Area")
	require.Contains(t, got, "#1 THE 3 RS: RECYCLE - REUSE - REDUCE")
	require.NotContains(t, got, "# Competence Area")
}

func TestExtractMarkdownDoesNotUseWordCellsToPromoteDenseProse(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					pdfTextCell(1, "Intro paragraph", 0, 0, 120, 10),
					pdfTextCell(2, "Alpha beta gamma", 0, 40, 180, 50),
					pdfTextCell(3, "Delta epsilon zeta", 0, 62, 180, 72),
					pdfTextCell(4, "Eta theta iota", 0, 84, 180, 94),
					pdfTextCell(5, "Kappa lambda mu", 0, 106, 180, 116),
					pdfTextCell(6, "Outro paragraph", 0, 150, 120, 160),
				},
				wordCells: []page.TextCell{
					pdfTextCell(1, "Alpha", 0, 40, 35, 50),
					pdfTextCell(2, "beta", 70, 40, 100, 50),
					pdfTextCell(3, "gamma", 140, 40, 180, 50),
					pdfTextCell(4, "Delta", 0, 62, 35, 72),
					pdfTextCell(5, "epsilon", 70, 62, 122, 72),
					pdfTextCell(6, "zeta", 140, 62, 170, 72),
					pdfTextCell(7, "Eta", 0, 84, 22, 94),
					pdfTextCell(8, "theta", 70, 84, 108, 94),
					pdfTextCell(9, "iota", 140, 84, 170, 94),
					pdfTextCell(10, "Kappa", 0, 106, 42, 116),
					pdfTextCell(11, "lambda", 70, 106, 118, 116),
					pdfTextCell(12, "mu", 140, 106, 158, 116),
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              4,
			MinCols:              3,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	})

	require.NoError(t, err)
	require.Equal(t, "Intro paragraph\n\nAlpha beta gamma\n\nDelta epsilon zeta\n\nEta theta iota\n\nKappa lambda mu\n\nOutro paragraph", got)
}

func TestExtractMarkdownDoesNotUseWordCellsToPromoteTwoColumnProse(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{
			{
				cells: []page.TextCell{
					pdfTextCell(1, "Alpha beta", 0, 40, 100, 50),
					pdfTextCell(2, "gamma", 140, 40, 180, 50),
					pdfTextCell(3, "Delta epsilon", 0, 62, 120, 72),
					pdfTextCell(4, "zeta", 140, 62, 170, 72),
					pdfTextCell(5, "Eta theta", 0, 84, 108, 94),
					pdfTextCell(6, "iota", 140, 84, 170, 94),
					pdfTextCell(7, "Kappa lambda", 0, 106, 118, 116),
					pdfTextCell(8, "mu", 140, 106, 158, 116),
				},
				wordCells: []page.TextCell{
					pdfTextCell(1, "Alpha", 0, 40, 35, 50),
					pdfTextCell(2, "beta", 70, 40, 100, 50),
					pdfTextCell(3, "gamma", 140, 40, 180, 50),
					pdfTextCell(4, "Delta", 0, 62, 35, 72),
					pdfTextCell(5, "epsilon", 70, 62, 122, 72),
					pdfTextCell(6, "zeta", 140, 62, 170, 72),
					pdfTextCell(7, "Eta", 0, 84, 22, 94),
					pdfTextCell(8, "theta", 70, 84, 108, 94),
					pdfTextCell(9, "iota", 140, 84, 170, 94),
					pdfTextCell(10, "Kappa", 0, 106, 42, 116),
					pdfTextCell(11, "lambda", 70, 106, 118, 116),
					pdfTextCell(12, "mu", 140, 106, 158, 116),
				},
			},
		},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables: true,
		TableDetection: doctable.DetectionOptions{
			MinRows:              4,
			MinCols:              2,
			RowTolerance:         6,
			ColumnTolerance:      12,
			TextOverlapThreshold: 0.3,
		},
	})

	require.NoError(t, err)
	require.Equal(t, "Alpha beta gamma\n\nDelta epsilon zeta\n\nEta theta iota\n\nKappa lambda mu", got)
}

func TestExtractMarkdownEmitsFontSizedHeadingByDefault(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "The Data Journey", 0, 0, 120, 18, 18),
				pdfTextCellWithFont(2, "Body text", 0, 44, 80, 54, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# The Data Journey\n\nBody text", got)
}

func TestExtractMarkdownLeavesBodySizedTopLineAsParagraph(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Opening paragraph", 0, 0, 120, 10, 10),
				pdfTextCellWithFont(2, "Body text", 0, 22, 80, 32, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "Opening paragraph\n\nBody text", got)
}

func TestExtractMarkdownMapsHeadingLevelsFromFontSizeBands(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Report Title", 0, 0, 140, 24, 24),
				pdfTextCellWithFont(2, "Findings", 0, 48, 90, 64, 16),
				pdfTextCellWithFont(3, "Body text", 0, 86, 80, 96, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# Report Title\n\n# Findings\n\nBody text", got)
}

func TestExtractMarkdownDetectsMultilineCoverTitleWhenTitleFontDominates(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "The Law Library of Congress", 156, 732, 455, 743, 10.5),
				pdfTextCellWithFont(2, "(202) 707-5080 • law@loc.gov", 182, 745, 430, 756, 10.5),
				pdfTextCellWithFont(3, "Restrictions on Land Ownership", 112, 274, 504, 300, 26),
				pdfTextCellWithFont(4, "by Foreigners in Selected", 155, 306, 461, 331, 26),
				pdfTextCellWithFont(5, "Jurisdictions", 228, 337, 379, 361, 26),
				pdfTextCellWithFont(6, "June 2023", 274, 396, 342, 410, 15),
				pdfTextCellWithFont(7, "LRA-D-PUB-002612", 258, 636, 354, 644, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "# Restrictions on Land Ownership by Foreigners in Selected Jurisdictions")
	require.NotContains(t, got, "# June 2023")
	require.NotContains(t, got, "# LRA-D-PUB-002612")
}

func TestExtractMarkdownKeepsCoverTitleLeadInAsParagraph(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Report Type:", 84, 305, 420, 363, 56),
				pdfTextCellWithFont(2, "Frontier Model", 84, 370, 468, 427, 56),
				pdfTextCellWithFont(3, "Preview", 84, 436, 288, 477, 56),
				pdfTextCellWithFont(4, "April 2026", 72, 632, 162, 648, 16),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# Report Type:")
	require.Contains(t, got, "Report Type: Frontier Model Preview")
}

func TestExtractMarkdownMergesAdjacentSameBandHeadingLines(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "The Data Jour", 0, 0, 116, 18, 18),
				pdfTextCellWithFont(2, "ney", 116, 19, 140, 37, 18),
				pdfTextCellWithFont(3, "Body text", 0, 66, 80, 76, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# The Data Journey\n\nBody text", got)
}

func TestExtractMarkdownMergesLowercaseWrappedHeadingLine(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Upstage offers 3 AI packs that process unstructured information and data,", 0, 40, 400, 58, 22),
				pdfTextCellWithFont(2, "making a tangible impact on your business", 0, 70, 260, 88, 22),
				pdfTextCellWithFont(3, "Body text", 0, 130, 80, 140, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# Upstage offers 3 AI packs that process unstructured information and data, making a tangible impact on your business\n\nBody text", got)
}

func TestExtractMarkdownMergesAlignedUppercaseWrappedHeadingLine(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Upstage aims to enrich your business by providing", 0, 40, 450, 58, 22),
				pdfTextCellWithFont(2, "Easy-to-Apply AI solutions", 0, 70, 230, 88, 22),
				pdfTextCellWithFont(3, "Body text", 0, 130, 80, 140, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# Upstage aims to enrich your business by providing Easy-to-Apply AI solutions\n\nBody text", got)
}

func TestExtractMarkdownDoesNotMergeStackedPeerHeadings(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Chapter 2", 0, 40, 80, 58, 22),
				pdfTextCellWithFont(2, "Narratives in Chuj", 0, 70, 180, 88, 22),
				pdfTextCellWithFont(3, "Body text", 0, 130, 80, 140, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# Chapter 2\n\n# Narratives in Chuj\n\nBody text", got)
}

func TestExtractMarkdownKeepsSplitHeadingOutOfDetectedTable(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "2. General", 0, 0, 58, 18, 18),
				pdfTextCellWithFont(2, "Profile of MSMEs", 64, 0, 160, 18, 18),
				pdfTextCellWithFont(3, "Name", 0, 42, 36, 52, 10),
				pdfTextCellWithFont(4, "Value", 100, 42, 140, 52, 10),
				pdfTextCellWithFont(5, "Ada", 0, 64, 28, 74, 10),
				pdfTextCellWithFont(6, "1", 100, 64, 108, 74, 10),
				pdfTextCellWithFont(7, "Bob", 0, 86, 28, 96, 10),
				pdfTextCellWithFont(8, "2", 100, 86, 108, 96, 10),
				pdfTextCellWithFont(9, "Cid", 0, 108, 28, 118, 10),
				pdfTextCellWithFont(10, "3", 100, 108, 108, 118, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# 2. General Profile of MSMEs\n\n| Name | Value |\n| ---- | ----: |\n| Ada  |     1 |\n| Bob  |     2 |\n| Cid  |     3 |", got)
}

func TestExtractMarkdownDetectsSameFontNumberedHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "12", 0, 40, 14, 50, 11),
				pdfTextCellWithFont(2, "Conclusion", 34, 39, 92, 51, 11),
				pdfTextCellWithFont(3, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# 12 Conclusion\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatTightImageCaptionTitleAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "16 Face Your World", 57, 318, 125, 325, 10),
				pdfTextCellWithFont(2, "A girl at work with the Interactor during the Face Your World participation process", 56, 331, 385, 340, 10),
				pdfTextCellWithFont(3, "Body text starts after the caption.", 56, 380, 220, 390, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "16 Face Your World A girl at work with the Interactor during the Face Your World participation process\n\nBody text starts after the caption.", got)
}

func TestExtractMarkdownDetectsShortBareNumberedHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "4 Al-Sadu Symbols and Social Significance", 0, 40, 240, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# 4 Al-Sadu Symbols and Social Significance\n\nBody text", got)
}

func TestExtractMarkdownDetectsSplitDottedNumberedHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "5.", 0, 40, 14, 50, 11),
				pdfTextCellWithFont(2, "Natural dispersal", 34, 39, 132, 51, 11),
				pdfTextCellWithFont(3, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# 5. Natural dispersal\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatDecimalMetricRowAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "0.3278 AutoEncoder", 0, 40, 120, 50, 12),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "0.3278 AutoEncoder\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatYearSentenceAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "2005. The program aims to help school.", 0, 40, 220, 50, 12),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "2005. The program aims to help school.\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatLongBareNumberedNoteAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "4 The argument positions of the Lagrangian are indicated by indices starting", 0, 40, 360, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "4 The argument positions of the Lagrangian are indicated by indices starting\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatBibliographyTitleLineAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 842},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Zheng Yuan, Hongyi Yuan, Chuanqi Tan, Wei Wang,", 70, 720, 290, 729, 9),
				pdfTextCellWithFont(2, "Songfang Huang, and Fei Huang. 2023. Rrhf:", 82, 731, 290, 740, 9),
				pdfTextCellWithFont(3, "Rank responses to align language models with", 82, 742, 289, 751, 9),
				pdfTextCellWithFont(4, "human feedback without tears. arXiv preprint", 82, 753, 289, 762, 9),
				pdfTextCellWithFont(5, "arXiv:2304.05302.", 82, 764, 156, 771, 9),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "Zheng Yuan, Hongyi Yuan, Chuanqi Tan, Wei Wang, Songfang Huang, and Fei Huang. 2023. Rrhf: Rank responses to align language models with human feedback without tears. arXiv preprint arXiv:2304.05302.", got)
}

func TestExtractMarkdownDoesNotTreatWrappedNumberedQuestionAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "10. Can you recall an example from your own life where you exhibited an Endowment Effect that", 68, 424, 555, 435, 11),
				pdfTextCellWithFont(2, "ultimately led to regret?", 90, 439, 210, 451, 11),
				pdfTextCellWithFont(3, "Body text", 68, 472, 120, 483, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# 10. Can you recall")
}

func TestExtractMarkdownDetectsSameFontAppendixHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "B.3 Prompt Engineering", 0, 40, 150, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# B.3 Prompt Engineering\n\nBody text", got)
}

func TestExtractMarkdownDetectsRomanNumeralHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "I. Introduction", 0, 40, 100, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# I. Introduction\n\nBody text", got)
}

func TestExtractMarkdownDetectsLowercaseLetterHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "n. In-store Sorting and Recycling Bins.", 0, 40, 210, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# n. In-store Sorting and Recycling Bins.\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatLowercaseLetterListItemAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "a. record the result after mixing.", 0, 40, 190, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "a. record the result after mixing.\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatLetteredBodyParagraphAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "a. Waste Management Fee Collection. At the Barangay level, only 5 respondents answered.", 0, 40, 420, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "a. Waste Management Fee Collection. At the Barangay level, only 5 respondents answered.\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatCompactLetteredAnswerListAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "b. The field of view becomes darker c. The size of the image increases", 0, 40, 390, 58, 18),
				pdfTextCellWithFont(2, "Body text", 0, 92, 120, 102, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "b. The field of view becomes darker c. The size of the image increases\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatLongLetteredNoteAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "v. Assumed selling price of wood pellet is $100 per tonne and appropriate.", 0, 40, 360, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "v. Assumed selling price of wood pellet is $100 per tonne and appropriate.\n\nBody text", got)
}

func TestExtractMarkdownDetectsHeadingWithinDetectedColumns(t *testing.T) {
	t.Parallel()

	var cells []page.TextCell
	cells = append(cells,
		pdfTextCellWithFont(1, "A Contributions", 70, 40, 165, 52, 12),
		pdfTextCellWithFont(2, "ability for in-context learning", 320, 40, 520, 52, 10),
	)
	index := 3
	for row := range 35 {
		y := 62 + float64(row)*14
		cells = append(cells,
			pdfTextCellWithFont(index, "left body text", 70, y, 250, y+10, 10),
			pdfTextCellWithFont(index+1, "right body text", 320, y, 520, y+10, 10),
		)
		index += 2
	}

	doc := fakeDocument{
		pages: []fakePage{{
			size:  geom.Size{Width: 600, Height: 800},
			cells: cells,
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "# A Contributions\n\n")
	require.NotContains(t, got, "# A Contributions ability for in-context learning")
}

func TestExtractMarkdownDetectsBoldColonHeading(t *testing.T) {
	t.Parallel()

	// A colon-terminated label is promoted only when visually distinguished. A
	// bold "Procedure:" qualifies; a same-font non-bold colon line is treated as
	// prose (a sentence lead-in), per the visual-analysis rule.
	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				{Index: 1, Text: "Procedure:", FontSize: 12, FontName: "Helvetica-Bold", Box: geom.Box{L: 0, T: 40, R: 70, B: 50, Origin: geom.TopLeft}},
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 12),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# Procedure:\n\nBody text", got)
}

func TestExtractMarkdownDetectsHeadingSharingBaselineWithOtherColumnBody(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "especially large for ARC and GSM8K.", 70, 620, 290, 630, 11),
				pdfTextCellWithFont(2, "5 Conclusion", 306, 621, 382, 629, 12),
				pdfTextCellWithFont(3, "Body text in the left column continues.", 70, 642, 290, 652, 11),
				pdfTextCellWithFont(4, "We introduce SOLAR 10.7B and its fine-tuned variant.", 306, 656, 526, 666, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "# 5 Conclusion")
}

func TestExtractMarkdownMergesSplitDecimalHeadingWithAlignedContinuation(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "9.5. Adapting to the New Normal: Changing", 94, 352, 320, 362, 10),
				pdfTextCellWithFont(2, "Moving into new products and services", 374, 352, 572, 362, 10),
				pdfTextCellWithFont(3, "Business Models", 95, 365, 171, 373, 10),
				pdfTextCellWithFont(4, "In all survey phases, several MSMEs changed their business models.", 95, 388, 320, 398, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "# 9.5. Adapting to the New Normal: Changing Business Models")
}

func TestExtractMarkdownDetectsVeryLargeSplitHeadingBesideOffPagePeer(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "1. Introduction and Methodology", -503, 74, -115, 101, 29),
				pdfTextCellWithFont(2, "2. General Profile of MSMEs", 161, 74, 499, 96, 29),
				pdfTextCellWithFont(3, "In July 2020, the survey established a general profile", 41, 125, 266, 134, 9.5),
				pdfTextCellWithFont(4, "Business characteristics.", 276, 125, 393, 133, 9.5),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "# 2. General Profile of MSMEs")
	require.NotContains(t, got, "# 1. Introduction and Methodology")
}

func TestExtractMarkdownUsesProseMetricWhenChartLabelsDominate(t *testing.T) {
	t.Parallel()

	cells := []page.TextCell{
		pdfTextCellWithFont(1, "6.2. Expectations for Re-Hiring Employees", 95, 593, 283, 602, 9.5),
		pdfTextCellWithFont(2, "they had no plans to re-hire and another 36% said", 329, 593, 554, 602, 9.5),
		pdfTextCellWithFont(3, "they didn't know whether they would re-hire or not. In", 329, 605, 554, 614, 9.5),
		pdfTextCellWithFont(4, "In July 2020, 81% of the MSMEs that had laid off", 95, 617, 320, 626, 9.5),
		pdfTextCellWithFont(5, "January 2021, 20% said they had no plans to re-hire", 329, 617, 554, 626, 9.5),
	}
	for i := range 30 {
		x := 140 + float64(i%10)*28
		y := 360 + float64(i/10)*24
		cells = append(cells, pdfTextCellWithFont(6+i, "20", x, y, x+8, y+9, 7))
	}

	doc := fakeDocument{
		pages: []fakePage{{
			size:  geom.Size{Width: 600, Height: 800},
			cells: cells,
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "# 6.2. Expectations for Re-Hiring Employees")
	require.NotContains(t, got, "# In July 2020")
}

func TestExtractMarkdownDropsSmallFigureInternalLabelsFromReadingOrder(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Waste Management Budget paragraph.", 135, 337, 512, 348, 11),
				pdfTextCellWithFont(2, "Waste Collection and Segregation paragraph.", 135, 675, 530, 685, 11),
				pdfTextCellWithFont(3, "Figure 20. Percentage of LGU Budget Allocated for Waste Management", 195, 635, 450, 644, 9),
				pdfTextCellWithFont(4, "44%", 254, 489, 269, 495, 8),
				pdfTextCellWithFont(5, "Below 5% of the LGU budget", 355, 491, 473, 501, 10),
				pdfTextCellWithFont(6, "12%", 183, 541, 198, 547, 8),
				pdfTextCellWithFont(7, "No Allocation", 355, 573, 410, 581, 10),
				pdfTextCellWithFont(8, "I don't know", 355, 593, 406, 601, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		ReadingOrder: true,
		DetectTables: false,
	})

	require.NoError(t, err)
	require.Equal(t, "Waste Management Budget paragraph.\n\nFigure 20. Percentage of LGU Budget Allocated for Waste Management\n\nWaste Collection and Segregation paragraph.", got)
}

func TestExtractMarkdownDoesNotPromoteSplitRunningHeaderFragment(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Answer the following questions.", 70, 34, 250, 44, 10),
				pdfTextCellWithFont(2, "MOHAVE COMMUNITY COLLEGE", 326, 33, 520, 45, 12),
				pdfTextCellWithFont(3, "Use complete sentences in your response.", 70, 92, 300, 102, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# MOHAVE COMMUNITY COLLEGE")
}

func TestExtractMarkdownDoesNotPromoteSplitTableHeaderFragment(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "PORT", 70, 120, 110, 132, 12),
				pdfTextCellWithFont(2, "SHIPCALLS", 312, 120, 390, 132, 12),
				pdfTextCellWithFont(3, "MANILA", 70, 150, 130, 162, 10),
				pdfTextCellWithFont(4, "12", 312, 150, 330, 162, 10),
				pdfTextCellWithFont(5, "BATANGAS", 70, 180, 145, 192, 10),
				pdfTextCellWithFont(6, "4", 312, 180, 322, 192, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# SHIPCALLS")
}

func TestExtractMarkdownDoesNotPromoteSplitNumericChartLabel(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Percent of respondents", 70, 220, 190, 232, 10),
				pdfTextCellWithFont(2, "7 - Year", 334, 220, 390, 232, 12),
				pdfTextCellWithFont(3, "More survey text follows below.", 70, 278, 250, 288, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# 7 - Year")
}

func TestExtractMarkdownDetectsIsolatedTitleCaseHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Functional Abstraction", 0, 40, 130, 50, 12),
				pdfTextCellWithFont(2, "Body text starts here", 0, 78, 150, 88, 11),
				pdfTextCellWithFont(3, "and continues here", 0, 92, 140, 102, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# Functional Abstraction\n\nBody text starts here and continues here", got)
}

func TestExtractMarkdownDoesNotTreatNonIsolatedTitleCaseLineAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Body text starts here", 0, 40, 150, 50, 11),
				pdfTextCellWithFont(2, "Functional Abstraction appears inline", 0, 54, 220, 64, 12),
				pdfTextCellWithFont(3, "and continues here", 0, 68, 140, 78, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "Body text starts here Functional Abstraction appears inline and continues here", got)
}

func TestExtractMarkdownDoesNotTreatCentredAffiliationAsIsolatedTitleHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Upstage AI, South Korea", 240, 120, 360, 132, 12),
				pdfTextCellWithFont(2, "{team}@upstage.ai", 40, 148, 230, 158, 11),
				pdfTextCellWithFont(3, "Body text starts here", 40, 188, 190, 198, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# Upstage AI, South Korea")
}

func TestExtractMarkdownDoesNotTreatTitleCaseLabelBeforeShortPeerLabelAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Learning Outcomes", 76, 120, 172, 132, 12),
				pdfTextCellWithFont(2, "Knowledge", 76, 164, 132, 176, 12),
				pdfTextCellWithFont(3, "To understand the material", 246, 164, 420, 174, 10.5),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# Learning Outcomes")
}

func TestExtractMarkdownDoesNotTreatSparseMultiColumnTitleRowAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 960, Height: 540},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "OCR", 240, 120, 260, 130, 12),
				pdfTextCellWithFont(2, "Recommendation", 466, 120, 551, 130, 12),
				pdfTextCellWithFont(3, "Product semantic search", 733, 120, 852, 130, 12),
				pdfTextCellWithFont(4, "A solution that recognises characters in an image", 142, 160, 430, 170, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# OCR Recommendation Product semantic search")
}

func TestExtractMarkdownDoesNotTreatShortAllCapsAcronymAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "SOLAR.", 0, 40, 60, 50, 11),
				pdfTextCellWithFont(2, "IEEE.", 0, 72, 50, 82, 11),
				pdfTextCellWithFont(3, "Body text", 0, 104, 120, 114, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "SOLAR.\n\nIEEE.\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatCourseCodeRunningHeaderAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "MOHAVE COMMUNITY COLLEGE BIO181", 0, 32, 220, 44, 12),
				pdfTextCellWithFont(2, "Body text", 0, 120, 120, 130, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "MOHAVE COMMUNITY COLLEGE BIO181\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatMultiEntryContentsLineAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Table of contents", 0, 32, 120, 44, 12),
				pdfTextCellWithFont(2, "1. Changing Practices, Shifting Sites 2. Core and Periphery of Play", 0, 72, 360, 84, 11),
				pdfTextCellWithFont(3, "Body text", 0, 120, 120, 130, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	// The multi-entry contents line ("1. … 2. …") must stay body text, not be
	// promoted to a heading. "Table of contents" is a 3-word line only 1.09x body
	// size, so with the section-name keyword map removed it is no longer promoted
	// by visual analysis alone — an accepted trade-off of AGENTS.md §11 compliance
	// (multi-word same-size section names are the class that still needs a
	// table-safe geometric signal).
	require.Equal(t, "Table of contents\n\n1. Changing Practices, Shifting Sites 2. Core and Periphery of Play\n\nBody text", got)
}

func TestExtractMarkdownDoesNotPromoteContentsPageEntriesAsHeadings(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Contents", 0, 32, 80, 44, 12),
				pdfTextCellWithFont(2, "1. Overview of OCR Pack", 0, 72, 160, 84, 11),
				pdfTextCellWithFont(3, "2. Product - Detail Specification", 0, 90, 220, 102, 11),
				pdfTextCellWithFont(4, "Body text", 0, 140, 120, 150, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	// The contents-page ENTRIES must not be promoted to headings; the "Contents"
	// title IS a heading (section-name keyword co-occurring with 1.09x near-
	// prominence, per the additive heading score).
	require.Equal(t, "# Contents\n\n1. Overview of OCR Pack 2. Product - Detail Specification\n\nBody text", got)
}

func TestExtractMarkdownDoesNotPromoteDenseIndexEntriesAsHeadings(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 600, Height: 800},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Preface", 70, 72, 118, 84, 13),
				pdfTextCellWithFont(2, "7", 510, 72, 516, 82, 11),
				pdfTextCellWithFont(3, "1 Introduction", 70, 86, 154, 98, 13),
				pdfTextCellWithFont(4, "10", 506, 86, 518, 96, 11),
				pdfTextCellWithFont(5, "1.1 Background", 88, 100, 178, 112, 13),
				pdfTextCellWithFont(6, "12", 506, 100, 518, 110, 11),
				pdfTextCellWithFont(7, "1.2 Method", 88, 114, 154, 126, 13),
				pdfTextCellWithFont(8, "14", 506, 114, 518, 124, 11),
				pdfTextCellWithFont(9, "2 Results", 70, 128, 130, 140, 13),
				pdfTextCellWithFont(10, "19", 506, 128, 518, 138, 11),
				pdfTextCellWithFont(11, "The narrative begins here with normal prose.", 70, 172, 310, 182, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdownWithOptions(context.Background(), doc, pdf.ExtractionOptions{
		DetectTables:    false,
		ReadingOrder:    true,
		DetectStructure: true,
		DetectHeadings:  true,
	})

	require.NoError(t, err)
	require.NotContains(t, got, "# 1 Introduction")
	require.NotContains(t, got, "# 1.1 Background")
	require.NotContains(t, got, "# 2 Results")
	require.Contains(t, got, "1 Introduction")
	require.Contains(t, got, "The narrative begins here with normal prose.")
}

func TestExtractMarkdownDoesNotDetectDenseIndexEntriesAsTable(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			size: geom.Size{Width: 612, Height: 792},
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Alpha", 72.04, 77.76, 116.86, 86.23, 11),
				pdfTextCellWithFont(2, "3", 534.23, 78.19, 539.56, 86.23, 11),
				pdfTextCellWithFont(3, "1 Introduction", 72.07, 94.84, 146.56, 103.31, 11),
				pdfTextCellWithFont(4, "10", 528.61, 95.27, 539.52, 103.31, 11),
				pdfTextCellWithFont(5, "1", 90.20, 112.35, 93.62, 120.23, 11),
				pdfTextCellWithFont(6, ".", 94.71, 118.98, 95.99, 120.39, 11),
				pdfTextCellWithFont(7, "1 Model training and characteristics", 96.90, 111.92, 278.60, 123.20, 11),
				pdfTextCellWithFont(8, "\u200b", 279.04, 120.21, 281.79, 120.23, 11),
				pdfTextCellWithFont(9, "11", 532.20, 112.35, 539.62, 120.23, 11),
				pdfTextCellWithFont(10, "1", 108.20, 129.43, 111.62, 137.31, 11),
				pdfTextCellWithFont(11, ".", 112.71, 136.06, 113.99, 137.47, 11),
				pdfTextCellWithFont(12, "1", 114.90, 129.43, 118.32, 137.31, 11),
				pdfTextCellWithFont(13, ".", 119.41, 136.06, 120.70, 137.47, 11),
				pdfTextCellWithFont(14, "1 Training data and process", 121.61, 129.00, 259.97, 140.28, 11),
				pdfTextCellWithFont(15, "\u200b", 260.42, 137.29, 263.17, 137.31, 11),
				pdfTextCellWithFont(16, "11", 532.20, 129.43, 539.62, 137.31, 11),
				pdfTextCellWithFont(17, "2 Results", 72.43, 146.08, 164.61, 154.55, 11),
				pdfTextCellWithFont(18, "16", 529.28, 146.51, 539.53, 154.55, 11),
				pdfTextCellWithFont(19, "The narrative begins here with normal prose.", 72.43, 190, 320, 201, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "|")
	require.NotContains(t, got, "# 1 Introduction")
	require.Contains(t, got, "1 Introduction")
	require.Contains(t, got, "The narrative begins here with normal prose.")
}

func TestExtractMarkdownKeepsHyphenatedNumberInHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "1.5. Migrant Workers More at Risk of COVID-19 Infection", 0, 40, 320, 52, 11),
				pdfTextCellWithFont(2, "Body text", 0, 84, 120, 94, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# 1.5. Migrant Workers More at Risk of COVID-19 Infection\n\nBody text", got)
}

func TestExtractMarkdownMergesWrappedLongDottedNumberedHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "3.", 86, 88, 94, 98, 13),
				pdfTextCellWithFont(2, "Perspective of supply and demand balance of wood pellets and cost", 114, 88, 484, 100, 13),
				pdfTextCellWithFont(3, "structure in Japan", 114, 107, 209, 118, 13),
				pdfTextCellWithFont(4, "According to a survey taken by the association", 86, 130, 484, 140, 11.5),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "# 3. Perspective of supply and demand balance of wood pellets and cost structure in Japan\n\nAccording to a survey taken by the association", got)
}

func TestExtractMarkdownKeepsDecimalHeadingBeforeBareNumberedPeerLabel(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "6.", 0, 40, 14, 50, 18),
				pdfTextCellWithFont(2, "ECO CIRCLE COMPETENCE FRAMEWORK", 34, 40, 260, 50, 18),
				pdfTextCellWithFont(3, "Competence Area #1 THE 3 RS: RECYCLE", 0, 88, 260, 100, 14),
				pdfTextCellWithFont(4, "Body text", 0, 136, 120, 146, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Contains(t, got, "# 6. ECO CIRCLE COMPETENCE FRAMEWORK")
}

func TestExtractMarkdownDoesNotTreatOrderedListItemAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "1.", 0, 40, 12, 50, 12),
				pdfTextCellWithFont(2, "Record the Question that is being investigated in this experiment.", 32, 39, 360, 51, 12),
				pdfTextCellWithFont(3, "Body text", 0, 80, 120, 90, 12),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "1. Record the Question that is being investigated in this experiment.\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatSplitNumberedProcedureStepAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "steps:", 0, 40, 36, 50, 11),
				pdfTextCellWithFont(2, "1.", 0, 72, 12, 82, 11),
				pdfTextCellWithFont(3, "Label four plastic bags", 28, 72, 150, 82, 11),
				pdfTextCellWithFont(4, "2. Weigh soil into each bag.", 0, 104, 190, 114, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.NotContains(t, got, "# 1. Label four plastic bags")
}

func TestExtractMarkdownDoesNotTreatLowercaseNumericFootnoteAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "2 architecture as our base model.", 0, 40, 180, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "2 architecture as our base model.\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatLowercaseColonFragmentAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "as 'Step 1:", 0, 40, 90, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "as 'Step 1:\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatArticleTitleAsAppendixHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "A Fountain in the Square", 0, 40, 150, 50, 11),
				pdfTextCellWithFont(2, "Body text", 0, 80, 120, 90, 11),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "A Fountain in the Square\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatNumericChartLabelAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "2016 - 2020 .", 0, 40, 90, 56, 18),
				pdfTextCellWithFont(2, "The Data Journey", 0, 80, 120, 98, 18),
				pdfTextCellWithFont(3, "Body text", 0, 124, 120, 134, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "2016 - 2020.\n\n# The Data Journey\n\nBody text", got)
}

func TestExtractMarkdownDoesNotTreatFigureCaptionAsHeading(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages: []fakePage{{
			cells: []page.TextCell{
				pdfTextCellWithFont(1, "Figure 6.1.1: Will they fire more staff?", 0, 40, 240, 54, 14),
				pdfTextCellWithFont(2, "6.2. Expectations for Re-Hiring Employees", 0, 90, 260, 104, 14),
				pdfTextCellWithFont(3, "Body text", 0, 132, 120, 142, 10),
			},
		}},
	}

	got, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.NoError(t, err)
	require.Equal(t, "Figure 6.1.1: Will they fire more staff?\n\n# 6.2. Expectations for Re-Hiring Employees\n\nBody text", got)
}

func TestExtractMarkdownWithOptionsProcessesPagesConcurrentlyInOrder(t *testing.T) {
	var mu sync.Mutex
	var once sync.Once
	inFlight := 0
	twoPagesRunning := make(chan struct{})

	blockUntilPeer := func(ctx context.Context, cells []page.TextCell) ([]page.TextCell, error) {
		mu.Lock()
		inFlight++
		if inFlight == 2 {
			once.Do(func() { close(twoPagesRunning) })
		}
		mu.Unlock()

		select {
		case <-twoPagesRunning:
			return cells, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	doc := fakeDocument{
		pages: []fakePage{
			{
				textCells: func(ctx context.Context) ([]page.TextCell, error) {
					return blockUntilPeer(ctx, []page.TextCell{
						pdfTextCell(1, "First", 0, 0, 40, 10),
					})
				},
			},
			{
				textCells: func(ctx context.Context) ([]page.TextCell, error) {
					return blockUntilPeer(ctx, []page.TextCell{
						pdfTextCell(1, "Second", 0, 0, 40, 10),
					})
				},
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, err := pdf.ExtractMarkdownWithOptions(ctx, doc, pdf.ExtractionOptions{
		DetectTables:     false,
		ReadingOrder:     false,
		MaxParallelPages: 2,
	})

	require.NoError(t, err)
	require.Equal(t, "First\n\nSecond", got)
}

func TestExtractMarkdownReturnsPageErrors(t *testing.T) {
	t.Parallel()

	doc := fakeDocument{
		pages:   []fakePage{{}},
		pageErr: errFake,
	}

	_, err := pdf.ExtractMarkdown(context.Background(), doc)

	require.ErrorIs(t, err, errFake)
}

type fakeDocument struct {
	pages   []fakePage
	pageErr error
}

func (d fakeDocument) PageCount(context.Context) (int, error) {
	return len(d.pages), nil
}

func (d fakeDocument) Page(_ context.Context, index int) (pdf.Page, error) {
	if d.pageErr != nil {
		return nil, d.pageErr
	}
	return d.pages[index], nil
}

func (d fakeDocument) Close() error {
	return nil
}

type fakePage struct {
	size       geom.Size
	cells      []page.TextCell
	wordCells  []page.TextCell
	formFields []page.FormField
	textCells  func(context.Context) ([]page.TextCell, error)
}

func (p fakePage) Size(context.Context) (geom.Size, error) {
	if p.size.Width > 0 || p.size.Height > 0 {
		return p.size, nil
	}
	return geom.Size{Width: 100, Height: 200}, nil
}

func (p fakePage) TextCells(ctx context.Context) ([]page.TextCell, error) {
	if p.textCells != nil {
		return p.textCells(ctx)
	}
	return append([]page.TextCell(nil), p.cells...), nil
}

func (p fakePage) WordTextCells(context.Context) ([]page.TextCell, error) {
	return append([]page.TextCell(nil), p.wordCells...), nil
}

func (p fakePage) FormFields(context.Context) ([]page.FormField, error) {
	return append([]page.FormField(nil), p.formFields...), nil
}

func (p fakePage) TextInRect(context.Context, geom.Box) (string, error) {
	return "", nil
}

type fakeError struct{}

func (fakeError) Error() string { return "fake error" }

var errFake fakeError

func pdfTextCell(index int, text string, l, t, r, b float64) page.TextCell {
	return pdfTextCellWithFont(index, text, l, t, r, b, 0)
}

func pdfTextCellWithFont(index int, text string, l, t, r, b, fontSize float64) page.TextCell {
	return page.TextCell{
		Index:    index,
		Text:     text,
		FontSize: fontSize,
		Box:      geom.Box{L: l, T: t, R: r, B: b, Origin: geom.TopLeft},
	}
}
