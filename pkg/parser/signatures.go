package parser

import (
	"context"
	"fmt"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/document"
)

// Signature describes unverified signature metadata. SigningTime is a claimed
// time, not a trusted timestamp. Contents includes the PDF string's padding.
type Signature = document.Signature

// Signatures inspects signed fields, certification, and usage-rights signatures. Unsigned
// fields are omitted. Malformed signature metadata returns an error instead of
// a partial list. Use verification before trusting any claimed metadata.
// Saving with WritePDF rewrites the file and does not preserve signatures.
func (d *Document) Signatures(ctx context.Context) ([]Signature, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil || d.doc == nil {
		return nil, fmt.Errorf("signatures: nil document")
	}
	return d.doc.Signatures()
}
