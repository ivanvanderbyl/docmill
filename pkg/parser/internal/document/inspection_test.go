package document

import (
	"context"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
)

func TestInspectActiveContent(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		active     bool
	}{
		{"ordinary string", "<< /Subject (JavaScript Launch EmbeddedFiles) >>", false},
		{"javascript action", "<< /S /JavaScript /JS (alert) >>", true},
		{"launch action", "<< /S /Launch >>", true},
		{"embedded files key", "<< /EmbeddedFiles << >> >>", true},
		{"javascript key", "<< /JavaScript [] >>", true},
		{"indirect array", "[5 0 R]", true},
		{"cycle", "[4 0 R]", false},
		{"stream dictionary", "<< /JavaScript [] /Length 0 >>\nstream\n\nendstream", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R >>",
				tc.body,
			}
			if tc.name == "indirect array" {
				bodies = append(bodies, "<< /S /Launch >>")
			}
			d, e := Open(buildPDF(bodies, "1 0 R"))
			require.Equal(t, parser.Success, e)
			got, err := d.HasActiveContent(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.active, got)
		})
	}
}

func TestInspectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&Document{}).HasActiveContent(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
