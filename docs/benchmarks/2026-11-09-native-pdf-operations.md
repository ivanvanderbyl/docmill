# Native PDF operation validation

Comparison: baseline `14d69c0`, implementation `1aa0f8b`. The baseline was
built from an isolated archive; the current runner was rebuilt with
`task benchmark:dpbench:build`. Both runs used the same 200-PDF DPBench corpus,
separate output directories, the native-only tool configuration, and Apple M4 Max.
The timed runs did not overlap the repository test suite. This is one paired
measurement, not a statistical latency claim.

Corpus manifest SHA-256: `3871a39de784975ca22500be7cf02e3ac28e47ce760a972282372ea83147e5d3`.

| Metric | Baseline | Current | Delta |
| --- | ---: | ---: | ---: |
| milliseconds_per_page | 13.8844769 | 13.99789624 | 0.11341934000000009 |
| cases | 200 | 200 | 0 |
| errors | 0 | 0 | 0 |
| extraction_accuracy | 0.9209395511341057 | 0.9209395511341057 | 0.0 |
| reading_order_nid | 0.8933053681369454 | 0.8933053681369454 | 0.0 |
| table_structure_teds | 0.7631260499558336 | 0.7631260499558336 | 0.0 |
| heading_level_mhs | 0.7681181181181179 | 0.7681181181181179 | 0.0 |

## Operation conformance

- `task check` passes formatting, vet, and all repository tests.
- `go test -race ./...` passes.
- The 200-PDF operation smoke test copies `[last, first, last]`, reopens each
  output, and compares page dimensions and extracted text cells to its source.
  All outputs pass independent `qpdf --check` without repairs or warnings.
- First-page image extraction succeeds on all 200 PDFs and returns 177 images.
  This verifies decoding and encoding, not pixel-level parity with PDFium.
- QPDF 12.3.2-generated encrypted fixtures exercise standard-security revisions
  2 through 6, user and owner passwords, empty passwords, Unicode passwords,
  metadata handling, and encrypted object streams. Independent QPDF checks
  validate 14 decrypted full-document and page-selection exports.
- Direct tests cover malformed input, cyclic references, invalid page ranges,
  duplicates, inherited attributes, discarded page content, image color spaces,
  masks, crypt-filter parameters, indirect signature types, and bounded
  regressions for reentrant parser locks.

The library APIs and supported-format limits are documented in
[Native PDF operations](../pdf-operations.md). This validation does not establish
backend migration, production rollout, or reproduction of the private PDF
from ALCOVA-BACKEND-15R.
