package parser_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
)

// fragmentedPDF hand-assembles a single-page PDF whose text is emitted as one
// text object per word — the fragmented-generator profile that makes
// MergeFragmentedCells re-extract nearly every line. lines*words fragments
// merge back into ~lines cells.
func fragmentedPDF(lines, words, wordLen int) []byte {
	var content bytes.Buffer
	for l := range lines {
		y := 760 - 8*l
		for w := range words {
			x := 40 + w*27
			word := strings.Repeat(string(rune('a'+(l+w)%26)), wordLen)
			fmt.Fprintf(&content, "BT /F1 8 Tf %d %d Td (%s) Tj ET\n", x, y, word)
		}
	}

	var buf bytes.Buffer
	offsets := make([]int, 0, 6)
	addObj := func(body string) {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	buf.WriteString("%PDF-1.4\n")
	addObj("<< /Type /Catalog /Pages 2 0 R >>")
	addObj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	addObj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 6 0 R >>")
	widths := strings.TrimSuffix(strings.Repeat("500 ", 95), " ")
	addObj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 32 /LastChar 126 /Widths [" + widths + "] /FontDescriptor 5 0 R >>")
	addObj("<< /Type /FontDescriptor /FontName /Helvetica /Flags 32 /FontBBox [-166 -225 1000 931] /ItalicAngle 0 /Ascent 718 /Descent -207 /CapHeight 718 /StemV 88 >>")
	addObj(fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", content.Len(), content.String()))
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return buf.Bytes()
}

// TestFragmentedFixtureParses guards the hand-assembled fixture: the fragments
// must survive extraction and merge down to roughly one cell per line.
func TestFragmentedFixtureParses(t *testing.T) {
	t.Parallel()
	const lines, words = 90, 15
	data := fragmentedPDF(lines, words, 6)
	ctx := context.Background()
	doc, err := parser.NewBackend().OpenBytes(ctx, data)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer doc.Close()
	pg, err := doc.Page(ctx, 0)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	cells, err := pg.TextCells(ctx)
	if err != nil {
		t.Fatalf("text cells: %v", err)
	}
	if len(cells) == 0 {
		t.Fatal("no text cells extracted from fixture")
	}
	if len(cells) > lines*2 {
		t.Fatalf("got %d cells for %d lines; fragments did not merge", len(cells), lines)
	}
}

// BenchmarkTextCellsFragmented measures the full TextCells path (parse + char
// stream + rects + fragment merge) on the fragmented fixture. This is the
// production shape behind slow pipeline.text_cells spans.
func BenchmarkTextCellsFragmented(b *testing.B) {
	data := fragmentedPDF(90, 15, 6)
	ctx := context.Background()
	backend := parser.NewBackend()
	b.ReportAllocs()
	for b.Loop() {
		doc, err := backend.OpenBytes(ctx, data)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		pg, err := doc.Page(ctx, 0)
		if err != nil {
			b.Fatalf("page: %v", err)
		}
		if _, err := pg.TextCells(ctx); err != nil {
			b.Fatalf("text cells: %v", err)
		}
		_ = doc.Close()
	}
}
