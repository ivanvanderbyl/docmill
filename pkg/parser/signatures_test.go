package parser_test

import (
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestSignaturesPublicAPI(t *testing.T) {
	ctx := context.Background()
	data := imagePDF("<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", "<< /FT /Sig /T (signature) /V << /Contents <abcd0000> /ByteRange [0 1 8 2] >> >>")
	d, err := parser.NewBackend().OpenBytes(ctx, data)
	require.NoError(t, err)
	native := d.(*parser.Document)
	signatures, err := native.Signatures(ctx)
	require.NoError(t, err)
	require.Len(t, signatures, 1)
	require.Equal(t, []byte{0xab, 0xcd, 0, 0}, signatures[0].Contents)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = native.Signatures(canceled)
	require.ErrorIs(t, err, context.Canceled)
	for _, invalid := range []*parser.Document{nil, {}} {
		_, err = invalid.Signatures(ctx)
		require.Error(t, err)
	}
}
