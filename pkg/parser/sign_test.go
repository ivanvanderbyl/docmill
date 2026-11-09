package parser_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/stretchr/testify/require"
)

func signingIdentity(t *testing.T, kind string) (crypto.Signer, *x509.Certificate, time.Time) {
	t.Helper()
	var key crypto.Signer
	var err error
	if kind == "rsa" {
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	} else {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	require.NoError(t, err)
	when := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Docmill signing test"}, NotBefore: when.Add(-time.Hour), NotAfter: when.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, cert, when
}

func signingDocument(t *testing.T) *parser.Document {
	t.Helper()
	data, err := os.ReadFile("testdata/security/plain.pdf")
	require.NoError(t, err)
	doc, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
	require.NoError(t, err)
	return doc.(*parser.Document)
}

func TestSignRSAAndECDSA(t *testing.T) {
	for _, kind := range []string{"rsa", "ecdsa"} {
		t.Run(kind, func(t *testing.T) {
			key, cert, when := signingIdentity(t, kind)
			doc := signingDocument(t)
			var out bytes.Buffer
			require.NoError(t, doc.Sign(context.Background(), &out, parser.SignOptions{Signer: key, Certificate: cert, SigningTime: when, FieldName: "approval", Reason: "Reviewed", Location: "Test"}))
			signed, err := (&parser.Backend{}).OpenBytes(context.Background(), out.Bytes())
			require.NoError(t, err)
			roots := x509.NewCertPool()
			roots.AddCert(cert)
			results, err := signed.(*parser.Document).VerifySignatures(context.Background(), parser.VerifyOptions{Roots: roots, CurrentTime: when})
			require.NoError(t, err)
			require.Len(t, results, 1)
			require.NoError(t, results[0].IntegrityError)
			require.NoError(t, results[0].TrustError)
			require.True(t, results[0].CoversWholeDocument)
			require.Equal(t, "approval", results[0].Signature.FieldName)
			require.Equal(t, "Reviewed", results[0].Signature.Reason)
			require.Equal(t, when, *results[0].ClaimedSigningTime)
			verifyCreatedSignatureWithOpenSSL(t, out.Bytes(), cert)
		})
	}
}

func verifyCreatedSignatureWithOpenSSL(t *testing.T, data []byte, cert *x509.Certificate) {
	t.Helper()
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		return
	}
	doc, err := (&parser.Backend{}).OpenBytes(context.Background(), data)
	require.NoError(t, err)
	signatures, err := doc.(*parser.Document).Signatures(context.Background())
	require.NoError(t, err)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600))
	for _, sig := range signatures {
		r := sig.ByteRange
		content := append(append([]byte{}, data[:r[1]]...), data[r[2]:r[2]+r[3]]...)
		cmsPath := filepath.Join(dir, "signature.cms")
		dataPath := filepath.Join(dir, "signed.bin")
		require.NoError(t, os.WriteFile(cmsPath, sig.Contents, 0600))
		require.NoError(t, os.WriteFile(dataPath, content, 0600))
		output, err := exec.Command(openssl, "cms", "-verify", "-binary", "-inform", "DER", "-in", cmsPath, "-content", dataPath, "-noverify", "-out", filepath.Join(dir, "verified")).CombinedOutput()
		require.NoError(t, err, string(output))
	}
}

func TestSecondSignaturePreservesFirst(t *testing.T) {
	key, cert, when := signingIdentity(t, "rsa")
	options := parser.SignOptions{Signer: key, Certificate: cert, SigningTime: when}
	var first bytes.Buffer
	require.NoError(t, signingDocument(t).Sign(context.Background(), &first, options))
	reopened, err := (&parser.Backend{}).OpenBytes(context.Background(), first.Bytes())
	require.NoError(t, err)
	var second bytes.Buffer
	require.NoError(t, reopened.(*parser.Document).Sign(context.Background(), &second, options))
	require.True(t, bytes.HasPrefix(second.Bytes(), first.Bytes()))
	twice, err := (&parser.Backend{}).OpenBytes(context.Background(), second.Bytes())
	require.NoError(t, err)
	results, err := twice.(*parser.Document).VerifySignatures(context.Background(), parser.VerifyOptions{})
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		require.NoError(t, result.IntegrityError)
	}
	require.False(t, results[0].CoversWholeDocument)
	require.True(t, results[1].CoversWholeDocument)
	verifyCreatedSignatureWithOpenSSL(t, second.Bytes(), cert)
}

type recordingSigner struct {
	crypto.Signer
	calls   int
	failure error
	bad     bool
	hash    crypto.Hash
	cancel  context.CancelFunc
}

func (s *recordingSigner) Sign(random io.Reader, digest []byte, opts crypto.SignerOpts) ([]byte, error) {
	s.calls++
	s.hash = opts.HashFunc()
	if s.cancel != nil {
		s.cancel()
	}
	if s.failure != nil {
		return nil, s.failure
	}
	if s.bad {
		return []byte("not a signature"), nil
	}
	return s.Signer.Sign(random, digest, opts)
}

func TestSigningCallsExternalKeyOnce(t *testing.T) {
	key, cert, when := signingIdentity(t, "rsa")
	for _, mode := range []string{"success", "failure", "bad signature", "capacity", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			signer := &recordingSigner{Signer: key}
			options := parser.SignOptions{Signer: signer, Certificate: cert, SigningTime: when, Hash: crypto.SHA384}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "failure":
				signer.failure = errors.New("remote signer unavailable")
			case "bad signature":
				signer.bad = true
			case "capacity":
				options.SignatureSize = 64
			case "canceled":
				signer.cancel = cancel
			}
			var out bytes.Buffer
			err := signingDocument(t).Sign(ctx, &out, options)
			require.Equal(t, 1, signer.calls)
			require.Equal(t, crypto.SHA384, signer.hash)
			if mode == "success" {
				require.NoError(t, err)
				require.NotZero(t, out.Len())
			} else {
				require.Error(t, err)
				require.Zero(t, out.Len())
			}
			if mode == "failure" {
				require.ErrorIs(t, err, signer.failure)
			}
			if mode == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}

func TestInvalidSigningOptionsDoNotInvokeSigner(t *testing.T) {
	key, cert, when := signingIdentity(t, "rsa")
	other, _, _ := signingIdentity(t, "ecdsa")
	for _, mode := range []string{"mismatched key", "nil certificate", "invalid DER", "expired", "not yet valid", "key usage", "weak hash", "negative capacity", "nil chain certificate", "pre-canceled", "nil writer", "typed nil signer"} {
		t.Run(mode, func(t *testing.T) {
			signer := &recordingSigner{Signer: key}
			options := parser.SignOptions{Signer: signer, Certificate: cert, SigningTime: when}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out bytes.Buffer
			var writer io.Writer = &out
			switch mode {
			case "mismatched key":
				signer.Signer = other
			case "nil certificate":
				options.Certificate = nil
			case "invalid DER":
				options.Certificate = &x509.Certificate{}
			case "expired":
				options.SigningTime = cert.NotAfter.Add(time.Second)
			case "not yet valid":
				options.SigningTime = cert.NotBefore.Add(-time.Second)
			case "key usage":
				template := *cert
				template.KeyUsage = x509.KeyUsageKeyEncipherment
				template.IsCA = false
				raw, err := x509.CreateCertificate(rand.Reader, &template, cert, key.Public(), key)
				require.NoError(t, err)
				options.Certificate, err = x509.ParseCertificate(raw)
				require.NoError(t, err)
			case "weak hash":
				options.Hash = crypto.SHA1
			case "negative capacity":
				options.SignatureSize = -1
			case "nil chain certificate":
				options.Chain = []*x509.Certificate{nil}
			case "pre-canceled":
				cancel()
			case "nil writer":
				writer = nil
			case "typed nil signer":
				var absent *rsa.PrivateKey
				options.Signer = absent
			}
			err := signingDocument(t).Sign(ctx, writer, options)
			require.Error(t, err)
			require.Zero(t, signer.calls)
			require.Zero(t, out.Len())
		})
	}
}

type shortSigningWriter struct{}

func (shortSigningWriter) Write(data []byte) (int, error) {
	return len(data) - 1, nil
}

func TestSigningReportsShortWrite(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	err := signingDocument(t).Sign(context.Background(), shortSigningWriter{}, parser.SignOptions{Signer: key, Certificate: cert, SigningTime: when, Hash: crypto.SHA512})
	require.ErrorIs(t, err, io.ErrShortWrite)
}

func TestSigningIncludesCertificateChain(t *testing.T) {
	rootKey, root, when := signingIdentity(t, "rsa")
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Leaf signer"}, NotBefore: when.Add(-time.Hour), NotAfter: when.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true}
	raw, err := x509.CreateCertificate(rand.Reader, template, root, key.Public(), rootKey)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(raw)
	require.NoError(t, err)
	var output bytes.Buffer
	require.NoError(t, signingDocument(t).Sign(context.Background(), &output, parser.SignOptions{Signer: key, Certificate: leaf, Chain: []*x509.Certificate{root}, SigningTime: when}))
	doc, err := (&parser.Backend{}).OpenBytes(context.Background(), output.Bytes())
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	results, err := doc.(*parser.Document).VerifySignatures(context.Background(), parser.VerifyOptions{Roots: roots, CurrentTime: when})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].IntegrityError)
	require.NoError(t, results[0].TrustError)
}

func TestInvalidCertificateChainDoesNotInvokeSigner(t *testing.T) {
	key, cert, when := signingIdentity(t, "rsa")
	_, unrelated, _ := signingIdentity(t, "ecdsa")
	signer := &recordingSigner{Signer: key}
	var output bytes.Buffer
	err := signingDocument(t).Sign(context.Background(), &output, parser.SignOptions{Signer: signer, Certificate: cert, Chain: []*x509.Certificate{unrelated}, SigningTime: when})
	require.Error(t, err)
	require.Zero(t, signer.calls)
	require.Zero(t, output.Len())
}

type publicOverrideSigner struct {
	crypto.Signer
	public crypto.PublicKey
}

func (s publicOverrideSigner) Public() crypto.PublicKey {
	return s.public
}

func TestSigningRejectsInvalidCurvePointBeforeCallingKey(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	recorded := &recordingSigner{Signer: key}
	invalid := &ecdsa.PublicKey{Curve: elliptic.P256(), X: big.NewInt(1), Y: big.NewInt(1)}
	signer := publicOverrideSigner{Signer: recorded, public: invalid}
	var output bytes.Buffer
	require.NotPanics(t, func() {
		err := signingDocument(t).Sign(context.Background(), &output, parser.SignOptions{Signer: signer, Certificate: cert, SigningTime: when})
		require.ErrorIs(t, err, parser.ErrInvalidSigningOptions)
	})
	require.Zero(t, recorded.calls)
	require.Zero(t, output.Len())
}

func TestSigningPreservesUnicodeFieldMetadata(t *testing.T) {
	key, cert, when := signingIdentity(t, "ecdsa")
	var output bytes.Buffer
	options := parser.SignOptions{Signer: key, Certificate: cert, SigningTime: when, FieldName: "同意✓", Reason: "Révisé ✓", Location: "München"}
	require.NoError(t, signingDocument(t).Sign(context.Background(), &output, options))
	doc, err := (&parser.Backend{}).OpenBytes(context.Background(), output.Bytes())
	require.NoError(t, err)
	signatures, err := doc.(*parser.Document).Signatures(context.Background())
	require.NoError(t, err)
	require.Len(t, signatures, 1)
	require.Equal(t, options.FieldName, signatures[0].FieldName)
	require.Equal(t, options.Reason, signatures[0].Reason)
	require.Equal(t, options.Location, signatures[0].Location)
}
