package document

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	"github.com/stretchr/testify/require"
)

func signingFixture(t *testing.T) *Document {
	t.Helper()
	data := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] >> >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] /Resources << >> >>",
		"<< /FT /Tx /T (name) /V (Ada) >>",
		"<< /T (Signature1) /Kids [6 0 R] >>",
		"<< /FT /Tx /T (child) /Parent 5 0 R >>",
	}, "1 0 R")
	d, e := Open(data)
	require.Equal(t, parser.Success, e)
	return d
}

func TestPreparedSignatureRangesAndMetadata(t *testing.T) {
	d := signingFixture(t)
	opts := SigningOptions{
		ContentsSize: 128,
		Name:         "Alice",
		Reason:       "Approuvé",
		Location:     "東京",
		SigningTime:  time.Date(2026, 11, 9, 12, 30, 45, 0, time.UTC),
	}
	prepared, err := d.PrepareSignature(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, "Signature2", prepared.FieldName)
	final, err := prepared.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	require.Equal(t, prepared.ContentsStart, prepared.ByteRange[1])
	require.Equal(t, prepared.ContentsEnd, prepared.ByteRange[2])
	require.Equal(t, int64(len(final)), prepared.ByteRange[2]+prepared.ByteRange[3])
	require.Equal(t, int64(258), prepared.ContentsEnd-prepared.ContentsStart)
	want := append(append([]byte{}, final[:prepared.ContentsStart]...), final[prepared.ContentsEnd:]...)
	require.Equal(t, want, prepared.SignedBytes())
	opened, e := Open(final)
	require.Equal(t, parser.Success, e)
	signatures, err := opened.Signatures()
	require.NoError(t, err)
	require.Len(t, signatures, 1)
	require.Equal(t, "Approuvé", signatures[0].Reason)
	require.Equal(t, "東京", signatures[0].Location)
	require.Equal(t, opts.SigningTime, signatures[0].SigningTime.UTC())
	require.Equal(t, []int64{prepared.ByteRange[0], prepared.ByteRange[1], prepared.ByteRange[2], prepared.ByteRange[3]}, signatures[0].ByteRange)
	require.Equal(t, "Ada", opened.AcroFormFieldValues()["name"])
	require.Equal(t, 1, opened.GetPageCount())
}

func TestPreparedSignatureAppendsWithoutChangingPriorRevision(t *testing.T) {
	first, err := signingFixture(t).PrepareSignature(context.Background(), SigningOptions{ContentsSize: 64})
	require.NoError(t, err)
	signed, err := first.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	d, e := Open(signed)
	require.Equal(t, parser.Success, e)
	second, err := d.PrepareSignature(context.Background(), SigningOptions{ContentsSize: 96})
	require.NoError(t, err)
	twice, err := second.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(twice, signed))
	opened, e := Open(twice)
	require.Equal(t, parser.Success, e)
	sigs, err := opened.Signatures()
	require.NoError(t, err)
	require.Len(t, sigs, 2)
	require.Equal(t, first.ContentsStart, sigs[0].ContentsStart)
	require.Equal(t, first.ContentsEnd, sigs[0].ContentsEnd)
	require.Equal(t, int64(len(signed)), sigs[0].ByteRange[2]+sigs[0].ByteRange[3])
	require.Equal(t, "Signature3", second.FieldName)
	validateSigningQPDF(t, twice)
}

func validateSigningQPDF(t *testing.T, data []byte) {
	t.Helper()
	qpdf, err := exec.LookPath("qpdf")
	if err != nil {
		t.Skip("qpdf unavailable")
	}
	path := filepath.Join(t.TempDir(), "signed.pdf")
	require.NoError(t, os.WriteFile(path, data, 0600))
	out, err := exec.Command(qpdf, "--check", path).CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestPreparedSignatureRejectsConflictsAndInvalidCapacity(t *testing.T) {
	d := signingFixture(t)
	for _, opts := range []SigningOptions{{FieldName: "name"}, {FieldName: "Signature1"}, {FieldName: "Signature1.child"}, {FieldName: "bad.name"}, {ContentsSize: -1}, {ContentsSize: 2 * 1024 * 1024}} {
		_, err := d.PrepareSignature(context.Background(), opts)
		require.Error(t, err)
	}
	p, err := d.PrepareSignature(context.Background(), SigningOptions{ContentsSize: 1})
	require.NoError(t, err)
	_, err = p.Finalize([]byte{0x30, 0})
	require.ErrorIs(t, err, ErrSignatureCapacity)
	_, err = p.Finalize(nil)
	require.Error(t, err)
	p, err = d.PrepareSignature(context.Background(), SigningOptions{ContentsSize: 16})
	require.NoError(t, err)
	_, err = p.Finalize([]byte{0x30, 1})
	require.Error(t, err)
	first, err := p.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	first[0] = 0
	second, err := p.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	require.Equal(t, byte('%'), second[0])
}

func TestPreparedSignatureRejectsSignedEditsAndRepairedXref(t *testing.T) {
	p, err := signingFixture(t).PrepareSignature(context.Background(), SigningOptions{ContentsSize: 64})
	require.NoError(t, err)
	data, err := p.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	d, e := Open(data)
	require.Equal(t, parser.Success, e)
	_, err = d.SetAcroFormFieldValues(map[string]string{"name": "Grace"})
	require.NoError(t, err)
	_, err = d.PrepareSignature(context.Background(), SigningOptions{})
	require.ErrorContains(t, err, "unsaved")
	damaged := append([]byte{}, data...)
	start := bytes.LastIndex(damaged, []byte("startxref\n")) + len("startxref\n")
	for i := start; damaged[i] >= '0' && damaged[i] <= '9'; i++ {
		damaged[i] = '0'
	}
	rebuilt, e := Open(damaged)
	require.Equal(t, parser.Success, e)
	_, err = rebuilt.PrepareSignature(context.Background(), SigningOptions{})
	require.ErrorContains(t, err, "xref")
}

func TestPreparedSignatureIncludesUnsignedEdits(t *testing.T) {
	d := signingFixture(t)
	_, err := d.SetAcroFormFieldValues(map[string]string{"name": "Grace"})
	require.NoError(t, err)
	prepared, err := d.PrepareSignature(context.Background(), SigningOptions{})
	require.NoError(t, err)
	data, err := prepared.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	reopened, e := Open(data)
	require.Equal(t, parser.Success, e)
	require.Equal(t, "Grace", reopened.AcroFormFieldValues()["name"])
}

func TestPreparedSignaturePreservesNonzeroGenerations(t *testing.T) {
	var data bytes.Buffer
	data.WriteString("%PDF-1.7\n")
	bodies := []string{"<< /Type /Catalog /Pages 2 3 R >>", "<< /Type /Pages /Count 1 /Kids [3 3 R] >>", "<< /Type /Page /Parent 2 3 R /MediaBox [0 0 200 300] /Resources << >> >>"}
	offsets := make([]int, len(bodies))
	for i, body := range bodies {
		offsets[i] = data.Len()
		fmt.Fprintf(&data, "%d 3 obj\n%s\nendobj\n", i+1, body)
	}
	xref := data.Len()
	data.WriteString("xref\n0 4\n0000000000 65535 f \n")
	for _, offset := range offsets {
		fmt.Fprintf(&data, "%010d 00003 n \n", offset)
	}
	fmt.Fprintf(&data, "trailer << /Root 1 3 R /Size 4 >>\nstartxref\n%d\n%%%%EOF\n", xref)
	source, e := Open(data.Bytes())
	require.Equal(t, parser.Success, e)
	first, err := source.PrepareSignature(context.Background(), SigningOptions{ContentsSize: 64})
	require.NoError(t, err)
	signed, err := first.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	// Simulate a signed input whose live objects have nonzero generations.
	// All substitutions preserve byte offsets and this test checks structure,
	// not CMS validity (the public signer tests cover cryptographic integrity).
	signed = bytes.ReplaceAll(signed, []byte(" 0 obj"), []byte(" 3 obj"))
	signed = bytes.ReplaceAll(signed, []byte(" 0 R"), []byte(" 3 R"))
	signed = bytes.ReplaceAll(signed, []byte("00000 n"), []byte("00003 n"))
	d, e := Open(signed)
	require.Equal(t, parser.Success, e)
	next, err := d.PrepareSignature(context.Background(), SigningOptions{ContentsSize: 64})
	require.NoError(t, err)
	twice, err := next.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	validateSigningQPDF(t, twice)
}

func TestIncrementalSignaturePreservesWideIntegerLexemes(t *testing.T) {
	d := signingFixture(t)
	d.rootDict.SetFor("WideInteger", objects.NewNumberFromString("2147483648"))
	revision, root, err := d.signatureRevision(SigningOptions{FieldName: "Wide", ContentsSize: 64})
	require.NoError(t, err)
	require.NoError(t, revision.writeObject(revision.records[root], 0, map[objects.Object]bool{}))
	require.Contains(t, revision.data.String(), "/WideInteger 2147483648")
	revision.data.Reset()
	require.NoError(t, revision.writeObject(objects.NewNumberFromString("2147483648"), 0, map[objects.Object]bool{}))
	require.Equal(t, "2147483648", revision.data.String())
}

func TestPreparedSignaturePreservesFirstRevisionAndAddsInvisibleWidget(t *testing.T) {
	d := signingFixture(t)
	prepared, err := d.PrepareSignature(context.Background(), SigningOptions{FieldName: "承認", ContentsSize: 64})
	require.NoError(t, err)
	final, err := prepared.Finalize([]byte{0x30, 0})
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(final, d.SourceBytes()))
	reopened, status := Open(final)
	require.Equal(t, parser.Success, status)
	annots := reopened.GetPageDict(0).GetArrayFor("Annots")
	require.NotNil(t, annots)
	require.Equal(t, 1, annots.Len())
	widget := annots.GetDictAt(0)
	require.NotNil(t, widget)
	require.Equal(t, "Widget", widget.GetNameFor("Subtype"))
	require.Equal(t, "承認", widget.GetUnicodeTextFor("T"))
	rectangle := widget.GetArrayFor("Rect")
	require.NotNil(t, rectangle)
	require.Equal(t, 4, rectangle.Len())
	for i := range 4 {
		require.Zero(t, rectangle.GetIntegerAt(i))
	}
	require.Same(t, reopened.GetPageDict(0), widget.GetDictFor("P"))
	validateSigningQPDF(t, final)
}

func TestPreparedSignatureRejectsUnsupportedPoliciesAndEncryption(t *testing.T) {
	for _, key := range []string{"DocMDP", "UR", "UR3"} {
		t.Run(key, func(t *testing.T) {
			d := signingFixture(t)
			permissions := objects.NewDictionary()
			permissions.SetFor(key, objects.NewDictionary())
			d.rootDict.SetFor("Perms", permissions)
			_, err := d.PrepareSignature(context.Background(), SigningOptions{})
			require.ErrorContains(t, err, "policy")
		})
	}
	encrypted, err := os.ReadFile("../../testdata/security/r6.pdf")
	require.NoError(t, err)
	d, status := OpenWithPassword(encrypted, "user")
	require.Equal(t, parser.Success, status)
	_, err = d.PrepareSignature(context.Background(), SigningOptions{})
	require.ErrorContains(t, err, "encrypted")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = signingFixture(t).PrepareSignature(canceled, SigningOptions{})
	require.ErrorIs(t, err, context.Canceled)
	_, err = signingFixture(t).PrepareSignature(context.Background(), SigningOptions{FieldName: " approval "})
	require.Error(t, err)
}
