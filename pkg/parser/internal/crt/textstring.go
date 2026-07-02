// Ported from PDF_DecodeText / StripLanguageCodes in
// core/fpdfapi/parser/fpdf_parser_decode.cpp @ pdfium 0db284a42.
//
// DecodePDFTextString replaces the role WideString plays at PDF text-string
// boundaries: it detects a UTF-16BE/LE or UTF-8 BOM and otherwise falls back to
// PDFDocEncoding, returning idiomatic Go UTF-8. UTF-16 surrogate pairs are
// resolved here (utf16.Decode), per plan 009's "no WideString type" rule.
package crt

import (
	"encoding/binary"
	"unicode/utf16"
)

// DecodePDFTextString decodes a PDF text string (PDF 1.7 §7.9.2.2) into UTF-8.
//
//   - 0xFE 0xFF prefix: the remainder is UTF-16BE.
//   - 0xFF 0xFE prefix: the remainder is UTF-16LE.
//   - 0xEF 0xBB 0xBF prefix: the remainder is UTF-8.
//   - otherwise: each byte is a PDFDocEncoding code point.
//
// In the BOM cases, Unicode language-tag escape regions (delimited by U+001B)
// are stripped, matching PDFium.
func DecodePDFTextString(data []byte) string {
	if len(data) >= 2 && data[0] == 0xfe && data[1] == 0xff {
		runes := decodeUTF16(data[2:], true)
		return string(stripLanguageCodes(runes))
	}
	if len(data) >= 2 && data[0] == 0xff && data[1] == 0xfe {
		runes := decodeUTF16(data[2:], false)
		return string(stripLanguageCodes(runes))
	}
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		runes := []rune(string(data[3:]))
		return string(stripLanguageCodes(runes))
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(pdfDocEncoding[b])
	}
	return string(runes)
}

// decodeUTF16 decodes a byte slice of UTF-16 code units (big- or little-endian)
// into runes, combining surrogate pairs. A trailing odd byte is ignored.
func decodeUTF16(data []byte, bigEndian bool) []rune {
	order := binary.ByteOrder(binary.BigEndian)
	if !bigEndian {
		order = binary.LittleEndian
	}
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		units = append(units, order.Uint16(data[i:]))
	}
	return utf16.Decode(units)
}

// stripLanguageCodes removes U+001B-delimited language metadata regions.
// Ported from StripLanguageCodes; an unterminated region runs to the end.
func stripLanguageCodes(runes []rune) []rune {
	out := runes[:0:0]
	for i := 0; i < len(runes); i++ {
		if runes[i] == 0x001B {
			for i++; i < len(runes) && runes[i] != 0x001B; i++ {
				// Skip until the terminating marker.
			}
			continue
		}
		out = append(out, runes[i])
	}
	return out
}
