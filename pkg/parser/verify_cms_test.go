package parser

import (
	"crypto"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"

	"github.com/digitorus/pkcs7"
	"github.com/stretchr/testify/require"
)

func testCMSAttribute(t *testing.T, oid asn1.ObjectIdentifier, value any) cmsAttribute {
	t.Helper()
	encoded, err := asn1.Marshal(value)
	require.NoError(t, err)
	return cmsAttribute{Type: oid, Value: asn1.RawValue{Class: 0, Tag: asn1.TagSet, IsCompound: true, Bytes: encoded}}
}

func TestCMSRequiredAuthenticatedAttributes(t *testing.T) {
	message := []byte("signed bytes")
	digest := sha256.Sum256(message)
	contentType := testCMSAttribute(t, pkcs7.OIDAttributeContentType, pkcs7.OIDData)
	messageDigest := testCMSAttribute(t, pkcs7.OIDAttributeMessageDigest, digest[:])
	wrongType := testCMSAttribute(t, pkcs7.OIDAttributeContentType, pkcs7.OIDEnvelopedData)
	wrongDigest := testCMSAttribute(t, pkcs7.OIDAttributeMessageDigest, []byte("wrong"))
	multipleValues := messageDigest
	multipleValues.Value.Bytes = append(bytesClone(messageDigest.Value.Bytes), messageDigest.Value.Bytes...)
	for _, tc := range []struct {
		name      string
		attrs     []cmsAttribute
		wantError bool
	}{
		{"valid", []cmsAttribute{contentType, messageDigest}, false},
		{"missing type", []cmsAttribute{messageDigest}, true},
		{"missing digest", []cmsAttribute{contentType}, true},
		{"duplicate type", []cmsAttribute{contentType, contentType, messageDigest}, true},
		{"duplicate digest", []cmsAttribute{contentType, messageDigest, messageDigest}, true},
		{"wrong type", []cmsAttribute{wrongType, messageDigest}, true},
		{"wrong digest", []cmsAttribute{contentType, wrongDigest}, true},
		{"multiple digest values", []cmsAttribute{contentType, multipleValues}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := authenticatedContent(message, tc.attrs, crypto.SHA256, &x509.Certificate{}, "adbe.pkcs7.detached")
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func bytesClone(b []byte) []byte { return append([]byte{}, b...) }

func TestCMSSignatureAlgorithmMustMatchDigest(t *testing.T) {
	_, err := cmsSignatureAlgorithm(pkix.AlgorithmIdentifier{Algorithm: pkcs7.OIDEncryptionAlgorithmRSASHA512}, crypto.SHA256)
	require.Error(t, err)
	_, err = cmsSignatureAlgorithm(pkix.AlgorithmIdentifier{Algorithm: pkcs7.OIDDigestAlgorithmECDSASHA256}, crypto.SHA256)
	require.NoError(t, err)
	_, err = cmsSignatureAlgorithm(pkix.AlgorithmIdentifier{Algorithm: pkcs7.OIDEncryptionAlgorithmRSA, Parameters: asn1.RawValue{FullBytes: []byte{0x30, 0}}}, crypto.SHA256)
	require.Error(t, err)
}

func TestCAdESSignedCertificateConstraints(t *testing.T) {
	cert := &x509.Certificate{Raw: []byte("certificate bytes"), RawIssuer: []byte{0x30, 0}, SerialNumber: big.NewInt(7)}
	digest := sha256.Sum256(cert.Raw)
	oid := asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}
	name, err := asn1.Marshal(asn1.RawValue{Class: 2, Tag: 4, IsCompound: true, Bytes: cert.RawIssuer})
	require.NoError(t, err)
	issuer := func(serial int64) asn1.RawValue {
		value, err := asn1.Marshal(struct {
			Issuer asn1.RawValue
			Serial *big.Int
		}{asn1.RawValue{Tag: 16, IsCompound: true, Bytes: name}, big.NewInt(serial)})
		require.NoError(t, err)
		return asn1.RawValue{FullBytes: value}
	}
	base := essCertificateID{Hash: digest[:]}
	for _, tc := range []struct {
		name      string
		ids       []essCertificateID
		policies  asn1.RawValue
		wantError bool
	}{
		{"single certificate", []essCertificateID{base}, asn1.RawValue{}, false},
		{"additional path constraints", []essCertificateID{base, base}, asn1.RawValue{}, true},
		{"policies", []essCertificateID{base}, asn1.RawValue{FullBytes: []byte{0x30, 0}}, true},
		{"matching issuer", []essCertificateID{{Hash: digest[:], IssuerSerial: issuer(7)}}, asn1.RawValue{}, false},
		{"mismatched serial", []essCertificateID{{Hash: digest[:], IssuerSerial: issuer(8)}}, asn1.RawValue{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attr := testCMSAttribute(t, oid, essSigningCertificate{Certs: tc.ids, Policies: tc.policies})
			err := checkSigningCertificate(map[string]asn1.RawValue{oid.String(): attr.Value}, cert)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
