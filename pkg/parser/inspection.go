package parser

import "context"

// HasActiveContent scans parsed objects for embedded-file declarations and
// JavaScript or Launch actions, including objects outside the page tree.
// It is a conservative upload-policy check, not a PDF sanitizer or a guarantee
// that the document is safe. It does not execute scripts or decode stream data.
func (d *Document) HasActiveContent(ctx context.Context) (bool, error) {
	return d.doc.HasActiveContent(ctx)
}
