# Native PDF operations

The native `parser.Backend` supports PDF page copying, merging, image extraction,
attachment enumeration, and password-protected documents. These APIs operate in
memory and do not require pdfcpu or a PDFium shared library.

Open a document and obtain its native operations:

```go
backend := &parser.Backend{}
opened, err := backend.OpenBytes(ctx, input)
if err != nil {
    return err
}
defer opened.Close()
doc := opened.(*parser.Document)
```

## Copy and merge pages

`doc.WritePages(ctx, writer, []int{2, 0, 2})` copies the third, first, and third
pages in that order. Indices are zero-based. Duplicate selections create separate
page dictionaries. `doc.WritePageRange(ctx, writer, 0, 3)` copies the first three
pages; its end index is exclusive. Empty and out-of-range selections return an
error before writing output.

The writer preserves page contents, resources, annotations, and inherited
MediaBox, CropBox, Rotate, and Resources values. It remaps indirect references
into the new document. It does not copy catalog-level outlines, attachments,
metadata, or the AcroForm field tree. Do not use page copying to preserve an
interactive form's document-level behavior or a digital signature. Use
`doc.WritePDF` when you need to serialize the whole document.

`parser.WritePageSelections` combines selections from open native documents.
`parser.MergePDFs(ctx, first, second)` combines every page from PDF byte slices.
For password-protected inputs, open each document with its password and use
`WritePageSelections`.

## Extract images

`doc.ExtractImages(ctx, nil)` extracts images from all pages. Supply zero-based
page indices to restrict extraction. Each result includes its page index,
resource name, dimensions, encoding, and encoded bytes. Decode `Data` with Go's
`image.Decode` after registering the JPEG and PNG decoders.

Image extraction returns image resources rather than a rendered page. Page text,
vector graphics, clipping, image placement, and blending are not a page preview.
Use a renderer when a complete page preview is required.

Supported image features include Gray, RGB, CMYK, Indexed, RGB matrix/TRC and grayscale TRC ICC
profiles, 1/2/4/8/16-bit samples, decode arrays, color-key and image masks,
soft masks, matte correction, JPEG, and supported CCITT fax encodings. JBIG2,
JPEG 2000, ICC lookup-table profiles, CMYK ICC profiles, and unsupported
color spaces return an error. CalGray, CalRGB, Lab, Separation, DeviceN, mixed
Group 3 fax, and Group 3 fax without end-of-line markers are not supported.
Masks must match the image dimensions; mask resampling is not supported. Extraction does not silently omit an image when decoding fails.

## Password-protected documents

`backend.OpenBytesWithPassword(ctx, input, password)` accepts a user or owner
password. `OpenBytes` tries the empty password. Use `errors.Is` to distinguish
`parser.ErrPasswordRequired`, `parser.ErrWrongPassword`,
`parser.ErrUnsupportedEncryption`, and `parser.ErrInvalidPDF`.

The standard security handler supports revisions 2 through 6 with RC4 or AES.
Mixed stream and string crypt filters and unsupported security handlers return
an error. Identity crypt-filter configurations still require password
authentication; this is stricter than PDFium's no-encryption shortcut. `doc.IsEncrypted(ctx)` identifies encrypted inputs even when they
open with an empty user password. `WritePDF` and page-copy operations write
unencrypted output. Passwords and access permissions are not copied to that
output.

## Inspect uploads

`doc.Attachments(ctx)` lists metadata from the catalog's EmbeddedFiles name tree.
It does not decode attachment payloads. Attachment names are untrusted metadata;
do not use them as filesystem paths without validation.

`doc.HasActiveContent(ctx)` scans parsed indirect objects, including objects
outside the page tree, for EmbeddedFiles and JavaScript keys and JavaScript or
Launch names. This conservatively implements an upload policy. It is not a
sanitizer, malware scanner, or proof that a PDF is safe. A policy may reject a
file when attachments are present or this check returns true. Handle inspection
errors explicitly.

## Conformance references

The implementation references PDFium revision
[`6688c576fd851f4159c6fae39396da50939de110`](https://pdfium.googlesource.com/pdfium/+/6688c576fd851f4159c6fae39396da50939de110/):

- `fpdfsdk/fpdf_ppo.cpp`, `core/fpdfapi/edit/cpdf_pageexporter.cpp`, and
  `core/fpdfapi/edit/cpdf_pageorganizer.cpp`: page copying and reference mapping.
- `fpdfsdk/fpdf_attachment.cpp`, `core/fpdfdoc/cpdf_nametree.cpp`, and
  `core/fpdfdoc/cpdf_filespec.cpp`: attachment metadata and filename precedence.
- `core/fpdfdoc/cpdf_action.cpp`: action names used by upload inspection.
- `core/fpdfapi/parser/cpdf_security_handler.cpp` and `cpdf_crypto_handler.cpp`:
  password authentication and object decryption.
- `core/fpdfapi/page/cpdf_dib.cpp` and image color-space implementations:
  image samples, decoding, and transparency.

Tests construct small PDFs to isolate each contract. Independent QPDF checks
validate serialization and encrypted fixtures where documented in the tests.
