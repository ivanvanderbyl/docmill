// Ported from core/fpdfapi/font/cpdf_cmap.{h,cpp} @ pdfium 0db284a42.
//
// CMap maps content-stream byte sequences to char codes and char codes to CIDs.
// The corpus uses the Identity-H/Identity-V fast path (coding=kCID, 2-byte
// tokenisation, CID==charcode); embedded stream CMaps go through CMapParser.
// The predefined CJK CMap data tables (embed_map_) are NOT compiled in
// (TODO plan 009), so predefined non-Identity names load as loaded=false.
package font

import (
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/syntax"
)

// codingScheme mirrors CPDF_CMap::CodingScheme.
type codingScheme uint8

const (
	oneByte        codingScheme = iota // OneByte
	twoBytes                           // TwoBytes
	mixedTwoBytes                      // MixedTwoBytes
	mixedFourBytes                     // MixedFourBytes
)

const kDirectMapTableSize = 65536

// codeRange is CPDF_CMap::CodeRange.
type codeRange struct {
	charSize int
	lower    [4]uint8
	upper    [4]uint8
}

// cidRange is CPDF_CMap::CIDRange.
type cidRange struct {
	startCode uint32
	endCode   uint32
	startCID  uint16
}

// byteRange is the predefined-table leading-byte segment.
type byteRange struct {
	first uint8
	last  uint8
}

// predefinedCMap is one row of kPredefinedCMaps.
type predefinedCMap struct {
	name    string
	charset cidSet
	coding  cidCoding
	scheme  codingScheme
	leading [2]byteRange
}

// cmap is CPDF_CMap.
type cmap struct {
	loaded   bool
	vertical bool

	charset cidSet
	scheme  codingScheme
	coding  cidCoding

	mixedTwoByteLeadingBytes   []bool // len 256 when used
	mixedFourByteLeadingRanges []codeRange
	directCharcodeToCIDTable   []uint16 // len 0 or 65536
	additionalCharcodeToCID    []cidRange
	// embedMap is always nil for the corpus (no compiled CJK tables).
}

// newCMapPredefined ports CPDF_CMap(ByteStringView bsPredefinedName).
func newCMapPredefined(name string) *cmap {
	c := &cmap{scheme: twoBytes, charset: cidSetUnknown, coding: cidCodingUnknown}
	if len(name) > 0 && name[len(name)-1] == 'V' {
		c.vertical = true
	}
	if name == "Identity-H" || name == "Identity-V" {
		c.coding = cidCodingCID
		c.loaded = true
		return c
	}
	m := getPredefinedCMapEntry(name)
	if m == nil {
		return c // unloaded
	}
	c.charset = m.charset
	c.coding = m.coding
	c.scheme = m.scheme
	if c.scheme == mixedTwoBytes {
		c.mixedTwoByteLeadingBytes = loadLeadingSegments(m)
	}
	// embed_map_ = FindEmbeddedCMap(...) is nil for the corpus -> stays unloaded.
	// TODO(plan 009): predefined CMap data tables; until then this CMap is
	// loaded=false (CIDFromCharCode falls through to the empty-table identity).
	return c
}

// newCMapEmbedded ports CPDF_CMap(span<const uint8_t> spEmbeddedData).
func newCMapEmbedded(data []byte) *cmap {
	c := &cmap{
		scheme:                   twoBytes,
		charset:                  cidSetUnknown,
		coding:                   cidCodingUnknown,
		directCharcodeToCIDTable: make([]uint16, kDirectMapTableSize),
	}
	parser := newCMapParser(c)
	sp := syntax.NewSimpleParser(data)
	for {
		word := sp.GetWord()
		if word == "" {
			break
		}
		parser.parseWord(word)
	}
	parser.finish() // models the C++ destructor flush.
	return c
}

// IsVertWriting ports CPDF_CMap::IsVertWriting.
func (c *cmap) IsVertWriting() bool { return c.vertical }

// GetCoding / GetCharset accessors.
func (c *cmap) GetCoding() cidCoding { return c.coding }
func (c *cmap) GetCharset() cidSet   { return c.charset }
func (c *cmap) IsLoaded() bool       { return c.loaded }

// CIDFromCharCode ports CPDF_CMap::CIDFromCharCode.
func (c *cmap) CIDFromCharCode(charcode uint32) uint16 {
	if c.coding == cidCodingCID {
		return uint16(charcode)
	}
	// embedMap is nil for the corpus.
	if len(c.directCharcodeToCIDTable) == 0 {
		return uint16(charcode)
	}
	if int(charcode) < len(c.directCharcodeToCIDTable) {
		return c.directCharcodeToCIDTable[charcode]
	}
	// lower_bound by end_code_ (first element with end_code_ >= charcode).
	add := c.additionalCharcodeToCID
	i := sort.Search(len(add), func(i int) bool { return add[i].endCode >= charcode })
	if i == len(add) || add[i].startCode > charcode {
		return 0
	}
	return add[i].startCID + uint16(charcode-add[i].startCode)
}

// GetNextChar ports CPDF_CMap::GetNextChar. It returns the decoded char code and
// the new offset.
func (c *cmap) GetNextChar(str []byte, offset int) (uint32, int) {
	next := func() uint8 {
		if offset < len(str) {
			b := str[offset]
			offset++
			return b
		}
		offset++ // PDFium increments past end too (offset only read as a cursor)
		return 0
	}
	switch c.scheme {
	case oneByte:
		if offset < len(str) {
			b := str[offset]
			offset++
			return uint32(b), offset
		}
		return 0, offset
	case twoBytes:
		b1 := uint32(0)
		if offset < len(str) {
			b1 = uint32(str[offset])
			offset++
		} else {
			offset++
		}
		b2 := uint32(0)
		if offset < len(str) {
			b2 = uint32(str[offset])
			offset++
		} else {
			offset++
		}
		return 256*b1 + b2, offset
	case mixedTwoBytes:
		var b1 uint8
		if offset < len(str) {
			b1 = str[offset]
			offset++
		} else {
			offset++
		}
		if !c.mixedTwoByteLeadingBytes[b1] {
			return uint32(b1), offset
		}
		var b2 uint8
		if offset < len(str) {
			b2 = str[offset]
			offset++
		} else {
			offset++
		}
		return 256*uint32(b1) + uint32(b2), offset
	case mixedFourBytes:
		var codes [4]uint8
		charSize := 1
		codes[0] = next()
		for {
			ret := checkFourByteCodeRange(codes[:charSize], c.mixedFourByteLeadingRanges)
			if ret == 0 {
				return 0, offset
			}
			if ret == 2 {
				var charcode uint32
				for i := 0; i < charSize; i++ {
					charcode = (charcode << 8) + uint32(codes[i])
				}
				return charcode, offset
			}
			if charSize == 4 || offset >= len(str) {
				return 0, offset
			}
			codes[charSize] = str[offset]
			offset++
			charSize++
		}
	}
	return 0, offset
}

// --- setters used by the parser ---

func (c *cmap) SetVertical(b bool)             { c.vertical = b }
func (c *cmap) SetCodingScheme(s codingScheme) { c.scheme = s }
func (c *cmap) SetCharset(set cidSet)          { c.charset = set }

// SetDirectCharcodeToCIDTableRange ports the same method.
func (c *cmap) SetDirectCharcodeToCIDTableRange(start, end uint32, startCID uint16) {
	for code := start; code <= end; code++ {
		c.directCharcodeToCIDTable[code] = startCID + uint16(code-start)
	}
}

// SetAdditionalMappings ports CPDF_CMap::SetAdditionalMappings (only stored when
// scheme==MixedFourBytes; sorted by end_code).
func (c *cmap) SetAdditionalMappings(mappings []cidRange) {
	if c.scheme != mixedFourBytes || len(mappings) == 0 {
		return
	}
	sort.SliceStable(mappings, func(i, j int) bool { return mappings[i].endCode < mappings[j].endCode })
	c.additionalCharcodeToCID = mappings
}

// SetMixedFourByteLeadingRanges ports the same method.
func (c *cmap) SetMixedFourByteLeadingRanges(ranges []codeRange) {
	c.mixedFourByteLeadingRanges = ranges
}

// --- free helpers ---

// loadLeadingSegments ports LoadLeadingSegments.
func loadLeadingSegments(m *predefinedCMap) []bool {
	segments := make([]bool, 256)
	for _, seg := range m.leading {
		if seg.first == 0 && seg.last == 0 {
			break
		}
		for b := int(seg.first); b <= int(seg.last); b++ {
			segments[b] = true
		}
	}
	return segments
}

// checkFourByteCodeRange ports CheckFourByteCodeRange (iterate ranges back to
// front). Returns 0 (no match), 1 (partial progress) or 2 (full match).
func checkFourByteCodeRange(codes []uint8, ranges []codeRange) int {
	for i := len(ranges); i > 0; i-- {
		r := ranges[i-1]
		if r.charSize < len(codes) {
			continue
		}
		iChar := 0
		for iChar < len(codes) {
			if codes[iChar] < r.lower[iChar] || codes[iChar] > r.upper[iChar] {
				break
			}
			iChar++
		}
		if iChar == r.charSize {
			return 2
		}
		if iChar > 0 {
			if len(codes) == r.charSize {
				return 2
			}
			return 1
		}
	}
	return 0
}

// getPredefinedCMapEntry ports GetPredefinedCMap (the table lookup; strips the
// trailing 2 chars of the name when len>2 to drop the -H/-V suffix).
func getPredefinedCMapEntry(name string) *predefinedCMap {
	id := name
	if len(id) > 2 {
		id = id[:len(id)-2]
	}
	for i := range kPredefinedCMaps {
		if id == kPredefinedCMaps[i].name {
			return &kPredefinedCMaps[i]
		}
	}
	return nil
}

// kPredefinedCMaps ports the kPredefinedCMaps table verbatim (cpdf_cmap.cpp:37).
// TODO(plan 009): these only light up once the compiled CJK fxcmap tables exist;
// until then a match still leaves the CMap loaded=false (embed_map_ nil).
var kPredefinedCMaps = []predefinedCMap{
	{"GB-EUC", cidSetGB1, cidCodingGB, mixedTwoBytes, [2]byteRange{{0xa1, 0xfe}}},
	{"GBpc-EUC", cidSetGB1, cidCodingGB, mixedTwoBytes, [2]byteRange{{0xa1, 0xfc}}},
	{"GBK-EUC", cidSetGB1, cidCodingGB, mixedTwoBytes, [2]byteRange{{0x81, 0xfe}}},
	{"GBKp-EUC", cidSetGB1, cidCodingGB, mixedTwoBytes, [2]byteRange{{0x81, 0xfe}}},
	{"GBK2K-EUC", cidSetGB1, cidCodingGB, mixedTwoBytes, [2]byteRange{{0x81, 0xfe}}},
	{"GBK2K", cidSetGB1, cidCodingGB, mixedTwoBytes, [2]byteRange{{0x81, 0xfe}}},
	{"UniGB-UCS2", cidSetGB1, cidCodingUCS2, twoBytes, [2]byteRange{}},
	{"UniGB-UTF16", cidSetGB1, cidCodingUTF16, twoBytes, [2]byteRange{}},
	{"B5pc", cidSetCNS1, cidCodingBig5, mixedTwoBytes, [2]byteRange{{0xa1, 0xfc}}},
	{"HKscs-B5", cidSetCNS1, cidCodingBig5, mixedTwoBytes, [2]byteRange{{0x88, 0xfe}}},
	{"ETen-B5", cidSetCNS1, cidCodingBig5, mixedTwoBytes, [2]byteRange{{0xa1, 0xfe}}},
	{"ETenms-B5", cidSetCNS1, cidCodingBig5, mixedTwoBytes, [2]byteRange{{0xa1, 0xfe}}},
	{"UniCNS-UCS2", cidSetCNS1, cidCodingUCS2, twoBytes, [2]byteRange{}},
	{"UniCNS-UTF16", cidSetCNS1, cidCodingUTF16, twoBytes, [2]byteRange{}},
	{"83pv-RKSJ", cidSetJapan1, cidCodingJIS, mixedTwoBytes, [2]byteRange{{0x81, 0x9f}, {0xe0, 0xfc}}},
	{"90ms-RKSJ", cidSetJapan1, cidCodingJIS, mixedTwoBytes, [2]byteRange{{0x81, 0x9f}, {0xe0, 0xfc}}},
	{"90msp-RKSJ", cidSetJapan1, cidCodingJIS, mixedTwoBytes, [2]byteRange{{0x81, 0x9f}, {0xe0, 0xfc}}},
	{"90pv-RKSJ", cidSetJapan1, cidCodingJIS, mixedTwoBytes, [2]byteRange{{0x81, 0x9f}, {0xe0, 0xfc}}},
	{"Add-RKSJ", cidSetJapan1, cidCodingJIS, mixedTwoBytes, [2]byteRange{{0x81, 0x9f}, {0xe0, 0xfc}}},
	{"EUC", cidSetJapan1, cidCodingJIS, mixedTwoBytes, [2]byteRange{{0x8e, 0x8e}, {0xa1, 0xfe}}},
	{"H", cidSetJapan1, cidCodingJIS, twoBytes, [2]byteRange{{0x21, 0x7e}}},
	{"V", cidSetJapan1, cidCodingJIS, twoBytes, [2]byteRange{{0x21, 0x7e}}},
	{"Ext-RKSJ", cidSetJapan1, cidCodingJIS, mixedTwoBytes, [2]byteRange{{0x81, 0x9f}, {0xe0, 0xfc}}},
	{"UniJIS-UCS2", cidSetJapan1, cidCodingUCS2, twoBytes, [2]byteRange{}},
	{"UniJIS-UCS2-HW", cidSetJapan1, cidCodingUCS2, twoBytes, [2]byteRange{}},
	{"UniJIS-UTF16", cidSetJapan1, cidCodingUTF16, twoBytes, [2]byteRange{}},
	{"KSC-EUC", cidSetKorea1, cidCodingKorea, mixedTwoBytes, [2]byteRange{{0xa1, 0xfe}}},
	{"KSCms-UHC", cidSetKorea1, cidCodingKorea, mixedTwoBytes, [2]byteRange{{0x81, 0xfe}}},
	{"KSCms-UHC-HW", cidSetKorea1, cidCodingKorea, mixedTwoBytes, [2]byteRange{{0x81, 0xfe}}},
	{"KSCpc-EUC", cidSetKorea1, cidCodingKorea, mixedTwoBytes, [2]byteRange{{0xa1, 0xfd}}},
	{"UniKS-UCS2", cidSetKorea1, cidCodingUCS2, twoBytes, [2]byteRange{}},
	{"UniKS-UTF16", cidSetKorea1, cidCodingUTF16, twoBytes, [2]byteRange{}},
}
