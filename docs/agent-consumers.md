# Guidance for agents integrating Docmill

Use this checklist when changing an application that consumes Docmill.
For runnable examples and API contracts, read [the integration guide](integration.md).
For changes inside Docmill, read the root [AGENTS.md](../AGENTS.md).

## Establish the consumer's contract

1. Inspect the consumer's `go.mod`, module replacements, current PDF calls, and
   relevant tests. Verify the Docmill version and the available exported APIs.
2. Identify each required operation separately: text conversion, page copying,
   forms, images, upload inspection, or signatures. Choose APIs from the
   [operation table](integration.md#choose-the-api).
3. Record behaviour that must survive the change: page numbering, output order,
   duplicate pages, form values, metadata, error classification, encryption, and
   signature handling. Preserve assertions that express those requirements.
4. Read the applicable [PDF operation](pdf-operations.md) and
   [signature](signatures.md) contracts before implementing the adapter.

## Implement against public APIs

- Use `pkg/pdf` for the backend-neutral extraction contract and `pkg/parser`
  for the native backend and additional PDF operations. Do not import parser
  internals or recreate their object traversal in the consumer.
- Convert one-based application page numbers at the adapter boundary. Keep
  Docmill's zero-based indices and exclusive range ends explicit in code.
- Set all desired boolean extraction options when using
  `ExtractMarkdownWithOptions`. A zero-valued options struct disables the
  optional features rather than selecting `ExtractMarkdown` defaults.
- Preserve errors with `%w` and classify the public password/parser errors with
  `errors.Is`. Do not turn unsupported formats into empty successful results.
- Keep contexts, input-size limits, job concurrency, and destination publication
  under the application's control. Do not claim that a context deadline is a
  hard resource limit.
- Use separate document instances or serialisation for form mutations. The
  concurrent-read tests do not establish concurrent-mutation support.

## Preserve output semantics

- Use page copying only when its documented exclusions are acceptable. Do not
  describe it as preserving whole-document forms, metadata, or signatures.
- Account for plaintext output from whole-document writes, page copying, and
  stamping when the source is encrypted.
- Treat image extraction as resource extraction, not page rendering. A PNG or
  JPEG conversion does not add support for an unsupported PDF image codec.
- Keep parsing success, active-content policy, signed-byte integrity,
  certificate trust, and current-document coverage as separate decisions.
- Verify exported bytes after reopening them when the consumer's contract
  depends on the saved file. Signature verification concerns original input
  bytes, not unsaved form edits.

## Validate and hand off

Run the consumer's existing tests and add coverage only for material contracts
that they do not already exercise. Use representative PDFs alongside minimal
fixtures. Report unsupported formats and changed output semantics explicitly.

For a library migration, compare the old and new implementations through the
actual consuming operation. Do not infer end-to-end performance from a parser
microbenchmark or compare `go run` startup with a compiled converter.

In the handoff, state the pinned module version or local replacement, changed
callers, preserved contracts, tests run, and any untested behaviour. Distinguish
local evidence from CI, deployment, and production evidence. Remove temporary
module replacements before publication unless the repository intentionally
maintains them.
