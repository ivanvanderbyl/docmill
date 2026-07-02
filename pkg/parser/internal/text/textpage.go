// Package text ports CPDF_TextPage from core/fpdftext/cpdf_textpage.{h,cpp} @
// pdfium 0db284a42: the reading-order stitcher + rect segmenter that backs
// FPDFText_* (text extraction, CountRects/GetRect, GetTextInRect).
//
// The pipeline is: New -> Init -> processObject walks the page objects, buffers
// text objects into mTextObjects with a same-visual-line ascending-x sort, then
// replays them through the consuming processTextObject which runs the
// generated-character state machine (processInsertObject -> kNone/kSpace/
// kLineBreak/kHyphen, then processGenerateCharacter) and emits per-char charInfo
// records via processTextObjectItems into RTL-aware temp buffers, flushed by
// closeTempLine (BiDi reorder) into charList/textBuf. Consumers read GetRectArray
// (rects) and GetAllText (reading-order text).
//
// COORDINATE SPACE (critical for the backend). The char boxes are built from the
// glyph's text-space bbox transformed by `matrix = TextMatrix * formMatrix`
// (text space -> PDF USER space). They are NOT pre-multiplied by the display
// matrix. Therefore:
//
//	Rect.Box and the GetTextByRect argument are in PDF USER SPACE, Y-UP
//	(origin bottom-left, the same space FPDFText_GetRect returns).
//
// The backend that consumes this must convert to the top-left page.TextCell
// contract itself (e.g. via Page.DisplayMatrix, which maps user space to device
// space y-down). This matches PDFium: FPDFText_GetRect returns page/user-space
// rects; the display matrix is used internally only for the line-flush /
// newline-suppression heuristics, never to transform the emitted char boxes.
//
// FACE-LESS FIDELITY (rect heights). font.GetCharBBox is a face-less
// approximation (uniform descriptor ascent/descent per glyph, faithful advance
// width — see font/charbbox.go). Consequently each emitted Rect.Box has the
// correct x-extent and reading-order grouping, but its vertical extent is the
// font's ascent/descent envelope rather than PDFium's tight per-glyph outline.
// Reading order, text content, and x-segmentation are faithful; per-rect height
// is an over-estimate for embedded fonts. This is the dominant, documented gap
// versus PDFium and is tracked for the glyph-outline follow-up.
package text

import (
	"math"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/font"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/page"
)

// Constants from the cpdf_textpage.cpp anonymous namespace.
const (
	kDefaultFontSize       = float32(1.0)
	kSizeEpsilon           = float32(0.01)
	kEpsilon               = float32(0.0001) // GetTextObjectWritingMode
	kThreshold             = float32(0.0872) // GetTextObjectWritingMode
	kTextCharRatioGapDelta = float32(0.07)   // ProcessTextObjectItems
)

// textOrientation mirrors CPDF_TextPage::TextOrientation.
type textOrientation uint8

const (
	orientUnknown textOrientation = iota
	orientHorizontal
	orientVertical
)

// generateCharacter mirrors CPDF_TextPage::GenerateCharacter.
type generateCharacter uint8

const (
	genNone generateCharacter = iota
	genSpace
	genLineBreak
	genHyphen
)

// markedContentState mirrors CPDF_TextPage::MarkedContentState.
type markedContentState uint8

const (
	mcPass markedContentState = iota
	mcDone
	mcDelay
)

// charSegment mirrors TextPageCharSegment {index, count}.
type charSegment struct{ index, count int }

// transformedTextObject mirrors CPDF_TextPage::TransformedTextObject.
type transformedTextObject struct {
	textObj    *page.TextObject
	formMatrix crt.Matrix
}

// Rect is the FPDFText_CountRects/GetRect output the backend consumes.
//
// Box is in PDF USER SPACE, Y-UP (origin bottom-left) — see the package doc.
// Text is the concatenation of the group's character unicodes (in stored,
// reading order).
type Rect struct {
	Text     string
	Box      crt.FloatRect
	FontSize float64

	// Font formatting from the rect's text object, surfaced for the
	// LineElement pipeline (bold/italic/code run splitting). Faithful to
	// the PDF /FontDescriptor (Flags/ItalicAngle/StemV) via font.Font.
	FontName   string
	FontWeight int
	FontFlags  int
}

// TextPage is the ported CPDF_TextPage.
type TextPage struct {
	page          *page.Page
	rtl           bool
	displayMatrix crt.Matrix

	charIndices []charSegment
	charList    []charInfo // char_list_ — the page char stream (rects + text)
	textBuf     []rune     // text_buf_ — the final reading-order string

	tempCharList []charInfo // temp_char_list_ — current visual line, pre-BiDi
	tempTextBuf  []rune     // temp_text_buf_

	prevTextObj *page.TextObject
	prevMatrix  crt.Matrix

	mTextObjects []transformedTextObject
	textlineDir  textOrientation
	curlineRect  crt.FloatRect

	selRects []crt.FloatRect // cached by CountRects, read by GetRect
}

// New ports CPDF_TextPage(page, rtl): capture the display matrix BEFORE Init
// (the newline-suppression heuristic keys off its exact float thresholds).
func New(p *page.Page, rtl bool) *TextPage {
	tp := &TextPage{
		page:          p,
		rtl:           rtl,
		displayMatrix: p.DisplayMatrix(),
		textlineDir:   orientUnknown,
	}
	tp.init()
	return tp
}

// init ports CPDF_TextPage::Init (cpdf_textpage.cpp:378).
func (tp *TextPage) init() {
	// Pre-size the char/text buffers to the page's total item count: charList
	// otherwise grows by repeated doubling across a dense page, and the
	// discarded backing arrays dominate allocation (88-byte CharInfo elements).
	// charList/textBuf accumulate the whole page; the temp buffers hold only the
	// current line (reset per line) so they are left to grow to the longest line.
	if n := estimateCharCount(tp.page.Objects()); n > 0 {
		tp.charList = make([]charInfo, 0, n)
		tp.textBuf = make([]rune, 0, n)
	}
	tp.processObject()
	tp.buildCharIndices()
}

// estimateCharCount sums the item counts of all text objects (recursing into
// forms) to size the char buffers up front.
func estimateCharCount(objs []page.PageObject) int {
	total := 0
	for _, o := range objs {
		switch v := o.(type) {
		case *page.TextObject:
			total += v.CountItems()
		case *page.FormObject:
			total += estimateCharCount(v.Objects())
		}
	}
	return total
}

// buildCharIndices ports the char_indices_ (text-index <-> char-index) build of
// Init: kGenerated or IsNormalCharacter chars are printable (count++),
// non-printable chars are gaps (advance index).
func (tp *TextPage) buildCharIndices() {
	nCount := tp.CountChars()
	if nCount != 0 {
		tp.charIndices = append(tp.charIndices, charSegment{0, 0})
	}

	skipped := false
	for i := range nCount {
		ci := tp.charList[i]
		if ci.charType == charGenerated || isNormalCharacter(ci) {
			tp.charIndices[len(tp.charIndices)-1].count++
			skipped = true
		} else {
			if skipped {
				tp.charIndices = append(tp.charIndices, charSegment{i + 1, 0})
				skipped = false
			} else {
				tp.charIndices[len(tp.charIndices)-1].index = i + 1
			}
		}
	}
}

// CountChars ports CPDF_TextPage::CountChars.
func (tp *TextPage) CountChars() int { return len(tp.charList) }

// GetAllText returns the full reading-order text (text_buf_ as a Go string).
func (tp *TextPage) GetAllText() string { return string(tp.textBuf) }

// CharIndexFromTextIndex ports CPDF_TextPage::CharIndexFromTextIndex.
func (tp *TextPage) CharIndexFromTextIndex(textIndex int) int {
	count := 0
	for _, info := range tp.charIndices {
		count += info.count
		if count > textIndex {
			return textIndex - count + info.count + info.index
		}
	}
	return -1
}

// TextIndexFromCharIndex ports CPDF_TextPage::TextIndexFromCharIndex.
func (tp *TextPage) TextIndexFromCharIndex(charIndex int) int {
	count := 0
	for _, info := range tp.charIndices {
		textIndex := charIndex - info.index
		if textIndex < info.count {
			if textIndex >= 0 {
				return textIndex + count
			}
			return -1
		}
		count += info.count
	}
	return -1
}

// --- ProcessObject / form recursion / orientation ---

// processObject ports CPDF_TextPage::ProcessObject (cpdf_textpage.cpp:742).
func (tp *TextPage) processObject() {
	// PDFium's CPDF_Page is already parsed when the TextPage is built; here
	// parsing is lazy and triggered by Objects(), so force it before reading the
	// active-object count (which iterates the parsed list).
	objs := tp.page.Objects()
	if tp.page.GetActivePageObjectCount() == 0 {
		return
	}

	tp.textlineDir = tp.findTextlineFlowOrientation()
	for idx, obj := range objs {
		if !obj.IsActive() {
			continue
		}
		switch o := obj.(type) {
		case *page.TextObject:
			tp.bufferTextObject(o, crt.IdentityMatrix(), objs, idx)
		case *page.FormObject:
			tp.processFormObject(o, crt.IdentityMatrix())
		}
	}
	for i := range tp.mTextObjects {
		tp.processTextObject(tp.mTextObjects[i])
	}
	tp.mTextObjects = nil
	tp.closeTempLine()
}

// processFormObject ports CPDF_TextPage::ProcessFormObject (768): recurse with
// curFormMatrix = form's own matrix LEFT-multiplied by the inherited matrix.
func (tp *TextPage) processFormObject(formObj *page.FormObject, formMatrix crt.Matrix) {
	curFormMatrix := formObj.FormMatrix().Multiply(formMatrix)
	children := formObj.Objects()
	for idx, obj := range children {
		if !obj.IsActive() {
			continue
		}
		switch o := obj.(type) {
		case *page.TextObject:
			tp.bufferTextObject(o, curFormMatrix, children, idx)
		case *page.FormObject:
			tp.processFormObject(o, curFormMatrix)
		}
	}
}

// findTextlineFlowOrientation ports FindTextlineFlowOrientation (654).
func (tp *TextPage) findTextlineFlowOrientation() textOrientation {
	nPageWidth := int(tp.page.Width())
	nPageHeight := int(tp.page.Height())
	if nPageWidth <= 0 || nPageHeight <= 0 {
		return orientUnknown
	}

	horizontalMask := make([]bool, nPageWidth)
	verticalMask := make([]bool, nPageHeight)
	fLineHeight := float32(0)
	nStartH := nPageWidth
	nEndH := 0
	nStartV := nPageHeight
	nEndV := 0
	for _, obj := range tp.page.Objects() {
		to, ok := obj.(*page.TextObject)
		if !ok || !to.IsActive() {
			continue
		}
		rect := to.Rect()
		minH := int(clampf(rect.Left, 0, float32(nPageWidth)))
		maxH := int(clampf(rect.Right, 0, float32(nPageWidth)))
		minV := int(clampf(rect.Bottom, 0, float32(nPageHeight)))
		maxV := int(clampf(rect.Top, 0, float32(nPageHeight)))
		if minH >= maxH || minV >= maxV {
			continue
		}
		for i := minH; i < maxH; i++ {
			horizontalMask[i] = true
		}
		for i := minV; i < maxV; i++ {
			verticalMask[i] = true
		}
		nStartH = min(nStartH, minH)
		nEndH = max(nEndH, maxH)
		nStartV = min(nStartV, minV)
		nEndV = max(nEndV, maxV)
		if fLineHeight <= 0 {
			fLineHeight = rect.Height()
		}
	}
	// C++: int32_t nDoubleLineHeight = 2 * fLineHeight; — the 2*fLineHeight is a
	// float, truncated to int on assignment (truncate AFTER the multiply).
	nDoubleLineHeight := int(2 * fLineHeight)
	if (nEndV - nStartV) < nDoubleLineHeight {
		return orientHorizontal
	}
	if (nEndH - nStartH) < nDoubleLineHeight {
		return orientVertical
	}
	nSumH := maskPercentFilled(horizontalMask, nStartH, nEndH)
	if nSumH > 0.8 {
		return orientHorizontal
	}
	nSumV := maskPercentFilled(verticalMask, nStartV, nEndV)
	if nSumH > nSumV {
		return orientHorizontal
	}
	if nSumH < nSumV {
		return orientVertical
	}
	return orientUnknown
}

func maskPercentFilled(mask []bool, start, end int) float32 {
	if start >= end {
		return 0
	}
	count := 0
	for i := start; i < end; i++ {
		if mask[i] {
			count++
		}
	}
	return float32(count) / float32(end-start)
}

// --- buffering ProcessTextObject (same-visual-line sort) ---

// bufferTextObject ports the 4-arg ProcessTextObject (881): fill mTextObjects,
// ascending-x within a visual line, flushing on a y-jump.
func (tp *TextPage) bufferTextObject(textObj *page.TextObject, formMatrix crt.Matrix,
	objList []page.PageObject, objPosIdx int) {
	if textObj.CountItems() == 0 {
		return
	}

	count := len(tp.mTextObjects)
	newObj := transformedTextObject{textObj: textObj, formMatrix: formMatrix}
	if count == 0 {
		tp.mTextObjects = append(tp.mTextObjects, newObj)
		return
	}
	if tp.isSameAsPreTextObject(textObj, objList, objPosIdx) {
		return
	}

	prevObj := tp.mTextObjects[count-1]
	nItem := prevObj.textObj.CountItems()
	if nItem == 0 {
		return
	}

	prevItems := prevObj.textObj.Items()
	item := prevItems[nItem-1]
	prevWidth := getCharWidthInt(item.CharCode, prevObj.textObj.Font())
	pw := float32(prevWidth) * prevObj.textObj.FontSize()
	prevWidthF := pw / 1000
	prevMatrix := prevObj.textObj.TextMatrix().Multiply(prevObj.formMatrix)
	prevWidthF = prevMatrix.TransformDistance(absf(prevWidthF))

	thisItem := textObj.Items()[0]
	thisWidthInt := getCharWidthInt(thisItem.CharCode, textObj.Font())
	tw := float32(thisWidthInt) * textObj.FontSize()
	thisWidthF := tw / 1000
	thisWidthF = absf(thisWidthF)
	thisMatrix := textObj.TextMatrix().Multiply(formMatrix)
	thisWidthF = thisMatrix.TransformDistance(absf(thisWidthF))

	threshold := maxf(prevWidthF, thisWidthF) / 4
	prevPos := tp.displayMatrix.Transform(prevObj.formMatrix.Transform(objPos(prevObj.textObj)))
	thisPos := tp.displayMatrix.Transform(formMatrix.Transform(objPos(textObj)))
	if absf(thisPos.Y-prevPos.Y) > threshold*2 {
		for i := range count {
			tp.processTextObject(tp.mTextObjects[i])
		}
		tp.mTextObjects = tp.mTextObjects[:0]
		tp.mTextObjects = append(tp.mTextObjects, newObj)
		return
	}

	for i := count; i > 0; i-- {
		prevTextObj := tp.mTextObjects[i-1]
		newPrevPos := tp.displayMatrix.Transform(
			prevTextObj.formMatrix.Transform(objPos(prevTextObj.textObj)))
		if thisPos.X >= newPrevPos.X {
			tp.mTextObjects = insertTransformed(tp.mTextObjects, i, newObj)
			return
		}
	}
	tp.mTextObjects = insertTransformed(tp.mTextObjects, 0, newObj)
}

func insertTransformed(s []transformedTextObject, i int, v transformedTextObject) []transformedTextObject {
	s = append(s, transformedTextObject{})
	copy(s[i+1:], s[i:])
	s[i] = v
	return s
}

// isSameAsPreTextObject ports IsSameAsPreTextObject (1538): look back up to 5
// preceding text objects in the same holder for a duplicate/overprint.
func (tp *TextPage) isSameAsPreTextObject(textObj *page.TextObject,
	objList []page.PageObject, iter int) bool {
	i := 0
	for i < 5 && iter != 0 {
		iter--
		other := objList[iter]
		otherText, ok := other.(*page.TextObject)
		if !ok || otherText == textObj {
			continue
		}
		if tp.isSameTextObject(otherText, textObj) {
			return true
		}
		i++
	}
	return false
}

// isSameTextObject ports IsSameTextObject (1479).
func (tp *TextPage) isSameTextObject(textObj1, textObj2 *page.TextObject) bool {
	if textObj1 == nil || textObj2 == nil {
		return false
	}
	rcPreObj := textObj2.Rect()
	rcCurObj := textObj1.Rect()
	if rcPreObj.IsEmpty() && rcCurObj.IsEmpty() {
		dbXdif := absf(rcPreObj.Left - rcCurObj.Left)
		nCount := len(tp.charList)
		if nCount >= 2 {
			dbSpace := tp.charList[nCount-2].charBox.Width()
			if dbXdif > dbSpace {
				return false
			}
		}
	}
	if !rcPreObj.IsEmpty() || !rcCurObj.IsEmpty() {
		rcPreObj.Intersect(rcCurObj)
		if rcPreObj.IsEmpty() {
			return false
		}
		if absf(rcPreObj.Width()-rcCurObj.Width()) > rcCurObj.Width()/2 {
			return false
		}
		if textObj2.FontSize() != textObj1.FontSize() {
			return false
		}
	}

	nPreCount := textObj2.CountItems()
	if nPreCount != textObj1.CountItems() {
		return false
	}
	if nPreCount == 0 {
		return true
	}

	items1 := textObj1.Items()
	items2 := textObj2.Items()
	var itemPer page.TextItem
	for i := range nPreCount {
		itemPer = items2[i]
		if items1[i].CharCode != items2[i].CharCode {
			return false
		}
	}

	diff := crt.PointF{
		X: objPos(textObj1).X - objPos(textObj2).X,
		Y: objPos(textObj1).Y - objPos(textObj2).Y,
	}
	fontSize := textObj2.FontSize()
	charSize := float32(getCharWidthInt(itemPer.CharCode, textObj2.Font()))
	maxPreSize := maxf(maxf(rcPreObj.Height(), rcPreObj.Width()), fontSize)
	return absf(diff.X) <= 0.9*charSize*fontSize/1000 && absf(diff.Y) <= maxPreSize/8
}

// --- consuming ProcessTextObject ---

// processTextObject ports the TransformedTextObject overload (1082).
func (tp *TextPage) processTextObject(obj transformedTextObject) {
	textObj := obj.textObj
	if textObj.CountItems() == 0 {
		return
	}

	formMatrix := obj.formMatrix
	ePreMKC := tp.preMarkedContent(textObj)
	if ePreMKC == mcDone {
		tp.prevTextObj = textObj
		tp.prevMatrix = formMatrix
		return
	}

	if tp.prevTextObj != nil {
		typ := tp.processInsertObject(textObj, formMatrix)
		if typ == genLineBreak {
			tp.curlineRect = textObj.Rect()
		} else {
			tp.curlineRect.Union(textObj.Rect())
		}
		if !tp.processGenerateCharacter(typ, textObj, formMatrix) {
			return
		}
	} else {
		tp.curlineRect = textObj.Rect()
	}

	if ePreMKC == mcDelay {
		tp.processMarkedContent(obj)
		tp.prevTextObj = textObj
		tp.prevMatrix = formMatrix
		return
	}

	tp.prevTextObj = textObj
	tp.prevMatrix = formMatrix

	bR2L := isRightToLeft(textObj)
	matrix := textObj.TextMatrix().Multiply(formMatrix)
	// Store both products in float32 locals before subtracting (no FMA).
	ad := matrix.A * matrix.D
	bc := matrix.B * matrix.C
	bIsBidiAndMirrorInverse := bR2L && (ad-bc) < 0
	iBufStartAppend := len(tp.tempTextBuf)
	iCharListStartAppend := len(tp.tempCharList)

	tp.processTextObjectItems(textObj, formMatrix, matrix)
	if bIsBidiAndMirrorInverse {
		tp.swapTempTextBuf(iCharListStartAppend, iBufStartAppend)
	}
}

// swapTempTextBuf ports SwapTempTextBuf (1064): reverse the just-appended temp
// char list and temp text buffer ranges in place.
func (tp *TextPage) swapTempTextBuf(iCharListStartAppend, iBufStartAppend int) {
	if iCharListStartAppend < len(tp.tempCharList) {
		fwd := iCharListStartAppend
		rev := len(tp.tempCharList) - 1
		for fwd < rev {
			tp.tempCharList[fwd], tp.tempCharList[rev] = tp.tempCharList[rev], tp.tempCharList[fwd]
			fwd++
			rev--
		}
	}
	if iBufStartAppend < len(tp.tempTextBuf) {
		i := iBufStartAppend
		j := len(tp.tempTextBuf) - 1
		for i < j {
			tp.tempTextBuf[i], tp.tempTextBuf[j] = tp.tempTextBuf[j], tp.tempTextBuf[i]
			i++
			j--
		}
	}
}

// isRightToLeft ports the namespace IsRightToLeft (165).
func isRightToLeft(textObj *page.TextObject) bool {
	f := textObj.Font()
	items := textObj.Items()
	for _, item := range items {
		switch getBidiClass(textItemUnicode(f, item)) {
		case bidiR, bidiAL:
			return textRunDirection(f, items) == dirRight
		}
	}
	return false
}

func textRunDirection(f *font.Font, items []page.TextItem) bidiDirection {
	str := make([]rune, 0, len(items))
	for _, item := range items {
		if wChar := textItemUnicode(f, item); wChar != 0 {
			str = append(str, wChar)
		}
	}
	return newBidiString(str).overallDir()
}

func textItemUnicode(f *font.Font, item page.TextItem) rune {
	if item.CharCode == page.InvalidCharCode {
		return 0
	}
	if f != nil {
		if wChar := f.UnicodeFromCharCode(item.CharCode); wChar != 0 {
			return wChar
		}
	}
	return rune(item.CharCode)
}

// --- helpers shared by float math ---

func clampf(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func absf(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func isFloatZero(f float32) bool { return f < 0.0001 && f > -0.0001 }

func hypotf(a, b float32) float32 {
	return float32(math.Hypot(float64(a), float64(b)))
}
