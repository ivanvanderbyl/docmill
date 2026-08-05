// TeX font-encoding resolution for fonts that carry no usable Unicode source.
//
// dvips (and some other TeX drivers) embed Computer Modern fonts as Type 3
// bitmap fonts with no /ToUnicode CMap and glyph names that are synthetic
// (the hex form "#XX" of the char code, the char code's own ASCII character,
// or a bare comma at code 0). The char codes follow the TeX font encodings —
// OT1 (text), OML (math italic), OMS (math symbols), OMX (math extension) —
// not ASCII, so without this resolution a minus sign (cmsy slot 0) extracts
// as nothing and an omega (cmmi slot 0x21) extracts as '!'.
//
// The encoding is identified from FONT METRICS alone (never from document
// text): the general invariant is that a font following a TeX encoding has
// per-glyph advance widths proportional to the published Computer Modern TFM
// widths for that encoding's slots. The font's /Widths vector, restricted to
// the glyphs actually present, is fitted against each candidate TFM width
// table with a scale-free least-squares fit; the relative mean absolute error
// says how well that encoding explains the observed metrics. Identification
// is deliberately conservative: it requires enough glyphs to be
// discriminating, a tight absolute error, and a clear margin over the best
// candidate of any other encoding. When the evidence is ambiguous NO encoding
// is chosen and existing behaviour is untouched — a wrong encoding is worse
// than no encoding.
//
// Calibration against real dvips output (24 subset fonts) and non-TeX
// controls (Helvetica, Times, Courier, Adobe Symbol metrics, whole and
// subsetted): every true Computer Modern subset with >= 4 glyphs fits its
// encoding with <= 0.9% error while the best wrong-encoding candidate is
// >= 7% away; every non-TeX control's best candidate is >= 4.6% away. The
// 2% + 4-point thresholds sit between those populations with headroom on
// both sides.
package font

import "math"

// texEncoding identifies one of the four standard TeX font encodings (the
// zero value means "not identified — leave the font alone").
type texEncoding int

const (
	texEncNone texEncoding = iota
	texEncCMR              // OT1: TeX text (cmr, cmbx, cmti, cmsl, ...)
	texEncCMMI             // OML: TeX math italic (cmmi)
	texEncCMSY             // OMS: TeX math symbols (cmsy)
	texEncCMEX             // OMX: TeX math extension (cmex)
	texEncCount
)

// texWidthCandidate is one reference width fingerprint: the published TFM
// advance widths of a standard Computer Modern font, in 1000-unit design
// space, indexed by char code (0 = no glyph at that slot).
type texWidthCandidate struct {
	name   string
	enc    texEncoding
	widths [128]uint16
}

// texUnicodeTable returns the charcode->Unicode table for an identified TeX
// encoding, nil for texEncNone.
func texUnicodeTable(enc texEncoding) *[256]rune {
	switch enc {
	case texEncCMR:
		return &texCMRUnicode
	case texEncCMMI:
		return &texCMMIUnicode
	case texEncCMSY:
		return &texCMSYUnicode
	case texEncCMEX:
		return &texCMEXUnicode
	default:
		return nil
	}
}

// Detection thresholds. Values from the calibration described in the package
// comment; all three must hold for an identification to be accepted.
const (
	// texMinFingerprintCodes is the minimum number of distinct glyphs needed
	// before a width fingerprint is trusted at all. A 1-3 glyph subset does
	// not carry enough metric evidence to name its encoding.
	texMinFingerprintCodes = 4
	// texMaxMatchError is the maximum relative mean absolute width error for
	// the winning candidate (2%). True matches calibrate at <= 0.9% (the gap
	// above that covers device-pixel quantisation of small bitmap fonts);
	// the closest observed false candidate is 4.6%.
	texMaxMatchError = 0.02
	// texMinRunnerUpMargin is how much worse (absolute, in relative-error
	// points) the best candidate of every OTHER encoding must be. True
	// matches calibrate with >= 7-point margins.
	texMinRunnerUpMargin = 0.04
)

// detectTeXEncoding fingerprints a font's advance-width vector (indexed by
// char code, 0 = glyph absent) against the Computer Modern TFM width tables
// and returns the TeX encoding the metrics follow, or texEncNone when the
// evidence is insufficient or ambiguous. The comparison is scale-free (widths
// may be in any unit: dvips Type 3 fonts use device pixels, Type 1 fonts use
// 1000-unit glyph space), so only the width RATIOS between slots matter.
func detectTeXEncoding(widths []float32) texEncoding {
	codes := make([]int, 0, 32)
	for c, w := range widths {
		if w <= 0 {
			continue
		}
		if c > 127 {
			// TeX encodings are 128-glyph fonts; a glyph above 0x7F rules
			// the whole family out.
			return texEncNone
		}
		codes = append(codes, c)
	}
	if len(codes) < texMinFingerprintCodes {
		return texEncNone
	}

	classErr := [texEncCount]float64{}
	for i := range classErr {
		classErr[i] = math.Inf(1)
	}
	for i := range texWidthCandidates {
		cand := &texWidthCandidates[i]
		// Least-squares scale fit s minimising sum (fw - s*cw)^2 over the
		// glyphs present. A candidate lacking any present glyph cannot
		// explain the font (subset fonts only contain slots that exist in
		// the real encoding) and is disqualified outright.
		var num, den, sum float64
		ok := true
		for _, c := range codes {
			cw := float64(cand.widths[c])
			if cw == 0 {
				ok = false
				break
			}
			fw := float64(widths[c])
			num += fw * cw
			den += cw * cw
			sum += fw
		}
		if !ok || den == 0 || sum == 0 {
			continue
		}
		s := num / den
		var mae float64
		for _, c := range codes {
			mae += math.Abs(float64(widths[c]) - s*float64(cand.widths[c]))
		}
		mae /= float64(len(codes))
		rel := mae / (sum / float64(len(codes)))
		if rel < classErr[cand.enc] {
			classErr[cand.enc] = rel
		}
	}

	best := texEncNone
	bestErr := math.Inf(1)
	for enc := texEncCMR; enc < texEncCount; enc++ {
		if classErr[enc] < bestErr {
			best = enc
			bestErr = classErr[enc]
		}
	}
	if best == texEncNone || bestErr > texMaxMatchError {
		return texEncNone
	}
	for enc := texEncCMR; enc < texEncCount; enc++ {
		if enc != best && classErr[enc] < bestErr+texMinRunnerUpMargin {
			// Another encoding explains the metrics almost as well:
			// ambiguous, so identify nothing.
			return texEncNone
		}
	}
	return best
}

// texSyntheticGlyphName reports whether a /Differences glyph name carries no
// information beyond the char code itself. dvips writes exactly three shapes
// of name for its Type 3 bitmap fonts: the two-hex-digit form of the code
// ("#XX", arriving already unescaped from the PDF name parser), the code's
// own ASCII character as a one-character name, and — as a fixed quirk — a
// comma for code 0. A name outside these shapes (e.g. /alpha, /eacute)
// asserts a real identity for the glyph and must disable TeX resolution.
func texSyntheticGlyphName(name string, code int) bool {
	if len(name) == 3 && name[0] == '#' && isHexDigit(name[1]) && isHexDigit(name[2]) {
		v := hexCharToInt(name[1])<<4 | hexCharToInt(name[2])
		return int(v) == code
	}
	if len(name) == 1 {
		if int(name[0]) == code {
			return true
		}
		return code == 0 && name == ","
	}
	return false
}

// texNamesUninformative reports whether a font's /Differences names (a nil or
// 256-entry slice indexed by char code) are absent or entirely synthetic in
// the sense of texSyntheticGlyphName.
func texNamesUninformative(charNames []string) bool {
	for code, name := range charNames {
		if name == "" {
			continue
		}
		if !texSyntheticGlyphName(name, code) {
			return false
		}
	}
	return true
}

// resolveTeXEncoding runs once at load time, after /Widths and /Encoding are
// parsed but before charNames is discarded. It gates the whole mechanism on
// the two conditions that mean "no better Unicode source exists":
//
//  1. the font has no /ToUnicode CMap (an explicit map always wins), and
//  2. the glyph names are absent or synthetic (real glyph names resolve
//     through the Adobe Glyph List instead).
//
// Only then is the width fingerprint consulted; on an accepted match the
// chosen table is cached for UnicodeFromCharCode's fallback path.
func (f *Font) resolveTeXEncoding() {
	if f.fontDict.KeyExist("ToUnicode") {
		return
	}
	if !texNamesUninformative(f.charNames) {
		return
	}
	arr := f.fontDict.GetArrayFor("Widths")
	if arr == nil {
		return
	}
	first := f.fontDict.GetIntegerWithDefaultFor("FirstChar", 0)
	if first < 0 || first > 255 {
		return
	}
	widths := make([]float32, 256)
	for i := 0; i < arr.Len() && first+i < 256; i++ {
		widths[first+i] = arr.GetFloatAt(i)
	}
	f.texUnicode = texUnicodeTable(detectTeXEncoding(widths))
}
