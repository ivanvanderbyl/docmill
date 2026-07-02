package document

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
)

func TestPageFormFieldsReadsParentFieldWidgetValue(t *testing.T) {
	t.Parallel()

	root := objects.NewDictionary()
	acroForm := root.SetNewDictFor("AcroForm")
	fields := acroForm.SetNewArrayFor("Fields")

	group := objects.NewDictionary()
	group.SetFor("T", objects.NewString("applicant", false))
	fields.Append(group)

	field := objects.NewDictionary()
	field.SetFor("Parent", group)
	field.SetFor("T", objects.NewString("name", false))
	field.SetFor("TU", objects.NewString("Applicant name", false))
	field.SetNewNameFor("FT", "Tx")
	field.SetFor("V", objects.NewString("Ada Lovelace", false))
	group.SetNewArrayFor("Kids").Append(field)

	widget := objects.NewDictionary()
	widget.SetNewNameFor("Subtype", "Widget")
	widget.SetFor("Parent", field)
	widget.SetFor("Rect", rectArray(120, 710, 260, 730))
	field.SetNewArrayFor("Kids").Append(widget)

	pageDict := objects.NewDictionary()
	pageDict.SetNewArrayFor("Annots").Append(widget)
	doc := &Document{rootDict: root}

	got := doc.PageFormFields(pageDict)

	require.Equal(t, []FormField{
		{
			Name:          "applicant.name",
			AlternateName: "Applicant name",
			Type:          "Tx",
			Value:         "Ada Lovelace",
			Rect:          crt.NewFloatRect(120, 710, 260, 730),
		},
	}, got)
}

func TestPageFormFieldWidgetsKeepsUnfilledFieldsAndEveryWidget(t *testing.T) {
	t.Parallel()

	root := objects.NewDictionary()
	acroForm := root.SetNewDictFor("AcroForm")
	fields := acroForm.SetNewArrayFor("Fields")

	// An unfilled text field: PageFormFields drops it, the layout walk must not.
	text := objects.NewDictionary()
	text.SetFor("T", objects.NewString("email", false))
	text.SetFor("TU", objects.NewString("Email address", false))
	text.SetNewNameFor("FT", "Tx")
	fields.Append(text)

	textWidget := objects.NewDictionary()
	textWidget.SetNewNameFor("Subtype", "Widget")
	textWidget.SetFor("Parent", text)
	textWidget.SetFor("Rect", rectArray(120, 710, 260, 730))
	text.SetNewArrayFor("Kids").Append(textWidget)

	// A radio group with two widgets: one box per widget, not one per field.
	radio := objects.NewDictionary()
	radio.SetFor("T", objects.NewString("colour", false))
	radio.SetNewNameFor("FT", "Btn")
	radio.SetNewNameFor("V", "Red")
	fields.Append(radio)

	radioKids := radio.SetNewArrayFor("Kids")
	radioWidgets := make([]*objects.Dictionary, 2)
	for i, bottom := range []float32{660, 630} {
		w := objects.NewDictionary()
		w.SetNewNameFor("Subtype", "Widget")
		w.SetFor("Parent", radio)
		w.SetFor("Rect", rectArray(120, bottom, 140, bottom+20))
		radioKids.Append(w)
		radioWidgets[i] = w
	}

	pageDict := objects.NewDictionary()
	annots := pageDict.SetNewArrayFor("Annots")
	annots.Append(textWidget)
	annots.Append(radioWidgets[0])
	annots.Append(radioWidgets[1])
	doc := &Document{rootDict: root}

	// Sanity-check the contrast: the filled-values walk drops the unfilled
	// text field and collapses the radio group to its first widget.
	require.Len(t, doc.PageFormFields(pageDict), 1)

	got := doc.PageFormFieldWidgets(pageDict)

	require.Equal(t, []FormField{
		{
			Name:          "email",
			AlternateName: "Email address",
			Type:          "Tx",
			Rect:          crt.NewFloatRect(120, 710, 260, 730),
		},
		{
			Name:  "colour",
			Type:  "Btn",
			Value: "Red",
			Rect:  crt.NewFloatRect(120, 660, 140, 680),
		},
		{
			Name:  "colour",
			Type:  "Btn",
			Value: "Red",
			Rect:  crt.NewFloatRect(120, 630, 140, 650),
		},
	}, got)
}

func TestAcroFormFieldValuesIncludesBlankFields(t *testing.T) {
	t.Parallel()

	doc, perr := Open(singlePageTextAcroFormPDF(t, ""))
	require.Equal(t, parser.Success, perr)

	got := doc.AcroFormFieldValues()

	require.Equal(t, map[string]string{"Applicant name": ""}, got)
}

func TestSetAcroFormFieldValuesWritesReopenablePDF(t *testing.T) {
	t.Parallel()

	doc, perr := Open(singlePageTextAcroFormPDF(t, "Ada Lovelace"))
	require.Equal(t, parser.Success, perr)

	changed, err := doc.SetAcroFormFieldValues(map[string]string{"Applicant name": "Grace Hopper"})
	require.NoError(t, err)
	require.Equal(t, 1, changed)

	var out bytes.Buffer
	require.NoError(t, doc.WritePDF(&out))

	reopened, perr := Open(out.Bytes())
	require.Equal(t, parser.Success, perr)
	require.Equal(t, map[string]string{"Applicant name": "Grace Hopper"}, reopened.AcroFormFieldValues())
}

func rectArray(left, bottom, right, top float32) *objects.Array {
	rect := objects.NewArray()
	rect.Append(objects.NewNumberFromFloat(left))
	rect.Append(objects.NewNumberFromFloat(bottom))
	rect.Append(objects.NewNumberFromFloat(right))
	rect.Append(objects.NewNumberFromFloat(top))
	return rect
}

func singlePageTextAcroFormPDF(t *testing.T, value string) []byte {
	t.Helper()

	return buildPDFObjects(t, []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << >> /Contents 4 0 R /Annots [7 0 R] >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Fields [6 0 R] >>",
		fmt.Sprintf("<< /FT /Tx /T (Applicant name) /V (%s) /Kids [7 0 R] >>", value),
		"<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [120 110 260 130] >>",
	}, "1 0 R")
}

func buildPDFObjects(t *testing.T, objects []string, root string) []byte {
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
