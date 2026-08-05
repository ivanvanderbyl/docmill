# DPBench Cross-Tool Benchmark

This benchmark reports the following metrics:

- extraction accuracy
- reading order (NID)
- table structure (TEDS)
- heading level (MHS)
- milliseconds per page

The default corpus source is [`docling-project/docling-dpbench`](https://huggingface.co/datasets/docling-project/docling-dpbench), which exposes 200 test documents with binary document data and ground-truth document annotations.

## Prepare The Corpus

Materialise the corpus into the manifest layout consumed by `docmill benchmark`:

```bash
task benchmark:dpbench:fetch -- --limit 200
```

The fetcher uses Hugging Face's rows API and only needs the Python standard
library. Use `--page-size N` to tune request size if the API or network is slow.
If your system Python has a stale TLS certificate store, pass a newer runtime:

```bash
task benchmark:dpbench:fetch PYTHON=/path/to/python3 -- --limit 200
```

This writes:

- `benchmarks/dpbench/corpus/manifest.json`
- `benchmarks/dpbench/corpus/pdf/*.pdf`
- `benchmarks/dpbench/corpus/groundtruth/*.md`

## Configure Tools

Copy `tools.example.json` to `tools.json` and edit commands for your local installs:

```bash
cp benchmarks/dpbench/tools.example.json benchmarks/dpbench/tools.json
```

The benchmark validates that the config contains all requested solution names:

- `docmill`
- `docling`
- `opendataloader`
- `markitdown`
- `pymupdf4llm`
- `opendataloader-hybrid`
- `liteparse`
- `pypdf`
- `pdf-inspector`

Use `-allow-missing` only for local smoke checks; a publishable benchmark should not use it.

The example config assumes these public package entrypoints:

```bash
python3 -m pip install docling "markitdown[pdf]" pymupdf4llm pypdf liteparse
python3 -m pip install opendataloader-pdf pdf-inspector
```

- The Python adapters use `benchmarks/dpbench/python_converter.py`; edit
  `python3` to your virtualenv interpreter when needed.
- OpenDataLoader exposes `opendataloader-pdf` and requires Java 11+ on `PATH`.
- `pdf-inspector` is Firecrawl's Rust parser; the Python wheel ships a prebuilt
  native binary, so no Rust toolchain is needed. It routes scanned pages to OCR
  instead of parsing them, and the adapter records that as an empty conversion —
  expect low scores on any scanned document in the corpus.
- OpenDataLoader hybrid mode additionally needs
  `python3 -m pip install "opendataloader-pdf[hybrid]"` and a running server:

```bash
opendataloader-pdf-hybrid --port 5002
```

Docling and OpenDataLoader hybrid may download model assets on first use. Warm
those caches before recording a publishable speed run if you want the timing to
measure conversion rather than setup.

## Run

```bash
task benchmark:dpbench -- \
  -hardware "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m)"
```

The task builds `bin/docmill-bench` before running so the native `docmill`
timing does not include per-document `go run` compile overhead.
Before running converters, the harness opens each corpus PDF with the native
parser and excludes image-only PDFs with no extractable native text.

The task writes:

- `docs/benchmarks/dpbench.md`
- `benchmarks/dpbench/results/dpbench.json`
- converter outputs under `benchmarks/dpbench/outputs/`

Generated corpus, output, and result directories are ignored by git.
