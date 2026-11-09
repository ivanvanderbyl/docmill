# Cryptographic signing validation

`Document.Sign` uses an application-supplied `crypto.Signer` and X.509
certificate to create an approval signature. It appends an incremental PDF
revision and verifies the completed output before writing it.

## Validation

- `task check` and `go test -race ./...` pass.
- RSA and ECDSA outputs pass independent OpenSSL CMS verification.
- `pdfsig` reports both signatures in a successively signed test document as
  valid: the first covers its earlier revision and the second covers the whole
  current document.
- Conformance tests cover sparse cross-reference sections, preceding xref
  streams, nonzero object generations, exact prior-revision bytes, wide integer
  serialization, invisible widgets, Unicode metadata, and preserved form edits.
- External-key tests check one invocation, callback failures, invalid signatures,
  cancellation, certificate/key mismatches, invalid chains, and capacity errors.
- Rejection tests verify zero key invocations for unsupported policy changes,
  encrypted documents, field collisions, and pending edits on signed inputs.
- Private test keys are generated for tests; no application keys are stored.

## Native extraction regression check

Baseline runner built from `42b152e`; current implementation based on `4b6b06f`.
Both use Go 1.27 and the same 200-PDF DPBench corpus, hardware, native tool
configuration, and separate output paths. Timings varied between runs; this
comparison does not establish a speedup.

| Pair | Baseline ms/page | Current ms/page |
| --- | ---: | ---: |
| initial | 13.11522 | 15.66119 |
| -repeat1 | 12.99607 | 12.85626 |
| -repeat2 | 13.04510 | 12.83214 |
| -repeat3 | 13.16042 | 12.48763 |
| Median | 13.08016 | 12.84420 |

Every run completed **200 cases with zero errors**. All four accuracy-score
deltas were **0**: extraction accuracy, reading-order NID, table-structure TEDS,
and heading-level MHS.

See [the signing API](../signatures.md#create-a-cryptographic-signature) for
supported algorithms, encryption and modification-policy limits, certificate
handling, and cancellation semantics.
