// Package parser is a pure-Go port of the PDFium parsing pipeline: load
// document, enumerate pages, parse content streams, decode text, map character
// codes to Unicode, and emit the line-rect segmentation that pkg/pdf consumes.
// It is a drop-in pkg/pdf.Backend that reproduces PDFium's behaviour
// bug-for-bug on malformed input.
//
// The parser is assembled bottom-up from the internal packages, mirroring
// PDFium's layering: crt -> codec/objects -> syntax -> parser -> document ->
// page -> font -> text. (PDFium's crypto layer sits between parser and
// document; encrypted PDFs are unsupported until plan 009 Phase E lands.)
package parser
