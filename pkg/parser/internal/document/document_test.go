package document

import (
	"bytes"
	"fmt"
	"sync"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
)

// buildPDF assembles a minimal classic-xref PDF from object bodies (object i+1),
// computing correct byte offsets for the 20-byte xref entries.
func buildPDF(objs []string, root string) []byte {
	var sb bytes.Buffer
	sb.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = sb.Len()
		fmt.Fprintf(&sb, "%d 0 obj %s endobj\n", i+1, body)
	}
	xrefPos := sb.Len()
	fmt.Fprintf(&sb, "xref\n0 %d\n", len(objs)+1)
	sb.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&sb, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&sb, "trailer << /Size %d /Root %s >>\nstartxref\n%d\n%%%%EOF", len(objs)+1, root, xrefPos)
	return sb.Bytes()
}

func TestOpenSinglePage(t *testing.T) {
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}, "1 0 R")

	doc, err := Open(pdf)
	if err != parser.Success {
		t.Fatalf("Open returned error %v", err)
	}
	if doc.GetPageCount() != 1 {
		t.Errorf("PageCount = %d, want 1", doc.GetPageCount())
	}
	// The catalog resolves and carries /Type /Catalog.
	cat := objects.ToDictionary(doc.GetOrParseIndirectObject(1))
	if cat == nil || cat.GetNameFor("Type") != "Catalog" {
		t.Error("object 1 did not resolve to the catalog")
	}
}

func TestOpenMultiPage(t *testing.T) {
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Type /Page /Parent 2 0 R >>",
		"<< /Type /Page /Parent 2 0 R >>",
	}, "1 0 R")

	doc, err := Open(pdf)
	if err != parser.Success {
		t.Fatalf("Open returned error %v", err)
	}
	if doc.GetPageCount() != 3 {
		t.Errorf("PageCount = %d, want 3", doc.GetPageCount())
	}
}

func TestOpenCountFromKidsTraversal(t *testing.T) {
	// No /Count on the Pages node and a nested branch: count must come from the
	// /Kids traversal (3 leaves under two branches).
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 6 0 R] >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [4 0 R 5 0 R] >>",
		"<< /Type /Page /Parent 3 0 R >>",
		"<< /Type /Page /Parent 3 0 R >>",
		"<< /Type /Page /Parent 2 0 R >>",
	}, "1 0 R")

	doc, err := Open(pdf)
	if err != parser.Success {
		t.Fatalf("Open returned error %v", err)
	}
	if doc.GetPageCount() != 3 {
		t.Errorf("PageCount = %d, want 3", doc.GetPageCount())
	}
}

func TestOpenRebuildOnBadXref(t *testing.T) {
	// A valid PDF whose startxref points at garbage forces RebuildCrossRef.
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R >>",
	}, "1 0 R")
	// Corrupt the startxref offset.
	corrupted := bytes.Replace(pdf, []byte("startxref\n9\n"), []byte("startxref\n99999\n"), 1)
	if bytes.Equal(corrupted, pdf) {
		// startxref offset isn't literally "9"; replace the trailing number block.
		idx := bytes.LastIndex(pdf, []byte("startxref\n"))
		corrupted = append(append([]byte{}, pdf[:idx]...), []byte("startxref\n99999\n%%EOF")...)
	}
	doc, err := Open(corrupted)
	if err != parser.Success {
		t.Fatalf("Open with bad xref returned error %v (rebuild should recover)", err)
	}
	if doc.GetPageCount() != 1 {
		t.Errorf("PageCount after rebuild = %d, want 1", doc.GetPageCount())
	}
	if !doc.parserRebuilt() {
		t.Error("expected the xref to have been rebuilt")
	}
}

// parserRebuilt exposes the rebuild flag for the test.
func (d *Document) parserRebuilt() bool { return d.prs.XRefTableRebuilt() }

func TestGetPageDictConcurrent(t *testing.T) {
	// Page-tree nodes with no /Type force getNodeType to normalise the shared
	// dictionaries in place, and the root's valid /Count means that only happens
	// lazily inside GetPageDict — which pkg/pdf's parallel page extraction calls
	// from many goroutines at once. Run under -race.
	pdf := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R 6 0 R] /Count 4 >>",
		"<< /Parent 2 0 R >>",
		"<< /Parent 2 0 R >>",
		"<< /Parent 2 0 R >>",
		"<< /Parent 2 0 R >>",
	}, "1 0 R")

	doc, err := Open(pdf)
	if err != parser.Success {
		t.Fatalf("Open returned error %v", err)
	}
	if doc.GetPageCount() != 4 {
		t.Fatalf("PageCount = %d, want 4", doc.GetPageCount())
	}
	var wg sync.WaitGroup
	for i := range doc.GetPageCount() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if doc.GetPageDict(i) == nil {
				t.Errorf("GetPageDict(%d) = nil", i)
			}
		}()
	}
	wg.Wait()
}
