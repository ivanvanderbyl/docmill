package parser_test

import (
	"context"
	"os"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// benchPDFPath returns the file to profile; override with BENCH_PDF (e.g. a
// large external PDF). Defaults to a corpus PDF so the benchmark is reproducible.
func benchPDFPath() string {
	if p := os.Getenv("BENCH_PDF"); p != "" {
		return p
	}
	return "../../.upstream-docling/tests/data/pdf/redp5110_sampled.pdf"
}

// BenchmarkExtractMarkdown profiles the full native pipeline (parse + per-page
// text extraction + markdown assembly) on a real PDF.
func BenchmarkExtractMarkdown(b *testing.B) {
	data, err := os.ReadFile(benchPDFPath())
	if err != nil {
		b.Skipf("bench PDF unavailable: %v", err)
	}
	backend := parser.NewBackend()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, err := backend.OpenBytes(ctx, data)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		md, err := docpdf.ExtractMarkdown(ctx, doc)
		if err != nil {
			b.Fatalf("extract: %v", err)
		}
		_ = doc.Close()
		if i == 0 {
			b.Logf("markdown bytes=%d", len(md))
		}
	}
}

// BenchmarkOpenAndCount profiles just parse + page-tree (no text extraction).
func BenchmarkOpenAndCount(b *testing.B) {
	data, err := os.ReadFile(benchPDFPath())
	if err != nil {
		b.Skipf("bench PDF unavailable: %v", err)
	}
	backend := parser.NewBackend()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doc, err := backend.OpenBytes(ctx, data)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		n, _ := doc.PageCount(ctx)
		_ = doc.Close()
		if i == 0 {
			b.Logf("pages=%d", n)
		}
	}
}

func TestExtractMarkdownParallelMatchesSerialBenchPDF(t *testing.T) {
	path := os.Getenv("BENCH_PDF")
	if path == "" {
		t.Skip("set BENCH_PDF to compare serial and parallel extraction")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read PDF: %v", err)
	}

	ctx := context.Background()
	backend := parser.NewBackend()

	serialDoc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		t.Fatalf("open serial: %v", err)
	}
	serial, err := docpdf.ExtractMarkdownWithOptions(ctx, serialDoc, docpdf.ExtractionOptions{
		DetectTables:     true,
		ReadingOrder:     true,
		MaxParallelPages: 1,
	})
	if closeErr := serialDoc.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("serial extract: %v", err)
	}

	parallelDoc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		t.Fatalf("open parallel: %v", err)
	}
	parallel, err := docpdf.ExtractMarkdown(ctx, parallelDoc)
	if closeErr := parallelDoc.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("parallel extract: %v", err)
	}

	if serial != parallel {
		t.Fatalf("parallel markdown differs from serial: serial bytes=%d parallel bytes=%d", len(serial), len(parallel))
	}
}
