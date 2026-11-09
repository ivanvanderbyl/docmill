package parser

import (
	"context"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/document"
)

// Attachment describes an embedded file's metadata. Name must not be used as a
// filesystem path without validation. Enumeration does not decode file payloads.
type Attachment = document.Attachment

// Attachments returns file specifications from the EmbeddedFiles name tree.
func (d *Document) Attachments(ctx context.Context) ([]Attachment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	attachments := d.doc.Attachments()
	return attachments, d.doc.DecryptionError()
}
