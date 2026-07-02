// Ported from core/fpdfapi/font/cpdf_tounicodemap.{h,cpp} @ pdfium 0db284a42.
//
// The /ToUnicode CMap parser: bfchar/bfrange -> a charcode -> WideString map.
// WideString is modelled as []uint16 (UTF-16 code units) so surrogate pairs and
// ligatures survive faithfully; conversion to a Go string happens only at the
// public API boundary. The std::map<uint32,std::set<uint32>> multimap is a Go
// map keyed by charcode whose value is a sorted, deduplicated uint32 set so that
// Lookup uses the smallest element and ReverseLookup iterates keys in ascending
// order (both ORDER-SENSITIVE — preserved here).
package font

import (
	"slices"
	"sort"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/syntax"
)

const (
	kCidLimit         = 0xffff
	kOutOfSpecBFLimit = 160000
)

// sortedUint32Set is std::set<uint32> (ascending, deduplicated).
type sortedUint32Set struct {
	vals []uint32
}

func (s *sortedUint32Set) insert(v uint32) {
	i := sort.Search(len(s.vals), func(i int) bool { return s.vals[i] >= v })
	if i < len(s.vals) && s.vals[i] == v {
		return // already present (dedup)
	}
	s.vals = append(s.vals, 0)
	copy(s.vals[i+1:], s.vals[i:])
	s.vals[i] = v
}

func (s *sortedUint32Set) begin() uint32 { return s.vals[0] }
func (s *sortedUint32Set) size() int     { return len(s.vals) }

func (s *sortedUint32Set) contains(v uint32) bool {
	i := sort.Search(len(s.vals), func(i int) bool { return s.vals[i] >= v })
	return i < len(s.vals) && s.vals[i] == v
}

// toUnicodeMap is CPDF_ToUnicodeMap.
type toUnicodeMap struct {
	multimap     map[uint32]*sortedUint32Set
	baseMap      *cid2UnicodeMap // predefined CJK; nil for the corpus
	multiCharVec [][]uint16
}

// newToUnicodeMap loads a ToUnicode CMap from a stream (CPDF_ToUnicodeMap ctor).
func newToUnicodeMap(stream *objects.Stream) *toUnicodeMap {
	m := &toUnicodeMap{multimap: map[uint32]*sortedUint32Set{}}
	m.load(stream)
	return m
}

// Lookup ports CPDF_ToUnicodeMap::Lookup (returns a []uint16, possibly empty).
func (m *toUnicodeMap) Lookup(charcode uint32) []uint16 {
	set, ok := m.multimap[charcode]
	if !ok {
		if m.baseMap == nil {
			return nil
		}
		u := m.baseMap.UnicodeFromCID(uint16(charcode))
		if u == 0 {
			return []uint16{0}
		}
		return []uint16{u}
	}
	value := set.begin()
	unicode := uint16(value & 0xffff)
	if unicode != 0xffff {
		return []uint16{unicode}
	}
	index := value >> 16
	if int(index) < len(m.multiCharVec) {
		return m.multiCharVec[index]
	}
	return nil
}

// ReverseLookup ports CPDF_ToUnicodeMap::ReverseLookup (ascending key order).
func (m *toUnicodeMap) ReverseLookup(unicode rune) uint32 {
	keys := make([]uint32, 0, len(m.multimap))
	for k := range m.multimap {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if m.multimap[k].contains(uint32(unicode)) {
			return k
		}
	}
	return 0
}

// getUnicodeCountByCharcodeForTesting ports the test helper.
func (m *toUnicodeMap) getUnicodeCountByCharcodeForTesting(cc uint32) int {
	if set, ok := m.multimap[cc]; ok {
		return set.size()
	}
	return 0
}

// stringToCode ports CPDF_ToUnicodeMap::StringToCode. ok=false is nullopt.
func stringToCode(str string) (uint32, bool) {
	n := len(str)
	if n <= 2 || str[0] != '<' || str[n-1] != '>' {
		return 0, false
	}
	var code uint32
	for i := 1; i < n-1; i++ {
		c := str[i]
		if pdfCharIsWhitespace(c) {
			continue
		}
		if !isHexDigit(c) {
			return 0, false
		}
		// code = code*16 + hex; saturating-uint32 overflow -> nullopt.
		const maxU = ^uint32(0)
		d := hexCharToInt(c)
		if code > (maxU-d)/16 {
			return 0, false
		}
		code = code*16 + d
	}
	return code, true
}

// stringToWideString ports CPDF_ToUnicodeMap::StringToWideString. On a non-hex
// byte it BREAKS keeping the partial result; trailing <4 nibbles are dropped.
func stringToWideString(str string) []uint16 {
	n := len(str)
	if n <= 2 || str[0] != '<' || str[n-1] != '>' {
		return nil
	}
	var result []uint16
	bytePos := 0
	var ch uint16
	for i := 1; i < n-1; i++ {
		c := str[i]
		if pdfCharIsWhitespace(c) {
			continue
		}
		if !isHexDigit(c) {
			break
		}
		ch = ch*16 + uint16(hexCharToInt(c))
		bytePos++
		if bytePos == 4 {
			result = append(result, ch)
			bytePos = 0
			ch = 0
		}
	}
	return result
}

// stringDataAdd ports the namespace StringDataAdd: big-endian +1 across UTF-16
// code units, growing a high unit on full overflow.
func stringDataAdd(str []uint16) []uint16 {
	var ret []uint16
	var value uint16 = 1
	for i := len(str); i > 0; i-- {
		ch := str[i-1] + value
		if ch < str[i-1] { // wraparound (carry)
			ret = append([]uint16{0}, ret...)
		} else {
			ret = append([]uint16{ch}, ret...)
			value = 0
		}
	}
	if value != 0 {
		ret = append([]uint16{value}, ret...)
	}
	return ret
}

// load ports CPDF_ToUnicodeMap::Load.
func (m *toUnicodeMap) load(stream *objects.Stream) {
	cidSet := cidSetUnknown
	acc := objects.NewStreamAcc(stream)
	acc.LoadAllDataFiltered()
	parser := syntax.NewSimpleParser(acc.GetSpan())
	previousWord := ""
	for {
		word := parser.GetWord()
		if word == "" {
			break
		}
		switch {
		case word == "beginbfchar":
			word = m.handleBeginBFChar(parser, previousWord)
		case word == "beginbfrange":
			word = m.handleBeginBFRange(parser, previousWord)
		case word == "/Adobe-Korea1-UCS2":
			cidSet = cidSetKorea1
		case word == "/Adobe-Japan1-UCS2":
			cidSet = cidSetJapan1
		case word == "/Adobe-CNS1-UCS2":
			cidSet = cidSetCNS1
		case word == "/Adobe-GB1-UCS2":
			cidSet = cidSetGB1
		}
		previousWord = word
	}
	if cidSet != cidSetUnknown {
		// TODO(plan 009): predefined CJK CID2Unicode tables. globals returns nil
		// for the corpus, so baseMap stays nil.
		m.baseMap = getCID2UnicodeMap(cidSet)
	}
}

type bfCharEntry struct {
	code uint32
	word string
}

// handleBeginBFChar ports CPDF_ToUnicodeMap::HandleBeginBFChar.
func (m *toUnicodeMap) handleBeginBFChar(parser *syntax.SimpleParser, previousWord string) string {
	var codeWords []bfCharEntry

	rawCount := stringToInt(previousWord)
	isValid := rawCount >= 0 && rawCount <= kOutOfSpecBFLimit
	expected := 0
	if isValid {
		expected = rawCount
	}

	var word string
	for {
		word = parser.GetWord()
		if word == "" || word == "endbfchar" {
			break
		}
		if !isValid {
			continue
		}
		code, ok := stringToCode(word)
		if !ok || code > kCidLimit {
			isValid = false
			continue
		}
		word = parser.GetWord()
		codeWords = append(codeWords, bfCharEntry{code: code, word: word})
		if len(codeWords) > expected {
			isValid = false
		}
	}

	if isValid && len(codeWords) == expected {
		for _, e := range codeWords {
			m.setCode(e.code, stringToWideString(e.word))
		}
	}
	return word
}

// range kinds for HandleBeginBFRange.
type rangeKind int

const (
	rangeCodeWord rangeKind = iota
	rangeSingleDest
	rangeMultiDest
)

type bfRange struct {
	kind rangeKind
	// CodeWordRange
	lowCode   uint32
	codeWords []string
	// MultimapSingleDestRange
	highCode   uint32
	startValue uint32
	// MultimapMultiDestRange
	retcodes [][]uint16
}

// handleBeginBFRange ports CPDF_ToUnicodeMap::HandleBeginBFRange.
func (m *toUnicodeMap) handleBeginBFRange(parser *syntax.SimpleParser, previousWord string) string {
	var ranges []bfRange

	rawCount := stringToInt(previousWord)
	isValid := rawCount >= 0 && rawCount <= kOutOfSpecBFLimit
	expected := 0
	if isValid {
		expected = rawCount
	}

	var word string
	for {
		word = parser.GetWord()
		if word == "" || word == "endbfrange" {
			break
		}
		if !isValid {
			continue
		}
		lowcode, ok := stringToCode(word)
		if !ok {
			isValid = false
			continue
		}
		word = parser.GetWord()
		highcodeOpt, ok := stringToCode(word)
		if !ok {
			isValid = false
			continue
		}
		low := lowcode
		high := (low & 0xffffff00) | (highcodeOpt & 0xff)
		if low > kCidLimit || high > kCidLimit || low > high {
			isValid = false
			continue
		}

		word = parser.GetWord()
		start := word
		if start == "[" {
			r := bfRange{kind: rangeCodeWord, lowCode: low}
			for code := low; code <= high; code++ {
				word = parser.GetWord()
				r.codeWords = append(r.codeWords, word)
			}
			ranges = append(ranges, r)
			if len(ranges) > expected {
				isValid = false
				continue
			}
			word = parser.GetWord()
			if word != "]" {
				isValid = false
			}
			continue
		}

		destcode := stringToWideString(start)
		if len(destcode) == 1 {
			v, ok := stringToCode(start)
			if !ok {
				isValid = false
				continue
			}
			ranges = append(ranges, bfRange{kind: rangeSingleDest, lowCode: low, highCode: high, startValue: v})
		} else {
			r := bfRange{kind: rangeMultiDest, lowCode: low}
			r.retcodes = append(r.retcodes, destcode)
			for code := low + 1; code <= high; code++ {
				retcode := stringDataAdd(r.retcodes[len(r.retcodes)-1])
				r.retcodes = append(r.retcodes, retcode)
			}
			ranges = append(ranges, r)
		}
		if len(ranges) > expected {
			isValid = false
		}
	}

	if isValid && len(ranges) == expected {
		for _, entry := range ranges {
			switch entry.kind {
			case rangeCodeWord:
				code := entry.lowCode
				for _, cw := range entry.codeWords {
					m.setCode(code, stringToWideString(cw))
					code++
				}
			case rangeSingleDest:
				value := entry.startValue
				for code := entry.lowCode; code <= entry.highCode; code++ {
					m.insertIntoMultimap(code, value)
					value++
				}
			case rangeMultiDest:
				code := entry.lowCode
				for _, rc := range entry.retcodes {
					m.insertIntoMultimap(code, m.getMultiCharIndexIndicator())
					m.multiCharVec = append(m.multiCharVec, rc)
					code++
				}
			}
		}
	}
	return word
}

// getMultiCharIndexIndicator ports GetMultiCharIndexIndicator (saturating, 0 on
// overflow).
func (m *toUnicodeMap) getMultiCharIndexIndicator() uint32 {
	const maxU = ^uint32(0)
	n := uint32(len(m.multiCharVec))
	if n > maxU/0x10000 {
		return 0
	}
	uni := n * 0x10000
	if uni > maxU-0xffff {
		return 0
	}
	return uni + 0xffff
}

// setCode ports CPDF_ToUnicodeMap::SetCode.
func (m *toUnicodeMap) setCode(srccode uint32, dest []uint16) {
	if len(dest) == 0 {
		return
	}
	if len(dest) == 1 {
		m.insertIntoMultimap(srccode, uint32(dest[0]))
	} else {
		m.insertIntoMultimap(srccode, m.getMultiCharIndexIndicator())
		m.multiCharVec = append(m.multiCharVec, dest)
	}
}

// insertIntoMultimap ports CPDF_ToUnicodeMap::InsertIntoMultimap.
func (m *toUnicodeMap) insertIntoMultimap(code uint32, destcode uint32) {
	set, ok := m.multimap[code]
	if !ok {
		set = &sortedUint32Set{}
		set.insert(destcode)
		m.multimap[code] = set
		return
	}
	set.insert(destcode)
}

// stringToInt ports the StringToInt used for the bfchar/bfrange count. It parses
// an optional sign and decimal digits, stopping at the first non-digit; "" -> 0.
// (FXSYS_atoi semantics: signed int32, overflow is not specially handled — the
// corpus counts are small.)
func stringToInt(s string) int {
	i := 0
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	n := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if neg {
		return -n
	}
	return n
}
