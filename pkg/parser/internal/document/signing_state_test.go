package document

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
)

func TestFormChangesRecordedForSigning(t *testing.T) {
	for _, tc := range []struct {
		name      string
		values    map[string]string
		changed   bool
		wantError bool
	}{
		{"no values", nil, false, false},
		{"unknown field", map[string]string{"missing": "value"}, false, true},
		{"signature field", map[string]string{"signature": "value"}, false, true},
		{"known field", map[string]string{"name": "after"}, true, false},
		{"partial update", map[string]string{"name": "after", "missing": "value"}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, status := Open(buildPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] >> >>",
				"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
				"<< /Type /Page /Parent 2 0 R >>",
				"<< /FT /Tx /T (name) /V (before) >>",
				"<< /FT /Sig /T (signature) >>",
			}, "1 0 R"))
			require.Equal(t, parser.Success, status)
			require.False(t, d.HasUnsavedChanges())
			_, err := d.SetAcroFormFieldValues(tc.values)
			require.Equal(t, tc.wantError, err != nil)
			require.Equal(t, tc.changed, d.HasUnsavedChanges())
		})
	}
}
