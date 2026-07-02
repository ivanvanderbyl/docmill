// Ported from core/fpdfapi/parser/cpdf_syntax_parser.{h,cpp} @ pdfium 0db284a42.
package syntax

import (
	"bytes"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
)

// kParserMaxRecursionDepth bounds malicious nesting (crbug fuzz fix).
const kParserMaxRecursionDepth = 64

// wordBufCap mirrors sizeof(word_buffer_)-1: the stored-word cap. Over-long
// tokens keep consuming input but stop appending at this length.
const wordBufCap = 256

// parseType mirrors CPDF_SyntaxParser::ParseType.
type parseType int

const (
	parseLoose parseType = iota
	parseStrict
)

// SyntaxParser lexes a whole PDF held in buf. Positions exposed via GetPos/SetPos
// are document-relative; the physical byte index is pos + headerOffset.
type SyntaxParser struct {
	buf          []byte
	headerOffset int
	pos          int

	wordBuf  []byte // shared word buffer (reused across tokens)
	wordSize int
	wordIsNu bool // last token was numeric-class only

	recursionDepth int
}

// New returns a parser over buf with a zero header offset.
func New(buf []byte) *SyntaxParser {
	return NewWithOffset(buf, 0)
}

// NewWithOffset returns a parser over buf whose document origin is headerOffset
// (the byte index of "%PDF").
func NewWithOffset(buf []byte, headerOffset int) *SyntaxParser {
	return &SyntaxParser{buf: buf, headerOffset: headerOffset, wordBuf: make([]byte, wordBufCap+1)}
}

// GetPos returns the document-relative cursor.
func (p *SyntaxParser) GetPos() int { return p.pos }

// SetPos clamps to the physical file length (matching PDFium).
func (p *SyntaxParser) SetPos(pos int) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(p.buf) {
		pos = len(p.buf)
	}
	p.pos = pos
}

// GetDocumentSize returns the document size (physical length minus header).
func (p *SyntaxParser) GetDocumentSize() int { return len(p.buf) - p.headerOffset }

func (p *SyntaxParser) getNextChar() (byte, bool) {
	i := p.pos + p.headerOffset
	if i < 0 || i >= len(p.buf) {
		return 0, false
	}
	ch := p.buf[i]
	p.pos++
	return ch, true
}

func (p *SyntaxParser) getCharAt(pos int) (byte, bool) {
	i := pos + p.headerOffset
	if i < 0 || i >= len(p.buf) {
		return 0, false
	}
	return p.buf[i], true
}

func (p *SyntaxParser) appendWord(ch byte) {
	if p.wordSize < len(p.wordBuf) {
		p.wordBuf[p.wordSize] = ch
	}
	p.wordSize++
}

func (p *SyntaxParser) word() string { return string(p.wordBuf[:p.wordSize]) }

// toNextWord skips whitespace and comments, leaving the cursor on the first real
// byte (it un-reads that byte).
func (p *SyntaxParser) toNextWord() {
	ch, ok := p.getNextChar()
	if !ok {
		return
	}
	for {
		for isWhitespace(ch) {
			ch, ok = p.getNextChar()
			if !ok {
				return
			}
		}
		if ch != '%' {
			break
		}
		for {
			ch, ok = p.getNextChar()
			if !ok {
				return
			}
			if isLineEnding(ch) {
				break
			}
		}
	}
	p.pos--
}

// toNextLine consumes the rest of the current line, including the EOL, with
// CR/LF/CRLF disambiguation.
func (p *SyntaxParser) toNextLine() {
	for {
		ch, ok := p.getNextChar()
		if !ok {
			return
		}
		if ch == '\n' {
			return
		}
		if ch == '\r' {
			nch, ok := p.getNextChar()
			if !(ok && nch == '\n') {
				p.pos--
			}
			return
		}
	}
}

// getNextWordInternal fills wordBuf/wordSize and sets wordIsNu. This is the core
// tokenizer (CPDF_SyntaxParser::GetNextWordInternal).
func (p *SyntaxParser) getNextWordInternal() {
	p.wordSize = 0
	isNumber := true
	p.toNextWord()

	ch, ok := p.getNextChar()
	if !ok {
		p.wordIsNu = true
		return
	}

	if isDelimiter(ch) {
		isNumber = false
		p.appendWord(ch)
		switch ch {
		case '/':
			for {
				ch, ok = p.getNextChar()
				if !ok {
					p.wordIsNu = isNumber
					return
				}
				if !isOther(ch) && !isNumeric(ch) { // whitespace or delimiter ends the name
					p.pos--
					break
				}
				if p.wordSize < wordBufCap {
					p.appendWord(ch)
				}
			}
		case '<':
			ch, ok = p.getNextChar()
			if !ok {
				p.wordIsNu = isNumber
				return
			}
			if ch == '<' {
				p.appendWord('<')
			} else {
				p.pos--
			}
		case '>':
			ch, ok = p.getNextChar()
			if !ok {
				p.wordIsNu = isNumber
				return
			}
			if ch == '>' {
				p.appendWord('>')
			} else {
				p.pos--
			}
		}
		p.wordIsNu = isNumber
		return
	}

	for {
		if p.wordSize < wordBufCap {
			p.appendWord(ch)
		}
		if !isNumeric(ch) {
			isNumber = false
		}
		ch, ok = p.getNextChar()
		if !ok {
			break
		}
		if isDelimiter(ch) || isWhitespace(ch) {
			p.pos--
			break
		}
	}
	p.wordIsNu = isNumber
}

// GetNextWord returns the next token and whether it was numeric.
func (p *SyntaxParser) GetNextWord() (string, bool) {
	p.getNextWordInternal()
	return p.word(), p.wordIsNu
}

// PeekNextWord returns the next token without advancing the cursor.
func (p *SyntaxParser) PeekNextWord() string {
	saved := p.pos
	p.getNextWordInternal()
	w := p.word()
	p.pos = saved
	return w
}

// GetKeyword returns the next token's bytes.
func (p *SyntaxParser) GetKeyword() string {
	w, _ := p.GetNextWord()
	return w
}

type readStatus int

const (
	readNormal readStatus = iota
	readBackslash
	readOctal
	readFinishOctal
	readCarriageReturn
)

// ReadString reads a literal '(...)' string body (the '(' token already
// consumed), handling escapes, nested parens, octal, and line continuations.
func (p *SyntaxParser) ReadString() string {
	ch, ok := p.getNextChar()
	if !ok {
		return ""
	}
	var buf []byte
	status := readNormal
	parlevel := 0
	escCode := 0
	for {
		reprocess := false
		switch status {
		case readNormal:
			if ch == ')' {
				if parlevel == 0 {
					return string(buf)
				}
				parlevel--
			} else if ch == '(' {
				parlevel++
			}
			if ch == '\\' {
				status = readBackslash
			} else {
				buf = append(buf, ch)
			}
		case readBackslash:
			if ch >= '0' && ch <= '7' {
				escCode = int(ch - '0')
				status = readOctal
			} else if ch == '\r' {
				status = readCarriageReturn
			} else {
				switch ch {
				case 'n':
					buf = append(buf, '\n')
				case 'r':
					buf = append(buf, '\r')
				case 't':
					buf = append(buf, '\t')
				case 'b':
					buf = append(buf, '\b')
				case 'f':
					buf = append(buf, '\f')
				default:
					if ch != '\n' {
						buf = append(buf, ch)
					}
				}
				status = readNormal
			}
		case readOctal:
			if ch >= '0' && ch <= '7' {
				escCode = escCode*8 + int(ch-'0')
				status = readFinishOctal
			} else {
				buf = append(buf, byte(escCode))
				status = readNormal
				reprocess = true
			}
		case readFinishOctal:
			status = readNormal
			if ch >= '0' && ch <= '7' {
				escCode = escCode*8 + int(ch-'0')
				buf = append(buf, byte(escCode))
			} else {
				buf = append(buf, byte(escCode))
				reprocess = true
			}
		case readCarriageReturn:
			status = readNormal
			if ch != '\n' {
				reprocess = true
			}
		}
		if reprocess {
			continue
		}
		ch, ok = p.getNextChar()
		if !ok {
			break
		}
	}
	return string(buf)
}

// ReadHexString reads a '<...>' hex string body (the '<' token already
// consumed), tolerating whitespace/junk and an odd final nibble.
func (p *SyntaxParser) ReadHexString() []byte {
	ch, ok := p.getNextChar()
	if !ok {
		return []byte{}
	}
	buf := []byte{}
	bFirst := true
	var code byte
	for {
		if ch == '>' {
			break
		}
		if isHexDigit(ch) {
			val := hexCharToInt(ch)
			if bFirst {
				code = byte(val * 16)
			} else {
				code += byte(val)
				buf = append(buf, code)
			}
			bFirst = !bFirst
		}
		ch, ok = p.getNextChar()
		if !ok {
			break
		}
	}
	if !bFirst {
		buf = append(buf, code)
	}
	return buf
}

// GetObjectBody parses a direct object, resolving references through holder.
func (p *SyntaxParser) GetObjectBody(holder objects.IndirectObjectHolder) objects.Object {
	return p.getObjectBodyInternal(holder, parseLoose)
}

func (p *SyntaxParser) getObjectBodyInternal(holder objects.IndirectObjectHolder, pt parseType) objects.Object {
	p.recursionDepth++
	defer func() { p.recursionDepth-- }()
	if p.recursionDepth > kParserMaxRecursionDepth {
		return nil
	}

	savedObjPos := p.pos
	p.getNextWordInternal()
	word := p.word()
	if len(word) == 0 {
		return nil
	}

	if p.wordIsNu { // "N", "N G", or "N G R"
		savedPos := p.pos
		p.getNextWordInternal()
		if !p.wordIsNu {
			p.pos = savedPos
			return objects.NewNumberFromString(word)
		}
		p.getNextWordInternal()
		if p.word() != "R" {
			p.pos = savedPos
			return objects.NewNumberFromString(word)
		}
		refnum := atoui(word)
		if refnum == objects.InvalidObjNum {
			return nil
		}
		return objects.NewReference(holder, refnum)
	}

	switch word {
	case "true":
		return objects.NewBoolean(true)
	case "false":
		return objects.NewBoolean(false)
	case "null":
		return objects.NewNull()
	case "(":
		return objects.NewString(p.ReadString(), false)
	case "<":
		return objects.NewString(string(p.ReadHexString()), true)
	case "[":
		arr := objects.NewArray()
		for {
			obj := p.getObjectBodyInternal(holder, parseLoose)
			if obj == nil {
				break
			}
			if obj.Type() != objects.TypeStream {
				arr.Append(obj)
			}
		}
		if pt == parseLoose || (p.wordSize == 1 && p.wordBuf[0] == ']') {
			return arr
		}
		return nil
	case "<<":
		return p.parseDictionary(holder, pt)
	}

	if word[0] == '/' {
		name := nameDecode(p.wordBuf[1:p.wordSize])
		return objects.NewName(name)
	}
	if word == ">>" {
		p.pos = savedObjPos
	}
	return nil
}

func (p *SyntaxParser) parseDictionary(holder objects.IndirectObjectHolder, pt parseType) objects.Object {
	dict := objects.NewDictionary()
	for {
		p.getNextWordInternal()
		inner := p.word()
		if len(inner) == 0 {
			return nil
		}
		savedPos := p.pos - len(inner)
		if inner == ">>" {
			break
		}
		if inner == "endobj" {
			p.pos = savedPos // recover: leave endobj for the caller
			break
		}
		if inner[0] != '/' {
			continue
		}
		key := nameDecode(p.wordBuf[:p.wordSize])
		if len(key) == 0 && pt == parseLoose {
			continue
		}
		obj := p.getObjectBodyInternal(holder, parseLoose)
		if obj == nil {
			if pt == parseLoose {
				continue
			}
			p.toNextLine()
			return nil
		}
		if len(key) > 1 && obj.Type() != objects.TypeStream {
			dict.SetFor(key[1:], obj)
		}
	}

	// Peek for a 'stream' keyword to upgrade the dict to a stream.
	savedPos := p.pos
	p.getNextWordInternal()
	if p.word() != "stream" {
		p.pos = savedPos
		return dict
	}
	return p.readStream(dict)
}

// readStream extracts a stream body following the 'stream' keyword.
func (p *SyntaxParser) readStream(dict *objects.Dictionary) objects.Object {
	length := -1
	if num := dict.GetNumberFor("Length"); num != nil {
		length = num.GetInteger()
	}

	p.toNextLine()
	streamStartPos := p.pos

	if length > 0 {
		end := streamStartPos + length
		if end < 0 || p.headerOffset+end > len(p.buf) {
			length = -1
		}
	}

	if length >= 0 {
		// Advance past the declared data and verify 'endstream' follows.
		p.SetPos(streamStartPos + length)
		p.pos += p.readEOLMarkers(p.pos)
		p.getNextWordInternal()
		if !p.wordMatches("endstream") {
			length = -1
			p.SetPos(streamStartPos)
		}
	}

	if length < 0 {
		streamEndPos := p.findStreamEndPos()
		if streamEndPos < 0 {
			return nil
		}
		length = streamEndPos - streamStartPos
		if length < 0 {
			return nil
		}
		p.SetPos(streamStartPos + length)
	}

	var data []byte
	if length > 0 {
		physStart := p.headerOffset + streamStartPos
		physEnd := physStart + length
		if physStart >= 0 && physEnd <= len(p.buf) && physStart <= physEnd {
			data = append([]byte(nil), p.buf[physStart:physEnd]...)
		}
	}
	stream := objects.NewStreamFromData(data, dict)

	// Skip a trailing 'endobj' if present, leaving it for the caller otherwise.
	endStreamOffset := p.pos
	p.getNextWordInternal()
	// Allow whitespace after endstream and before a newline.
	for {
		ch, ok := p.getNextChar()
		if !ok {
			break
		}
		if isLineEnding(ch) || !isWhitespace(ch) {
			break
		}
	}
	p.SetPos(p.pos - 1)
	numMarkers := p.readEOLMarkers(p.pos)
	if p.wordSize == 6 && numMarkers != 0 && string(p.wordBuf[:6]) == "endobj" {
		p.SetPos(endStreamOffset)
	}
	return stream
}

func (p *SyntaxParser) wordMatches(s string) bool {
	if p.wordSize < len(s) {
		return false
	}
	return string(p.wordBuf[:len(s)]) == s
}

// readEOLMarkers returns 2 for CRLF, 1 for a lone CR or LF, else 0.
func (p *SyntaxParser) readEOLMarkers(pos int) int {
	b1, _ := p.getCharAt(pos)
	b2, _ := p.getCharAt(pos + 1)
	if b1 == '\r' && b2 == '\n' {
		return 2
	}
	if b1 == '\r' || b1 == '\n' {
		return 1
	}
	return 0
}

// findStreamEndPos finds the document position where the stream data ends,
// scanning for the earlier of 'endstream'/'endobj' as whole words. This is the
// fallback for a stream whose /Length is absent or indirect (unresolved at parse
// time); the cursor at entry is the stream-data start.
func (p *SyntaxParser) findStreamEndPos() int {
	// findWordPos leaves the cursor just past a successful match, so each search
	// must start from the same stream-data origin and the lower-bound guard must
	// compare against that origin rather than the mutated cursor.
	start := p.pos
	es := p.findWordPos("endstream")
	p.SetPos(start)
	eo := p.findWordPos("endobj")
	p.SetPos(start)
	if es < 0 && eo < 0 {
		return -1
	}
	if es < 0 {
		es = eo
	} else if eo < 0 {
		eo = es
	} else if es > eo {
		es = eo
	}
	if p.readEOLMarkers(es-2) == 2 {
		es -= 2
	} else if p.readEOLMarkers(es-1) == 1 {
		es -= 1
	}
	if es < start {
		return -1
	}
	return es
}

// findWordPos returns the document position of the next whole-word match of
// word at or after the cursor, or -1, restoring the cursor on failure.
func (p *SyntaxParser) findWordPos(word string) int {
	saved := p.pos
	for {
		off := p.findTag(word)
		if off < 0 {
			break
		}
		start := p.pos - len(word)
		if p.isWholeWord(start, len(p.buf), word, true) {
			return start
		}
	}
	p.pos = saved
	return -1
}

// findTag advances the cursor to just past the next occurrence of tag and
// returns its offset relative to where the search began, or -1 (cursor at EOF).
func (p *SyntaxParser) findTag(tag string) int {
	start := p.pos
	i := bytes.Index(p.buf[p.pos+p.headerOffset:], []byte(tag))
	if i < 0 {
		p.pos = p.GetDocumentSize()
		return -1
	}
	p.pos = p.pos + i + len(tag)
	return p.pos - len(tag) - start
}

// isWholeWord reports whether tag at document position startpos is bounded by
// word separators. checkKeyword treats adjacent delimiters as also breaking.
func (p *SyntaxParser) isWholeWord(startpos, limit int, tag string, checkKeyword bool) bool {
	taglen := len(tag)
	if taglen == 0 {
		return false
	}
	checkLeft := !isDelimiter(tag[0]) && !isWhitespace(tag[0])
	checkRight := !isDelimiter(tag[taglen-1]) && !isWhitespace(tag[taglen-1])
	if checkRight && startpos+taglen <= limit {
		if ch, ok := p.getCharAt(startpos + taglen); ok {
			if isNumeric(ch) || isOther(ch) || (checkKeyword && isDelimiter(ch)) {
				return false
			}
		}
	}
	if checkLeft && startpos > 0 {
		if ch, ok := p.getCharAt(startpos - 1); ok {
			if isNumeric(ch) || isOther(ch) || (checkKeyword && isDelimiter(ch)) {
				return false
			}
		}
	}
	return true
}

// GetIndirectObject parses a full "N G obj <body>" indirect object header,
// restoring the cursor and returning nil on a malformed header.
func (p *SyntaxParser) GetIndirectObject(holder objects.IndirectObjectHolder) objects.Object {
	return p.getIndirectObjectWithType(holder, parseLoose)
}
