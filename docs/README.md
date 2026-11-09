# Docmill documentation

## Use Docmill

- [Integration guide](integration.md): choose APIs, convert a PDF, configure
  extraction, handle errors, and manage output and concurrency.
- [Guidance for consuming agents](agent-consumers.md): integrate Docmill or
  replace another library while preserving the application's contract.
- [Native PDF operations](pdf-operations.md): page copying and merging, image
  formats, encryption, attachments, and active-content inspection.
- [Signature images and verification](signatures.md): visible image placement,
  signature metadata, byte integrity, certificate trust, and coverage.
- [CLI quick start](../README.md#usage): conversion and AcroForm commands.
- [Go API reference](https://pkg.go.dev/github.com/ivanvanderbyl/docmill/v2/pkg/parser):
  published native-parser APIs. Use `go doc` in your checkout for its exact version.

## Develop and evaluate Docmill

- [Repository agent guidance](../AGENTS.md): code style, ownership, validation,
  and rules for document-general extraction algorithms.
- [DPBench workflow](../benchmarks/dpbench/README.md): corpus setup, tool
  configuration, and cross-tool evaluation.
- [Native PDF operation validation](benchmarks/2026-11-09-native-pdf-operations.md)
  and [signature operation validation](benchmarks/2026-11-09-signature-operations.md):
  dated evidence with its scope and limits.
- [Historical incident regressions](pdf-incident-regressions.md): public
  regression cases and what they establish.
- [Observability](../deploy/observability/README.md): OpenTelemetry setup and
  signals.

Documents under `plans/` and `research/` record proposals and investigations.
They do not establish current API availability. Benchmark reports describe the
versions, hardware, and datasets recorded in each report.
