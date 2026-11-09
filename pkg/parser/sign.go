package parser

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/digitorus/pkcs7"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/document"
)

// SignOptions supplies an application-owned signing key and certificate.
// Signer must support RSA PKCS#1 v1.5 or ECDSA and honor crypto.Hash options.
// Chain contains optional issuer certificates, ordered from issuer to root.
// No private keys are stored or read from configuration by docmill.
type SignOptions struct {
	Signer      crypto.Signer
	Certificate *x509.Certificate
	Chain       []*x509.Certificate
	// FieldName defaults to an unused SignatureN name.
	FieldName string
	Reason    string
	Location  string
	// SigningTime defaults to now and is a claimed time, not a trusted timestamp.
	SigningTime time.Time
	// Hash defaults to SHA256; SHA384 and SHA512 are also supported.
	Hash crypto.Hash
	// SignatureSize reserves CMS bytes: default 32768, maximum 1048576.
	SignatureSize int
}

// Sign appends a detached signature to the original PDF and writes the result.
// It invokes Signer.Sign at most once and never retries to enlarge the reserve.
// Encrypted, certified, rights-enabled, or edited inputs must first be converted
// deliberately to a suitable new PDF. The signer adapter owns cancellation of
// its own remote calls because crypto.Signer has no context parameter.
func (d *Document) Sign(ctx context.Context, w io.Writer, options SignOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d == nil || d.doc == nil || w == nil {
		return fmt.Errorf("%w: nil document or writer", ErrInvalidSigningOptions)
	}
	normalized, err := normalizeSignOptions(options)
	if err != nil {
		return err
	}
	if d.doc.IsEncrypted() {
		return fmt.Errorf("%w: reopen an explicitly decrypted copy before signing", ErrInvalidSigningOptions)
	}
	priorCount, err := d.validatePriorSignatures(ctx)
	if err != nil {
		return err
	}
	output, err := d.appendSignature(ctx, normalized, priorCount)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	n, err := w.Write(output)
	if err == nil && n != len(output) {
		return io.ErrShortWrite
	}
	return err
}

func (d *Document) appendSignature(ctx context.Context, options SignOptions, priorCount int) ([]byte, error) {
	prepared, err := d.doc.PrepareSignature(ctx, document.SigningOptions{
		FieldName:    options.FieldName,
		ContentsSize: options.SignatureSize,
		Name:         options.Certificate.Subject.CommonName,
		Reason:       options.Reason,
		Location:     options.Location,
		SigningTime:  options.SigningTime,
	})
	if err != nil {
		return nil, err
	}
	content := prepared.SignedBytes()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	der, err := createDetachedSignature(content, options)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output, err := prepared.Finalize(der)
	if err != nil {
		return nil, err
	}
	if err := verifyGeneratedPDF(ctx, output, prepared.FieldName, options.Certificate, priorCount+1); err != nil {
		return nil, err
	}
	return output, nil
}

func verifyGeneratedPDF(ctx context.Context, output []byte, fieldName string, cert *x509.Certificate, wantCount int) error {
	opened, err := (&Backend{}).OpenBytes(ctx, output)
	if err != nil {
		return fmt.Errorf("parse generated signed PDF: %w", err)
	}
	defer opened.Close()
	results, err := opened.(*Document).VerifySignatures(ctx, VerifyOptions{})
	if err != nil {
		return fmt.Errorf("inspect generated signed PDF: %w", err)
	}
	if len(results) != wantCount {
		return errors.New("generated PDF changed the prior signature count")
	}
	found := 0
	for _, result := range results {
		if result.IntegrityError != nil {
			return fmt.Errorf("verify generated signed PDF: %w", result.IntegrityError)
		}
		if result.Signature.FieldName != fieldName {
			continue
		}
		found++
		if !result.CoversWholeDocument || result.Signer == nil || !bytes.Equal(result.Signer.Raw, cert.Raw) {
			return errors.New("generated signature has incorrect coverage or signer")
		}
	}
	if found != 1 {
		return errors.New("generated signature field is missing or duplicated")
	}
	return nil
}

// ErrInvalidSigningOptions indicates invalid signing credentials or options.
var ErrInvalidSigningOptions = errors.New("invalid PDF signing options")

// ErrSignatureCapacity indicates that the single produced CMS signature exceeds
// SignatureSize. No PDF is written and the signer is not called a second time.
var ErrSignatureCapacity = document.ErrSignatureCapacity

func normalizeSignOptions(options SignOptions) (SignOptions, error) {
	if nilCryptoSigner(options.Signer) || options.Certificate == nil {
		return options, fmt.Errorf("%w: signer and certificate required", ErrInvalidSigningOptions)
	}
	cert, err := x509.ParseCertificate(options.Certificate.Raw)
	if err != nil {
		return options, fmt.Errorf("%w: invalid certificate DER: %v", ErrInvalidSigningOptions, err)
	}
	options.Certificate = cert
	if err := checkSigningKey(options.Signer, cert); err != nil {
		return options, err
	}
	if options.SigningTime.IsZero() {
		options.SigningTime = time.Now()
	}
	options.SigningTime = options.SigningTime.UTC().Truncate(time.Second)
	if options.SigningTime.Before(cert.NotBefore) || options.SigningTime.After(cert.NotAfter) {
		return options, fmt.Errorf("%w: certificate is not valid at SigningTime", ErrInvalidSigningOptions)
	}
	if options.Hash == 0 {
		options.Hash = crypto.SHA256
	}
	if _, err := signingDigestOID(options.Hash); err != nil {
		return options, err
	}
	if options.SignatureSize == 0 {
		options.SignatureSize = 32 * 1024
	}
	if options.SignatureSize < 1 || options.SignatureSize > 1<<20 {
		return options, fmt.Errorf("%w: SignatureSize must be between 1 and 1048576 bytes", ErrInvalidSigningOptions)
	}
	options.Chain, err = canonicalSigningChain(options.Chain)
	return options, err
}

func nilCryptoSigner(signer crypto.Signer) bool {
	if signer == nil {
		return true
	}
	value := reflect.ValueOf(signer)
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return value.IsNil()
	}
	return false
}

func checkSigningKey(signer crypto.Signer, cert *x509.Certificate) error {
	if cert.KeyUsage != 0 && cert.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		return fmt.Errorf("%w: certificate does not allow document signatures", ErrInvalidSigningOptions)
	}
	switch cert.PublicKey.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
	default:
		return fmt.Errorf("%w: only RSA and ECDSA signing supported", ErrUnsupportedSignature)
	}
	expected, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
	if err != nil {
		return fmt.Errorf("%w: certificate public key: %v", ErrInvalidSigningOptions, err)
	}
	actual, err := signingPublicKeyDER(signer.Public())
	if err != nil || !bytes.Equal(actual, expected) {
		return fmt.Errorf("%w: signer public key does not match certificate", ErrInvalidSigningOptions)
	}
	return nil
}

func canonicalSigningChain(chain []*x509.Certificate) ([]*x509.Certificate, error) {
	result := make([]*x509.Certificate, len(chain))
	for i, cert := range chain {
		if cert == nil {
			return nil, fmt.Errorf("%w: nil chain certificate %d", ErrInvalidSigningOptions, i)
		}
		parsed, err := x509.ParseCertificate(cert.Raw)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid chain certificate %d", ErrInvalidSigningOptions, i)
		}
		result[i] = parsed
	}
	return result, nil
}

func signingDigestOID(hash crypto.Hash) (asn1.ObjectIdentifier, error) {
	switch hash {
	case crypto.SHA256:
		return pkcs7.OIDDigestAlgorithmSHA256, nil
	case crypto.SHA384:
		return pkcs7.OIDDigestAlgorithmSHA384, nil
	case crypto.SHA512:
		return pkcs7.OIDDigestAlgorithmSHA512, nil
	}
	return nil, fmt.Errorf("%w: signing digest must be SHA256, SHA384, or SHA512", ErrUnsupportedSignature)
}

func (d *Document) validatePriorSignatures(ctx context.Context) (int, error) {
	results, err := d.VerifySignatures(ctx, VerifyOptions{})
	if err != nil {
		return 0, err
	}
	for _, result := range results {
		if result.IntegrityError != nil {
			return 0, fmt.Errorf("existing signature %q is not intact: %w", result.Signature.FieldName, result.IntegrityError)
		}
	}
	return len(results), nil
}

func createDetachedSignature(content []byte, options SignOptions) ([]byte, error) {
	signed, err := pkcs7.NewSignedData(content)
	if err != nil {
		return nil, err
	}
	oid, err := signingDigestOID(options.Hash)
	if err != nil {
		return nil, err
	}
	signed.SetDigestAlgorithm(oid)
	config := pkcs7.SignerInfoConfig{
		ExtraSignedAttributes: []pkcs7.Attribute{
			{Type: pkcs7.OIDAttributeSigningTime, Value: options.SigningTime},
		},
	}
	if err := signed.AddSignerChain(options.Certificate, options.Signer, options.Chain, config); err != nil {
		return nil, fmt.Errorf("create CMS signature: %w", err)
	}
	signed.Detach()
	return signed.Finish()
}

func signingPublicKeyDER(public crypto.PublicKey) ([]byte, error) {
	switch key := public.(type) {
	case *rsa.PublicKey:
		if key == nil || key.N == nil || key.E <= 0 || key.N.Sign() <= 0 {
			return nil, errors.New("invalid RSA public key")
		}
	case *ecdsa.PublicKey:
		if key == nil || key.Curve == nil || key.X == nil || key.Y == nil {
			return nil, errors.New("invalid ECDSA public key")
		}
		if !key.Curve.IsOnCurve(key.X, key.Y) {
			return nil, errors.New("invalid ECDSA public point")
		}
	default:
		return nil, errors.New("unsupported signing public key")
	}
	return x509.MarshalPKIXPublicKey(public)
}
