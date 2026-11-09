# Signature images, signing, and verification

Docmill can place an image on a PDF, create a cryptographic signature, and
inspect or verify existing PDF digital signatures. These are separate operations. An image of handwriting does not
cryptographically sign a document.

## Place an image

Use `WriteImageStamps` with an `image.Image` decoded or created by your Go code:

```go
err := doc.WriteImageStamps(ctx, output, []parser.ImageStamp{
    {
        PageIndex: 0,
        Rect: geom.Box{L: 72, T: 100, R: 216, B: 148, Origin: geom.TopLeft},
        Image: signatureImage,
    },
})
```

Page indices are zero-based. Rectangles use PDF points in the displayed page,
after cropping and rotation. The image stretches to fill the rectangle. Its
alpha channel is preserved. Images are painted over page content and beneath
annotations. This operation copies the whole document without modifying the
source, retaining forms and other catalog structures.

The output is unencrypted. Signed documents are rejected because the writer
creates a new file rather than an incremental update. It cannot preserve an
existing signature while changing a page. Use
`errors.Is(err, parser.ErrSignedDocument)` to distinguish this refusal.
The text form setter also rejects signature fields; setting a field's string value is not a signing operation.

## Create a cryptographic signature

Use `Sign` with a Go `crypto.Signer` and its X.509 certificate:

```go
err := doc.Sign(ctx, output, parser.SignOptions{
    Signer: key,
    Certificate: certificate,
    Chain: issuerCertificates,
    FieldName: "Approval",
    Reason: "Approved",
})
```

The signer may hold a local private key or adapt an HSM or remote key service.
Docmill does not load or store private keys. Signing currently supports RSA
PKCS#1 v1.5 and ECDSA. The signer receives standard `crypto.Hash` options;
RSA-PSS signing is not provided by this API, although verification supports it.
The output uses the `adbe.pkcs7.detached` signature subfilter.

`Hash` defaults to `crypto.SHA256`; SHA-384 and SHA-512 are also supported.
`SigningTime` defaults to now and is a claimed time, not a trusted timestamp.
`Chain` contains optional issuer certificates in issuer-to-root order.
The signing key must match `Certificate`. The certificate must permit signing
and be valid at the selected signing time. This does not establish external
trust in the certificate.

`FieldName` identifies a new, invisible signature field. It defaults to an
available name and must not collide with an existing field. Explicit names must
not contain periods or leading/trailing whitespace. To include a
handwritten image in the signed content, stamp the unsigned document first,
reopen that output, and then sign it.

Signing appends a PDF revision. For an already-signed document, the original
bytes are preserved and existing signatures must remain cryptographically
intact. Pending in-memory edits to such a document are rejected. Do not mutate form
values concurrently with signing. For an unsigned
document, pending form edits are included in a fresh snapshot before signing.
Encrypted inputs, certification permissions, and usage-rights signatures are
rejected rather than silently changing their protections.

`SignatureSize` reserves CMS capacity before the key is called. Its default is
32 KiB and its maximum is 1 MiB. The signer is called at most once per attempt. If its CMS result
exceeds that capacity, `ErrSignatureCapacity` is returned without writing a PDF
or retrying the signer. The generated signature is verified before output is
written. Writer failures can still leave partial output, as with other `io.Writer`
APIs.

Context cancellation is checked around the operation. `crypto.Signer.Sign` has
no context parameter, so a remote adapter must arrange cancellation of its own
request. No timestamp service or revocation service is contacted by signing.

## Inspect signatures

`doc.Signatures(ctx)` returns claimed signature metadata, including the field
name, reason, subfilter, byte ranges, signature contents, and claimed signing
time. Inspection alone authenticates none of these claims. Malformed signature
structures return an error rather than a partial list of apparent successes.

The parser retains the original input bytes independently from the caller's
buffer. Verification checks those original bytes, not pending in-memory form
edits or a reserialized document. To verify saved output, reopen that output.

## Verify signed bytes and certificate chains

```go
results, err := doc.VerifySignatures(ctx, parser.VerifyOptions{
    Roots: trustedRoots,
    CurrentTime: verificationTime,
})
if err != nil {
    return err
}
for _, result := range results {
    // Evaluate these independently under your application's policy.
    fmt.Println(result.IntegrityValid, result.TrustValid,
        result.CoversWholeDocument, result.HasUnsignedChanges)
}
```

A nil root pool does not perform certificate-chain validation and reports
`ErrNoTrustRoots`. Integrity verification still runs. Supply an explicit root
pool appropriate to your application's trust policy. For offline verification,
construct it with `x509.NewCertPool` and add the trusted root certificates.
The verifier can use embedded intermediate certificates, but embedding a root
certificate does not make it trusted. An explicitly supplied system root pool
may use platform-specific trust validation, including OS network behavior.
A zero `CurrentTime` uses the current time. A claimed signing date is not used
to bypass certificate validity checks.

An intact signature may cover an earlier revision while the current PDF has
additional changes. It may also have an untrusted or expired signing
certificate. Check integrity, certificate trust, and document coverage
separately. `IntegrityError` and `TrustError` explain failures for each signature.
The top-level error reports failures such as invalid signature structures or
context cancellation. An unsigned PDF returns an empty result list; a nil
error alone does not establish that any signature exists.

By default, verification performs no network access. It does not fetch
certificates, revocation lists, OCSP responses, or timestamp services. The
explicit-system-root-pool exception described above follows OS behavior.
Timestamp and revocation results are reported as `not_checked`. The verifier does not judge whether later changes
are permitted by DocMDP. It does not produce a legal-validity determination.

Supported cryptographic signatures use detached CMS/PKCS#7 in
`adbe.pkcs7.detached` or `ETSI.CAdES.detached` PDF signatures, including RSA
PKCS#1, RSA-PSS, and ECDSA with supported SHA-2 digests. Unsupported formats
return a failed result rather than a success. SHA-1 is not supported. CAdES
support checks a single signing-certificate-v2 identifier; additional
certificate-path constraints or certificate policies are rejected. RSA-PSS
requires matching message and MGF1 hashes, digest-length salt, and trailer 1.
The image-stamping API does not create a cryptographic signature or edit an
already-signed document. Use `Sign` for incremental approval signatures.

## Conformance sources

The PDF signature model follows
[PDFium's signature APIs](https://pdfium.googlesource.com/pdfium/+/6688c576fd851f4159c6fae39396da50939de110/fpdfsdk/fpdf_signature.cpp).
CMS verification follows [RFC 5652](https://www.rfc-editor.org/rfc/rfc5652).
PDF byte-range checks bind the omitted region to the inspected signature's
original Contents token rather than trusting reserialized objects.
