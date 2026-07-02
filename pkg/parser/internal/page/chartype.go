// Character-classification helpers ported from core/fpdfapi/parser/
// fpdf_parser_utility.cpp (kPDFCharTypes) and core/fxcrt FXSYS helpers
// @ pdfium 0db284a42. These mirror the unexported equivalents in internal/syntax
// (which the content tokenizer cannot reach) and PDF_NameDecode.
package page

// PDF char classes (kPDFCharTypes). The malformed-input quirk: NUL (0x00),
// 0x80 and 0xFF are whitespace; '+', '-', '.' and digits are numeric;
// the bracket/paren/angle/brace/slash/percent set are delimiters.
const (
	clsWhitespace = iota
	clsNumeric
	clsDelimiter
	clsOther
)

var charClass [256]byte

func init() {
	for i := range charClass {
		charClass[i] = clsOther
	}
	for _, c := range []byte{0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20, 0x80, 0xFF} {
		charClass[c] = clsWhitespace
	}
	for _, c := range []byte{'+', '-', '.'} {
		charClass[c] = clsNumeric
	}
	for c := byte('0'); c <= '9'; c++ {
		charClass[c] = clsNumeric
	}
	for _, c := range []byte{'%', '(', ')', '/', '<', '>', '[', ']', '{', '}'} {
		charClass[c] = clsDelimiter
	}
}

func charIsWhitespace(c byte) bool { return charClass[c] == clsWhitespace }
func charIsNumeric(c byte) bool    { return charClass[c] == clsNumeric }
func charIsDelimiter(c byte) bool  { return charClass[c] == clsDelimiter }
func charIsOther(c byte) bool      { return charClass[c] == clsOther }

func charIsLineEnding(c byte) bool { return c == '\r' || c == '\n' }

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexCharToInt(c byte) int {
	if c <= '9' {
		if c >= '0' {
			return int(c - '0')
		}
		return 0
	}
	u := c &^ 0x20
	if u >= 'A' && u <= 'F' {
		return int(u-'A') + 10
	}
	return 0
}

func isOctalDigit(c byte) bool { return c >= '0' && c <= '7' }

func decimalCharToInt(c byte) int {
	if c >= '0' && c <= '9' {
		return int(c - '0')
	}
	return 0
}

// nameDecode ports PDF_NameDecode: #xx hex escapes, decoded only when there are
// two more bytes after the '#' (strict i+2 < len), matching the internal/syntax
// helper.
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
