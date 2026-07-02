// Ported from core/fpdfapi/page/cpdf_streamparser.{h,cpp} @ pdfium 0db284a42.
//
// The content-stream tokenizer (distinct from the file-level syntax parser).
// ParseNextElement classifies each token as {EndOfData, Number, Keyword, Name,
// Other}; ReadNextObject parses full PDF objects for arrays/dicts/strings (the
// TJ array, inline-image dict, marked-content property dict). Inline-image
// binary data is not decoded — the interpreter's BI handler re-scans for EI.
package page

import (
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
)

// StreamElementType mirrors CPDF_StreamParser::ElementType.
type StreamElementType int

const (
	// ElemEndOfData signals the stream is exhausted.
	ElemEndOfData StreamElementType = iota
	// ElemNumber is a numeric token.
	ElemNumber
	// ElemKeyword is an operator.
	ElemKeyword
	// ElemName is a /name token (the leading '/' is included in the word).
	ElemName
	// ElemOther is a full object (string/array/dict/boolean/null).
	ElemOther
)

const (
	kMaxWordLength         = 255
	kMaxNestedParsingLevel = 512
	kMaxStringLength       = 32767
)

// StreamParser is the content tokenizer over a []byte with a position cursor.
type StreamParser struct {
	buf     []byte
	pos     uint32
	word    []byte         // current word, cap-bounded at kMaxWordLength
	lastObj objects.Object // result of ReadNextObject for ElemOther
}

// newStreamParser builds a tokenizer over buf.
func newStreamParser(buf []byte) *StreamParser {
	return &StreamParser{buf: buf, word: make([]byte, 0, kMaxWordLength+1)}
}

// GetWord returns the current word.
func (p *StreamParser) GetWord() []byte { return p.word }

// GetPos returns the cursor.
func (p *StreamParser) GetPos() uint32 { return p.pos }

// SetPos sets the cursor.
func (p *StreamParser) SetPos(pos uint32) { p.pos = pos }

// GetObject returns the last full object parsed by ReadNextObject.
func (p *StreamParser) GetObject() objects.Object { return p.lastObj }

func (p *StreamParser) inBounds() bool { return p.pos < uint32(len(p.buf)) }

// ParseNextElement ports CPDF_StreamParser::ParseNextElement
// (cpdf_streamparser.cpp:238).
func (p *StreamParser) ParseNextElement() StreamElementType {
	p.lastObj = nil
	p.word = p.word[:0]
	if !p.inBounds() {
		return ElemEndOfData
	}

	ch := p.buf[p.pos]
	p.pos++
	for {
		for charIsWhitespace(ch) {
			if !p.inBounds() {
				return ElemEndOfData
			}
			ch = p.buf[p.pos]
			p.pos++
		}
		if ch != '%' {
			break
		}
		for {
			if !p.inBounds() {
				return ElemEndOfData
			}
			ch = p.buf[p.pos]
			p.pos++
			if charIsLineEnding(ch) {
				break
			}
		}
	}

	if charIsDelimiter(ch) && ch != '/' {
		p.pos--
		p.lastObj = p.readNextObject(false, false, 0)
		return ElemOther
	}

	bIsNumber := true
	for {
		if len(p.word) < kMaxWordLength {
			p.word = append(p.word, ch)
		}
		if !charIsNumeric(ch) {
			bIsNumber = false
		}
		if !p.inBounds() {
			break
		}
		ch = p.buf[p.pos]
		p.pos++
		if charIsDelimiter(ch) || charIsWhitespace(ch) {
			p.pos--
			break
		}
	}

	if bIsNumber {
		return ElemNumber
	}
	if p.word[0] == '/' {
		return ElemName
	}
	if len(p.word) == 4 {
		if string(p.word) == "true" {
			p.lastObj = objects.NewBoolean(true)
			return ElemOther
		}
		if string(p.word) == "null" {
			p.lastObj = objects.NewNull()
			return ElemOther
		}
	} else if len(p.word) == 5 {
		if string(p.word) == "false" {
			p.lastObj = objects.NewBoolean(false)
			return ElemOther
		}
	}
	return ElemKeyword
}

// readNextObject ports CPDF_StreamParser::ReadNextObject
// (cpdf_streamparser.cpp:326).
func (p *StreamParser) readNextObject(bAllowNestedArray, bInArray bool, level uint32) objects.Object {
	bIsNumber := p.getNextWord()
	if len(p.word) == 0 || level > kMaxNestedParsingLevel {
		return nil
	}

	if bIsNumber {
		return objects.NewNumberFromString(string(p.word))
	}

	first := p.word[0]
	switch first {
	case '/':
		return objects.NewName(nameDecode(p.word[1:]))
	case '(':
		return objects.NewString(p.readString(), false)
	case '<':
		if len(p.word) == 1 {
			return objects.NewString(string(p.readHexString()), true)
		}
		dict := objects.NewDictionary()
		for {
			p.getNextWord()
			if len(p.word) == 2 && p.word[0] == '>' {
				break
			}
			if len(p.word) == 0 || p.word[0] != '/' {
				return nil
			}
			key := nameDecode(p.word[1:])
			v := p.readNextObject(true, bInArray, level+1)
			if v == nil {
				return nil
			}
			dict.SetFor(key, v)
		}
		return dict
	case '[':
		if !bAllowNestedArray && bInArray {
			return nil
		}
		arr := objects.NewArray()
		for {
			v := p.readNextObject(bAllowNestedArray, true, level+1)
			if v != nil {
				arr.Append(v)
				continue
			}
			if len(p.word) == 0 || p.word[0] == ']' {
				break
			}
		}
		return arr
	}

	switch string(p.word) {
	case "false":
		return objects.NewBoolean(false)
	case "true":
		return objects.NewBoolean(true)
	case "null":
		return objects.NewNull()
	}
	return nil
}

// getNextWord ports CPDF_StreamParser::GetNextWord (cpdf_streamparser.cpp:414):
// it handles /name, <<, >> as delimiter words. Returns whether the word is a
// number.
func (p *StreamParser) getNextWord() bool {
	p.word = p.word[:0]
	bIsNumber := true
	if !p.inBounds() {
		return bIsNumber
	}

	ch := p.buf[p.pos]
	p.pos++
	for {
		for charIsWhitespace(ch) {
			if !p.inBounds() {
				return bIsNumber
			}
			ch = p.buf[p.pos]
			p.pos++
		}
		if ch != '%' {
			break
		}
		for {
			if !p.inBounds() {
				return bIsNumber
			}
			ch = p.buf[p.pos]
			p.pos++
			if charIsLineEnding(ch) {
				break
			}
		}
	}

	if charIsDelimiter(ch) {
		bIsNumber = false
		p.word = append(p.word, ch)
		switch ch {
		case '/':
			for {
				if !p.inBounds() {
					return bIsNumber
				}
				ch = p.buf[p.pos]
				p.pos++
				if !charIsOther(ch) && !charIsNumeric(ch) {
					p.pos--
					return bIsNumber
				}
				if len(p.word) < kMaxWordLength {
					p.word = append(p.word, ch)
				}
			}
		case '<':
			if !p.inBounds() {
				return bIsNumber
			}
			ch = p.buf[p.pos]
			p.pos++
			if ch == '<' {
				p.word = append(p.word, ch)
			} else {
				p.pos--
			}
		case '>':
			if !p.inBounds() {
				return bIsNumber
			}
			ch = p.buf[p.pos]
			p.pos++
			if ch == '>' {
				p.word = append(p.word, ch)
			} else {
				p.pos--
			}
		}
		return bIsNumber
	}

	for {
		if len(p.word) < kMaxWordLength {
			p.word = append(p.word, ch)
		}
		if !charIsNumeric(ch) {
			bIsNumber = false
		}
		if !p.inBounds() {
			return bIsNumber
		}
		ch = p.buf[p.pos]
		p.pos++
		if charIsDelimiter(ch) || charIsWhitespace(ch) {
			p.pos--
			break
		}
	}
	return bIsNumber
}

// readString ports CPDF_StreamParser::ReadString (cpdf_streamparser.cpp:505):
// the literal-string 5-state escape machine. Returns at most kMaxStringLength
// bytes.
// clampBytes truncates an over-long string body (PDFium caps literal and hex
// strings at kMaxStringLength).
func clampBytes(buf []byte) []byte {
	if len(buf) > kMaxStringLength {
		return buf[:kMaxStringLength]
	}
	return buf
}

// clampString is clampBytes for literal strings. A package func avoids a
// per-call closure allocation on the hot content-parsing path.
func clampString(buf []byte) string { return string(clampBytes(buf)) }

func (p *StreamParser) readString() string {
	if !p.inBounds() {
		return ""
	}
	var buf []byte
	parlevel := 0
	status := 0
	iEscCode := 0
	ch := p.buf[p.pos]
	p.pos++
	for {
		switch status {
		case 0:
			switch {
			case ch == ')':
				if parlevel == 0 {
					return clampString(buf)
				}
				parlevel--
				buf = append(buf, ')')
			case ch == '(':
				parlevel++
				buf = append(buf, '(')
			case ch == '\\':
				status = 1
			default:
				buf = append(buf, ch)
			}
		case 1:
			switch {
			case isOctalDigit(ch):
				iEscCode = decimalCharToInt(ch)
				status = 2
			case ch == '\r':
				status = 4
			case ch == '\n':
				// do nothing
				status = 0
			case ch == 'n':
				buf = append(buf, '\n')
				status = 0
			case ch == 'r':
				buf = append(buf, '\r')
				status = 0
			case ch == 't':
				buf = append(buf, '\t')
				status = 0
			case ch == 'b':
				buf = append(buf, '\b')
				status = 0
			case ch == 'f':
				buf = append(buf, '\f')
				status = 0
			default:
				buf = append(buf, ch)
				status = 0
			}
		case 2:
			if isOctalDigit(ch) {
				iEscCode = iEscCode*8 + decimalCharToInt(ch)
				status = 3
			} else {
				buf = append(buf, byte(iEscCode))
				status = 0
				continue // reprocess ch without advancing
			}
		case 3:
			if isOctalDigit(ch) {
				iEscCode = iEscCode*8 + decimalCharToInt(ch)
				buf = append(buf, byte(iEscCode))
				status = 0
			} else {
				buf = append(buf, byte(iEscCode))
				status = 0
				continue // reprocess ch
			}
		case 4:
			status = 0
			if ch != '\n' {
				continue // reprocess ch
			}
		}
		if !p.inBounds() {
			return clampString(buf)
		}
		ch = p.buf[p.pos]
		p.pos++
	}
}

// readHexString ports CPDF_StreamParser::ReadHexString
// (cpdf_streamparser.cpp:598).
func (p *StreamParser) readHexString() []byte {
	if !p.inBounds() {
		return nil
	}
	var buf []byte
	bFirst := true
	var code byte
	for p.inBounds() {
		ch := p.buf[p.pos]
		p.pos++
		if ch == '>' {
			break
		}
		if !isHexDigit(ch) {
			continue
		}
		val := hexCharToInt(ch)
		if bFirst {
			code = byte(val * 16)
		} else {
			code += byte(val)
			buf = append(buf, code)
		}
		bFirst = !bFirst
	}
	if !bFirst {
		buf = append(buf, code)
	}
	return clampBytes(buf)
}
