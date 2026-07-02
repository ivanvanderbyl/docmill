package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunBenchmarkReportsMissingRequiredFlags(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runBenchmark(context.Background(), nil, &stdout, &stderr)

	require.Equal(t, 1, code)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "-corpus and -tools are required")
}

func TestRunBenchmarkRejectsMissingRequiredToolsByDefault(t *testing.T) {
	t.Parallel()

	root := writeBenchmarkCorpus(t, "hello")
	toolsPath := filepath.Join(t.TempDir(), "tools.json")
	require.NoError(t, os.WriteFile(toolsPath, []byte(`{
		"tools": [
			{"name": "docmill", "command": ["docmill", "{{input}}"], "output_mode": "stdout"}
		]
	}`), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runBenchmark(context.Background(), []string{"-corpus", root, "-tools", toolsPath}, &stdout, &stderr)

	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "missing required tools")
	require.Contains(t, stderr.String(), "docling")
}

func TestRunBenchmarkWritesMarkdownAndJSONReports(t *testing.T) {
	t.Setenv("DOCMILL_BENCHMARK_HELPER", "stdout")

	root := writeBenchmarkCorpus(t, "hello")
	tmp := t.TempDir()
	toolsPath := filepath.Join(tmp, "tools.json")
	markdownPath := filepath.Join(tmp, "report.md")
	jsonPath := filepath.Join(tmp, "report.json")
	require.NoError(t, os.WriteFile(toolsPath, []byte(`{
		"tools": [
			{"name": "docmill", "command": [`+quoteJSON(os.Args[0])+`, "-test.run=TestBenchmarkHelperProcess"], "output_mode": "stdout"}
		]
	}`), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := runBenchmark(context.Background(), []string{
		"-corpus", root,
		"-tools", toolsPath,
		"-out", markdownPath,
		"-json", jsonPath,
		"-date", "2026-06-21",
		"-hardware", "test machine",
		"-allow-missing",
	}, &stdout, &stderr)

	require.Equal(t, 0, code, stderr.String())
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())

	report, err := os.ReadFile(markdownPath)
	require.NoError(t, err)
	require.Contains(t, string(report), "# Benchmarks")
	require.Contains(t, string(report), "| docmill | - | **1.00** | **1.00** | 0.00 | 0.00 |")

	jsonReport, err := os.ReadFile(jsonPath)
	require.NoError(t, err)
	require.Contains(t, string(jsonReport), `"corpus_name": "fixture"`)
	require.Contains(t, string(jsonReport), `"name": "docmill"`)
	require.Contains(t, string(jsonReport), `"milliseconds_per_page"`)
}

func TestBenchmarkHelperProcess(t *testing.T) {
	if os.Getenv("DOCMILL_BENCHMARK_HELPER") == "" {
		return
	}
	_, _ = os.Stdout.WriteString("hello")
	os.Exit(0)
}

func writeBenchmarkCorpus(t *testing.T, groundTruth string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pdf"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "groundtruth"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pdf", "sample.pdf"), benchmarkTextPDF("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "groundtruth", "sample.md"), []byte(groundTruth), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{
		"name": "fixture",
		"cases": [
			{"id": "sample", "pdf_path": "pdf/sample.pdf", "ground_truth_path": "groundtruth/sample.md", "pages": 1}
		]
	}`), 0o644))
	return root
}

func benchmarkTextPDF(text string) []byte {
	content := "BT /F1 12 Tf 100 700 Td (" + text + ") Tj ET"
	objects := []struct {
		id   int
		body string
	}{
		{id: 1, body: "<< /Type /Catalog /Pages 2 0 R >>"},
		{id: 2, body: "<< /Type /Pages /Kids [3 0 R] /Count 1 >>"},
		{id: 3, body: "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>"},
		{id: 4, body: "<< /Length " + fmt.Sprint(len(content)+1) + " >>\nstream\n" + content + "\nendstream"},
		{id: 5, body: "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 126 /Widths [" + benchmarkRepeatedNumber(95, 500) + "] /FontDescriptor << /Ascent 718 /Descent -207 /FontBBox [-166 -225 1000 931] >> >>"},
	}

	var b bytes.Buffer
	offsets := make(map[int]int, len(objects))
	b.WriteString("%PDF-1.4\n")
	for _, obj := range objects {
		offsets[obj.id] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", obj.id, obj.body)
	}
	xrefOffset := b.Len()
	fmt.Fprintf(&b, "xref\n0 6\n0000000000 65535 f \n")
	for id := 1; id <= 5; id++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return b.Bytes()
}

func benchmarkRepeatedNumber(count int, value int) string {
	var b bytes.Buffer
	for i := range count {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprint(&b, value)
	}
	return b.String()
}

func quoteJSON(s string) string {
	var b bytes.Buffer
	b.WriteByte('"')
	for _, r := range s {
		if r == '\\' || r == '"' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
