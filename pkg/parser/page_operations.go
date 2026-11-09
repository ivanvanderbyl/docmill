package parser

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/document"
)

// PageSelection identifies zero-based pages to copy, in output order. Repeated
// indices produce independent page dictionaries with the same content.
type PageSelection struct {
	Document *Document
	Pages    []int
}

// WritePages writes a new PDF containing the selected zero-based pages. It
// leaves the source unchanged and excludes unselected page content. Catalog
// metadata, outlines, document attachments, and document-level form trees are
// not copied. Page resources, annotations, and inherited attributes are copied.
func (d *Document) WritePages(ctx context.Context, w io.Writer, pages []int) error {
	if d == nil {
		return fmt.Errorf("write pages: nil document")
	}
	return d.doc.WritePages(ctx, w, pages)
}

// WritePageRange writes pages in the zero-based half-open interval [start,end).
// An empty or out-of-bounds interval returns an error without writing output.
func (d *Document) WritePageRange(ctx context.Context, w io.Writer, start, end int) error {
	if d == nil || d.doc == nil {
		return fmt.Errorf("write page range: nil document")
	}
	if start < 0 || end <= start || end > d.doc.GetPageCount() {
		return fmt.Errorf("write page range: invalid range [%d,%d)", start, end)
	}
	pages := make([]int, end-start)
	for i := range pages {
		pages[i] = start + i
	}
	return d.WritePages(ctx, w, pages)
}

// WritePageSelections writes one PDF by concatenating page selections. Page
// indices are zero-based and each selection may reorder or repeat pages.
// The same catalog-level exclusions as WritePages apply.
func WritePageSelections(ctx context.Context, w io.Writer, selections []PageSelection) error {
	internal := make([]document.PageSelection, len(selections))
	for i, s := range selections {
		if s.Document == nil {
			return fmt.Errorf("write page selections: nil document at %d", i)
		}
		internal[i] = document.PageSelection{Document: s.Document.doc, Pages: s.Pages}
	}
	return document.WritePageSelections(ctx, w, internal)
}

// MergePDFs combines all pages from the supplied PDFs into one PDF, in input
// order. It copies page-level structures as described by WritePages; it does
// not combine catalog-level metadata, outlines, attachments, or form trees.
func MergePDFs(ctx context.Context, inputs ...[]byte) ([]byte, error) {
	selections := make([]PageSelection, 0, len(inputs))
	backend := &Backend{}
	for _, data := range inputs {
		opened, err := backend.OpenBytes(ctx, data)
		if err != nil {
			return nil, err
		}
		d := opened.(*Document)
		pages := make([]int, d.doc.GetPageCount())
		for i := range pages {
			pages[i] = i
		}
		selections = append(selections, PageSelection{Document: d, Pages: pages})
	}
	var out bytes.Buffer
	if err := WritePageSelections(ctx, &out, selections); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
