// Ported from core/fpdfapi/parser/cpdf_simple_parser.{h,cpp} @ pdfium
// 0db284a42. A lighter, non-recursive tokenizer over an in-memory []byte, used
// by ToUnicode/CMap parsing. Unlike SyntaxParser it does NO string-escape
// processing.
package syntax

// SimpleParser tokenizes data with a plain cursor.
type SimpleParser struct {
	data []byte
	pos  int
}

// NewSimpleParser returns a parser over data.
func NewSimpleParser(data []byte) *SimpleParser { return &SimpleParser{data: data} }

// GetWord returns the next token (a view of data as a string), or "" at EOF.
func (sp *SimpleParser) GetWord() string {
	startChar, ok := sp.skipSpacesAndComments()
	if !ok {
		return ""
	}
	startPos := sp.pos - 1
	if !isDelimiter(startChar) {
		return sp.handleNonDelimiter()
	}
	switch startChar {
	case '/':
		return sp.handleName()
	case '<':
		return sp.handleBeginAngle()
	case '>':
		return sp.handleEndAngle()
	case '(':
		return sp.handleParens()
	default:
		return sp.dataTo(startPos)
	}
}

func (sp *SimpleParser) dataTo(start int) string { return string(sp.data[start:sp.pos]) }

func (sp *SimpleParser) skipSpacesAndComments() (byte, bool) {
	for {
		if sp.pos >= len(sp.data) {
			return 0, false
		}
		ch := sp.data[sp.pos]
		sp.pos++
		for isWhitespace(ch) {
			if sp.pos >= len(sp.data) {
				return 0, false
			}
			ch = sp.data[sp.pos]
			sp.pos++
		}
		if ch != '%' {
			return ch, true
		}
		for {
			if sp.pos >= len(sp.data) {
				return 0, false
			}
			ch = sp.data[sp.pos]
			sp.pos++
			if isLineEnding(ch) {
				break
			}
		}
	}
}

func (sp *SimpleParser) handleName() string {
	start := sp.pos - 1
	for sp.pos < len(sp.data) {
		ch := sp.data[sp.pos]
		if isWhitespace(ch) || isDelimiter(ch) {
			return sp.dataTo(start)
		}
		sp.pos++
	}
	return "" // EOF before a terminator -> empty
}

func (sp *SimpleParser) handleNonDelimiter() string {
	start := sp.pos - 1
	for sp.pos < len(sp.data) {
		ch := sp.data[sp.pos]
		if isDelimiter(ch) || isWhitespace(ch) {
			break
		}
		sp.pos++
	}
	return sp.dataTo(start)
}

func (sp *SimpleParser) handleBeginAngle() string {
	start := sp.pos - 1
	if sp.pos >= len(sp.data) {
		return sp.dataTo(start)
	}
	ch := sp.data[sp.pos]
	sp.pos++
	if ch == '<' { // "<<"
		return sp.dataTo(start)
	}
	for sp.pos < len(sp.data) && ch != '>' {
		ch = sp.data[sp.pos]
		sp.pos++
	}
	return sp.dataTo(start)
}

func (sp *SimpleParser) handleEndAngle() string {
	start := sp.pos - 1
	if sp.pos < len(sp.data) && sp.data[sp.pos] == '>' {
		sp.pos++
	}
	return sp.dataTo(start)
}

func (sp *SimpleParser) handleParens() string {
	start := sp.pos - 1
	level := 1
	for sp.pos < len(sp.data) && level > 0 {
		ch := sp.data[sp.pos]
		sp.pos++
		if ch == '(' {
			level++
		} else if ch == ')' {
			level--
		}
	}
	return sp.dataTo(start)
}
