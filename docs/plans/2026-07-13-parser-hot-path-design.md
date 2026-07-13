# Parser Hot-Path Optimisation Design

## Goal

Reduce native PDF-to-Markdown latency without changing extracted text, tables,
headings, formatting, or reading order.

## Evidence

The production trace spent about 4.20 seconds converting ten pages. The page
spans accounted for virtually the whole conversion, while table detection,
reading order, assembly, and rendering accounted for only about six percent.
The native backend currently constructs the same interpreted page for size,
text, form, and ruling queries, and constructs the same text page separately
for prose rectangles and word rectangles. Rectangle text extraction also scans
the complete character stream once per rectangle.

## Approach

Optimise in two independently measurable stages:

1. Cache the interpreted page, page size, and text page on each native `Page`
   handle. All existing public methods keep their contracts and output paths;
   they only reuse immutable parsed state.
2. If the first benchmark leaves rectangle extraction dominant, add a batched
   text extraction primitive that preserves the existing `GetTextByRect`
   inclusion and generated-space semantics while avoiding a full character
   scan for every rectangle.

Each stage must begin with a failing regression test, retain exact corpus
outputs, and receive its own 200-PDF DPBench comparison. No detection threshold
or document-content heuristic changes are in scope.

## Validation

Run focused parser tests, the full Go test suite, and the prescribed compiled
native-runner DPBench protocol. Accept a change only with zero added errors and
no regression in extraction accuracy, reading order NID, table TEDS, or heading
MHS.
