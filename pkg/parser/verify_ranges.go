package parser

import (
	"bytes"
	"fmt"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/syntax"
)

func signatureContent(source []byte, sig Signature) ([]byte, int64, error) {
	bad := func(reason string) ([]byte, int64, error) {
		return nil, 0, fmt.Errorf("%w: %s", ErrInvalidSignatureRange, reason)
	}
	if len(sig.ByteRange) != 4 {
		return bad("exactly two signed intervals required")
	}
	r := sig.ByteRange
	size := int64(len(source))
	if !validSignatureIntervals(r, size) {
		return bad("invalid or overflowing interval")
	}
	end := r[2] + r[3]
	if r[1] != sig.ContentsStart || r[2] != sig.ContentsEnd || sig.ContentsStart < 0 {
		return bad("gap is not the inspected signature Contents")
	}
	if r[2]-r[1] < 2 || r[3] == 0 {
		return bad("empty signature gap or signed suffix")
	}
	token := source[r[1]:r[2]]
	if !matchesSignatureContents(token, sig.Contents) {
		return bad("gap is not the original Contents string")
	}

	// Each accepted signature covers a complete PDF revision ending in EOF.
	// Any later incremental update is explicitly outside that signature's coverage.
	tail := bytes.TrimSpace(source[r[2]:end])
	if !bytes.HasSuffix(tail, []byte("%%EOF")) {
		return bad("signed revision does not end at PDF EOF")
	}
	signed := make([]byte, 0, r[1]+r[3])
	signed = append(signed, source[:r[1]]...)
	signed = append(signed, source[r[2]:end]...)
	return signed, end, nil
}

func validSignatureIntervals(r []int64, size int64) bool {
	return r[0] == 0 && r[1] >= 0 && r[2] >= r[1] && r[2] <= size && r[3] >= 0 && r[3] <= size-r[2]
}

func matchesSignatureContents(token, contents []byte) bool {
	parser := syntax.New(token)
	str := objects.ToString(parser.GetObjectBody(nil))
	return str != nil && parser.GetPos() == len(token) && bytes.Equal([]byte(str.GetString()), contents)
}
