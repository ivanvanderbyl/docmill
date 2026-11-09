# Integrate Docmill in a Go application

Use Docmill to extract Markdown from PDFs with embedded text, inspect PDF
resources, or write PDFs through the native Go parser. This guide targets Go
application developers. For individual operation contracts, see
[Native PDF operations](pdf-operations.md) and
[Signature images and verification](signatures.md).

## Choose the API

Import paths use the module prefix `github.com/ivanvanderbyl/docmill/v2`.

| Task | API | Result or constraint |
| --- | --- | --- |
| Convert a PDF to Markdown | `parser.NewBackend`, then `pdf.ExtractMarkdown` | Extracts embedded text and reconstructs document structure; no OCR. |
| Read page count, dimensions, or text | `pdf.Document` and `pdf.Page` | Common backend interfaces. |
| Open an encrypted PDF | `(*parser.Backend).OpenBytesWithPassword` | Returns a `pdf.Document` backed by the native parser. |
| Copy, reorder, or truncate pages | `(*parser.Document).WritePages` or `WritePageRange` | Creates a new PDF with page-level content. |
| Merge PDFs | `parser.MergePDFs` or `WritePageSelections` | Copies pages; does not merge document-level form trees. |
| Extract image resources | `(*parser.Document).ExtractImages` | Encoded PNG/JPEG resources, not rendered page screenshots. |
| Read or fill AcroForm values | `AcroFormValues`, `SetAcroFormValues`, `WritePDF` | Filling changes in-memory values; save explicitly. |
| Read form geometry and labels | `(*parser.Page).FormFieldBoxes` | Includes unfilled fields and individual widgets. |
| Inspect attachment metadata or active content | `Attachments`, `HasActiveContent` | Inspection results for an application policy; no sanitisation. |
| Place a visible image or verify a digital signature | See [signature APIs](signatures.md) | Image placement, byte integrity, and certificate trust have distinct contracts. |

The CLI exposes PDF/JSON conversion, AcroForm commands, and benchmarking.
Use the Go APIs for the additional operations in this table.

The extra PDF operations belong to `*parser.Document`; they are not methods on
the smaller `pdf.Document` interface. When you open with `*parser.Backend`, the
returned document has this native type. Do not assume that type when your
application accepts arbitrary backend implementations.

## Select a dependency version

Pin the module version in your application's `go.mod` and commit `go.sum`.
Confirm that the selected version contains each API you use. Documentation in a
checkout describes that checkout; it does not establish that the same APIs have
been released in the module proxy.

For development against a local checkout, add a temporary module replacement:

```bash
go mod edit -replace=github.com/ivanvanderbyl/docmill/v2=/absolute/path/to/docmill
```

Remove the local replacement before publishing a consumer that must build on
other machines. The current repository requires Go 1.27.

## Convert a PDF to Markdown

This complete program reads a PDF, converts it with the default extraction
options, and writes Markdown to standard output:

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser"
	"github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: convert input.pdf")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read PDF: %w", err)
	}

	backend := parser.NewBackend()
	defer backend.Close()

	doc, err := backend.OpenBytes(ctx, data)
	if err != nil {
		return fmt.Errorf("open PDF: %w", err)
	}
	defer doc.Close()

	markdown, err := pdf.ExtractMarkdown(ctx, doc)
	if err != nil {
		return fmt.Errorf("extract Markdown: %w", err)
	}
	_, err = fmt.Fprint(os.Stdout, markdown)
	return err
}
```

The native backend parses in memory. Its close methods currently release no
external resources, but retaining the close calls keeps the consumer compatible
with the interface lifecycle.

### Set extraction options explicitly

`ExtractMarkdown` enables reading order, paragraph/list structure, headings,
and table detection. Inline bold, italic, and code formatting is disabled.

`ExtractMarkdownWithOptions` uses the options you supply; it does not merge
boolean fields with the defaults. To preserve default features while enabling
inline formatting and limiting page concurrency:

```go
markdown, err := pdf.ExtractMarkdownWithOptions(ctx, doc, pdf.ExtractionOptions{
	DetectTables:           true,
	ReadingOrder:           true,
	DetectStructure:        true,
	DetectHeadings:         true,
	EnableInlineFormatting: true,
	MaxParallelPages:       2,
})
```

`MaxParallelPages: 1` selects serial extraction. Values at or below zero use a
bounded default based on `GOMAXPROCS`, capped at 12 workers and the page count.
Bound document concurrency in your service as well as page concurrency within
each conversion.

An empty Markdown result does not establish that the PDF is empty. Image-only
pages have no embedded text to extract. Route documents requiring OCR to an
OCR-capable component; Docmill does not perform OCR.

## Open a password-protected document

```go
backend := &parser.Backend{}
opened, err := backend.OpenBytesWithPassword(ctx, input, password)
if err != nil {
	return fmt.Errorf("open PDF: %w", err)
}
defer opened.Close()
doc := opened.(*parser.Document)
```

Use `errors.Is` on returned or wrapped errors:

| Error | Consumer response |
| --- | --- |
| `parser.ErrPasswordRequired` | Request a password if the application supports encrypted input. |
| `parser.ErrWrongPassword` | Report that the supplied password did not authenticate. |
| `parser.ErrUnsupportedEncryption` | Report an unsupported encryption scheme. |
| `parser.ErrInvalidPDF` | Report a PDF that could not be parsed. |
| Context cancellation or deadline error | Stop the operation and propagate the caller's cancellation. |

`OpenBytes` tries the empty password. An encrypted PDF can therefore open
successfully without a supplied password. Call `IsEncrypted` when that distinction
matters to your application. Handle errors from later reads too: some objects
are decoded lazily.

## Copy pages and save output

The following fragment writes the third page, followed by the first page twice:

```go
var output bytes.Buffer
if err := doc.WritePages(ctx, &output, []int{2, 0, 0}); err != nil {
	return fmt.Errorf("copy PDF pages: %w", err)
}
if err := os.WriteFile("selected.pdf", output.Bytes(), 0600); err != nil {
	return fmt.Errorf("save PDF: %w", err)
}
```

The fragment uses `bytes`, `fmt`, and `os`. It assumes `doc` is an open
`*parser.Document` with at least three pages.

Indices are zero-based. `WritePageRange(ctx, writer, start, end)` uses an exclusive
end index: `[0, 3)` means the first three pages. A nil selection passed to
`ExtractImages` means all pages; a nil or empty page selection passed to
`WritePages` is invalid.

Page copying retains page content, resources, annotations, and inherited page
attributes. It excludes document-level outlines, attachments, metadata, and the
AcroForm field tree. It is not a way to preserve interactive forms or digital
signatures. Whole-document `WritePDF` also rewrites the file and does not preserve
existing digital signatures.

`WritePDF`, page copying, and image stamping produce unencrypted output. Decide
whether plaintext output is acceptable before exporting an encrypted input.
These operations leave the source document unchanged; `SetAcroFormValues` changes
its in-memory form state.

For a file destination, write to a buffer or temporary file and publish it only
after the operation succeeds. An `io.Writer` error can leave partial output.
Reopen saved output when validating the file that will be delivered.

## Manage ownership and concurrency

- Pass a request-scoped context through opening, extraction, inspection, and
  export. Cancellation is checked at operation boundaries and during some
  traversal loops; it is not a hard CPU or memory limit.
- Apply input-size and concurrent-job limits before loading untrusted files.
  The public APIs accept whole byte slices, and image extraction can allocate
  decoded pixels in addition to the source PDF.
- Read-only text, image, attachment, form-value, and active-content operations
  can share a native document. Full and selected-page exports are covered by
  the same concurrent-read regression tests.
- Serialise form mutations with reads and exports, or use separate document
  instances. Do not infer concurrent-mutation support from concurrent-read tests.
- Treat returned data as caller-owned results. Do not modify shared results
  while another goroutine reads them.

## Handle unsupported operations

Image decoding supports a defined set of codecs, colour spaces, and masks.
Extraction returns an error for unsupported images rather than silently
omitting them. See the [image support list](pdf-operations.md#extract-images)
before selecting an application fallback.

`Attachments` returns metadata, not extracted attachment payloads.
`HasActiveContent` reports a conservative set of declarations and actions; a
false result does not certify a document as safe. Keep application acceptance
policy separate from parsing success.

For signatures, check the [verification contract](signatures.md) before accepting
an output. A non-empty list, intact signed bytes, a trusted certificate chain,
and coverage of the current PDF are separate conditions.

## Verify an integration change

Use consumer-level tests with your representative PDFs. Check both success and
failure paths: password errors, invalid ranges, unsupported images, cancelled
requests, and output writer failures as relevant to the features you use.
For exports, reopen the output and assert the pages, text, forms, or image data
your application needs to retain.

When replacing another PDF library, record its observable contract first.
Compare page numbering, empty-input handling, error classification, retained
metadata, encryption, and signature behaviour explicitly. Parser recovery is not
proof that two libraries interpret every malformed PDF identically.

For changes to Docmill itself, follow [repository guidance](../AGENTS.md),
run `task check`, and use its paired native benchmark protocol when extraction
speed or output can change.
