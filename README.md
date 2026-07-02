# docmill

A Go port of [Docling](https://github.com/docling-project/docling)'s
deterministic PDF→Markdown core: Docling-compatible geometry, OTSL table
decoding, table-grid reconstruction, and Markdown rendering, plus a PDFium-based
text extractor.

## Status & scope

Implemented: text extraction (PDFium), conservative aligned text-table
detection, OTSL → Markdown tables, JSON-document → Markdown.

Not yet implemented: reading-order beyond PDFium's native order, paragraph
assembly, headings, lists, page header/footer removal, images, formulas, OCR,
hyperlinks.

## Packages

- `pkg/geom` — boxes, coordinate origins, intersections (docling-compatible).
- `pkg/page` — text cells and segmented-page queries.
- `pkg/table` — OTSL parsing, `TableData`/grid, region reconstruction, text-table detection.
- `pkg/render` — Markdown serialisation via `github.com/ivanvanderbyl/markdown`.
- `pkg/pdf` — backend interfaces and the extraction pipeline.
- `pkg/parser` — the **pure-Go PDF parsing engine** (a from-scratch PDFium port; the product backend).

## Build & test

Tasks are run with [Task](https://taskfile.dev)
(`go install github.com/go-task/task/v3/cmd/task@latest`):

    task check          # gofmt check, go vet, go test (whole module)
    task --list         # show all tasks
    go test ./... -count=1

## Run

`docmill` is a single binary with subcommands (run `docmill help` for the list):

    # PDF -> Markdown (native backend; bare path or `convert`)
    go run ./cmd/docmill path/to/input.pdf

    # JSON document -> Markdown
    echo '{"items":[{"type":"paragraph","text":"hello"}]}' | go run ./cmd/docmill json

## DPBench cross-tool benchmark

The cross-tool benchmark compares the native `docmill` CLI with Docling,
opendataloader, opendataloader-hybrid, pymupdf4llm, markitdown, pypdf, and
liteparse on extraction accuracy, reading order (NID), table structure
(TEDS), heading level (MHS), and milliseconds per page. Image-only PDFs are
excluded so the benchmark measures born-native PDFs only.

The default corpus source is Hugging Face
[`docling-project/docling-dpbench`](https://huggingface.co/datasets/docling-project/docling-dpbench).
Setup and tool configuration are documented in
[`benchmarks/dpbench/`](benchmarks/dpbench/README.md):

    task benchmark:dpbench:fetch -- --limit 200
    cp benchmarks/dpbench/tools.example.json benchmarks/dpbench/tools.json
    task benchmark:dpbench -- -hardware "Apple M4 Max"

## Observability

docmill is instrumented with OpenTelemetry (traces + metrics) and exports via
OTLP/HTTP — metrics to VictoriaMetrics, traces to VictoriaTraces. It is a no-op
unless enabled, so there is zero overhead when off. Enable it by setting
`DOCMILL_OTEL=1` and the OTLP endpoints, then watch per-stage latency in
Grafana:

    export DOCMILL_OTEL=1
    export OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=http://localhost:8428/opentelemetry/v1/metrics
    export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://localhost:10428/insert/opentelemetry/v1/traces
    task obs:run -- path/to/input.pdf      # or: task obs:bench -- -n 8 path/to/input.pdf

Setup, the metric/span reference, the importable Grafana dashboard, and example
queries are in [`deploy/observability/`](deploy/observability/README.md).
