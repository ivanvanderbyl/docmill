// Package syntax is the byte-level PDF lexer ported from
// core/fpdfapi/parser/cpdf_syntax_parser.{h,cpp} and cpdf_simple_parser.{h,cpp}
// @ pdfium 0db284a42, with the char-classification table from
// fpdf_parser_utility.cpp.
//
// Per plan 009 design §5 the parser operates over the WHOLE PDF as a single
// []byte with plain index arithmetic; PDFium's sliding file_buf_/buf_offset_
// refill state machine and CPDF_ReadValidator are dropped. See plan 009 Phase C.
package syntax

// Character classes from kPDFCharTypes[256] (fpdf_parser_utility.cpp). Note the
// malformed-input quirk: NUL (0x00), 0x80 and 0xFF are whitespace.
const (
	charWhitespace = iota
	charNumeric
	charDelimiter
	charOther
)

var pdfCharType [256]byte

func init() {
	for i := range pdfCharType {
		pdfCharType[i] = charOther
	}
	for _, c := range []byte{0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20, 0x80, 0xFF} {
		pdfCharType[c] = charWhitespace
	}
	for _, c := range []byte{'+', '-', '.'} {
		pdfCharType[c] = charNumeric
	}
	for c := byte('0'); c <= '9'; c++ {
		pdfCharType[c] = charNumeric
	}
	for _, c := range []byte{'%', '(', ')', '/', '<', '>', '[', ']', '{', '}'} {
		pdfCharType[c] = charDelimiter
	}
}

func isWhitespace(c byte) bool { return pdfCharType[c] == charWhitespace }
func isNumeric(c byte) bool    { return pdfCharType[c] == charNumeric }
func isDelimiter(c byte) bool  { return pdfCharType[c] == charDelimiter }
func isOther(c byte) bool      { return pdfCharType[c] == charOther }

// isLineEnding is CR/LF only (not table-based).
func isLineEnding(c byte) bool { return c == '\r' || c == '\n' }

// isHexDigit matches FXSYS_IsHexDigit: 0-9 a-f A-F, excluding bytes >= 0x80.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// hexCharToInt matches FXSYS_HexCharToInt.
func hexCharToInt(c byte) int {
	if c <= '9' {
		if c >= '0' {
			return int(c - '0')
		}
		return 0
	}
	u := c &^ 0x20 // uppercase
	if u >= 'A' && u <= 'F' {
		return int(u-'A') + 10
	}
	return 0
}

// atoui matches FXSYS_atoui: optional sign, decimal digits, saturating at
// MaxUint32, two's-complement negation. "4294967295" -> 0xFFFFFFFF.
func atoui(s string) uint32 {
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	var num uint32
	const maxU = ^uint32(0)
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		d := uint32(s[i] - '0')
		if num > (maxU-d)/10 {
			num = maxU
		} else {
			num = num*10 + d
		}
		i++
	}
	if neg {
		num = ^num + 1
	}
	return num
}

// nameDecode ports PDF_NameDecode: #xx hex escapes, decoded only when there are
// two more bytes after the '#' (strict i+2 < len).
func nameDecode(orig []byte) string {
	out := make([]byte, 0, len(orig))
	n := len(orig)
	for i := 0; i < n; i++ {
		if orig[i] == '#' && i+2 < n {
			out = append(out, byte(hexCharToInt(orig[i+1])*16+hexCharToInt(orig[i+2])))
			i += 2
		} else {
			out = append(out, orig[i])
		}
	}
	return string(out)
}
