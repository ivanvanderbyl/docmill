# Benchmarks

Evaluated on 200 born-native PDF documents with ground-truth Markdown annotations from the dpbench corpus.

- Benchmark date: `2026-06-22`
- Corpus: 200 documents with ground-truth Markdown annotations (78 with tables, 111 with headings)
- Scope: born-native PDFs only
- Hardware: arm64
- Metrics: NID (reading order), TEDS (table structure), MHS (heading hierarchy)
- All scores normalised to [0, 1] - higher is better
- Extraction accuracy and NID are averaged over successful documents; TEDS is averaged over documents with tables; MHS is averaged over documents with headings
- Competitor commands and versions are loaded from the benchmark tool config

## Accuracy Metrics

| Solution | Version | Extraction accuracy | Reading order (NID) | Table structure (TEDS) | Heading level (MHS) |
| --- | --- | ---: | ---: | ---: | ---: |
| docmill | - | **0.92** | 0.25 | 0.73 | **0.79** |
| docling | 2.104.0 | 0.91 | 0.27 | 0.48 | 0.00 |
| opendataloader | 2.4.7 | 0.69 | 0.23 | 0.71 | 0.65 |
| markitdown | 0.1.6 | 0.88 | 0.15 | 0.56 | 0.00 |
| pymupdf4llm | 1.27.2.3 | 0.89 | 0.24 | 0.72 | 0.00 |
| opendataloader-hybrid | 2.4.7 | 0.00 | 0.00 | 0.00 | 0.00 |
| liteparse | 2.1.1 | 0.89 | 0.11 | **0.74** | 0.67 |
| pypdf | 6.13.3 | 0.88 | 0.00 | 0.49 | 0.00 |

## Speed

| Solution | Milliseconds per page |
| --- | ---: |
| opendataloader-hybrid | 0.0 |
| docmill | **26.0** |
| pypdf | 84.8 |
| opendataloader | 250.8 |
| pymupdf4llm | 411.7 |
| markitdown | 419.0 |
| liteparse | 831.9 |
| docling | 5365.1 |

## Relative Speed Callouts

- docmill is `3x` faster than `pypdf`
- docmill is `10x` faster than `opendataloader`
- docmill is `16x` faster than `pymupdf4llm`
- docmill is `16x` faster than `markitdown`
- docmill is `32x` faster than `liteparse`
- docmill is `206x` faster than `docling`
