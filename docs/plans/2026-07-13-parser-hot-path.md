# Parser Hot-Path Optimisation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove redundant native page and text-page work, then benchmark the exact output and latency impact on 200 DPBench PDFs.

**Architecture:** Cache immutable interpreted state inside each native parser `Page` handle, preserving every public extraction contract. Consider batched rectangle text extraction only as a separately tested and benchmarked second stage.

**Tech Stack:** Go, native docmill PDF parser, Testify, DPBench benchmark harness.

---

### Task 1: Record the baseline

**Files:**
- Generate: `benchmarks/dpbench/results/native-baseline.json`
- Generate: `benchmarks/dpbench/results/native-baseline.md`

1. Materialise exactly 200 DPBench cases.
2. Rebuild `bin/docmill-bench` from untouched code.
3. Run the native-only benchmark into isolated baseline paths.
4. Record latency, errors, cases, and all four quality scores.

### Task 2: Cache page interpretation

**Files:**
- Modify: `pkg/parser/backend.go`
- Test: `pkg/parser/backend_cache_test.go`

1. Write a failing test proving repeated loads on one page handle return the
   same interpreted page.
2. Run the focused test and confirm it fails for pointer inequality.
3. Add per-handle cached page, size, error, and `sync.Once` state.
4. Run the focused test and existing parser tests.

### Task 3: Cache text-page interpretation

**Files:**
- Modify: `pkg/parser/backend.go`
- Test: `pkg/parser/backend_cache_test.go`

1. Write a failing test proving prose and word extraction share one text-page
   interpretation.
2. Run the focused test and confirm the missing cache behaviour fails.
3. Add a `sync.Once`-guarded text-page accessor and route `TextCells`,
   `WordTextCells`, and `TextInRect` through it.
4. Run focused and full tests, then format the Go files.

### Task 4: Benchmark cached interpretation

**Files:**
- Generate: `benchmarks/dpbench/results/native-current.json`
- Generate: `benchmarks/dpbench/results/native-current.md`

1. Rebuild the compiled runner.
2. Run the same 200 cases into isolated current paths.
3. Compare latency, errors, cases, all quality scores, and output files.
4. Stop if the gain is material and the remaining hotspot is not established;
   otherwise begin a separate red-green cycle for batched rectangle extraction.

### Task 5: Final verification

1. Run `gofmt` on changed Go files.
2. Run `go test ./...` with a writable Go cache.
3. Run `git diff --check`.
4. Report exact benchmark deltas and any environment caveats.
