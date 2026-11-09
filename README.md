# docmill

**Fast, deterministic PDF → Markdown in pure Go.**

[![Go Reference](https://pkg.go.dev/badge/github.com/ivanvanderbyl/docmill/v2.svg)](https://pkg.go.dev/github.com/ivanvanderbyl/docmill/v2)
![Go 1.27+](https://img.shields.io/badge/go-1.27%2B-00ADD8)
![License BSL 1.1](https://img.shields.io/badge/license-BSL%201.1-blue)

docmill converts born-digital PDFs to clean Markdown — headings, paragraphs,
lists, and tables — without a network call, an ML model, or a line of cgo. It
ports the deterministic core of [Docling](https://github.com/docling-project/docling)
to Go and drives it with a from-scratch, pure-Go PDF parsing engine modelled on
[PDFium](https://pdfium.googlesource.com/pdfium/). The default converter runs without
a PDFium shared library. It extracts embedded text and reconstructs document
structure using deterministic algorithms.

Layout extraction uses geometry, font metrics, spacing, and document structure.
Supported formats and extraction limits are described in the
[integration guide](docs/integration.md).

## Highlights

- **Pure Go, no cgo.** The converter needs no PDFium shared library and no build
  toolchain beyond `go`. `go install` gives you a static binary.
- **Measured performance.** Dated DPBench reports record extraction quality and
  latency for specific versions and hardware. See [Benchmarks](#benchmarks).
- **Deterministic extraction.** No model weights or sampling. Pin the version
  and extraction options when comparing output.
- **Structure-aware.** Reading-order recovery, paragraph assembly, font-driven
  heading levels (nested coherently across pages), list detection, and
  borderless-table reconstruction — all on by default.
- **Tables done properly.** OTSL decoding plus geometry-based grid
  reconstruction for tables that have no ruling lines, including tables that
  span a page break.
- **AcroForm support.** Export field values, export field geometry and labels,
  or fill a form and write a new PDF.
- **Observable.** Optional OpenTelemetry traces and metrics, disabled by default.

## Documentation

| Reader or task | Start here |
| --- | --- |
| Integrate the Go library | [Integration guide](docs/integration.md) |
| Agent updating a consuming application | [Consumer agent guide](docs/agent-consumers.md) |
| Copy pages, extract images, or handle encrypted files | [Native PDF operations](docs/pdf-operations.md) |
| Place images or work with digital signatures | [Signature guide](docs/signatures.md) |
| Change Docmill itself | [Repository guidance](AGENTS.md) |

See the [documentation index](docs/README.md) for validation reports and reference material.
The docs in a checkout describe that checkout. Check your pinned module version
before relying on a newly added API.

## Install

```bash
go install github.com/ivanvanderbyl/docmill/v2/cmd/docmill@latest
```

Or build from a clone:

```bash
go build -o docmill ./cmd/docmill
```

## Usage

### Convert a PDF

```bash
docmill path/to/input.pdf          # bare path → Markdown on stdout
docmill convert path/to/input.pdf  # explicit form, identical result
```

### Convert a JSON document

Pipe a document-model JSON on stdin to render it without a PDF:

```bash
echo '{"items":[{"type":"paragraph","text":"hello"}]}' | docmill json
```

### Work with AcroForm fields

```bash
docmill forms export input.pdf              # field values as JSON
docmill forms layout input.pdf              # per-page field boxes + labels as JSON
docmill forms fill input.pdf out.pdf v.json # fill fields and write a new PDF
```

`docmill help` lists every command.

## Library

docmill is a library first; the CLI is a thin wrapper. Open a document with the
pure-Go backend and render it in two calls:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: convert input.pdf")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read PDF: %w", err)
	}

	backend := parser.NewBackend()
	defer backend.Close()

	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		return fmt.Errorf("open PDF: %w", err)
	}
	defer doc.Close()

	markdown, err := pdf.ExtractMarkdown(ctx, doc)
	if err != nil {
		return fmt.Errorf("extract Markdown: %w", err)
	}
	_, err = fmt.Fprint(os.Stdout, markdown)
	return err
}
```

`pdf.ExtractMarkdown` turns on reading order, structure, headings, and table
detection. `pdf.ExtractMarkdownWithOptions` takes explicit options rather than
merging them with these defaults. See [extraction options](docs/integration.md#set-extraction-options-explicitly)
for inline formatting and bounded page concurrency.

### Other PDF operations

The native parser also supports page copying and merging, embedded-image
extraction, attachment metadata, and standard password encryption. See
[Native PDF operations](docs/pdf-operations.md) for APIs, indexing conventions,
validation checks, and supported formats.
[Signature images and verification](docs/signatures.md) covers image placement,
PDF digital signatures, and certificate trust.

## How it works

`pkg/pdf` defines a small `Backend`/`Document`/`Page` contract and runs a fixed
pipeline over the text cells a backend returns:

1. **Parse** the PDF into positioned text cells (`pkg/parser`).
2. **Order** the cells into reading order (`pkg/pdf/readingorder*.go`).
3. **Assemble** cells into a shared visual-line model, then into paragraphs,
   headings, and lists (`pkg/pdf/assemble.go`, `headings.go`, `structure.go`).
4. **Detect tables** — OTSL grids and borderless text tables reconstructed from
   cell geometry — and stitch tables that continue across a page break
   (`pkg/table`, `pkg/pdf/connect.go`).
5. **Render** the assembled blocks to Markdown (`pkg/render`).

The default backend, `pkg/parser`, is a bottom-up pure-Go port of PDFium's
parsing stack — object parsing, cross-reference tables, content streams, font
and encoding handling, and Unicode mapping — that follows PDFium's parsing model
where supported, including recovery paths for malformed input.

### Packages

| Package | Responsibility |
| --- | --- |
| `pkg/parser` | Pure-Go PDF parsing engine (the default backend). |
| `pkg/pdf` | Backend interfaces and the extraction pipeline. |
| `pkg/table` | OTSL parsing, grid reconstruction, borderless-table detection. |
| `pkg/render` | Markdown serialisation. |
| `pkg/textline` | Shared visual-line model used across the pipeline. |
| `pkg/page` | Text cells and segmented-page queries. |
| `pkg/geom` | Boxes, coordinate origins, and intersections (Docling-compatible). |
| `pkg/forms` | AcroForm field labelling. |
| `pkg/telemetry` | OpenTelemetry setup (no-op unless enabled). |

## Benchmarks

The following historical cross-tool snapshot was recorded on 22 June 2026,
using 200 born-native PDFs from the
[`docling-project/docling-dpbench`](https://huggingface.co/datasets/docling-project/docling-dpbench)
corpus (arm64, image-only PDFs excluded). All scores are normalised to `[0, 1]`;
higher is better.

| Tool | Extraction | Reading order (NID) | Tables (TEDS) | Headings (MHS) | ms/page |
| --- | ---: | ---: | ---: | ---: | ---: |
| **docmill** | **0.92** | 0.25 | 0.73 | **0.79** | **26** |
| docling | 0.91 | 0.27 | 0.48 | 0.00 | 5365 |
| pymupdf4llm | 0.89 | 0.24 | 0.72 | 0.00 | 412 |
| liteparse | 0.89 | 0.11 | **0.74** | 0.67 | 832 |
| markitdown | 0.88 | 0.15 | 0.56 | 0.00 | 419 |
| pypdf | 0.88 | 0.00 | 0.49 | 0.00 | 85 |
| opendataloader | 0.69 | 0.23 | 0.71 | 0.65 | 251 |

These numbers describe that run, not current releases of every tool. See the
[original report](docs/benchmarks/dpbench.md), the more recent
[native-operation comparison](docs/benchmarks/2026-11-09-native-pdf-operations.md),
and the [reproduction workflow](benchmarks/dpbench/README.md). Compare runs only
when their corpus, scoring, tool configuration, and hardware match.

## Observability

docmill is instrumented with OpenTelemetry traces and metrics exported over
OTLP/HTTP. Export is disabled unless you enable it:

```bash
export DOCMILL_OTEL=1
export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://localhost:8428/opentelemetry/v1/metrics
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://localhost:10428/insert/opentelemetry/v1/traces
task obs:run -- path/to/input.pdf
```

Setup, the span and metric reference, and an importable Grafana dashboard live in
[`deploy/observability/`](deploy/observability/README.md).

## Limitations

docmill targets born-digital PDFs. It does not yet handle scanned or image-only
pages for text extraction (no OCR), mathematical formulas, or hyperlinks.
Image extraction and encrypted documents have format-specific limits listed in
[Native PDF operations](docs/pdf-operations.md).

## Development

Tasks run through [Task](https://taskfile.dev):

```bash
task check      # gofmt check, go vet, and the full test suite
task --list     # show every task
go test ./... -count=1
```

## Acknowledgements

docmill stands on two open projects: the
[Docling](https://github.com/docling-project/docling) document-conversion core
(MIT) and Google's [PDFium](https://pdfium.googlesource.com/pdfium/) (BSD),
whose parsing behaviour informs the native engine.

## License

docmill is licensed under the [Business Source License 1.1](LICENSE). You may
use, modify, and self-host it freely; you may not offer it to third parties as a
hosted or managed service without a commercial licence. On 2 July 2030 each
released version converts to the Apache License 2.0.
