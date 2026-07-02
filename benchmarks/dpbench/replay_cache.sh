#!/bin/sh
# replay_cache.sh <cache-subdir> <input.pdf> <output.md>
#
# Replays a frozen competitor output from benchmarks/dpbench/cache/<cache-subdir>/
# for the given input PDF, copying it to <output.md>. Used by tools.cached.json
# so docmill benchmark reruns score against cached competitor outputs without
# re-running the (slow) converters. Populate the cache once with
# tools.cachepop.json; see benchmarks/dpbench/rerun.sh.
set -eu
sub=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')
input="$2"
output="$3"
here=$(CDPATH= cd "$(dirname "$0")" && pwd)
base=$(basename "$input")
base=${base%.pdf}
src="$here/cache/$sub/$base.md"
if [ ! -f "$src" ]; then
  echo "replay_cache: cache miss: $src" >&2
  exit 1
fi
cp "$src" "$output"
