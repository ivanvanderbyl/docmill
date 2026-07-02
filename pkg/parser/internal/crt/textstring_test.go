// Ported behaviour from PDF_DecodeText in
// core/fpdfapi/parser/fpdf_parser_decode.cpp @ pdfium 0db284a42.
// Confirms the string<->[]byte/UTF mapping replaces WideString correctly
// (BOM detection, PDFDocEncoding fallback, surrogate pairs, language strip).
package crt

import "testing"

func TestDecodePDFTextStringUTF16BE(t *testing.T) {
	got := DecodePDFTextString([]byte{0xfe, 0xff, 0x00, 0x41, 0x00, 0x42})
	if got != "AB" {
		t.Errorf("UTF-16BE decode = %q, want %q", got, "AB")
	}
}

func TestDecodePDFTextStringUTF16LE(t *testing.T) {
	got := DecodePDFTextString([]byte{0xff, 0xfe, 0x41, 0x00, 0x42, 0x00})
	if got != "AB" {
		t.Errorf("UTF-16LE decode = %q, want %q", got, "AB")
	}
}

func TestDecodePDFTextStringPDFDocEncoding(t *testing.T) {
	// Plain ASCII passes through unchanged.
	if got := DecodePDFTextString([]byte("Hello")); got != "Hello" {
		t.Errorf("ASCII decode = %q, want %q", got, "Hello")
	}
	// Byte 0x80 maps to U+2022 (bullet) in PDFDocEncoding.
	if got := DecodePDFTextString([]byte{0x80}); got != "•" {
		t.Errorf("0x80 decode = %q, want bullet", got)
	}
	// Byte 0x18 maps to U+02D8 (breve).
	if got := DecodePDFTextString([]byte{0x18}); got != "˘" {
		t.Errorf("0x18 decode = %q, want breve", got)
	}
}

func TestDecodePDFTextStringSurrogatePair(t *testing.T) {
	// UTF-16BE D83D DE00 -> U+1F600 grinning face.
	got := DecodePDFTextString([]byte{0xfe, 0xff, 0xd8, 0x3d, 0xde, 0x00})
	if got != "\U0001F600" {
		t.Errorf("surrogate decode = %q, want grinning face", got)
	}
}

func TestDecodePDFTextStringStripsLanguageCodes(t *testing.T) {
	// UTF-16BE: A <0x1B> x <0x1B> B  ->  "AB".
	data := []byte{0xfe, 0xff, 0x00, 0x41, 0x00, 0x1b, 0x00, 0x78, 0x00, 0x1b, 0x00, 0x42}
	if got := DecodePDFTextString(data); got != "AB" {
		t.Errorf("language-strip decode = %q, want %q", got, "AB")
	}
}
