package image

import (
	"bytes"
	"fmt"
	"io"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"golang.org/x/image/ccitt"
)

// decodeFax maps the PDF parameters used by PDFium's FaxModule::CreateDecoder
// to the Go fax codec. Mixed Group 3 and EOL-free Group 3 need a different codec.
func decodeFax(data []byte, d *objects.Dictionary, w, h int) ([]byte, error) {
	params := d.GetDictFor("DecodeParms")
	if a := d.GetArrayFor("DecodeParms"); a != nil && a.Len() > 0 {
		params = a.GetDictAt(a.Len() - 1)
	}
	k, columns, rows := 0, 1728, 0
	align, black, eol := false, false, false
	if params != nil {
		k = params.GetIntegerFor("K")
		columns = params.GetIntegerWithDefaultFor("Columns", 1728)
		rows = params.GetIntegerFor("Rows")
		align = params.GetBooleanFor("EncodedByteAlign", false)
		black = params.GetBooleanFor("BlackIs1", false)
		eol = params.GetBooleanFor("EndOfLine", false)
	}
	if columns != w || (rows != 0 && rows != h) {
		return nil, fmt.Errorf("image: fax dimensions differ from image")
	}
	format := ccitt.Group4
	if k >= 0 {
		if k != 0 || !eol {
			return nil, fmt.Errorf("image: unsupported Group 3 fax parameters")
		}
		format = ccitt.Group3
	}
	reader := ccitt.NewReader(bytes.NewReader(data), ccitt.MSB, format, w, h, &ccitt.Options{Align: align, Invert: black})
	out := make([]byte, ((w+7)/8)*h)
	if _, err := io.ReadFull(reader, out); err != nil {
		return nil, fmt.Errorf("image: invalid fax data: %w", err)
	}
	return out, nil
}
