package parser

import (
	"bytes"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSignatureByteRangeConformance(t *testing.T) {
	source := []byte("prefix<0000>suffix\n%%EOF\n")
	start := int64(bytes.Index(source, []byte("<")))
	end := start + 6
	valid := []int64{0, start, end, int64(len(source)) - end}
	for _, tc := range []struct {
		name                 string
		ranges               []int64
		tokenStart, tokenEnd int64
		contents             []byte
		wantError            bool
	}{
		{"exact Contents gap", valid, start, end, []byte{0, 0}, false},
		{"prefix not signed", []int64{1, start, end, int64(len(source)) - end}, start, end, []byte{0, 0}, true},
		{"overflow", []int64{0, start, end, math.MaxInt64}, start, end, []byte{0, 0}, true},
		{"negative", []int64{0, -1, end, 10}, start, end, []byte{0, 0}, true},
		{"overlap", []int64{0, start, end - 1, 10}, start, end, []byte{0, 0}, true},
		{"extra gap", []int64{0, start - 1, end, 10}, start, end, []byte{0, 0}, true},
		{"wrong dictionary token", valid, start + 1, end + 1, []byte{0, 0}, true},
		{"unknown provenance", valid, -1, -1, []byte{0, 0}, true},
		{"contents mismatch", valid, start, end, []byte{1, 2}, true},
		{"extra range", []int64{0, start, end, 2, end + 2, 10}, start, end, []byte{0, 0}, true},
		{"incomplete revision", []int64{0, start, end, 3}, start, end, []byte{0, 0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sig := Signature{ByteRange: tc.ranges, ContentsStart: tc.tokenStart, ContentsEnd: tc.tokenEnd, Contents: tc.contents}
			data, revision, err := signatureContent(source, sig)
			if tc.wantError {
				require.ErrorIs(t, err, ErrInvalidSignatureRange)
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(len(source)), revision)
			require.Equal(t, []byte("prefixsuffix\n%%EOF\n"), data)
		})
	}
}
