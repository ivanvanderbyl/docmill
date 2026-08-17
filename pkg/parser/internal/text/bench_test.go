package text

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/page"
)

// benchFragmentedPage builds a TextPage with the pathological profile seen in
// production traces: many text lines over a large char list. Returned boxes are
// the line boxes MergeFragmentedCells would re-extract (one per merged group).
func benchFragmentedPage(lines, charsPerLine int) (*TextPage, []crt.FloatRect) {
	chars := make([]charInfo, 0, lines*charsPerLine)
	boxes := make([]crt.FloatRect, 0, lines)
	for line := range lines {
		to := &page.TextObject{}
		bottom := float32(line * 14)
		top := bottom + 12
		for i := range charsPerLine {
			left := float32(i * 6)
			ci := normalChar(to, box(left, bottom, left+5, top), rune('a'+i%26))
			ci.origin.Y = bottom
			chars = append(chars, ci)
		}
		boxes = append(boxes, box(-1, bottom-1, float32(charsPerLine*6)+1, top+1))
	}
	return tpWith(chars), boxes
}

// BenchmarkMergedTextScalar measures the pre-batching MergeFragmentedCells
// re-extraction: one full GetTextByRect char-list scan per merged group.
func BenchmarkMergedTextScalar(b *testing.B) {
	tp, boxes := benchFragmentedPage(250, 80)
	b.ReportAllocs()
	for b.Loop() {
		for _, bx := range boxes {
			_ = tp.GetTextByRect(bx)
		}
	}
}

// BenchmarkMergedTextBatched measures the batched replacement: one banded
// GetTextByRects pass covering every merged group's box.
func BenchmarkMergedTextBatched(b *testing.B) {
	tp, boxes := benchFragmentedPage(250, 80)
	b.ReportAllocs()
	for b.Loop() {
		_ = tp.GetTextByRects(boxes)
	}
}
