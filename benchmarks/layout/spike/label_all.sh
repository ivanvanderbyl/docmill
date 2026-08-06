#!/usr/bin/env bash
# Task 0 step 1: run the HURIDOCS labeller (fast=true) over the spike corpus.
set -u
cd "$(dirname "$0")"
for pdf in pdfs/*.pdf; do
  name=$(basename "$pdf" .pdf)
  out="labels/$name.json"
  if [ -s "$out" ]; then echo "skip $name"; continue; fi
  echo "labelling $name ..."
  code=$(curl -s -X POST -F "file=@$pdf" -F 'fast=true' localhost:5060 -o "$out" -w '%{http_code}')
  echo "  $name -> http=$code bytes=$(stat -c%s "$out")"
  if [ "$code" != "200" ]; then rm -f "$out"; fi
done
echo DONE
