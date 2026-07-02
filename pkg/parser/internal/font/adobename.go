// Ported from core/fxge/freetype/fx_freetype.cpp:108 (FXFT_unicode_from_adobe_name),
// core/fxge/fx_font.cpp:153 (UnicodeFromAdobeName), and FreeType's
// ft_get_adobe_glyph_index trie walk from third_party/freetype/include/pstables.h
// @ pdfium 0db284a42.
//
// The byte-classification helpers mirror FXSYS_IsHexDigit / FXSYS_HexCharToInt /
// FXSYS_IsDecimalDigit / PDFCharIsWhitespace, which live in unexported helpers of
// the syntax package, so the font package carries its own copies.
package font

// pdfCharIsWhitespace mirrors PDFCharIsWhitespace (fpdf_parser_utility): the PDF
// whitespace set NUL, TAB, LF, FF, CR, SP (note: 0x80/0xFF are NOT whitespace
// here, unlike the lexer's char table).
func pdfCharIsWhitespace(c byte) bool {
	switch c {
	case 0x00, 0x09, 0x0A, 0x0C, 0x0D, 0x20:
		return true
	default:
		return false
	}
}

// isHexDigit mirrors FXSYS_IsHexDigit.
func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// hexCharToInt mirrors FXSYS_HexCharToInt (returns 0 for a non-hex byte).
func hexCharToInt(c byte) uint32 {
	if c <= '9' {
		if c >= '0' {
			return uint32(c - '0')
		}
		return 0
	}
	u := c &^ 0x20 // uppercase
	if u >= 'A' && u <= 'F' {
		return uint32(u-'A') + 10
	}
	return 0
}

// isDecimalDigit mirrors FXSYS_IsDecimalDigit.
func isDecimalDigit(c byte) bool { return c >= '0' && c <= '9' }

// kVariantBit is FreeType's high "variant" marker for glyph names ending in a
// `.suffix`; UnicodeFromAdobeName strips it via & 0x7FFFFFFF.
const kVariantBit = 0x80000000

// UnicodeFromAdobeName returns the Unicode scalar for an Adobe glyph name, or 0.
// It ports fx_font.cpp:153 UnicodeFromAdobeName: the FreeType routine's value
// masked with 0x7FFFFFFF (so the variant bit is discarded).
func unicodeFromAdobeName(name string) rune {
	return rune(ftUnicodeFromAdobeName(name) & 0x7FFFFFFF)
}

// ftUnicodeFromAdobeName ports FXFT_unicode_from_adobe_name (fx_freetype.cpp:108).
// `name` is a NUL-free Go string (the byte at index len(name) is treated as the
// terminating '\0').
func ftUnicodeFromAdobeName(name string) uint32 {
	// charAt returns the byte at i, or 0 (the '\0' terminator) at/after the end.
	charAt := func(i int) byte {
		if i < 0 || i >= len(name) {
			return 0
		}
		return name[i]
	}

	// "uni" + exactly four UPPERCASE hex digits.
	if charAt(0) == 'u' && charAt(1) == 'n' && charAt(2) == 'i' {
		var value uint32
		p := 3
		count := 4
		for ; count > 0; count, p = count-1, p+1 {
			c := charAt(p)
			d := uint32(c) - '0'
			if d >= 10 {
				d = uint32(c) - 'A'
				if d >= 6 {
					d = 16
				} else {
					d += 10
				}
			}
			if d >= 16 {
				break
			}
			value = (value << 4) + d
		}
		if count == 0 {
			if charAt(p) == 0 {
				return value
			}
			if charAt(p) == '.' {
				return value | kVariantBit
			}
		}
	}

	// "u" + four to six UPPERCASE hex digits.
	if charAt(0) == 'u' {
		var value uint32
		p := 1
		count := 6
		for ; count > 0; count, p = count-1, p+1 {
			c := charAt(p)
			d := uint32(c) - '0'
			if d >= 10 {
				d = uint32(c) - 'A'
				if d >= 6 {
					d = 16
				} else {
					d += 10
				}
			}
			if d >= 16 {
				break
			}
			value = (value << 4) + d
		}
		if count <= 2 {
			if charAt(p) == 0 {
				return value
			}
			if charAt(p) == '.' {
				return value | kVariantBit
			}
		}
	}

	// Non-initial '.' variant suffix: look up the part before the dot in the AGL.
	dot := -1
	for p := 0; p < len(name); p++ {
		if name[p] == '.' && p > 0 {
			dot = p
			break
		}
	}
	if dot < 0 {
		return ftGetAdobeGlyphIndex(name, len(name))
	}
	return ftGetAdobeGlyphIndex(name, dot) | kVariantBit
}

// ftGetAdobeGlyphIndex ports FreeType's ft_get_adobe_glyph_index trie walk over
// the embedded ftAdobeGlyphList. `limit` is the exclusive end index in name to
// match (so "A.swash" with limit=1 matches "A").
func ftGetAdobeGlyphIndex(name string, limit int) uint32 {
	list := ftAdobeGlyphList
	at := func(i int) int { return int(list[i]) }

	namePos := 0
	// nameByte returns the next character and advances; only valid while
	// namePos < limit.
	if name == "" || namePos >= limit {
		return 0
	}

	c := int(name[namePos])
	namePos++
	count := at(1)
	p := 2

	min, max := 0, count
	for min < max {
		mid := (min + max) >> 1
		q := p + mid*2
		q = (at(q) << 8) | at(q+1)
		c2 := at(q) & 127
		if c2 == c {
			p = q
			goto Found
		}
		if c2 < c {
			min = mid + 1
		} else {
			max = mid
		}
	}
	return 0

Found:
	for {
		if namePos >= limit {
			if (at(p)&128) == 0 && (at(p+1)&128) != 0 {
				return uint32((at(p+2) << 8) | at(p+3))
			}
			return 0
		}
		c = int(name[namePos])
		namePos++
		if at(p)&128 != 0 {
			p++
			if c != (at(p) & 127) {
				return 0
			}
			continue
		}

		p++
		count = at(p) & 127
		if at(p)&128 != 0 {
			p += 2
		}
		p++

		found := false
		for ; count > 0; count, p = count-1, p+2 {
			offset := (at(p) << 8) | at(p+1)
			q := offset
			if c == (at(q) & 127) {
				p = q
				found = true
				break
			}
		}
		if !found {
			return 0
		}
	}
}
