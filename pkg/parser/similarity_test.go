package parser_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// TestNativeMarkdownSimilarityToGolden is a tier-5-style quality gate for the
// native (pure-Go) backend: it extracts Markdown for every corpus PDF and
// compares it to the plan-004 golden snapshots (generated from real PDFium) via
// token Jaccard. Byte-identical parity is out of reach while the font port is
// face-less (descriptor glyph boxes, not rasterised outlines — see plan 009
// F/G/H/I notes), so this asserts a mean floor and logs per-PDF scores to track
// regressions and progress. Clean-text PDFs already score ~0.9+.
func TestNativeMarkdownSimilarityToGolden(t *testing.T) {
	root := repoRoot(t)
	pdfDir := filepath.Join(root, ".upstream-docling", "tests", "data", "pdf")
	goldDir := filepath.Join(root, "pkg", "parser", "testdata", "golden")
	if _, err := os.Stat(pdfDir); err != nil {
		t.Skipf("corpus not available: %v", err)
	}
	entries, err := filepath.Glob(filepath.Join(pdfDir, "*.pdf"))
	if err != nil || len(entries) == 0 {
		t.Skip("no corpus PDFs")
	}
	sort.Strings(entries)

	backend := parser.NewBackend()
	var sum float64
	var n int
	for _, p := range entries {
		base := strings.TrimSuffix(filepath.Base(p), ".pdf")
		gold, err := os.ReadFile(filepath.Join(goldDir, base+".md"))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		doc, err := backend.OpenBytes(context.Background(), data)
		if err != nil {
			t.Errorf("%s: native open: %v", base, err)
			continue
		}
		got, err := docpdf.ExtractMarkdown(context.Background(), doc)
		_ = doc.Close()
		if err != nil {
			t.Errorf("%s: native ExtractMarkdown: %v", base, err)
			continue
		}
		s := tokenJaccard(string(gold), got)
		t.Logf("similarity %-30s %.3f", base, s)
		sum += s
		n++
	}
	if n == 0 {
		t.Skip("no golden snapshots paired with corpus PDFs")
	}
	mean := sum / float64(n)
	t.Logf("MEAN native-vs-golden Jaccard over %d PDFs: %.3f", n, mean)
	const floor = 0.75 // current mean ~0.89; floor catches regressions with margin.
	if mean < floor {
		t.Errorf("mean native Markdown similarity %.3f below floor %.2f", mean, floor)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func tokenJaccard(a, b string) float64 {
	sa, sb := tokenSet(a), tokenSet(b)
	if len(sa) == 0 && len(sb) == 0 {
		return 1
	}
	inter := 0
	for tok := range sa {
		if sb[tok] {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for tok := range strings.FieldsSeq(strings.ToLower(s)) {
		set[tok] = true
	}
	return set
}
