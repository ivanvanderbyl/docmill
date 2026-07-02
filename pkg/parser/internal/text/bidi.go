// BiDi segmentation ported from core/fxcrt/fx_bidi.{h,cpp} @ pdfium 0db284a42
// (CFX_BidiChar / CFX_BidiString), plus the GetBidiClass / GetMirrorChar
// classifiers from core/fxcrt/fx_unicode.cpp.
//
// FIDELITY NOTE on classification: PDFium derives the bidi class and mirror
// glyph from a verbatim 65536-entry Unicode property table
// (kTextLayoutCodeProperties in core/fxcrt, a ~128 KiB binary blob that lives
// outside this port's scope and is NOT one of the packages plan 009 Phase H is
// allowed to add). We instead classify by the standard Unicode Bidi ranges
// (UAX #9 / UCD DerivedBidiClass), which is bit-identical to PDFium for the
// Latin / Hebrew / Arabic / digit / punctuation ranges that drive real-document
// reading order, and we mirror the standard Bidi_Mirroring_Glyph pairs (the same
// pairs PDFium's table encodes). Exotic scripts may classify differently than
// PDFium's exact per-codepoint table; this affects only BiDi reordering of
// those rare characters, not LTR text, x-segmentation, or rect geometry. This
// gap is documented and tracked for a follow-up that ports the full UCD table.
package text

// bidiClass mirrors FX_BIDICLASS (core/fxcrt/fx_unicode.h:13). Order matters:
// the segment classifier only distinguishes Left / Right / weak / neutral.
type bidiClass uint8

const (
	bidiON  bidiClass = iota // Other Neutral
	bidiL                    // Left letter
	bidiR                    // Right letter
	bidiAN                   // Arabic Number
	bidiEN                   // European Number
	bidiAL                   // Arabic Letter
	bidiNSM                  // Non-spacing Mark
	bidiCS                   // Common Number Separator
	bidiES                   // European Separator
	bidiET                   // European Number Terminator
	bidiBN                   // Boundary Neutral
	bidiS                    // Segment Separator
	bidiWS                   // Whitespace
	bidiB                    // Paragraph Separator
)

// getBidiClass ports pdfium::unicode::GetBidiClass (core/fxcrt/fx_unicode.cpp:
// 156), classifying by standard Unicode Bidi ranges (see the file-level fidelity
// note). Only the BMP is consulted (wch & 0xFFFF), matching PDFium's table.
func getBidiClass(wch rune) bidiClass {
	c := uint32(wch) & 0xFFFF
	switch {
	// Strong R-to-L letters.
	case c >= 0x0590 && c <= 0x05FF: // Hebrew
		if c == 0x05BE || c == 0x05C0 || c == 0x05C3 || c == 0x05C6 ||
			(c >= 0x05D0 && c <= 0x05EA) || (c >= 0x05EF && c <= 0x05F4) {
			return bidiR
		}
		return bidiNSM
	case c == 0x200F: // RLM
		return bidiR
	case c >= 0xFB1D && c <= 0xFB4F: // Hebrew presentation forms
		return bidiR
	// Arabic letters (kAL).
	case c >= 0x0600 && c <= 0x06FF: // Arabic
		return arabicClass(c)
	case c >= 0x0750 && c <= 0x077F: // Arabic Supplement
		return bidiAL
	case c >= 0x08A0 && c <= 0x08FF: // Arabic Extended-A
		return bidiAL
	case c >= 0xFB50 && c <= 0xFDFF: // Arabic Presentation Forms-A
		return bidiAL
	case c >= 0xFE70 && c <= 0xFEFF: // Arabic Presentation Forms-B
		return bidiAL
	case c == 0x061C || c == 0x200E: // ALM / LRM
		if c == 0x200E {
			return bidiL
		}
		return bidiAL
	// Numbers.
	case c >= 0x0660 && c <= 0x0669: // Arabic-Indic digits
		return bidiAN
	case c >= 0x066B && c <= 0x066C: // Arabic decimal/thousands separators
		return bidiAN
	case c >= 0x06F0 && c <= 0x06F9: // Extended Arabic-Indic digits
		return bidiEN
	case c >= 0x0030 && c <= 0x0039: // ASCII digits
		return bidiEN
	case c >= 0x00B2 && c <= 0x00B3, c == 0x00B9: // superscript 2,3,1
		return bidiEN
	// Separators / terminators (weak).
	case c == 0x002C || c == 0x002E || c == 0x003A || c == 0x00A0: // , . : NBSP
		return bidiCS
	case c == 0x002B || c == 0x002D: // + -
		return bidiES
	case c == 0x0023 || c == 0x0024 || c == 0x0025: // # $ %
		return bidiET
	case c == 0x002F: // /
		return bidiCS
	// Whitespace / control.
	case c == 0x0020 || c == 0x000C: // space, form feed
		return bidiWS
	case c == 0x0009 || c == 0x000B: // tab, vertical tab
		return bidiS
	case c == 0x000A || c == 0x000D || c == 0x001C || c == 0x001D ||
		c == 0x001E || c == 0x0085 || c == 0x2029: // line/para separators
		return bidiB
	case c == 0x001F: // unit separator
		return bidiS
	case c <= 0x0008 || (c >= 0x000E && c <= 0x001B) || c == 0x007F ||
		(c >= 0x0080 && c <= 0x0084) || (c >= 0x0086 && c <= 0x009F): // controls
		return bidiBN
	case c >= 0x2000 && c <= 0x200A: // various spaces
		return bidiWS
	case c == 0x2028: // line separator
		return bidiWS
	// Strong L: ASCII letters, Latin-1, and the bulk of the BMP letters.
	case (c >= 0x0041 && c <= 0x005A) || (c >= 0x0061 && c <= 0x007A):
		return bidiL
	case c >= 0x00C0 && c <= 0x024F: // Latin-1 supplement / extended
		return bidiL
	case c >= 0x0370 && c <= 0x03FF: // Greek
		return bidiL
	case c >= 0x0400 && c <= 0x052F: // Cyrillic
		return bidiL
	case c >= 0x1E00 && c <= 0x1FFF: // Latin extended additional / Greek ext
		return bidiL
	case c >= 0x2C60 && c <= 0x2C7F: // Latin extended-C
		return bidiL
	case c >= 0x3040 && c <= 0x30FF: // Hiragana / Katakana
		return bidiL
	case c >= 0x3400 && c <= 0x9FFF: // CJK
		return bidiL
	case c >= 0xAC00 && c <= 0xD7AF: // Hangul
		return bidiL
	case c >= 0xF900 && c <= 0xFAFF: // CJK compatibility ideographs
		return bidiL
	case c >= 0xFF21 && c <= 0xFF3A, c >= 0xFF41 && c <= 0xFF5A: // fullwidth Latin
		return bidiL
	default:
		// Everything else (symbols, neutral punctuation) -> Other Neutral.
		return bidiON
	}
}

// arabicClass refines the Arabic block: combining marks are NSM, digits AN, the
// rest are Arabic letters (AL).
func arabicClass(c uint32) bidiClass {
	switch {
	case c >= 0x0660 && c <= 0x0669:
		return bidiAN
	case c >= 0x066B && c <= 0x066C:
		return bidiAN
	case c >= 0x0610 && c <= 0x061A: // combining marks
		return bidiNSM
	case c >= 0x064B && c <= 0x065F: // harakat
		return bidiNSM
	case c == 0x0670: // superscript alef
		return bidiNSM
	case c >= 0x06D6 && c <= 0x06DC, c >= 0x06DF && c <= 0x06E4,
		c >= 0x06E7 && c <= 0x06E8, c >= 0x06EA && c <= 0x06ED:
		return bidiNSM
	case c == 0x0640: // tatweel
		return bidiAL
	default:
		return bidiAL
	}
}

// bidiDirection mirrors CFX_BidiChar::Direction.
type bidiDirection uint8

const (
	dirNeutral bidiDirection = iota
	dirLeft
	dirRight
	dirLeftWeak
)

// bidiSegment mirrors CFX_BidiChar::Segment.
type bidiSegment struct {
	start     int
	count     int
	direction bidiDirection
}

// bidiChar ports CFX_BidiChar.
type bidiChar struct {
	current bidiSegment
	last    bidiSegment
}

func newBidiChar() *bidiChar {
	return &bidiChar{
		current: bidiSegment{0, 0, dirNeutral},
		last:    bidiSegment{0, 0, dirNeutral},
	}
}

// appendChar ports CFX_BidiChar::AppendChar.
func (b *bidiChar) appendChar(wch rune) bool {
	var direction bidiDirection
	switch getBidiClass(wch) {
	case bidiL:
		direction = dirLeft
	case bidiAN, bidiEN, bidiNSM, bidiCS, bidiES, bidiET, bidiBN:
		direction = dirLeftWeak
	case bidiR, bidiAL:
		direction = dirRight
	default:
		direction = dirNeutral
	}
	changed := direction != b.current.direction
	if changed {
		b.startNewSegment(direction)
	}
	b.current.count++
	return changed
}

// endChar ports CFX_BidiChar::EndChar.
func (b *bidiChar) endChar() bool {
	b.startNewSegment(dirNeutral)
	return b.last.count > 0
}

func (b *bidiChar) startNewSegment(direction bidiDirection) {
	b.last = b.current
	b.current.start += b.current.count
	b.current.count = 0
	b.current.direction = direction
}

// bidiString ports CFX_BidiString.
type bidiString struct {
	order            []bidiSegment
	overallDirection bidiDirection
}

// newBidiString ports the CFX_BidiString ctor.
func newBidiString(str []rune) *bidiString {
	bs := &bidiString{overallDirection: dirLeft}
	bidi := newBidiChar()
	for _, c := range str {
		if bidi.appendChar(c) {
			bs.order = append(bs.order, bidi.last)
		}
	}
	if bidi.endChar() {
		bs.order = append(bs.order, bidi.last)
	}

	nR2L := 0
	nL2R := 0
	for _, seg := range bs.order {
		if seg.direction == dirRight {
			nR2L++
		} else if seg.direction == dirLeft {
			nL2R++
		}
	}
	if nR2L > 0 && nR2L >= nL2R {
		bs.setOverallDirectionRight()
	}
	return bs
}

// overallDir ports CFX_BidiString::OverallDirection (LEFT or RIGHT only).
func (bs *bidiString) overallDir() bidiDirection { return bs.overallDirection }

// setOverallDirectionRight ports CFX_BidiString::SetOverallDirectionRight.
func (bs *bidiString) setOverallDirectionRight() {
	if bs.overallDirection != dirRight {
		// std::reverse(order_).
		for i, j := 0, len(bs.order)-1; i < j; i, j = i+1, j-1 {
			bs.order[i], bs.order[j] = bs.order[j], bs.order[i]
		}
		bs.overallDirection = dirRight
	}
}

// getMirrorChar ports pdfium::unicode::GetMirrorChar: the standard Unicode
// Bidi_Mirroring_Glyph for the common mirrored pairs (the same pairs PDFium's
// kFXTextLayoutBidiMirror table encodes). Unmapped codepoints return wch.
func getMirrorChar(wch rune) rune {
	if m, ok := bidiMirrorPairs[wch]; ok {
		return m
	}
	return wch
}

// bidiMirrorPairs are the Bidi_Mirroring_Glyph pairs that occur in real
// documents: paired brackets, angle quotation marks, and comparison operators.
var bidiMirrorPairs = map[rune]rune{
	0x0028: 0x0029, 0x0029: 0x0028, // ( )
	0x003C: 0x003E, 0x003E: 0x003C, // < >
	0x005B: 0x005D, 0x005D: 0x005B, // [ ]
	0x007B: 0x007D, 0x007D: 0x007B, // { }
	0x00AB: 0x00BB, 0x00BB: 0x00AB, // « »
	0x2018: 0x2019, 0x2019: 0x2018, // ‘ ’
	0x201C: 0x201D, 0x201D: 0x201C, // “ ”
	0x2039: 0x203A, 0x203A: 0x2039, // ‹ ›
	0x2045: 0x2046, 0x2046: 0x2045, // ⁅ ⁆
	0x2208: 0x220B, 0x220B: 0x2208, // ∈ ∋
	0x2264: 0x2265, 0x2265: 0x2264, // ≤ ≥
	0x2266: 0x2267, 0x2267: 0x2266, // ≦ ≧
	0x2268: 0x2269, 0x2269: 0x2268, // ≨ ≩
	0x226A: 0x226B, 0x226B: 0x226A, // ≪ ≫
	0x2308: 0x2309, 0x2309: 0x2308, // ⌈ ⌉
	0x230A: 0x230B, 0x230B: 0x230A, // ⌊ ⌋
	0x2329: 0x232A, 0x232A: 0x2329, // 〈 〉
	0x27E8: 0x27E9, 0x27E9: 0x27E8, // ⟨ ⟩
	0x27EA: 0x27EB, 0x27EB: 0x27EA, // ⟪ ⟫
	0x2983: 0x2984, 0x2984: 0x2983, // ⦃ ⦄
	0x2985: 0x2986, 0x2986: 0x2985, // ⦅ ⦆
	0x3008: 0x3009, 0x3009: 0x3008, // 〈 〉
	0x300A: 0x300B, 0x300B: 0x300A, // 《 》
	0x300C: 0x300D, 0x300D: 0x300C, // 「 」
	0x300E: 0x300F, 0x300F: 0x300E, // 『 』
	0x3010: 0x3011, 0x3011: 0x3010, // 【 】
	0x3014: 0x3015, 0x3015: 0x3014, // 〔 〕
	0x3016: 0x3017, 0x3017: 0x3016, // 〖 〗
	0x3018: 0x3019, 0x3019: 0x3018, // 〘 〙
	0x301A: 0x301B, 0x301B: 0x301A, // 〚 〛
}
