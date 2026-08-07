// Ported from core/fpdfapi/page/cpdf_textobject.{h,cpp} @ pdfium 0db284a42.
//
// TextObject owns the decoded glyph run. SetSegments builds the char-code /
// char-pos vectors (with kInvalidCharCode sentinels at TJ segment boundaries);
// CalcPositionData walks them to compute each glyph's running advance (origin)
// plus the object's bounding rect, and returns the post-run pen delta.
package page

import (
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/font"
)

// InvalidCharCode is CPDF_Font::kInvalidCharCode (static_cast<uint32_t>(-1)):
// the sentinel between TJ segments that carries the kerning, not a glyph.
const InvalidCharCode uint32 = 0xFFFFFFFF

// TextItem is the per-glyph record the textpage consumes.
type TextItem struct {
	// CharCode is the glyph char code, or InvalidCharCode for a kerning sentinel.
	CharCode uint32
	// Origin is the glyph origin in text space (pre text-matrix). The textpage
	// maps it to user space via TextObject.TextMatrix().
	Origin crt.PointF
}

// charBBox is the integer 1000-unit glyph-space bounding box (FX_RECT).
type charBBox struct{ left, top, right, bottom int }

// getCharBBox adapts font.GetCharBBox into TextObject's internal rect shape.
func getCharBBox(f *font.Font, charCode uint32) charBBox {
	if f == nil {
		return charBBox{}
	}
	left, bottom, right, top := f.GetCharBBox(charCode)
	return charBBox{left: left, top: top, right: right, bottom: bottom}
}

// countChar ports the per-segment char count (CPDF_Font::CountChar): walk the
// segment with NextChar and count the codes. CID fonts consume 1-2 bytes per
// code; simple fonts one byte.
func countChar(f *font.Font, segment []byte) int {
	count := 0
	offset := 0
	for offset < len(segment) {
		_, next := f.NextChar(segment, offset)
		if next <= offset {
			// Defensive: NextChar must advance; guard against a stuck loop on a
			// degenerate font/codespace.
			offset++
		} else {
			offset = next
		}
		count++
	}
	return count
}

// TextObject implements PageObject. It is built by the interpreter's
// AddTextObject.
type TextObject struct {
	baseObject
	pos        crt.PointF // pos_: the Tm origin (e,f) of GetTextMatrix
	charCodes  []uint32   // char_codes_ (includes InvalidCharCode sentinels)
	charPos    []float32  // char_pos_; len == len(charCodes)-1
	itemsCache []TextItem // lazily-built Items() result (read path only)
}

// newTextObject ports CPDF_TextObject(content_stream).
func newTextObject(contentStream int32) *TextObject {
	return &TextObject{baseObject: newBaseObject(contentStream)}
}

func (t *TextObject) getType() objectType { return typeText }

// Kind reports that this is a text-showing object.
func (t *TextObject) Kind() ObjectKind { return KindText }

// Font returns the active font for this run.
func (t *TextObject) Font() *font.Font { return t.graphicStates.textState.GetFont() }

// FontSize returns the Tf size in effect for this run.
func (t *TextObject) FontSize() float32 { return t.graphicStates.textState.GetFontSize() }

// GetTextRenderMode returns the Tr mode in effect for this run.
func (t *TextObject) GetTextRenderMode() TextRenderingMode {
	return t.graphicStates.textState.GetTextMode()
}

// TextMatrix ports CPDF_TextObject::GetTextMatrix (cpdf_textobject.cpp:194): the
// text-space -> user-space matrix. The cached matrix is stored TRANSPOSED as
// [a, b, c, d]; GetTextMatrix returns Matrix(pm[0], pm[2], pm[1], pm[3], pos.x,
// pos.y), so matrix.b = pm[2] and matrix.c = pm[1]. The object position is the
// translation (e, f). Glyph origin on the page = TextMatrix.Transform(Origin).
func (t *TextObject) TextMatrix() crt.Matrix {
	pm := t.graphicStates.textState.GetMatrix()
	return crt.NewMatrix(pm[0], pm[2], pm[1], pm[3], t.pos.X, t.pos.Y)
}

// setTextMatrix ports CPDF_TextObject::SetTextMatrix (the inverse transposition)
// then recomputes position data. Not used on the build path (the interpreter
// sets position via setPosition), but kept faithful for completeness.
func (t *TextObject) setTextMatrix(m crt.Matrix) {
	pm := t.graphicStates.textState.MutableMatrix()
	pm[0] = m.A
	pm[1] = m.C
	pm[2] = m.B
	pm[3] = m.D
	t.pos = crt.PointF{X: m.E, Y: m.F}
	t.calcPositionDataInternal()
}

// setPosition ports CPDF_TextObject::SetPosition: the object origin in user
// space (already passed through mt_content_to_user_ by the caller).
func (t *TextObject) setPosition(p crt.PointF) { t.pos = p }

// CountItems ports CountItems: number of raw items (sentinels included).
func (t *TextObject) CountItems() int { return len(t.charCodes) }

// CountChars ports CountChars: items that are not sentinels.
func (t *TextObject) CountChars() int {
	n := 0
	for _, c := range t.charCodes {
		if c != InvalidCharCode {
			n++
		}
	}
	return n
}

// GetCharCode ports GetCharCode(index): the index-th non-sentinel code, or
// InvalidCharCode if out of range.
func (t *TextObject) GetCharCode(index int) uint32 {
	count := 0
	for _, c := range t.charCodes {
		if c == InvalidCharCode {
			continue
		}
		if count != index {
			count++
			continue
		}
		return c
	}
	return InvalidCharCode
}

// getItemInfo ports GetItemInfo(index) by RAW item index (sentinels included).
// For the corpus path (Latin simple fonts, Identity-H horizontal CID) the item
// origin is (charPos[index-1], 0) for index>0, else (0,0). Vertical-CID is
// deferred (no GetVertOrigin in the face-less font port).
func (t *TextObject) getItemInfo(index int) TextItem {
	info := TextItem{CharCode: t.charCodes[index]}
	if index > 0 {
		info.Origin = crt.PointF{X: t.charPos[index-1], Y: 0}
	}
	return info
}

// Items ports the raw item list (GetItemInfo over CountItems), in order. The
// result is cached: the textpage calls this repeatedly per object on the read
// path, where the object is immutable (cache is cleared whenever charPos/
// charCodes change on the build path).
func (t *TextObject) Items() []TextItem {
	if t.itemsCache != nil {
		return t.itemsCache
	}
	items := make([]TextItem, len(t.charCodes))
	for i := range t.charCodes {
		items[i] = t.getItemInfo(i)
	}
	t.itemsCache = items
	return items
}

// setSegments ports CPDF_TextObject::SetSegments (cpdf_textobject.cpp:210):
// builds charCodes/charPos from a TJ-style array of strings + per-segment
// kernings, stashing the kerning at the sentinel's predecessor slot. It does
// NOT call CalcPositionData (the interpreter calls that afterwards with Tz).
func (t *TextObject) setSegments(strings [][]byte, kernings []float32) {
	nSegs := len(strings)
	if nSegs == 0 {
		return
	}
	t.charCodes = nil
	t.charPos = nil
	t.itemsCache = nil
	f := t.Font()

	nChars := nSegs - 1
	for _, s := range strings {
		nChars += countChar(f, s)
	}
	if nChars <= 0 {
		return
	}
	t.charCodes = make([]uint32, nChars)
	t.charPos = make([]float32, nChars-1)

	index := 0
	for i, segment := range strings {
		offset := 0
		for offset < len(segment) {
			code, next := f.NextChar(segment, offset)
			if next <= offset {
				offset++
			} else {
				offset = next
			}
			t.charCodes[index] = code
			index++
		}
		if i != nSegs-1 {
			t.charPos[index-1] = kernings[i]
			t.charCodes[index] = InvalidCharCode
			index++
		}
	}
}

// calcPositionData ports CPDF_TextObject::CalcPositionData (cpdf_textobject.cpp:
// 274): runs the internal width-advance engine and returns the post-run pen
// delta to apply to the text matrix, pre-multiplied by Tz for horizontal text.
// (Vertical-CID returns {0, curpos}; deferred -> always horizontal here.)
func (t *TextObject) calcPositionData(horzScale float32) crt.PointF {
	curpos := t.calcPositionDataInternal()
	scaled := curpos * horzScale
	return crt.PointF{X: scaled, Y: 0}
}

// calcPositionDataInternal ports CPDF_TextObject::CalcPositionDataInternal
// (cpdf_textobject.cpp:283) — the width-advance + bbox engine, horizontal
// branch (the corpus path; vertical-CID needs GetVertOrigin/GetVertWidth which
// the face-less font port lacks). It overwrites charPos[i-1] with the running
// advance for each real glyph (so getItemInfo can read it back as the origin).
//
// All products are stored in float32 locals before summing, and each `x *
// fontsize / 1000` is evaluated left-to-right ((x*fontsize)/1000), matching the
// C++ and the no-FMA discipline.
func (t *TextObject) calcPositionDataInternal() float32 {
	t.itemsCache = nil // charPos is rewritten below; drop any stale Items() cache
	var curpos float32
	minX := float32(10000)
	maxX := float32(-10000)
	minY := float32(10000)
	maxY := float32(-10000)
	f := t.Font()
	ts := &t.graphicStates.textState
	fontsize := ts.GetFontSize()

	for i := 0; i < len(t.charCodes); i++ {
		charcode := t.charCodes[i]
		if i > 0 {
			if charcode == InvalidCharCode {
				// Sentinel: apply the stashed TJ kerning as a negative advance,
				// in glyph(1000) units. (charPos[i-1]*fontsize) into a local.
				prod := t.charPos[i-1] * fontsize
				curpos -= prod / 1000
				continue
			}
			t.charPos[i-1] = curpos // record running advance for this glyph
		}

		cr := getCharBBox(f, charcode)

		// y extents accumulate in raw glyph units, scaled once at the end.
		minY = min3(minY, float32(cr.top), float32(cr.bottom))
		maxY = max3(maxY, float32(cr.top), float32(cr.bottom))

		leftProd := float32(cr.left) * fontsize
		charLeft := curpos + leftProd/1000
		rightProd := float32(cr.right) * fontsize
		charRight := curpos + rightProd/1000
		minX = min3(minX, charLeft, charRight)
		maxX = max3(maxX, charLeft, charRight)

		widthProd := f.GetCharWidthF(charcode) * fontsize
		charwidth := widthProd / 1000

		curpos += charwidth
		if charcode == ' ' {
			// Tw applies only to a single-byte space 0x20. For a CID font whose
			// space encodes as 2 bytes Tw is skipped; the face-less CID path
			// has no GetCharSize, so we gate on "not a CID font" which is the
			// corpus-faithful condition (simple fonts always single-byte space).
			if !f.IsCID() {
				curpos += ts.GetWordSpace()
			}
		}
		curpos += ts.GetCharSpace()
	}

	// Horizontal: scale the y extents once at the end.
	minY = (minY * fontsize) / 1000
	maxY = (maxY * fontsize) / 1000

	t.rectFromExtents(minX, minY, maxX, maxY)
	return curpos
}

// rectFromExtents sets OriginalRect from the glyph extents, transforms it by the
// text matrix into the user-space Rect, and inflates by half the line width for
// stroke modes (cpdf_textobject.cpp:351).
func (t *TextObject) rectFromExtents(minX, minY, maxX, maxY float32) {
	original := crt.NewFloatRect(minX, minY, maxX, maxY)
	rect := t.TextMatrix().TransformRect(original)
	if TextRenderingModeIsStrokeMode(t.graphicStates.textState.GetTextMode()) {
		half := t.graphicStates.graphState.GetLineWidth() / 2
		rect.Inflate(half, half, half, half)
	}
	t.SetRect(rect)
}

// min3/max3 mirror std::min/std::max over three float32 values.
func min3(a, b, c float32) float32 {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func max3(a, b, c float32) float32 {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}
