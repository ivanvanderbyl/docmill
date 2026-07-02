// Ported from core/fxge/cfx_fontmapper.cpp @ pdfium 0db284a42 — the Base-14
// classification subset (GetStandardFontName / IsSymbolicFont / IsFixedFont)
// needed by CPDF_Type1Font::Load. The full system-font substitution machinery is
// out of scope for text extraction. The alias table lives in fontmapper_data.go.
package font

import "strings"

// getStandardFontName ports CFX_FontMapper::GetStandardFontName. On a Base-14
// match it canonicalises *name to the kBase14FontNames entry and returns the
// StandardFont id; otherwise it returns (0, false) and leaves the name. The
// match is case-insensitive (FXSYS_stricmp).
func getStandardFontName(name *string) (standardFont, bool) {
	for i := range kAltFontNames {
		if strings.EqualFold(kAltFontNames[i].name, *name) {
			id := kAltFontNames[i].index
			*name = base14FontNames[id]
			return id, true
		}
	}
	return 0, false
}

// isSymbolicFont ports CFX_FontMapper::IsSymbolicFont.
func isSymbolicStandardFont(font standardFont) bool {
	return font == stdSymbol || font == stdDingbats
}

// isFixedFont ports CFX_FontMapper::IsFixedFont.
func isFixedStandardFont(font standardFont) bool {
	return font == stdCourier || font == stdCourierBold ||
		font == stdCourierBoldOblique || font == stdCourierOblique
}
