package parser_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func signatureOptions(t *testing.T) parser.VerifyOptions {
	t.Helper()
	root, err := os.ReadFile("testdata/signatures/root.pem")
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(root))
	leaf, err := os.ReadFile("testdata/signatures/rsa.pem")
	require.NoError(t, err)
	block, _ := pem.Decode(leaf)
	require.NotNil(t, block)
	certificate, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return parser.VerifyOptions{Roots: roots, CurrentTime: certificate.NotBefore.Add(time.Hour)}
}

func verifyFixture(t *testing.T, data []byte, options parser.VerifyOptions) []parser.SignatureVerification {
	t.Helper()
	document, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
	require.NoError(t, err)
	results, err := document.(*parser.Document).VerifySignatures(context.Background(), options)
	require.NoError(t, err)
	return results
}

func TestVerifyDetachedSignatures(t *testing.T) {
	options := signatureOptions(t)
	for _, name := range []string{"rsa", "rsa-ber", "ecdsa", "pss", "cades"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/signatures/" + name + ".pdf")
			require.NoError(t, err)
			results := verifyFixture(t, data, options)
			require.Len(t, results, 1)
			result := results[0]
			require.NoError(t, result.IntegrityError)
			require.True(t, result.IntegrityValid)
			require.NoError(t, result.TrustError)
			require.True(t, result.TrustValid)
			require.True(t, result.CoversWholeDocument)
			require.False(t, result.HasUnsignedChanges)
			require.Equal(t, int64(len(data)), result.SignedRevisionLength)
			require.NotNil(t, result.Signer)
			require.Equal(t, parser.VerificationNotChecked, result.TimestampStatus)
			require.Equal(t, parser.VerificationNotChecked, result.RevocationStatus)
		})
	}
}

func TestSignatureIntegrityTrustAndCoverageAreIndependent(t *testing.T) {
	original, err := os.ReadFile("testdata/signatures/rsa.pdf")
	require.NoError(t, err)
	options := signatureOptions(t)
	t.Run("tampered signed bytes", func(t *testing.T) {
		data := bytes.Replace(original, []byte("200 200"), []byte("201 200"), 1)
		results := verifyFixture(t, data, options)
		require.Len(t, results, 1)
		require.Error(t, results[0].IntegrityError)
		require.False(t, results[0].IntegrityValid)
	})
	t.Run("untrusted embedded root", func(t *testing.T) {
		options.Roots = x509.NewCertPool()
		results := verifyFixture(t, original, options)
		require.Len(t, results, 1)
		require.NoError(t, results[0].IntegrityError)
		require.True(t, results[0].IntegrityValid)
		require.Error(t, results[0].TrustError)
		require.False(t, results[0].TrustValid)
	})
	t.Run("expired now", func(t *testing.T) {
		options := signatureOptions(t)
		options.CurrentTime = options.CurrentTime.AddDate(2, 0, 0)
		results := verifyFixture(t, original, options)
		require.NoError(t, results[0].IntegrityError)
		require.Error(t, results[0].TrustError)
	})
	t.Run("appended unsigned revision", func(t *testing.T) {
		data := append(append([]byte{}, original...), []byte("\n% appended unsigned bytes\n")...)
		results := verifyFixture(t, data, signatureOptions(t))
		require.NoError(t, results[0].IntegrityError)
		require.False(t, results[0].CoversWholeDocument)
		require.True(t, results[0].HasUnsignedChanges)
		require.Equal(t, int64(len(original)), results[0].SignedRevisionLength)
	})
}

func TestInvalidCMSSignatureCannotPassDigestCheckAlone(t *testing.T) {
	data, err := os.ReadFile("testdata/signatures/rsa.pdf")
	require.NoError(t, err)
	doc, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
	require.NoError(t, err)
	signatures, err := doc.(*parser.Document).Signatures(context.Background())
	require.NoError(t, err)
	require.Len(t, signatures, 1)
	var raw asn1.RawValue
	_, err = asn1.Unmarshal(signatures[0].Contents, &raw)
	require.NoError(t, err)
	position := int(signatures[0].ContentsStart) + 1 + 2*(len(raw.FullBytes)-1)
	if data[position] == '0' {
		data[position] = '1'
	} else {
		data[position] = '0'
	}
	results := verifyFixture(t, data, signatureOptions(t))
	require.Error(t, results[0].IntegrityError)
	require.False(t, results[0].IntegrityValid)
}

func TestMultipleSignedRevisions(t *testing.T) {
	data, err := os.ReadFile("testdata/signatures/multiple.pdf")
	require.NoError(t, err)
	results := verifyFixture(t, data, signatureOptions(t))
	require.Len(t, results, 2)
	for _, result := range results {
		require.NoError(t, result.IntegrityError)
		require.NoError(t, result.TrustError)
	}
	require.False(t, results[0].CoversWholeDocument)
	require.True(t, results[0].HasUnsignedChanges)
	require.True(t, results[1].CoversWholeDocument)
	require.False(t, results[1].HasUnsignedChanges)
	require.Less(t, results[0].SignedRevisionLength, results[1].SignedRevisionLength)
}

func TestSignatureVerificationWithoutTrustRoots(t *testing.T) {
	data, err := os.ReadFile("testdata/signatures/rsa.pdf")
	require.NoError(t, err)
	results := verifyFixture(t, data, parser.VerifyOptions{})
	require.True(t, results[0].IntegrityValid)
	require.ErrorIs(t, results[0].TrustError, parser.ErrNoTrustRoots)
	require.False(t, results[0].TrustValid)
}

func TestUnsupportedSignatureProfile(t *testing.T) {
	data, err := os.ReadFile("testdata/signatures/rsa.pdf")
	require.NoError(t, err)
	data = bytes.Replace(data, []byte("adbe.pkcs7.detached"), []byte("test.pkcs7.detached"), 1)
	results := verifyFixture(t, data, signatureOptions(t))
	require.ErrorIs(t, results[0].IntegrityError, parser.ErrUnsupportedSignature)
}

func TestSignatureFixturesVerifyWithOpenSSL(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("OpenSSL unavailable")
	}
	for _, name := range []string{"rsa", "rsa-ber", "ecdsa", "pss", "cades", "multiple"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/signatures/" + name + ".pdf")
			require.NoError(t, err)
			doc, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
			require.NoError(t, err)
			signatures, err := doc.(*parser.Document).Signatures(context.Background())
			require.NoError(t, err)
			for _, signature := range signatures {
				r := signature.ByteRange
				content := append(append([]byte{}, data[:r[1]]...), data[r[2]:r[2]+r[3]]...)
				dir := t.TempDir()
				cmsPath := filepath.Join(dir, "signature.cms")
				dataPath := filepath.Join(dir, "signed.bin")
				require.NoError(t, os.WriteFile(cmsPath, signature.Contents, 0600))
				require.NoError(t, os.WriteFile(dataPath, content, 0600))
				command := exec.Command(openssl, "cms", "-verify", "-binary", "-inform", "DER", "-in", cmsPath, "-content", dataPath, "-CAfile", "testdata/signatures/root.pem", "-purpose", "any", "-out", filepath.Join(dir, "verified.bin"))
				output, err := command.CombinedOutput()
				require.NoError(t, err, string(output))
			}
		})
	}
}
