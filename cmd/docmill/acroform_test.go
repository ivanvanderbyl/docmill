package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestDispatchFormsExportWritesFieldValuesJSON(t *testing.T) {
	t.Parallel()

	input := filepath.Join(t.TempDir(), "form.pdf")
	require.NoError(t, os.WriteFile(input, singlePageAcroFormPDFForCLI(t, "Ada Lovelace"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill", "forms", "export", input}, stringsReader(""), &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Empty(t, stderr.String())

	var got acroformValueFile
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	require.Equal(t, map[string]string{"Applicant name": "Ada Lovelace"}, got.Fields)
}

func TestDispatchFormsFillWritesUpdatedPDF(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := filepath.Join(dir, "form.pdf")
	output := filepath.Join(dir, "filled.pdf")
	require.NoError(t, os.WriteFile(input, singlePageAcroFormPDFForCLI(t, "Ada Lovelace"), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	values := `{"fields":{"Applicant name":"Grace Hopper"}}`

	code := dispatch(context.Background(), []string{"docmill", "forms", "fill", input, output}, stringsReader(values), &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())

	data, err := os.ReadFile(output)
	require.NoError(t, err)
	backend := parser.NewBackend()
	doc, err := backend.OpenBytes(context.Background(), data)
	require.NoError(t, err)
	defer doc.Close()

	nativeDoc, ok := doc.(*parser.Document)
	require.True(t, ok)
	got, err := nativeDoc.AcroFormValues(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]string{"Applicant name": "Grace Hopper"}, got)
}

func TestDispatchFormsFillReadsValuesFromOptionalJSONPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	input := filepath.Join(dir, "form.pdf")
	output := filepath.Join(dir, "filled.pdf")
	valuesPath := filepath.Join(dir, "values.json")
	require.NoError(t, os.WriteFile(input, singlePageAcroFormPDFForCLI(t, "Ada Lovelace"), 0o644))
	require.NoError(t, os.WriteFile(valuesPath, []byte(`{"fields":{"Applicant name":"Katherine Johnson"}}`), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill", "forms", "fill", input, output, valuesPath}, stringsReader(""), &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Empty(t, stdout.String())
	require.Empty(t, stderr.String())

	data, err := os.ReadFile(output)
	require.NoError(t, err)
	backend := parser.NewBackend()
	doc, err := backend.OpenBytes(context.Background(), data)
	require.NoError(t, err)
	defer doc.Close()

	nativeDoc, ok := doc.(*parser.Document)
	require.True(t, ok)
	got, err := nativeDoc.AcroFormValues(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]string{"Applicant name": "Katherine Johnson"}, got)
}

func TestDispatchFormsLayoutWritesFieldBoxesJSON(t *testing.T) {
	t.Parallel()

	input := filepath.Join(t.TempDir(), "form.pdf")
	require.NoError(t, os.WriteFile(input, twoFieldAcroFormPDFForCLI(t), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill", "forms", "layout", input}, stringsReader(""), &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Empty(t, stderr.String())

	var got formsLayoutFile
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	require.Equal(t, formsLayoutFile{Pages: []formsLayoutPage{
		{
			Page:   1,
			Width:  300,
			Height: 200,
			Fields: []formsLayoutField{
				{
					Name:        "Applicant name",
					Label:       "Full legal name",
					LabelSource: "tooltip",
					Type:        "Tx",
					Value:       "Ada Lovelace",
					// MediaBox height 200, /Rect [120 110 260 130] bottom-left ->
					// top-left origin: top = 200-130, bottom = 200-110.
					Box: formsLayoutBox{Left: 120, Top: 70, Right: 260, Bottom: 90},
				},
				{
					Name: "Email",
					Type: "Tx",
					// /Rect [120.4567 80.1234 260.9876 100.5]: float32 storage
					// noise is rounded to 0.001pt (top = 200-100.5, bottom =
					// 200-80.1234).
					Box: formsLayoutBox{Left: 120.457, Top: 99.5, Right: 260.988, Bottom: 119.877},
				},
			},
		},
	}}, got)
}

func TestDispatchFormsLayoutReportsFieldGroupsAndOnStateLabels(t *testing.T) {
	t.Parallel()

	input := filepath.Join(t.TempDir(), "form.pdf")
	require.NoError(t, os.WriteFile(input, checkboxPairAcroFormPDFForCLI(t), 0o644))

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := dispatch(context.Background(), []string{"docmill", "forms", "layout", input}, stringsReader(""), &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Empty(t, stderr.String())

	var got formsLayoutFile
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got))
	require.Len(t, got.Pages, 1)

	// No page text exists, so the labels come from the semantic /AP on-state
	// names; the two widgets share one field and form a "field" group.
	require.Equal(t, []formsLayoutField{
		{
			Name:        "Member",
			Label:       "Yes",
			LabelSource: "state",
			Type:        "Btn",
			OnState:     "Yes",
			Box:         formsLayoutBox{Left: 50, Top: 39, Right: 61, Bottom: 50},
		},
		{
			Name:        "Member",
			Label:       "No",
			LabelSource: "state",
			Type:        "Btn",
			OnState:     "No",
			Box:         formsLayoutBox{Left: 120, Top: 39, Right: 131, Bottom: 50},
		},
	}, got.Pages[0].Fields)
	require.Equal(t, []formsLayoutGroup{
		{Kind: "field", Name: "Member", Fields: []int{0, 1}},
	}, got.Pages[0].Groups)
}

func stringsReader(s string) *bytes.Reader {
	return bytes.NewReader([]byte(s))
}

// checkboxPairAcroFormPDFForCLI is a one-page form with a single checkbox
// field rendered as two widgets whose /AP on-states are Yes and No.
func checkboxPairAcroFormPDFForCLI(t *testing.T) []byte {
	t.Helper()

	return buildCLIPDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << >> /Contents 4 0 R /Annots [7 0 R 8 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [6 0 R] >>",
		"<< /FT /Btn /T (Member) /Kids [7 0 R 8 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [50 150 61 161] /AP << /N << /Yes 4 0 R /Off 4 0 R >> >> >>",
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [120 150 131 161] /AP << /N << /No 4 0 R /Off 4 0 R >> >> >>",
	}, "1 0 R")
}

// twoFieldAcroFormPDFForCLI is a one-page form with a filled, labelled text
// field and an unfilled one (which forms export omits but forms layout keeps).
func twoFieldAcroFormPDFForCLI(t *testing.T) []byte {
	t.Helper()

	return buildCLIPDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << >> /Contents 4 0 R /Annots [7 0 R 9 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [6 0 R 8 0 R] >>",
		"<< /FT /Tx /T (Applicant name) /TU (Full legal name) /V (Ada Lovelace) /Kids [7 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [120 110 260 130] >>",
		"<< /FT /Tx /T (Email) /Kids [9 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 8 0 R /Rect [120.4567 80.1234 260.9876 100.5] >>",
	}, "1 0 R")
}

func singlePageAcroFormPDFForCLI(t *testing.T, value string) []byte {
	t.Helper()

	return buildCLIPDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << >> /Contents 4 0 R /Annots [7 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [6 0 R] >>",
		fmt.Sprintf("<< /FT /Tx /T (Applicant name) /V (%s) /Kids [7 0 R] >>", value),
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [120 110 260 130] >>",
	}, "1 0 R")
}

func buildCLIPDFObjects(t *testing.T, objects []string, root string) []byte {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, len(objects))
	buf.WriteString("%PDF-1.4\n")
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root %s >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, root, xrefOffset)
	return buf.Bytes()
}
