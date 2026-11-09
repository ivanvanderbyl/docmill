package parser

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCMSFramingRejectsMalformedAndTrailingData(t *testing.T) {
	definite := []byte{0x30, 3, 2, 1, 1}
	indefinite := []byte{0x30, 0x80, 2, 1, 1, 0, 0}
	deep := bytes.Repeat([]byte{0x30, 0x80}, 66)
	deep = append(deep, bytes.Repeat([]byte{0, 0}, 66)...)
	for _, tc := range []struct {
		name      string
		input     []byte
		wantError bool
	}{
		{"DER plus zero padding", append(bytesClone(definite), 0, 0), false},
		{"BER plus zero padding", append(bytesClone(indefinite), 0, 0), false},
		{"nonzero trailing bytes", append(bytesClone(indefinite), 1), true},
		{"truncated header", []byte{0x30}, true},
		{"missing end marker", []byte{0x30, 0x80, 2, 1, 1}, true},
		{"indefinite primitive", []byte{4, 0x80, 0, 0}, true},
		{"overflow length", []byte{0x30, 0x88, 255, 255, 255, 255, 255, 255, 255, 255}, true},
		{"deep nesting", deep, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cmsEnvelopeBytes(tc.input)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func FuzzCMSFraming(f *testing.F) {
	f.Add([]byte{0x30, 0x80, 2, 1, 1, 0, 0})
	f.Add([]byte{0x30, 3, 2, 1, 1})
	f.Add([]byte{0x30, 0x80, 0x30, 0x82, 255, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			t.Skip()
		}
		if framed, err := cmsEnvelopeBytes(data); err == nil {
			_, _, _, _ = verifyCMS([]byte("signed data"), framed, "adbe.pkcs7.detached")
		}
	})
}
