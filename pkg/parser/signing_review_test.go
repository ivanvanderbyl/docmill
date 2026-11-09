package parser_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func signingReviewPDF(catalogExtra string, fields ...string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] >>",
	}
	return imagePDF(append(objects, fields...)...)
}

func TestSigningRejectsUnsafeChangesBeforeCallingKey(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	cases := []struct {
		name  string
		data  []byte
		field string
	}{
		{
			name: "certification",
			data: signingReviewPDF("/Perms << /DocMDP 4 0 R >>", "<< /Type /Sig /Contents <00> >>"),
		},
		{
			name: "legacy usage rights",
			data: signingReviewPDF("/Perms << /UR 4 0 R >>", "<< /Type /Sig /Contents <00> >>"),
		},
		{
			name: "usage rights",
			data: signingReviewPDF("/Perms << /UR3 4 0 R >>", "<< /Type /Sig /Contents <00> >>"),
		},
		{
			name:  "existing terminal field",
			data:  signingReviewPDF("/AcroForm << /Fields [4 0 R] >>", "<< /FT /Tx /T (existing) /V (keep) >>"),
			field: "existing",
		},
		{
			name:  "existing parent field",
			data:  signingReviewPDF("/AcroForm << /Fields [4 0 R] >>", "<< /T (parent) /Kids [5 0 R] >>", "<< /FT /Tx /T (child) /V (keep) /Parent 4 0 R >>"),
			field: "parent",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := parser.NewBackend().OpenBytes(context.Background(), tc.data)
			require.NoError(t, err)
			signer := &recordingSigner{Signer: key}
			var output bytes.Buffer
			err = doc.(*parser.Document).Sign(context.Background(), &output, parser.SignOptions{
				Signer:      signer,
				Certificate: cert,
				SigningTime: when,
				FieldName:   tc.field,
			})
			require.Error(t, err)
			require.Zero(t, signer.calls, "refused changes must not reach an HSM or remote signer")
			require.Empty(t, output.Bytes())
		})
	}
	// A successful control ensures that the guards are not merely an API which
	// rejects every request before exercising the external-key boundary.
	signer := &recordingSigner{Signer: key}
	var output bytes.Buffer
	require.NoError(t, signingDocument(t).Sign(context.Background(), &output, parser.SignOptions{
		Signer:      signer,
		Certificate: cert,
		SigningTime: when,
	}))
	require.Equal(t, 1, signer.calls)
}

func TestSigningRejectsEncryptedInputBeforeCallingKey(t *testing.T) {
	data, err := os.ReadFile("testdata/security/r6.pdf")
	require.NoError(t, err)
	doc, err := (&parser.Backend{}).OpenBytesWithPassword(context.Background(), data, "user")
	require.NoError(t, err)
	key, cert, when := signingIdentity(t, "ecdsa")
	signer := &recordingSigner{Signer: key}
	var output bytes.Buffer
	err = doc.(*parser.Document).Sign(context.Background(), &output, parser.SignOptions{
		Signer:      signer,
		Certificate: cert,
		SigningTime: when,
	})
	require.Error(t, err)
	require.Zero(t, signer.calls)
	require.Empty(t, output.Bytes())
}

func TestSigningDoesNotMutateSourceDocument(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	doc := signingDocument(t)
	ctx := context.Background()
	values, err := doc.AcroFormValues(ctx)
	require.NoError(t, err)
	var before bytes.Buffer
	require.NoError(t, doc.WritePDF(ctx, &before))
	var signed bytes.Buffer
	require.NoError(t, doc.Sign(ctx, &signed, parser.SignOptions{
		Signer:      key,
		Certificate: cert,
		SigningTime: when,
		FieldName:   "reviewed",
	}))
	originalSignatures, err := doc.Signatures(ctx)
	require.NoError(t, err)
	require.Empty(t, originalSignatures)
	remainingValues, err := doc.AcroFormValues(ctx)
	require.NoError(t, err)
	require.Equal(t, values, remainingValues)
	var after bytes.Buffer
	require.NoError(t, doc.WritePDF(ctx, &after))
	require.Equal(t, before.Bytes(), after.Bytes())
	reopened, err := parser.NewBackend().OpenBytes(ctx, signed.Bytes())
	require.NoError(t, err)
	signatures, err := reopened.(*parser.Document).Signatures(ctx)
	require.NoError(t, err)
	require.Len(t, signatures, 1)
}

func TestSigningCoversPendingUnsignedEdits(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	source := signingDocument(t)
	ctx := context.Background()
	_, err := source.SetAcroFormValues(ctx, map[string]string{"private": "Updated before signing"})
	require.NoError(t, err)
	var output bytes.Buffer
	require.NoError(t, source.Sign(ctx, &output, parser.SignOptions{
		Signer:      key,
		Certificate: cert,
		SigningTime: when,
	}))
	reopened, err := parser.NewBackend().OpenBytes(ctx, output.Bytes())
	require.NoError(t, err)
	signed := reopened.(*parser.Document)
	values, err := signed.AcroFormValues(ctx)
	require.NoError(t, err)
	require.Equal(t, "Updated before signing", values["private"])
	results, err := signed.VerifySignatures(ctx, parser.VerifyOptions{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].IntegrityError)
	require.True(t, results[0].CoversWholeDocument)
}

func TestSigningRejectsPendingEditsToSignedDocument(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	ctx := context.Background()
	var first bytes.Buffer
	require.NoError(t, signingDocument(t).Sign(ctx, &first, parser.SignOptions{
		Signer:      key,
		Certificate: cert,
		SigningTime: when,
	}))
	opened, err := parser.NewBackend().OpenBytes(ctx, first.Bytes())
	require.NoError(t, err)
	source := opened.(*parser.Document)
	_, err = source.SetAcroFormValues(ctx, map[string]string{"private": "Pending signed edit"})
	require.NoError(t, err)
	signer := &recordingSigner{Signer: key}
	var second bytes.Buffer
	err = source.Sign(ctx, &second, parser.SignOptions{
		Signer:      signer,
		Certificate: cert,
		SigningTime: when,
	})
	require.ErrorContains(t, err, "unsaved")
	require.Zero(t, signer.calls)
	require.Empty(t, second.Bytes())
}

// Add a no-op incremental xref-stream revision to test signing a source whose
// last startxref targets a stream, while retaining a real earlier CMS signature.
func signingReviewXrefStream(t *testing.T, original []byte) []byte {
	t.Helper()
	tail := original[bytes.LastIndex(original, []byte("trailer")):]
	rootMatch := regexp.MustCompile(`/Root\s+(\d+)\s+(\d+)\s+R`).FindSubmatch(tail)
	sizeMatch := regexp.MustCompile(`/Size\s+(\d+)`).FindSubmatch(tail)
	require.Len(t, rootMatch, 3)
	require.Len(t, sizeMatch, 2)
	number, err := strconv.Atoi(string(sizeMatch[1]))
	require.NoError(t, err)
	previousText := bytes.Fields(original[bytes.LastIndex(original, []byte("startxref"))+len("startxref"):])
	require.NotEmpty(t, previousText)
	previous, err := strconv.ParseInt(string(previousText[0]), 10, 64)
	require.NoError(t, err)
	var output bytes.Buffer
	output.Write(original)
	output.WriteByte('\n')
	offset := output.Len()
	var entry [7]byte
	entry[0] = 1
	binary.BigEndian.PutUint32(entry[1:5], uint32(offset))
	fmt.Fprintf(&output, "%d 0 obj\n<< /Type /XRef /Size %d /Root %s %s R /Prev %d /Index [%d 1] /W [1 4 2] /Length 7 >>\nstream\n", number, number+1, rootMatch[1], rootMatch[2], previous, number)
	output.Write(entry[:])
	fmt.Fprintf(&output, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", offset)
	return output.Bytes()
}

func TestSigningAfterXrefStreamRevisionPreservesSignatures(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	options := parser.SignOptions{
		Signer:      key,
		Certificate: cert,
		SigningTime: when,
	}
	ctx := context.Background()
	var first bytes.Buffer
	require.NoError(t, signingDocument(t).Sign(ctx, &first, options))
	updated := signingReviewXrefStream(t, first.Bytes())
	opened, err := parser.NewBackend().OpenBytes(ctx, updated)
	require.NoError(t, err)
	earlier, err := opened.(*parser.Document).VerifySignatures(ctx, parser.VerifyOptions{})
	require.NoError(t, err)
	require.Len(t, earlier, 1)
	require.NoError(t, earlier[0].IntegrityError)
	require.True(t, earlier[0].HasUnsignedChanges)
	var second bytes.Buffer
	require.NoError(t, opened.(*parser.Document).Sign(ctx, &second, options))
	require.True(t, bytes.HasPrefix(second.Bytes(), updated))
	result, err := parser.NewBackend().OpenBytes(ctx, second.Bytes())
	require.NoError(t, err)
	signatures, err := result.(*parser.Document).VerifySignatures(ctx, parser.VerifyOptions{})
	require.NoError(t, err)
	require.Len(t, signatures, 2)
	for _, signature := range signatures {
		require.NoError(t, signature.IntegrityError)
	}
	require.True(t, signatures[0].HasUnsignedChanges)
	require.True(t, signatures[1].CoversWholeDocument)
}
