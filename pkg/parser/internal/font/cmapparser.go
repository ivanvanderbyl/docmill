// Ported from core/fpdfapi/font/cpdf_cmapparser.{h,cpp} @ pdfium 0db284a42.
//
// A small state machine over the words of an embedded CMap stream that fills the
// owning CMap's codespace/cid tables. The C++ destructor flushes the accumulated
// additional mappings and leading ranges; Go models that with finish().
package font

// cmapParserStatus mirrors the parser status enum.
type cmapParserStatus int

const (
	kStart cmapParserStatus = iota
	kProcessingCidChar
	kProcessingCidRange
	kProcessingRegistry
	kProcessingOrdering
	kProcessingSupplement
	kProcessingWMode
	kProcessingCodeSpaceRange
)

// cmapParser is CPDF_CMapParser.
type cmapParser struct {
	cmap          *cmap
	status        cmapParserStatus
	codeSeq       int
	ranges        []codeRange
	pendingRanges []codeRange
	additional    []cidRange
	lastWord      string
	codePoints    [4]uint32
}

func newCMapParser(c *cmap) *cmapParser {
	return &cmapParser{cmap: c, status: kStart}
}

// finish models the C++ destructor: flush accumulated mappings/ranges.
func (p *cmapParser) finish() {
	p.cmap.SetAdditionalMappings(p.additional)
	p.cmap.SetMixedFourByteLeadingRanges(p.ranges)
}

// parseWord ports CPDF_CMapParser::ParseWord.
func (p *cmapParser) parseWord(word string) {
	switch {
	case word == "begincidchar":
		p.status = kProcessingCidChar
		p.codeSeq = 0
	case word == "begincidrange":
		p.status = kProcessingCidRange
		p.codeSeq = 0
	case word == "endcidrange" || word == "endcidchar":
		p.status = kStart
	case word == "/WMode":
		p.status = kProcessingWMode
	case word == "/Registry":
		p.status = kProcessingRegistry
	case word == "/Ordering":
		p.status = kProcessingOrdering
	case word == "/Supplement":
		p.status = kProcessingSupplement
	case word == "begincodespacerange":
		p.status = kProcessingCodeSpaceRange
		p.codeSeq = 0
	case word == "usecmap":
		// usecmap is not followed (predefined merge unsupported).
	case p.status == kProcessingCidChar:
		p.handleCid(word)
	case p.status == kProcessingCidRange:
		p.handleCid(word)
	case p.status == kProcessingRegistry:
		p.status = kStart
	case p.status == kProcessingOrdering:
		p.cmap.SetCharset(charsetFromOrdering(cmapGetString(word)))
		p.status = kStart
	case p.status == kProcessingSupplement:
		p.status = kStart
	case p.status == kProcessingWMode:
		p.cmap.SetVertical(getCMapCode(word) != 0)
		p.status = kStart
	case p.status == kProcessingCodeSpaceRange:
		p.handleCodeSpaceRange(word)
	}
	p.lastWord = word
}

// handleCid ports CPDF_CMapParser::HandleCid.
func (p *cmapParser) handleCid(word string) {
	bChar := p.status == kProcessingCidChar
	p.codePoints[p.codeSeq] = getCMapCode(word)
	p.codeSeq++
	required := 3
	if bChar {
		required = 2
	}
	if p.codeSeq < required {
		return
	}
	start := p.codePoints[0]
	var end uint32
	var startCID uint16
	if bChar {
		end = start
		startCID = uint16(p.codePoints[1])
	} else {
		end = p.codePoints[1]
		startCID = uint16(p.codePoints[2])
	}
	if end < kDirectMapTableSize {
		p.cmap.SetDirectCharcodeToCIDTableRange(start, end, startCID)
	} else {
		p.additional = append(p.additional, cidRange{startCode: start, endCode: end, startCID: startCID})
	}
	p.codeSeq = 0
}

// handleCodeSpaceRange ports CPDF_CMapParser::HandleCodeSpaceRange.
func (p *cmapParser) handleCodeSpaceRange(word string) {
	if word != "endcodespacerange" {
		if word == "" || word[0] != '<' {
			return
		}
		if p.codeSeq%2 == 1 {
			if r, ok := getCodeRange(p.lastWord, word); ok {
				p.pendingRanges = append(p.pendingRanges, r)
			}
		}
		p.codeSeq++
		return
	}
	nSegs := len(p.ranges) + len(p.pendingRanges)
	if nSegs == 1 {
		var first codeRange
		if len(p.ranges) > 0 {
			first = p.ranges[0]
		} else {
			first = p.pendingRanges[0]
		}
		if first.charSize == 2 {
			p.cmap.SetCodingScheme(twoBytes)
		} else {
			p.cmap.SetCodingScheme(oneByte)
		}
	} else if nSegs > 1 {
		p.cmap.SetCodingScheme(mixedFourBytes)
		p.ranges = append(p.ranges, p.pendingRanges...)
		p.pendingRanges = nil
	}
	p.status = kStart
}

// cmapGetString ports CMap_GetString: drop the first 2 bytes (or "" when len<=2).
func cmapGetString(word string) string {
	if len(word) <= 2 {
		return ""
	}
	return word[2:]
}

// getCMapCode ports CPDF_CMapParser::GetCode. With a '<' prefix it reads hex
// until the first non-hex; otherwise decimal until the first non-decimal; an
// uint32 overflow yields 0.
func getCMapCode(word string) uint32 {
	if word == "" {
		return 0
	}
	const maxU = ^uint32(0)
	var num uint32
	if word[0] == '<' {
		for i := 1; i < len(word) && isHexDigit(word[i]); i++ {
			d := hexCharToInt(word[i])
			if num > (maxU-d)/16 {
				return 0
			}
			num = num*16 + d
		}
		return num
	}
	for i := 0; i < len(word) && isDecimalDigit(word[i]); i++ {
		d := uint32(word[i] - '0')
		if num > (maxU-d)/10 {
			return 0
		}
		num = num*10 + d
	}
	return num
}

// getCodeRange ports CPDF_CMapParser::GetCodeRange.
func getCodeRange(first, second string) (codeRange, bool) {
	if first == "" || first[0] != '<' {
		return codeRange{}, false
	}
	i := 1
	for ; i < len(first); i++ {
		if first[i] == '>' {
			break
		}
	}
	charSize := (i - 1) / 2
	if charSize > 4 {
		return codeRange{}, false
	}
	var r codeRange
	r.charSize = charSize
	for j := 0; j < r.charSize; j++ {
		digit1 := charAtOr(first, j*2+1, 0)
		digit2 := charAtOr(first, j*2+2, 0)
		r.lower[j] = uint8(hexCharToInt(digit1)*16 + hexCharToInt(digit2))
	}
	size := len(second)
	for j := 0; j < r.charSize; j++ {
		i1 := j*2 + 1
		i2 := i1 + 1
		var digit1, digit2 byte = '0', '0'
		if i1 < size {
			digit1 = second[i1]
		}
		if i2 < size {
			digit2 = second[i2]
		}
		r.upper[j] = uint8(hexCharToInt(digit1)*16 + hexCharToInt(digit2))
	}
	return r, true
}

// charAtOr returns s[i] or def for an out-of-range index. (The lower-byte loop
// in GetCodeRange reads first[2i+1]/first[2i+2]; PDFium reads past the '>' into
// whatever follows in the ByteStringView; for the tested inputs the indices are
// always in range, and a non-hex byte yields 0 via hexCharToInt regardless.)
func charAtOr(s string, i int, def byte) byte {
	if i < 0 || i >= len(s) {
		return def
	}
	return s[i]
}
