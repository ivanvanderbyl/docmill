// Ported from core/fpdftext/cpdf_textpage.cpp GetUnicodeNormalization @ pdfium
// 0db284a42, over the verbatim tables in normalization_data.go.
package text

// normalizationMaps mirrors kUnicodeDataNormalizationMaps (the span array of
// {Map2, Map3, Map4}); indexed by (wFind>>12)-2.
var normalizationMaps = [3][]uint16{
	unicodeDataNormalizationMap2[:],
	unicodeDataNormalizationMap3[:],
	unicodeDataNormalizationMap4[:],
}

// getUnicodeNormalization ports GetUnicodeNormalization (cpdf_textpage.cpp:101).
// It returns the canonical/compatibility decomposition for ligatures and a few
// presentation forms; for an un-mapped char it returns the char itself.
func getUnicodeNormalization(wch rune) []rune {
	w := uint32(wch) & 0xFFFF
	wFind := uint32(unicodeDataNormalization[w])
	if wFind == 0 {
		return []rune{rune(w)}
	}
	if wFind >= 0x8000 {
		return []rune{rune(unicodeDataNormalizationMap1[wFind-0x8000])}
	}
	idx := wFind & 0x0FFF
	sel := wFind >> 12
	pMap := normalizationMaps[sel-2][idx:]
	if sel == 4 {
		wFind = uint32(pMap[0])
		pMap = pMap[1:]
	} else {
		wFind = sel // the count is the selector value for sel==2,3 (front length)
	}
	// For sel 2/3 the decomposition length is the selector itself (2 or 3); for
	// sel 4 the length was read from the map's first element above.
	out := make([]rune, 0, wFind)
	for i := uint32(0); i < wFind; i++ {
		out = append(out, rune(pMap[i]))
	}
	return out
}
