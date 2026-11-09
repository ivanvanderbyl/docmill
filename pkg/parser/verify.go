package parser

import (
	"context"
	"crypto/x509"
	"errors"
	"time"
)

// VerifyOptions configures certificate-chain validation. Nil Roots skips trust
// validation. Use a pool built with x509.NewCertPool for local validation; a
// SystemCertPool may invoke platform trust behavior. CurrentTime defaults to now.
type VerifyOptions struct {
	Roots         *x509.CertPool
	Intermediates *x509.CertPool
	CurrentTime   time.Time
}

// VerificationNotChecked means no verification was performed for this property.
const VerificationNotChecked = "not_checked"

// SignatureVerification separates signed-byte integrity, certificate trust,
// and coverage. IntegrityValid alone does not authenticate the current PDF.
// ClaimedSigningTime is authenticated CMS metadata, not a trusted timestamp.
type SignatureVerification struct {
	Signature            Signature
	IntegrityValid       bool
	IntegrityError       error
	TrustValid           bool
	TrustError           error
	Signer               *x509.Certificate
	SignedRevisionLength int64
	CoversWholeDocument  bool
	HasUnsignedChanges   bool
	ClaimedSigningTime   *time.Time
	TimestampStatus      string
	RevocationStatus     string
}

// VerifySignatures verifies supported detached CMS signatures in the original
// input, not unsaved in-memory edits. The default performs no network access.
// Signature-specific failures are reported in the corresponding result.
// It does not validate timestamps, revocation, or changes allowed by DocMDP.
func (d *Document) VerifySignatures(ctx context.Context, options VerifyOptions) ([]SignatureVerification, error) {
	signatures, err := d.Signatures(ctx)
	if err != nil {
		return nil, err
	}
	if options.CurrentTime.IsZero() {
		options.CurrentTime = time.Now()
	}
	source := d.doc.SourceBytes()
	results := make([]SignatureVerification, 0, len(signatures))
	for _, signature := range signatures {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, verifySignature(source, signature, options))
	}
	return results, nil
}

// ErrInvalidSignatureRange means the signed byte ranges do not match the
// original signature Contents token, or do not cover a whole revision.
var ErrInvalidSignatureRange = errors.New("invalid PDF signature byte range")

// ErrSignatureNotChecked means a preceding error prevented this check.
var ErrSignatureNotChecked = errors.New("signature check not performed")

func verifySignature(source []byte, signature Signature, options VerifyOptions) SignatureVerification {
	result := SignatureVerification{
		Signature:        signature,
		TrustError:       ErrSignatureNotChecked,
		TimestampStatus:  VerificationNotChecked,
		RevocationStatus: VerificationNotChecked,
	}
	signed, revision, err := signatureContent(source, signature)
	if err != nil {
		result.IntegrityError = err
		return result
	}
	result.SignedRevisionLength = revision
	result.CoversWholeDocument = revision == int64(len(source))
	result.HasUnsignedChanges = revision < int64(len(source))
	cert, certificates, claimed, err := verifyCMS(signed, signature.Contents, signature.SubFilter)
	result.Signer = cert

	if err != nil {
		result.IntegrityError = err
		return result
	}
	result.IntegrityValid = true
	result.ClaimedSigningTime = claimed
	result.TrustError = verifySignerTrust(cert, certificates, options)
	result.TrustValid = result.TrustError == nil
	return result
}

func verifySignerTrust(cert *x509.Certificate, certificates []*x509.Certificate, options VerifyOptions) error {
	if options.Roots == nil {
		return ErrNoTrustRoots
	}
	if cert.KeyUsage != 0 && cert.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		return errors.New("signer certificate does not permit digital signatures")
	}
	intermediates := x509.NewCertPool()
	if options.Intermediates != nil {
		intermediates = options.Intermediates.Clone()
	}
	for _, candidate := range certificates {
		if !candidate.Equal(cert) {
			intermediates.AddCert(candidate)
		}
	}
	_, err := cert.Verify(x509.VerifyOptions{
		Roots:         options.Roots,
		Intermediates: intermediates,
		CurrentTime:   options.CurrentTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	return err
}

// ErrNoTrustRoots means no explicit trust anchors were supplied. Cryptographic
// integrity is still checked, but certificate trust is not established.
var ErrNoTrustRoots = errors.New("no signature trust roots supplied")

// ErrUnsupportedSignature means the signature uses an unsupported profile,
// signer arrangement, or cryptographic algorithm.
var ErrUnsupportedSignature = errors.New("unsupported PDF signature")
