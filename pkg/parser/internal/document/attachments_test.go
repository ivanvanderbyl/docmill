package document

import (
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAttachmentsNameTree(t *testing.T) {
	for _, tc := range []struct {
		name, tree string
		want       []Attachment
	}{
		{
			"flat",
			"<< /Names [(key) 5 0 R] >>",
			[]Attachment{{Name: "résumé.pdf", Description: "report"}},
		},
		{
			"nested",
			"<< /Kids [6 0 R] >>",
			[]Attachment{{Name: "résumé.pdf", Description: "report"}},
		},
		{
			"cycle",
			"<< /Kids [4 0 R 6 0 R] >>",
			[]Attachment{{Name: "résumé.pdf", Description: "report"}},
		},
		{
			"odd trailing key",
			"<< /Names [(key) 5 0 R (orphan)] >>",
			[]Attachment{{Name: "résumé.pdf", Description: "report"}},
		},
		{"invalid value", "<< /Names [(key) null] >>", nil},
		{"empty", "<< /Names [] >>", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := Open(buildPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 4 0 R >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>", "<< /Type /Page /Parent 2 0 R >>", tc.tree,
				"<< /Type /Filespec /F (fallback.pdf) /UF <FEFF007200E900730075006D00E9002E007000640066> /Desc (report) >>",
				"<< /Names [(key) 5 0 R] >>",
			}, "1 0 R"))
			require.Equal(t, parser.Success, e)
			require.Equal(t, tc.want, d.Attachments())
		})
	}
}

func TestAttachmentFilenameFallback(t *testing.T) {
	for _, tc := range []struct{ spec, want string }{
		{"<< /F (plain.txt) >>", "plain.txt"},
		{"<< /UF () /F (fallback.txt) >>", "fallback.txt"},
		{"<< /DOS (dos.txt) /Unix (unix.txt) >>", "dos.txt"},
		{"<< /FS /URL /Unix (ignored.txt) >>", ""},
		{"(string.txt)", "string.txt"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			d, e := Open(buildPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(key) 4 0 R] >> >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R >>",
				tc.spec,
			}, "1 0 R"))
			require.Equal(t, parser.Success, e)
			require.Equal(t, []Attachment{{Name: tc.want}}, d.Attachments())
		})
	}
}
