package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
)

func selectionPDF(t *testing.T) *Document {
	t.Helper()
	d, e := Open(buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Secret (catalog private) >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 2 /MediaBox [0 0 300 400] /Resources 6 0 R /Rotate 90 >>",
		"<< /Type /Pages /Parent 2 0 R /Kids [] /Count 0 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 7 0 R /Annots [9 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 200] /Contents 8 0 R >>",
		"<< /Font << /F1 10 0 R >> >>",
		"<< /Length 3 >>\nstream\none\nendstream",
		"<< /Length 16 >>\nstream\nPRIVATE_DISCARD\nendstream",
		"<< /Type /Annot /Subtype /Text /P 4 0 R /Rect [0 0 10 10] /Contents (note) >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, "1 0 R"))
	require.Equal(t, parser.Success, e)
	return d
}

func TestWritePagesSelection(t *testing.T) {
	for _, indices := range [][]int{{0}, {1, 0}, {0, 0}, {1}} {
		t.Run(fmt.Sprint(indices), func(t *testing.T) {
			d := selectionPDF(t)
			var before bytes.Buffer
			require.NoError(t, d.WritePDF(&before))
			var out bytes.Buffer
			require.NoError(t, d.WritePages(context.Background(), &out, indices))
			got, e := Open(out.Bytes())
			require.Equal(t, parser.Success, e)
			require.Equal(t, len(indices), got.GetPageCount())
			for i, index := range indices {
				page := got.GetPageDict(i)
				require.NotNil(t, page)
				require.Equal(t, "Page", page.GetNameFor("Type"))
				media := page.GetArrayFor("MediaBox")
				require.NotNil(t, media)
				if index == 0 {
					require.Equal(t, 300, media.GetIntegerAt(2))
				} else {
					require.Equal(t, 100, media.GetIntegerAt(2))
				}
				require.Equal(t, 90, page.GetIntegerFor("Rotate"))
				require.NotNil(t, page.GetDictFor("Resources").GetDictFor("Font").GetDictFor("F1"))
				if index == 0 {
					annot := page.GetArrayFor("Annots").GetDictAt(0)
					require.Same(t, page, annot.GetDictFor("P"))
				}
			}
			if len(indices) == 1 && indices[0] == 0 {
				require.NotContains(t, out.String(), "PRIVATE_DISCARD")
			}
			require.NotContains(t, out.String(), "catalog private")
			var after bytes.Buffer
			require.NoError(t, d.WritePDF(&after))
			require.Equal(t, before.Bytes(), after.Bytes())
		})
	}
}

func TestWritePagesValidation(t *testing.T) {
	d := selectionPDF(t)
	for _, indices := range [][]int{nil, {}, {-1}, {2}, {0, 2}} {
		var out bytes.Buffer
		require.Error(t, d.WritePages(context.Background(), &out, indices))
		require.Empty(t, out.Bytes())
	}
	var out bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, d.WritePages(ctx, &out, []int{0}), context.Canceled)
	require.Error(t, d.WritePages(context.Background(), nil, []int{0}))
}

func TestMergePageSelections(t *testing.T) {
	a := selectionPDF(t)
	b := selectionPDF(t)
	var out bytes.Buffer
	require.NoError(t, WritePageSelections(context.Background(), &out, []PageSelection{{Document: a, Pages: []int{1}}, {Document: b, Pages: []int{0, 0}}}))
	got, e := Open(out.Bytes())
	require.Equal(t, parser.Success, e)
	require.Equal(t, 3, got.GetPageCount())
	require.Equal(t, 100, got.GetPageDict(0).GetArrayFor("MediaBox").GetIntegerAt(2))
	require.Equal(t, 300, got.GetPageDict(1).GetArrayFor("MediaBox").GetIntegerAt(2))
	require.NotEqual(t, got.GetPageDict(1).GetObjNum(), got.GetPageDict(2).GetObjNum())
}

func TestWritePagesRejectsDirectCycles(t *testing.T) {
	d := selectionPDF(t)
	extra := objects.NewDictionary()
	extra.SetFor("Self", extra)
	// Direct cycles cannot be represented in a PDF and must fail cleanly.
	d.GetPageDict(0).SetFor("Bad", extra)
	var out bytes.Buffer
	require.Error(t, d.WritePages(context.Background(), &out, []int{0}))
	require.Empty(t, out.Bytes())
}

func TestWritePagesDefaultsAndInheritedCropBox(t *testing.T) {
	for _, tc := range []struct {
		name, parent string
		want         int
	}{
		{"defaults", "", 612},
		{"crop fallback", "/CropBox [0 0 250 350]", 250},
		{"media preferred", "/MediaBox [0 0 400 500] /CropBox [0 0 250 350]", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, e := Open(buildPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Count 1 /Kids [3 0 R] " + tc.parent + " >>",
				"<< /Type /Page /Parent 2 0 R >>",
			}, "1 0 R"))
			require.Equal(t, parser.Success, e)
			var out bytes.Buffer
			require.NoError(t, d.WritePages(context.Background(), &out, []int{0}))
			copied, e := Open(out.Bytes())
			require.Equal(t, parser.Success, e)
			page := copied.GetPageDict(0)
			require.Equal(t, tc.want, page.GetArrayFor("MediaBox").GetIntegerAt(2))
			require.NotNil(t, page.GetDictFor("Resources"))
		})
	}
}

func TestWritePagesReferenceGraph(t *testing.T) {
	d := selectionPDF(t)
	// Both forward page destinations and cycles through indirect annotations must
	// refer to the new objects, without pulling in the original page tree.
	annotation := d.GetPageDict(0).GetArrayFor("Annots").GetDictAt(0)
	annotation.SetFor("Peer", objects.NewReference(d, 9))
	target := objects.NewArray()
	target.Append(objects.NewReference(d, 5))
	target.Append(objects.NewName("Fit"))
	annotation.SetFor("Dest", target)
	for _, indices := range [][]int{{0, 1}, {0}} {
		var out bytes.Buffer
		require.NoError(t, d.WritePages(context.Background(), &out, indices))
		copied, e := Open(out.Bytes())
		require.Equal(t, parser.Success, e)
		note := copied.GetPageDict(0).GetArrayFor("Annots").GetDictAt(0)
		require.Same(t, note, note.GetDictFor("Peer"))
		dest := note.GetArrayFor("Dest")
		if len(indices) == 2 {
			require.Same(t, copied.GetPageDict(1), dest.GetDictAt(0))
		} else {
			require.NotContains(t, out.String(), "PRIVATE_DISCARD")
			require.Nil(t, dest.GetDictAt(0))
		}
	}
}

func TestWritePagesQPDFValidation(t *testing.T) {
	qpdf, err := exec.LookPath("qpdf")
	if err != nil {
		t.Skip("qpdf is not installed")
	}
	var out bytes.Buffer
	require.NoError(t, selectionPDF(t).WritePages(context.Background(), &out, []int{1, 0, 0}))
	path := filepath.Join(t.TempDir(), "selected.pdf")
	require.NoError(t, os.WriteFile(path, out.Bytes(), 0600))
	log, err := exec.Command(qpdf, "--check", path).CombinedOutput()
	require.NoError(t, err, string(log))
}

type shortPageWriter struct{}

func (shortPageWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }
func TestWritePagesShortWriter(t *testing.T) {
	require.ErrorIs(t, selectionPDF(t).WritePages(context.Background(), shortPageWriter{}, []int{0}), io.ErrShortWrite)
}

func TestWritePagesPreservesEncodedStreams(t *testing.T) {
	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	_, err := zw.Write([]byte("BT /F1 12 Tf (encoded text) Tj ET"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	d, e := Open(buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /Contents 4 0 R >>",
		fmt.Sprintf("<< /Filter /FlateDecode /Length %d >>\nstream\n%s\nendstream", compressed.Len(), compressed.Bytes()),
	}, "1 0 R"))
	require.Equal(t, parser.Success, e)
	var out bytes.Buffer
	require.NoError(t, d.WritePages(context.Background(), &out, []int{0}))
	copied, e := Open(out.Bytes())
	require.Equal(t, parser.Success, e)
	stream := objects.ToStream(copied.GetPageDict(0).GetDirectObjectFor("Contents"))
	require.NotNil(t, stream)
	require.Equal(t, "FlateDecode", stream.Dict().GetNameFor("Filter"))
	raw := objects.NewStreamAcc(stream)
	raw.LoadAllDataRaw()
	require.Equal(t, compressed.Bytes(), raw.GetSpan())
	decoded := objects.NewStreamAcc(stream)
	decoded.LoadAllDataFiltered()
	require.Equal(t, "BT /F1 12 Tf (encoded text) Tj ET", string(decoded.GetSpan()))
}

func TestWritePagesCyclicInheritance(t *testing.T) {
	d := selectionPDF(t)
	page := d.GetPageDict(0)
	page.SetFor("Parent", objects.NewReference(d, page.GetObjNum()))
	var out bytes.Buffer
	require.NoError(t, d.WritePages(context.Background(), &out, []int{0}))
	copied, e := Open(out.Bytes())
	require.Equal(t, parser.Success, e)
	require.Equal(t, 612, copied.GetPageDict(0).GetArrayFor("MediaBox").GetIntegerAt(2))
}
