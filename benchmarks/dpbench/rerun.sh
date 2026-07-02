#!/bin/sh
# Fast docmill DPBench rerun: rebuilds docmill and scores it against the frozen
# competitor cache (no slow converters re-run). Populate the cache first with:
#   go run ./cmd/docmill benchmark -corpus benchmarks/dpbench/corpus \
#     -tools benchmarks/dpbench/tools.cachepop.json -outputs benchmarks/dpbench/cache \
#     -json benchmarks/dpbench/results/cachepop.json -allow-missing
set -eu
cd "$(CDPATH= cd "$(dirname "$0")/../.." && pwd)"
go build -o bin/docmill-bench ./cmd/docmill
go run ./cmd/docmill benchmark \
  -corpus benchmarks/dpbench/corpus \
  -tools benchmarks/dpbench/tools.cached.json \
  -outputs benchmarks/dpbench/outputs/cached-rerun \
  -out benchmarks/dpbench/results/cached.md \
  -json benchmarks/dpbench/results/cached.json \
  -allow-missing -hardware "$(sysctl -n machdep.cpu.brand_string 2>/dev/null || uname -m)"
echo "report: benchmarks/dpbench/results/cached.md"
