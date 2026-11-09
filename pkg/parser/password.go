package parser

import (
	"context"
	"errors"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/document"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// ErrPasswordRequired means the PDF cannot be opened with an empty password.
var ErrPasswordRequired = errors.New("PDF password required")

// ErrWrongPassword means the supplied password did not authenticate.
var ErrWrongPassword = errors.New("incorrect PDF password")

// ErrUnsupportedEncryption means the PDF uses an unsupported security handler.
var ErrUnsupportedEncryption = errors.New("unsupported PDF encryption")

// ErrInvalidPDF means the PDF structure cannot be parsed.
var ErrInvalidPDF = errors.New("invalid PDF")

// OpenBytesWithPassword opens a PDF using the supplied user or owner password.
func (b *Backend) OpenBytesWithPassword(ctx context.Context, data []byte, password string) (docpdf.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d, e := document.OpenWithPassword(append([]byte(nil), data...), password)
	if e != parser.Success {
		if e == parser.PasswordError && password != "" {
			return nil, ErrWrongPassword
		}
		return nil, parserError(e)
	}
	return &Document{doc: d}, nil
}

// IsEncrypted reports whether the source PDF is encrypted, including PDFs that
// open with an empty user password. Saving with WritePDF produces plaintext.
func (d *Document) IsEncrypted(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return d.doc.IsEncrypted(), nil
}
