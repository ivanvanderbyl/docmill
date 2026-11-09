# Agent Guidance

## Start Here

Read [the documentation index](docs/README.md) to find the relevant public
contract. For work in a consuming application, follow
[the consumer agent guide](docs/agent-consumers.md).

Before editing, inspect the branch, the diff against its base, and the current
working tree. Preserve unrelated changes. When another agent owns active work,
keep shared-file edits coordinated and stage only your own changes.

## Code and Documentation Map

| Area | Purpose |
| --- | --- |
| `pkg/parser` | Public native backend and PDF operations. |
| `pkg/parser/internal` | PDF objects, syntax, encryption, document structure, page content, fonts, and text decoding. |
| `pkg/pdf` | Backend interfaces, extraction options, reading order, and document assembly. |
| `pkg/table`, `pkg/render` | Table reconstruction and Markdown output. |
| `pkg/page`, `pkg/geom`, `pkg/textline`, `pkg/forms` | Shared extraction, geometry, text-line, and form helpers. |
| `cmd/docmill` | CLI commands; check dispatch and help before documenting a command. |
| `docs/integration.md`, `docs/pdf-operations.md`, `docs/signatures.md` | Public integration contracts. |
| `docs/plans`, `docs/research` | Historical designs and investigations; verify against implemented code. |

Update the relevant public guide when an exported API, default, error, or output
contract changes. Keep executable examples consistent with the module's Go
version and compile them before handoff. Use relative links in repository docs.
Do not describe a checkout-only API as released without checking availability
in the consumer's pinned version.

## Integration Contracts

- Keep `pkg/pdf` backend interfaces distinct from extra methods on native
  `*parser.Document` and `*parser.Page` values.
- Preserve zero-based page indices, exclusive range ends, duplicate-page order,
  and the documented meaning of nil selections.
- Preserve the difference between default Markdown extraction and explicitly
  supplied boolean options.
- Document whole-document rewrites, page-copy exclusions, plaintext output from
  encrypted inputs, and signature consequences alongside affected operations.
- Preserve public error identities and distinguish unsupported input from empty
  successful output. Test saved output by reopening it when serialization changes.
- Keep read-only concurrency separate from mutation safety. Retain the race
  regressions when changing parser state, caches, or serialization.
- Keep signature inspection, integrity, certificate trust, and document coverage
  distinct. Follow the signature guide for the specific operation being changed.

## Go Coding Style

Apply these rules to handwritten Go code, including tests and examples.

- Use idiomatic Go 1.27 language and standard-library helpers when they simplify code and preserve behaviour.
- Write function, method, and anonymous-function bodies on multiple lines, including trivial accessors and empty marker methods. Put named struct definitions on multiple lines. Keep ordinary `struct{}` channel and set types and values compact.
- Separate top-level declarations and distinct logical steps with blank lines. Expand long composite literals so their fields and nested values are easy to scan. Avoid multiple statements on one line.
- Keep functions small and focused. Prefer early returns and shallow control flow. Extract cohesive operations into clearly named helpers when that makes the caller easier to understand.
- Keep cyclomatic complexity at or below 15 for each new or changed function, including tests and anonymous functions. Treat 15 as a ceiling, not a target.
- Comment sparingly. Explain non-obvious contracts, ordering, concurrency, or resource ownership. Remove comments that only repeat the code. Preserve required directives and useful API documentation.
- Before committing Go changes, run `go fix ./...` from the repository root. Review its changes and run the relevant checks. This repository has one Go module.
- Run `gofmt` on changed Go files before committing. Run `task check` for formatting, vet, and tests across the module.
- Change generators or their inputs when generated code needs to change. Do not hand-edit generated output to enforce this style.

## Optimisation Principles

This project is building a generalised PDF to Markdown converter. Optimisations must improve real-world extraction, not just local benchmark scores. The target is state-of-the-art latency per page and accuracy for text, tables, headings, lists, formatting, and reading order across a broad range of documents.

Use benchmarks to measure speed, quality, and regressions. Do not tune implementation behaviour to the benchmark corpus, file names, document IDs, fixture paths, hashes, exact strings, titles, publisher templates, or any other corpus-specific signal.

Detection logic must be algorithmic, deterministic, repeatable, and explainable. Prefer document-model signals such as geometry, font metrics, spans, reading order, alignment, spacing, cell boundaries, page structure, and cross-page consistency. When using statistical thresholds or learned components, keep the inputs and invariants document-general and validate against diverse PDFs.

Do not classify headings, paragraphs, lists, captions, tables, or formatting by relying on character patterns as the primary signal. Examples of overfitting include assuming heading shape from punctuation, case, numbering, prefixes, bullet characters, repeated words, or the literal text content of a document. Character cues may only be supporting evidence inside a broader layout or structure algorithm that remains valid across unrelated documents.

Speed improvements must preserve output quality. A latency change is not acceptable if it regresses text accuracy, table structure, heading levels, formatting fidelity, reading order, or error rate unless the trade-off is explicit, measured, and accepted for a documented mode.

When adding heuristics, document the general invariant they encode, add tests that cover both positive and negative cases, and smoke-test on real PDFs when the change affects extraction behaviour. Prefer conservative detection with clear false-positive guards over broad rules that look good on one corpus but fail on prose-heavy or unusual documents.

## Validation

For documentation-only work, check local links and compile changed Go examples.
Use the native benchmark protocol when a change may affect the runner's speed or output.

For Go changes, run affected package tests while iterating and `task check`
before handoff. Run `go test -race ./...` for shared-state, cache, concurrency,
or serialization changes. Use synthetic PDFs to isolate contracts and real PDFs
to check extraction or export changes. Report any skipped fixture-dependent
checks and distinguish unit evidence from corpus or external-validator evidence.

Make focused signed commits after verified increments. Preserve unrelated staged
and unstaged changes. Do not disable signing when the signing service is unavailable.

### Native Runner Performance Benchmark

Use this protocol when a change may affect the native `docmill` runner's speed or output quality. The goal is to compare the current change against the same 200-PDF DPBench corpus with the same local corpus, tool config, hardware, and output paths.

The benchmark corpus is `docling-project/docling-dpbench`, materialised under `benchmarks/dpbench/corpus`. Prepare it once:

```bash
task benchmark:dpbench:fetch -- --limit 200
```

For native-runner-only comparisons, use a minimal tool config so external converter availability does not block validation. Save this as `benchmarks/dpbench/tools.native.json`:

```json
{
  "tools": [
    {
      "name": "docmill",
      "command": ["bin/docmill-bench", "{{input}}"],
      "output_mode": "stdout",
      "timeout_seconds": 120
    }
  ]
}
```

Run the baseline from the comparison commit or branch first. Rebuild before every benchmark run so `bin/docmill-bench` matches the checked-out code:

```bash
task benchmark:dpbench:build
go run ./cmd/docmill benchmark \
  -corpus benchmarks/dpbench/corpus \
  -tools benchmarks/dpbench/tools.native.json \
  -outputs benchmarks/dpbench/outputs/native-baseline \
  -out benchmarks/dpbench/results/native-baseline.md \
  -json benchmarks/dpbench/results/native-baseline.json \
  -allow-missing \
  -hardware "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m)"
```

Run the same command after applying the change, using separate output paths:

```bash
task benchmark:dpbench:build
go run ./cmd/docmill benchmark \
  -corpus benchmarks/dpbench/corpus \
  -tools benchmarks/dpbench/tools.native.json \
  -outputs benchmarks/dpbench/outputs/native-current \
  -out benchmarks/dpbench/results/native-current.md \
  -json benchmarks/dpbench/results/native-current.json \
  -allow-missing \
  -hardware "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m)"
```

Compare `docmill` in the two JSON files. The key performance field is `milliseconds_per_page`; also check `errors`, `cases`, and all accuracy scores so a speedup does not hide a correctness regression.

```bash
python3 - <<'PY'
import json
from pathlib import Path

def native(path):
    data = json.loads(Path(path).read_text())
    return next(tool for tool in data["tools"] if tool["name"] == "docmill")

base = native("benchmarks/dpbench/results/native-baseline.json")
cur = native("benchmarks/dpbench/results/native-current.json")

fields = [
    ("milliseconds_per_page", lambda t: t["milliseconds_per_page"]),
    ("errors", lambda t: t["errors"]),
    ("cases", lambda t: t["cases"]),
    ("extraction_accuracy", lambda t: t["scores"]["extraction_accuracy"]),
    ("reading_order_nid", lambda t: t["scores"]["reading_order_nid"]),
    ("table_structure_teds", lambda t: t["scores"]["table_structure_teds"]),
    ("heading_level_mhs", lambda t: t["scores"]["heading_level_mhs"]),
]

for name, get in fields:
    b = get(base)
    c = get(cur)
    delta = c - b if isinstance(b, (int, float)) and isinstance(c, (int, float)) else None
    print(f"{name}: baseline={b} current={c} delta={delta}")
PY
```

Validation rules:

- Do not compare `go run` conversion timings against compiled-runner timings; `go run` includes Go toolchain startup.
- Use the same 200-PDF corpus for baseline and current runs.
- Keep baseline and current output directories separate.
- Record `milliseconds_per_page`, `errors`, and score deltas in the final handoff.
- If publishing a cross-tool result, use `benchmarks/dpbench/tools.json` with all required competitors and omit `-allow-missing`.
