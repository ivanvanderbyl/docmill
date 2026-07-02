# PDFium Runtime Benchmark

Date: 2026-06-19

Fixture: `.upstream-docling/tests/data/pdf/normal_4pages.pdf`

Command (the benchmark is now the `bench` subcommand of `cmd/docmill`; it was
originally run via the standalone `cmd/docmill-pdfium-bench`, consolidated 2026-06-20):

```bash
env \
  PKG_CONFIG_PATH=/path/to/docmill/.deps/pdfium/lib/pkgconfig \
  DYLD_LIBRARY_PATH=/path/to/docmill/.deps/pdfium/lib \
  go run -tags pdfium_cgo ./cmd/docmill bench \
    -n 5 \
    -backend all \
    .upstream-docling/tests/data/pdf/normal_4pages.pdf
```

Result:

```text
backend init_ms  iterations total_ms avg_ms bytes
wasm    1184.029 5          221.582  44.316 18551
cgo-mt  111.596  5          93.745   18.749 18551
```

Notes:

- `cgo-mt` uses `github.com/klippa-app/go-pdfium/multi_threaded` with one worker.
- The worker is launched with `go run -tags pdfium_cgo ./cmd/docmill worker`.
- The benchmark needs PDFium visible to `pkg-config` and the dynamic loader.
- In the Codex sandbox, the go-plugin worker could not bind its Unix socket, so the benchmark was run unsandboxed.
