package benchdp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCorpusReadsManifestAndSortsCases(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeNativeTextPDF(t, root, "pdf/b.pdf", "B")
	writeNativeTextPDF(t, root, "pdf/a.pdf", "A")
	writeCorpusFile(t, root, "groundtruth/b.md", "B")
	writeCorpusFile(t, root, "groundtruth/a.md", "A")
	writeCorpusFile(t, root, "manifest.json", `{
		"name": "dpbench",
		"source": "https://huggingface.co/datasets/docling-project/docling-dpbench",
		"cases": [
			{"id": "b", "pdf_path": "pdf/b.pdf", "ground_truth_path": "groundtruth/b.md", "pages": 2},
			{"id": "a", "pdf_path": "pdf/a.pdf", "ground_truth_path": "groundtruth/a.md", "pages": 1}
		]
	}`)

	corpus, err := LoadCorpus(root)

	require.NoError(t, err)
	require.Equal(t, "dpbench", corpus.Name)
	require.Equal(t, "https://huggingface.co/datasets/docling-project/docling-dpbench", corpus.Source)
	require.Len(t, corpus.Cases, 2)
	require.Equal(t, "a", corpus.Cases[0].ID)
	require.Equal(t, filepath.Join(root, "pdf/a.pdf"), corpus.Cases[0].PDFPath)
	require.Equal(t, filepath.Join(root, "groundtruth/a.md"), corpus.Cases[0].GroundTruthPath)
	require.Equal(t, 1, corpus.Cases[0].Pages)
	require.Equal(t, "b", corpus.Cases[1].ID)
}

func TestLoadCorpusSkipsImageOnlyPDFs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeNativeTextPDF(t, root, "pdf/native.pdf", "Native text")
	writeImageOnlyPDF(t, root, "pdf/image-only.pdf")
	writeCorpusFile(t, root, "groundtruth/native.md", "Native text")
	writeCorpusFile(t, root, "groundtruth/image-only.md", "OCR text")
	writeCorpusFile(t, root, "manifest.json", `{
		"name": "dpbench",
		"cases": [
			{"id": "native", "pdf_path": "pdf/native.pdf", "ground_truth_path": "groundtruth/native.md", "pages": 1},
			{"id": "image-only", "pdf_path": "pdf/image-only.pdf", "ground_truth_path": "groundtruth/image-only.md", "pages": 1}
		]
	}`)

	corpus, err := LoadCorpus(root)

	require.NoError(t, err)
	require.Len(t, corpus.Cases, 1)
	require.Equal(t, "native", corpus.Cases[0].ID)
	require.Equal(t, 1, corpus.SkippedImageOnlyCases)
}

func TestLoadCorpusReportsMissingFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeCorpusFile(t, root, "manifest.json", `{
		"name": "broken",
		"cases": [
			{"id": "missing", "pdf_path": "pdf/missing.pdf", "ground_truth_path": "groundtruth/missing.md"}
		]
	}`)

	_, err := LoadCorpus(root)

	require.Error(t, err)
	require.Contains(t, err.Error(), "missing.pdf")
}

func writeCorpusFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func writeNativeTextPDF(t *testing.T, root, rel, text string) {
	t.Helper()
	content := fmt.Sprintf("BT /F1 12 Tf 100 700 Td (%s) Tj ET", text)
	writePDF(t, root, rel, content, "<< /Font << /F1 5 0 R >> >>", []pdfObject{{
		id:   5,
		body: `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths [` + repeatedNumber(95, 500) + `] /FontDescriptor << /Ascent 718 /Descent -207 /FontBBox [-166 -225 1000 931] >> >>`,
	}})
}

func writeImageOnlyPDF(t *testing.T, root, rel string) {
	t.Helper()
	writePDF(t, root, rel, "q 100 0 0 100 10 10 cm /Im1 Do Q", "<< /XObject << /Im1 5 0 R >> >>", []pdfObject{{
		id:   5,
		body: "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream",
	}})
}

type pdfObject struct {
	id   int
	body string
}

func writePDF(t *testing.T, root, rel, content, resources string, extra []pdfObject) {
	t.Helper()
	objects := []pdfObject{
		{id: 1, body: "<< /Type /Catalog /Pages 2 0 R >>"},
		{id: 2, body: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{id: 3, body: fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources %s /Contents 4 0 R >>", resources)},
		{id: 4, body: fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content)+1, content)},
	}
	objects = append(objects, extra...)

	var buf bytes.Buffer
	offsets := make(map[int]int, len(objects))
	buf.WriteString("%PDF-1.4\n")
	for _, obj := range objects {
		offsets[obj.id] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", obj.id, obj.body)
	}
	xrefOffset := buf.Len()
	size := len(objects) + 1
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", size)
	for id := 1; id < size; id++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", size, xrefOffset)

	path := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
}

func repeatedNumber(count int, value int) string {
	var b bytes.Buffer
	for i := range count {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprint(&b, value)
	}
	return b.String()
}
