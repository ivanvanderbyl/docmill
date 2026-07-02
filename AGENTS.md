# Agent Guidance

## Optimisation Principles

This project is building a generalised PDF to Markdown converter. Optimisations must improve real-world extraction, not just local benchmark scores. The target is state-of-the-art latency per page and accuracy for text, tables, headings, lists, formatting, and reading order across a broad range of documents.

Use benchmarks to measure speed, quality, and regressions. Do not tune implementation behaviour to the benchmark corpus, file names, document IDs, fixture paths, hashes, exact strings, titles, publisher templates, or any other corpus-specific signal.

Detection logic must be algorithmic, deterministic, repeatable, and explainable. Prefer document-model signals such as geometry, font metrics, spans, reading order, alignment, spacing, cell boundaries, page structure, and cross-page consistency. When using statistical thresholds or learned components, keep the inputs and invariants document-general and validate against diverse PDFs.

Do not classify headings, paragraphs, lists, captions, tables, or formatting by relying on character patterns as the primary signal. Examples of overfitting include assuming heading shape from punctuation, case, numbering, prefixes, bullet characters, repeated words, or the literal text content of a document. Character cues may only be supporting evidence inside a broader layout or structure algorithm that remains valid across unrelated documents.

Speed improvements must preserve output quality. A latency change is not acceptable if it regresses text accuracy, table structure, heading levels, formatting fidelity, reading order, or error rate unless the trade-off is explicit, measured, and accepted for a documented mode.

When adding heuristics, document the general invariant they encode, add tests that cover both positive and negative cases, and smoke-test on real PDFs when the change affects extraction behaviour. Prefer conservative detection with clear false-positive guards over broad rules that look good on one corpus but fail on prose-heavy or unusual documents.

## Validation

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
