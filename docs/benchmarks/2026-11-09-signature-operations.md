# Signature operation validation

## Functional validation

- `task check` and `go test -race ./...` pass.
- OpenSSL-generated RSA PKCS#1, RSA-PSS, ECDSA, CAdES, BER-wrapped CMS, and
  multiple-revision PDF fixtures pass independent CMS verification.
- Signature tests reject tampered content, relocated Contents gaps, malformed
  byte ranges, unsupported algorithms, duplicate signed attributes, invalid
  certificate bindings, and unsupported certificate-policy constraints.
- Signature inspection tests cover exact original-byte provenance, escaped
  strings, deep inheritance, cycles, certification and usage-rights signatures.
- Stamp tests use independent Poppler rendering and QPDF checks across crop,
  rotation, both coordinate origins, transparency, existing content transforms,
  and trailing content comments. Existing signatures prevent any output.
- The public [DocuSign policy PDF](https://pds.dsf.docusign.net/keynectisicsqualifiedca.pdf)
  has two signatures. Both pass integrity verification and both correctly report
  that later bytes fall outside their signed revisions. This matches `pdfsig`.
  The default invocation does not establish certificate trust because no roots
  are supplied. Vendor-specific behavior beyond this sample is not assumed.

## Native runner comparison

Baseline: `d4dd6f2`, built from an isolated archive. Current: the active worktree
based on `70eea1c`, including the concurrent style refactor. Both runners use
Go 1.27 and the same local 200-PDF DPBench corpus and hardware. Each run uses
separate baseline/current output paths and the native-only tool configuration.
These measurements show local variability; they do not establish a speedup.

| Pair | Baseline ms/page | Current ms/page |
| --- | ---: | ---: |
| initial | 13.64353 | 14.57155 |
| -repeat1 | 14.44558 | 14.30758 |
| -repeat2 | 14.25623 | 13.50529 |
| -repeat3 | 12.86701 | 13.23283 |
| Median | 13.94988 | 13.90644 |

Every run: **200 cases, zero errors**. All four accuracy-score deltas are **0**:
extraction accuracy, reading order NID, table structure TEDS, heading level MHS.

Measured binary SHA-256 values:

- Baseline: `0ebf01e319b0aeea1548baf341b6958b7cd32a95db981cecc637358e77aab841`
- Current: `55aef700367f838f57cc6fe353acd68648cacb88cf82b684a65383bb8fda4732`

See [Signature images and verification](../signatures.md) for API contracts and
supported-format limits. Timestamp validation, revocation checking, DocMDP
change-permission analysis, and cryptographic signing are not provided.
