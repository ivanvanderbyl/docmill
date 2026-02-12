package pdfmarkdown

import (
	"math"
	"sort"
	"strings"
	"testing"
)

// TestLineGrouping_ListItemScenario tests baseline-aware line grouping with
// a list item that spans multiple visual elements on the same line.
func TestLineGrouping_ListItemScenario(t *testing.T) {
	words := []EnrichedWord{
		// Line 1: "Your current holdings are structured across the following entities:"
		{Text: "Your", Box: Rect{X0: 72.03, Y0: 250.66, X1: 94.37, Y1: 258.78}, Baseline: 256.58, FontSize: 14.67, XHeight: 8.0},
		{Text: "current", Box: Rect{X0: 97.69, Y0: 250.85, X1: 131.40, Y1: 258.78}, Baseline: 256.58, FontSize: 14.67, XHeight: 8.0},
		{Text: "holdings", Box: Rect{X0: 135.25, Y0: 250.66, X1: 175.05, Y1: 260.95}, Baseline: 258.75, FontSize: 14.67, XHeight: 8.0},
		{Text: "are", Box: Rect{X0: 178.93, Y0: 252.74, X1: 193.97, Y1: 258.78}, Baseline: 256.58, FontSize: 14.67, XHeight: 8.0},
		{Text: "structured", Box: Rect{X0: 197.81, Y0: 250.66, X1: 245.56, Y1: 258.78}, Baseline: 256.58, FontSize: 14.67, XHeight: 8.0},
		{Text: "across", Box: Rect{X0: 249.79, Y0: 252.74, X1: 281.35, Y1: 258.78}, Baseline: 256.58, FontSize: 14.67, XHeight: 8.0},
		{Text: "the", Box: Rect{X0: 285.03, Y0: 250.66, X1: 299.66, Y1: 258.78}, Baseline: 256.58, FontSize: 14.67, XHeight: 8.0},
		{Text: "following", Box: Rect{X0: 303.26, Y0: 250.53, X1: 345.20, Y1: 260.95}, Baseline: 258.75, FontSize: 14.67, XHeight: 8.0},
		{Text: "entities:", Box: Rect{X0: 349.40, Y0: 250.66, X1: 385.91, Y1: 258.78}, Baseline: 256.58, FontSize: 14.67, XHeight: 8.0},

		// Line 2: "1. Acme Group (AG-001) - Holding entity for asset management"
		{Text: "1.", Box: Rect{X0: 91.20, Y0: 277.17, X1: 98.21, Y1: 285.20}, Baseline: 283.00, FontSize: 14.67, XHeight: 8.0},
		{Text: "Acme", Box: Rect{X0: 108.40, Y0: 277.07, X1: 137.79, Y1: 285.33}, Baseline: 283.13, FontSize: 14.67, XHeight: 8.0},
		{Text: "Group", Box: Rect{X0: 142.40, Y0: 277.20, X1: 176.24, Y1: 287.50}, Baseline: 285.30, FontSize: 14.67, XHeight: 8.0},
		{Text: "Entity", Box: Rect{X0: 179.70, Y0: 277.20, X1: 206.21, Y1: 285.32}, Baseline: 283.12, FontSize: 14.67, XHeight: 8.0},
		// Separator hyphen far to the right
		{Text: "-", Box: Rect{X0: 280.62, Y0: 281.82, X1: 283.59, Y1: 282.84}, Baseline: 280.64, FontSize: 14.67, XHeight: 0.6},
		// Continuation: "(AG-001) Holding entity for asset management"
		{Text: "(AG-001)", Box: Rect{X0: 210.06, Y0: 277.07, X1: 276.82, Y1: 287.50}, Baseline: 285.30, FontSize: 14.67, XHeight: 8.0},
		{Text: "Holding", Box: Rect{X0: 287.83, Y0: 277.20, X1: 351.03, Y1: 287.50}, Baseline: 285.30, FontSize: 14.67, XHeight: 8.0},
		{Text: "entity", Box: Rect{X0: 354.37, Y0: 277.39, X1: 375.48, Y1: 285.32}, Baseline: 283.12, FontSize: 14.67, XHeight: 8.0},
		{Text: "for", Box: Rect{X0: 378.71, Y0: 277.07, X1: 391.59, Y1: 285.32}, Baseline: 283.12, FontSize: 14.67, XHeight: 8.0},
		{Text: "asset", Box: Rect{X0: 395.21, Y0: 277.20, X1: 429.48, Y1: 285.32}, Baseline: 283.12, FontSize: 14.67, XHeight: 8.0},
		{Text: "management", Box: Rect{X0: 433.35, Y0: 277.20, X1: 485.38, Y1: 285.32}, Baseline: 283.12, FontSize: 14.67, XHeight: 8.0},
	}

	t.Logf("=== BEFORE SORTING ===")
	for i, w := range words {
		t.Logf("%d: %q at X=%.2f Y=(%.2f-%.2f)", i, w.Text, w.Box.X0, w.Box.Y0, w.Box.Y1)
	}

	sort.Slice(words, func(i, j int) bool {
		wordI := words[i]
		wordJ := words[j]

		overlapY0 := math.Max(wordI.Box.Y0, wordJ.Box.Y0)
		overlapY1 := math.Min(wordI.Box.Y1, wordJ.Box.Y1)
		overlapHeight := overlapY1 - overlapY0
		minHeight := math.Min(wordI.Box.Height(), wordJ.Box.Height())

		if overlapHeight > minHeight*0.3 {
			return wordI.Box.X0 < wordJ.Box.X0
		}

		return wordI.Box.Y0 < wordJ.Box.Y0
	})

	t.Logf("\n=== AFTER SORTING ===")
	for i, w := range words {
		t.Logf("%d: %q at X=%.2f Y=(%.2f-%.2f)", i, w.Text, w.Box.X0, w.Box.Y0, w.Box.Y1)
	}

	lines := groupWordsIntoLinesBaseline(words)

	t.Logf("\n=== AFTER GROUPING ===")
	t.Logf("Total lines: %d", len(lines))

	for li, line := range lines {
		lineText := ""
		for _, w := range line.Words {
			lineText += w.Text + " "
		}
		t.Logf("Line %d: %q", li, lineText)
	}

	if len(lines) != 2 {
		t.Errorf("Expected 2 lines, got %d", len(lines))
		for i, line := range lines {
			lineText := ""
			for _, w := range line.Words {
				lineText += w.Text + " "
			}
			t.Logf("  Line %d (%d words): %s", i, len(line.Words), lineText)
		}
	}

	if len(lines) >= 2 {
		listLine := lines[1]
		listText := ""
		for _, w := range listLine.Words {
			listText += w.Text + " "
		}

		if !strings.Contains(listText, "1.") || !strings.Contains(listText, "Entity") ||
			!strings.Contains(listText, "(AG-001)") || !strings.Contains(listText, "management") {
			t.Errorf("List item incomplete: %q", listText)
		} else {
			t.Logf("✓ List item correctly grouped: %q", strings.TrimSpace(listText))
		}
	}
}
