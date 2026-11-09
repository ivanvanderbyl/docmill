package parser

import (
	"bytes"
	"crypto"
	"crypto/subtle"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"

	_ "crypto/sha256"
	_ "crypto/sha512"

	"github.com/digitorus/pkcs7"
)

type cmsAttribute struct {
	Type  asn1.ObjectIdentifier
	Value asn1.RawValue `asn1:"set"`
}

func verifyCMS(content, padded []byte, subFilter string) (*x509.Certificate, []*x509.Certificate, *time.Time, error) {
	if subFilter != "adbe.pkcs7.detached" && subFilter != "ETSI.CAdES.detached" {
		return nil, nil, nil, fmt.Errorf("%w: SubFilter %q", ErrUnsupportedSignature, subFilter)
	}
	der, err := cmsEnvelopeBytes(padded)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := checkDetachedContent(der); err != nil {
		return nil, nil, nil, err
	}
	message, err := pkcs7.Parse(der)
	if err != nil {
		return nil, nil, nil, err
	}
	cert := message.GetOnlySigner()
	if cert == nil {
		return nil, nil, nil, fmt.Errorf("%w: CMS must contain exactly one identified signer", ErrUnsupportedSignature)
	}
	signer := message.Signers[0]
	if !cmsNullOrAbsent(signer.DigestAlgorithm.Parameters) {
		return cert, message.Certificates, nil, fmt.Errorf("%w: digest parameters", ErrUnsupportedSignature)
	}
	hash, err := signatureHash(signer.DigestAlgorithm.Algorithm)
	if err != nil {
		return cert, message.Certificates, nil, err
	}
	attrs := make([]cmsAttribute, len(signer.AuthenticatedAttributes))
	for i, a := range signer.AuthenticatedAttributes {
		attrs[i] = cmsAttribute{Type: a.Type, Value: a.Value}
	}
	data, claimed, err := authenticatedContent(content, attrs, hash, cert, subFilter)
	if err != nil {
		return cert, message.Certificates, claimed, err
	}
	algorithm, err := cmsSignatureAlgorithm(signer.DigestEncryptionAlgorithm, hash)
	if err == nil {
		err = cert.CheckSignature(algorithm, data, signer.EncryptedDigest)
	}
	return cert, message.Certificates, claimed, err
}

func signatureHash(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(pkcs7.OIDDigestAlgorithmSHA256):
		return crypto.SHA256, nil
	case oid.Equal(pkcs7.OIDDigestAlgorithmSHA384):
		return crypto.SHA384, nil
	case oid.Equal(pkcs7.OIDDigestAlgorithmSHA512):
		return crypto.SHA512, nil
	default:
		return 0, fmt.Errorf("%w: CMS digest %s", ErrUnsupportedSignature, oid)
	}
}

func authenticatedContent(content []byte, attrs []cmsAttribute, hash crypto.Hash, cert *x509.Certificate, subFilter string) ([]byte, *time.Time, error) {
	if len(attrs) == 0 {
		if subFilter == "ETSI.CAdES.detached" {
			return nil, nil, errors.New("CAdES requires signed attributes")
		}
		return content, nil, nil
	}
	values := make(map[string]asn1.RawValue, len(attrs))
	for _, a := range attrs {
		key := a.Type.String()
		if _, exists := values[key]; exists {
			return nil, nil, errors.New("duplicate CMS signed attribute")
		}
		values[key] = a.Value
	}
	var typ asn1.ObjectIdentifier
	if err := readCMSAttribute(values, pkcs7.OIDAttributeContentType, &typ); err != nil || !typ.Equal(pkcs7.OIDData) {
		return nil, nil, errors.New("CMS content-type attribute must be id-data")
	}
	var digest []byte
	if err := readCMSAttribute(values, pkcs7.OIDAttributeMessageDigest, &digest); err != nil {
		return nil, nil, err
	}
	h := hash.New()
	_, _ = h.Write(content)
	if subtle.ConstantTimeCompare(digest, h.Sum(nil)) != 1 {
		return nil, nil, errors.New("signed byte digest mismatch")
	}
	var claimed *time.Time
	if _, ok := values[pkcs7.OIDAttributeSigningTime.String()]; ok {
		var when time.Time
		if err := readCMSAttribute(values, pkcs7.OIDAttributeSigningTime, &when); err != nil {
			return nil, nil, err
		}
		claimed = &when
	}
	if subFilter == "ETSI.CAdES.detached" {
		if err := checkSigningCertificate(values, cert); err != nil {
			return nil, claimed, err
		}
	}
	encoded, err := asn1.Marshal(struct {
		Attributes []cmsAttribute `asn1:"set"`
	}{attrs})
	if err != nil {
		return nil, claimed, err
	}
	var raw asn1.RawValue
	if _, err := asn1.Unmarshal(encoded, &raw); err != nil {
		return nil, claimed, err
	}
	return raw.Bytes, claimed, nil
}

func readCMSAttribute(values map[string]asn1.RawValue, oid asn1.ObjectIdentifier, out any) error {
	value, ok := values[oid.String()]
	if !ok {
		return fmt.Errorf("missing CMS signed attribute %s", oid)
	}
	if value.Class != 0 || value.Tag != asn1.TagSet {
		return errors.New("CMS attribute is not a value SET")
	}
	rest, err := asn1.Unmarshal(value.Bytes, out)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return errors.New("CMS attribute must contain one value")
	}
	return nil
}

func cmsSignatureAlgorithm(algorithm pkix.AlgorithmIdentifier, hash crypto.Hash) (x509.SignatureAlgorithm, error) {
	if algorithm.Algorithm.Equal(pkcs7.OIDEncryptionAlgorithmRSAPSS) {
		return cmsPSSAlgorithm(algorithm.Parameters.FullBytes, hash)
	}
	if len(algorithm.Parameters.FullBytes) > 0 && !bytes.Equal(algorithm.Parameters.FullBytes, []byte{5, 0}) {
		return 0, fmt.Errorf("%w: algorithm parameters", ErrUnsupportedSignature)
	}
	rsa := algorithm.Algorithm.Equal(pkcs7.OIDEncryptionAlgorithmRSA)
	pairs := []struct {
		hash          crypto.Hash
		rsa, ec       asn1.ObjectIdentifier
		rsaAlg, ecAlg x509.SignatureAlgorithm
	}{
		{crypto.SHA256, pkcs7.OIDEncryptionAlgorithmRSASHA256, pkcs7.OIDDigestAlgorithmECDSASHA256, x509.SHA256WithRSA, x509.ECDSAWithSHA256},
		{crypto.SHA384, pkcs7.OIDEncryptionAlgorithmRSASHA384, pkcs7.OIDDigestAlgorithmECDSASHA384, x509.SHA384WithRSA, x509.ECDSAWithSHA384},
		{crypto.SHA512, pkcs7.OIDEncryptionAlgorithmRSASHA512, pkcs7.OIDDigestAlgorithmECDSASHA512, x509.SHA512WithRSA, x509.ECDSAWithSHA512},
	}
	for _, pair := range pairs {
		if hash != pair.hash {
			continue
		}
		if rsa || algorithm.Algorithm.Equal(pair.rsa) {
			return pair.rsaAlg, nil
		}
		if algorithm.Algorithm.Equal(pair.ec) {
			return pair.ecAlg, nil
		}
	}
	return 0, fmt.Errorf("%w: mismatched CMS signature algorithm", ErrUnsupportedSignature)
}

type essCertificateID struct {
	HashAlgorithm pkix.AlgorithmIdentifier `asn1:"optional"`
	Hash          []byte
	IssuerSerial  asn1.RawValue `asn1:"optional"`
}

type essSigningCertificate struct {
	Certs    []essCertificateID
	Policies asn1.RawValue `asn1:"optional"`
}

func checkSigningCertificate(values map[string]asn1.RawValue, cert *x509.Certificate) error {
	oid := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	var signing essSigningCertificate
	if err := readCMSAttribute(values, oid, &signing); err != nil {
		return err
	}
	if len(signing.Certs) != 1 || len(signing.Policies.FullBytes) != 0 {
		return fmt.Errorf("%w: CAdES certificate-path constraints or policies", ErrUnsupportedSignature)
	}
	id := signing.Certs[0]
	hash := crypto.SHA256
	if !cmsNullOrAbsent(id.HashAlgorithm.Parameters) {
		return fmt.Errorf("%w: ESS hash parameters", ErrUnsupportedSignature)
	}
	if len(id.HashAlgorithm.Algorithm) != 0 {
		var err error
		hash, err = signatureHash(id.HashAlgorithm.Algorithm)
		if err != nil {
			return err
		}
	}
	h := hash.New()
	_, _ = h.Write(cert.Raw)
	if subtle.ConstantTimeCompare(id.Hash, h.Sum(nil)) != 1 {
		return errors.New("CAdES signer certificate digest mismatch")
	}
	if len(id.IssuerSerial.FullBytes) != 0 {
		return checkESSIssuerSerial(id.IssuerSerial.FullBytes, cert)
	}
	return nil
}

func checkESSIssuerSerial(encoded []byte, cert *x509.Certificate) error {
	var id struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}
	rest, err := asn1.Unmarshal(encoded, &id)
	if err != nil || len(rest) != 0 || id.Serial == nil || cert.SerialNumber == nil || id.Serial.Cmp(cert.SerialNumber) != 0 {
		return errors.New("CAdES issuer serial mismatch")
	}
	if id.Issuer.Class != 0 || id.Issuer.Tag != asn1.TagSequence {
		return errors.New("invalid CAdES issuer names")
	}
	names := id.Issuer.Bytes
	matched := false
	for len(names) > 0 {
		var name asn1.RawValue
		names, err = asn1.Unmarshal(names, &name)
		if err != nil {
			return err
		}
		if name.Class == 2 && name.Tag == 4 && bytes.Equal(name.Bytes, cert.RawIssuer) {
			matched = true
		}
	}
	if matched {
		return nil
	}
	return errors.New("CAdES issuer directoryName mismatch")
}

func cmsPSSAlgorithm(encoded []byte, hash crypto.Hash) (x509.SignatureAlgorithm, error) {
	var parameters struct {
		Hash       pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:0"`
		MGF        pkix.AlgorithmIdentifier `asn1:"explicit,optional,tag:1"`
		SaltLength int                      `asn1:"explicit,optional,tag:2,default:20"`
		Trailer    int                      `asn1:"explicit,optional,tag:3,default:1"`
	}
	rest, err := asn1.Unmarshal(encoded, &parameters)
	if err != nil || len(rest) != 0 {
		return 0, errors.New("invalid RSA-PSS parameters")
	}
	declared, err := signatureHash(parameters.Hash.Algorithm)
	if err != nil || declared != hash || parameters.SaltLength != hash.Size() || parameters.Trailer != 1 {
		return 0, fmt.Errorf("%w: RSA-PSS hash, salt, or trailer", ErrUnsupportedSignature)
	}
	if !parameters.MGF.Algorithm.Equal(pkcs7.OIDMaskGenerationFunctionMGF1) {
		return 0, fmt.Errorf("%w: RSA-PSS mask generation", ErrUnsupportedSignature)
	}
	var maskHash pkix.AlgorithmIdentifier
	rest, err = asn1.Unmarshal(parameters.MGF.Parameters.FullBytes, &maskHash)
	if err != nil || len(rest) != 0 || !maskHash.Algorithm.Equal(parameters.Hash.Algorithm) {
		return 0, errors.New("RSA-PSS MGF hash mismatch")
	}
	switch hash {
	case crypto.SHA256:
		return x509.SHA256WithRSAPSS, nil
	case crypto.SHA384:
		return x509.SHA384WithRSAPSS, nil
	case crypto.SHA512:
		return x509.SHA512WithRSAPSS, nil
	}
	return 0, fmt.Errorf("%w: RSA-PSS hash", ErrUnsupportedSignature)
}

func cmsNullOrAbsent(value asn1.RawValue) bool {
	return len(value.FullBytes) == 0 || bytes.Equal(value.FullBytes, []byte{5, 0})
}
