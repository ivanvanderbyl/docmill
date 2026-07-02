// Ported from core/fpdfapi/font/cpdf_cid2unicodemap.{h,cpp} and the CIDSet/
// CIDCoding enums in core/fpdfapi/cmaps/fpdf_cmaps.h @ pdfium 0db284a42.
//
// CID2UnicodeMap is a stub for the corpus: the predefined CJK CID->Unicode
// tables (GB/CNS/Japan/Korea) are NOT compiled in, so globals.GetEmbeddedToUnicode
// returns empty and only the CIDSetUnicode identity branch is live.
// TODO(plan 009): predefined CMap / CID2Unicode tables for non-embedded CJK.
package font

// cidSet mirrors CIDSet (fpdf_cmaps.h).
type cidSet uint8

const (
	cidSetUnknown cidSet = iota // CIDSET_UNKNOWN
	cidSetGB1                   // CIDSET_GB1
	cidSetCNS1                  // CIDSET_CNS1
	cidSetJapan1                // CIDSET_JAPAN1
	cidSetKorea1                // CIDSET_KOREA1
	cidSetUnicode               // CIDSET_UNICODE
	cidSetNumSets               // CIDSET_NUM_SETS == 6
)

// cidCoding mirrors CIDCoding (fpdf_cmaps.h). The numeric order matters only for
// the Windows codepage table, which the extraction path does not use.
type cidCoding uint8

const (
	cidCodingUnknown cidCoding = iota // kUNKNOWN
	cidCodingGB                       // kGB
	cidCodingBig5                     // kBIG5
	cidCodingJIS                      // kJIS
	cidCodingKorea                    // kKOREA
	cidCodingUCS2                     // kUCS2
	cidCodingCID                      // kCID
	cidCodingUTF16                    // kUTF16
)

// cid2UnicodeMap is CPDF_CID2UnicodeMap.
type cid2UnicodeMap struct {
	charset     cidSet
	embeddedMap []uint16 // empty for the corpus (no compiled CJK tables)
}

// IsLoaded ports CPDF_CID2UnicodeMap::IsLoaded.
func (m *cid2UnicodeMap) IsLoaded() bool { return len(m.embeddedMap) > 0 }

// UnicodeFromCID ports CPDF_CID2UnicodeMap::UnicodeFromCID.
func (m *cid2UnicodeMap) UnicodeFromCID(cid uint16) uint16 {
	if m.charset == cidSetUnicode {
		return cid
	}
	if int(cid) < len(m.embeddedMap) {
		return m.embeddedMap[cid]
	}
	return 0
}

// getCID2UnicodeMap is the CPDF_FontGlobals::GetCID2UnicodeMap stub. Only the
// Unicode charset yields a loaded (identity) map; the CJK charsets have no
// compiled embedded tables (TODO plan 009), so their map is unloaded.
func getCID2UnicodeMap(charset cidSet) *cid2UnicodeMap {
	if charset == cidSetUnknown || charset >= cidSetNumSets {
		return nil
	}
	return &cid2UnicodeMap{charset: charset}
}

// charsetFromOrdering ports CPDF_CMapParser::CharsetFromOrdering.
func charsetFromOrdering(ordering string) cidSet {
	names := []string{"", "GB1", "CNS1", "Japan1", "Korea1", "UCS"}
	for charset := 1; charset < len(names); charset++ {
		if ordering == names[charset] {
			return cidSet(charset)
		}
	}
	return cidSetUnknown
}
