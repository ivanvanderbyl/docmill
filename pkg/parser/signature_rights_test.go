package parser_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestImageStampRejectsUsageRightsSignatures(t *testing.T) {
	for _, key := range []string{"UR", "UR3"} {
		t.Run(key, func(t *testing.T) {
			data := imagePDF("<< /Type /Catalog /Pages 2 0 R /Perms << /"+key+" 4 0 R >> >>", "<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>", "<< /Type /Sig /Contents <abcd> >>")
			opened, err := parser.NewBackend().OpenBytes(context.Background(), data)
			require.NoError(t, err)
			var out bytes.Buffer
			err = opened.(*parser.Document).WriteImageStamps(context.Background(), &out, []parser.ImageStamp{{PageIndex: 0, Rect: geom.Box{L: 10, T: 10, R: 30, B: 30}, Image: stampPixels()}})
			require.ErrorIs(t, err, parser.ErrSignedDocument)
			require.Empty(t, out.Bytes())
		})
	}
}
