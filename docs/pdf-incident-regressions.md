# PDF incident regression coverage

The native parser has regressions for the failure conditions that motivated
replacing pdfcpu. These tests use synthetic PDFs containing the relevant PDF
structures. They do not contain customer files or establish that the original
customer PDFs have been replayed.

| Incident | Recorded failure | Native regression |
| --- | --- | --- |
| [ALCOVA-BACKEND-15R](https://alcova.sentry.io/issues/ALCOVA-BACKEND-15R) | pdfcpu rejected a `/Pages` node with `/Count 0` and `/Kids []` while extracting pages 1–4 | `TestIncident15REmptyPageTreeBranchRoundTrip` checks an empty branch before, between, and after real pages; full, partial, single, and reordered duplicate selections preserve page count, dimensions, text, and order. |
| [AI-1654](https://linear.app/alcova/issue/AI-1654/getemailriskscore-500s-pdfcpu-panic-on-pdf-attachment-truncation) | `migrateNamedDests` indexed an empty destination array during attachment truncation | `TestIncident1654EmptyNamedDestinationsDoNotBreakTruncation` covers empty array destinations and missing or empty dictionary destinations through page copying and whole-document save. |
| [AI-1660](https://linear.app/alcova/issue/AI-1660/guard-pdfcpu-calls-in-pdfpreview-and-forms-against-panics) | Preview and form calls exposed the same unguarded parser-panic risk | Malformed-operation tests cover invalid form fields and annotations, truncated image samples and encodings, truncated PDFs, and invalid public handles. Valid fields remain readable and writable; invalid image data returns an error. |
| [AI-1925](https://linear.app/alcova/issue/AI-1925/prevent-pdfcpu-config-races-from-terminating-fresh-instances) | Concurrent first use raced while creating a shared config file; pdfcpu then called `os.Exit` | `TestNativeFirstUseIgnoresPDFCPUConfig` runs first use in a child process with a corrupt config. `TestConcurrentPDFOperations` runs text, image, attachment, form, and serialization operations against shared and independent documents under the race detector. |

## Corrections found by these tests

Whole-document serialization no longer assigns object numbers back into source
objects or changes source stream dictionaries to update `/Length`. Page-tree
classification no longer writes missing `/Type` entries into shared source
dictionaries. These changes allow serialization and read operations to share a
document without those races. Concurrent form mutation is outside this contract.

Invalid form-document and page handles return errors instead of panicking.
Malformed nested or cyclic form-value arrays are ignored without recursive
traversal; valid scalar selections remain available.

## Reproduce

```sh
go test -race ./pkg/parser -run 'TestIncident|TestConcurrent|TestNativeFirstUse|TestFormsIgnore|TestFormAPIs|TestImageAPIs|TestTruncatedFiles' -count=1
task check
go test -race ./...
```

The Sentry event for ALCOVA-BACKEND-15R has no PDF attachment. Its recorded
page-tree structure and the backend regression in PR #2853 provide the fixture
shape. AI-1654 and AI-1660 also lack retained customer PDFs in the reviewed
issue context. Backend integration must separately preserve error handling and
on-send fallback behavior; these are library-level regressions.

## Validation result

`task check` and `go test -race ./...` pass. A compiled-runner comparison
against `0e6e9e6` on the same 200-PDF DPBench corpus completed with zero errors
and zero deltas for extraction accuracy, reading order, table structure, and
heading levels. Latency measured 13.40 ms/page before and 14.03 ms/page after.
This is one local paired measurement. Baseline and current output paths were
kept separate.
