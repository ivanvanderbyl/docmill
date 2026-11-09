package document

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
)

func TestTextFormSetterDoesNotOverwriteSignatureFields(t *testing.T) {
	for _, value := range []string{"", "/V 5 0 R"} {
		t.Run(value, func(t *testing.T) {
			d, err := Open(buildPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
				"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
				"<< /Type /Page /Parent 2 0 R >>",
				"<< /FT /Sig /T (approval) " + value + " >>",
				"<< /Type /Sig /SubFilter /adbe.pkcs7.detached /Contents <010203> /ByteRange [0 1 2 3] >>",
			}, "1 0 R"))
			require.Equal(t, parser.Success, err)
			field := d.GetOrParseIndirectObject(4).GetDict()
			before := field.GetObjectFor("V")
			changed, setErr := d.SetAcroFormFieldValues(map[string]string{"approval": "scribble"})
			require.ErrorContains(t, setErr, "signature")
			require.Zero(t, changed)
			require.Equal(t, before, field.GetObjectFor("V"))
		})
	}
}
