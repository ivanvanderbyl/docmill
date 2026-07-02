# docmill

**Fast, deterministic PDF → Markdown in pure Go.**

[![Go Reference](https://pkg.go.dev/badge/github.com/ivanvanderbyl/docmill.svg)](https://pkg.go.dev/github.com/ivanvanderbyl/docmill)
![Go 1.26+](https://img.shields.io/badge/go-1.26%2B-00ADD8)
![License BSL 1.1](https://img.shields.io/badge/license-BSL%201.1-blue)

docmill converts born-digital PDFs to clean Markdown — headings, paragraphs,
lists, and tables — without a network call, an ML model, or a line of cgo. It
ports the deterministic core of [Docling](https://github.com/docling-project/docling)
to Go and drives it with a from-scratch, pure-Go PDF parsing engine modelled on
[PDFium](https://pdfium.googlesource.com/pdfium/). The result is a single static
binary that converts a page in tens of milliseconds and produces the same output
every time.

Every layout decision is algorithmic and explainable. docmill infers structure
from geometry, font metrics, and spacing — never from the literal words on the
page — so it generalises across documents instead of overfitting a corpus.

## Highlights

- **Pure Go, no cgo.** The converter needs no PDFium shared library and no build
  toolchain beyond `go`. `go install` gives you a static binary.
- **Fast.** ~26 ms/page on born-native PDFs — the quickest tool in the DPBench
  cross-tool comparison by a wide margin. See [Benchmarks](#benchmarks).
- **Deterministic.** The same PDF always yields byte-identical Markdown. No
  sampling, no model weights, no hidden state.
- **Structure-aware.** Reading-order recovery, paragraph assembly, font-driven
  heading levels (nested coherently across pages), list detection, and
  borderless-table reconstruction — all on by default.
- **Tables done properly.** OTSL decoding plus geometry-based grid
  reconstruction for tables that have no ruling lines, including tables that
  span a page break.
- **AcroForm support.** Export field values, export field geometry and labels,
  or fill a form and write a new PDF.
- **Observable.** Optional OpenTelemetry traces and metrics, off by default with
  zero overhead.

## Install

```bash
go install github.com/ivanvanderbyl/docmill/cmd/docmill@latest
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

	"github.com/ivanvanderbyl/docmill/pkg/parser"
	"github.com/ivanvanderbyl/docmill/pkg/pdf"
)

func main() {
	ctx := context.Background()

	data, err := os.ReadFile("input.pdf")
	if err != nil {
		panic(err)
	}

	backend := parser.NewBackend()
	defer backend.Close()

	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		panic(err)
	}
	defer doc.Close()

	markdown, err := pdf.ExtractMarkdown(ctx, doc)
	if err != nil {
		panic(err)
	}

	fmt.Print(markdown)
}
```

`pdf.ExtractMarkdown` turns on reading order, structure, headings, and table
detection. Use `pdf.ExtractMarkdownWithOptions` for finer control — for example
to opt into inline bold/italic/code formatting or to bound page-level
parallelism.

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
and encoding handling, and Unicode mapping — that reproduces PDFium's behaviour
on malformed input rather than rejecting it.

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

Measured on 200 born-native PDFs from the
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

docmill leads on extraction accuracy and heading level while running 3–200×
faster than every other tool measured. Reading order (NID) is the current weak
spot and the focus of ongoing work. Reproduce the numbers and see the full tool
set in [`benchmarks/dpbench/`](benchmarks/dpbench/README.md).

## Observability

docmill is instrumented with OpenTelemetry traces and metrics exported over
OTLP/HTTP. It is a no-op — and adds zero overhead — unless you enable it:

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
pages (no OCR), encrypted PDFs, mathematical formulas, embedded-image
extraction, or hyperlinks.

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
whose parsing behaviour the native engine mirrors.

## License

docmill is licensed under the [Business Source License 1.1](LICENSE). You may
use, modify, and self-host it freely; you may not offer it to third parties as a
hosted or managed service without a commercial licence. On 2 July 2030 each
released version converts to the Apache License 2.0.
