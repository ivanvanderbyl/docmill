// The generated-character state machine, per-char emission, marked-content
// (ActualText), BiDi flush, and rect/text consumers. Ported from
// core/fpdftext/cpdf_textpage.cpp @ pdfium 0db284a42.
package text

import (
	"unicode"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/font"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/page"
)

// isPrint ports the C isprint() used in the ActualText printability scans: a
// printable ASCII character (0x20..0x7E). PDFium calls isprint with values
// already gated to <= 0x80, matching this.
func isPrint(c rune) bool { return c >= 0x20 && c <= 0x7E }

// isWAlpha / isWAlnum port FXSYS_iswalpha / FXSYS_iswalnum used by IsHyphen.
func isWAlpha(c rune) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		(c >= 0x00C0 && c <= 0x024F) || // Latin-1 supplement / extended letters
		(c >= 0x0370 && c <= 0x03FF) || // Greek
		(c >= 0x0400 && c <= 0x04FF) // Cyrillic
}

func isWAlnum(c rune) bool {
	return isWAlpha(c) || (c >= '0' && c <= '9')
}

// getPrevCharInfo ports GetPrevCharInfo (1196): temp list back, else char list
// back, else nil.
func (tp *TextPage) getPrevCharInfo() *charInfo {
	if n := len(tp.tempCharList); n > 0 {
		return &tp.tempCharList[n-1]
	}
	if n := len(tp.charList); n > 0 {
		return &tp.charList[n-1]
	}
	return nil
}

// findPreviousTextObject ports FindPreviousTextObject (1051).
func (tp *TextPage) findPreviousTextObject() {
	prev := tp.getPrevCharInfo()
	if prev == nil {
		return
	}
	if prev.textObject != nil {
		tp.prevTextObj = prev.textObject
	}
}

// getTextObjectWritingMode ports GetTextObjectWritingMode (1134).
func (tp *TextPage) getTextObjectWritingMode(textObj *page.TextObject) textOrientation {
	if textObj == nil {
		return tp.textlineDir
	}
	nChars := textObj.CountChars()
	if nChars <= 1 {
		return tp.textlineDir
	}
	items := textObj.Items()
	// GetCharInfo(i) is the i-th NON-sentinel item; map char index -> item index.
	first := charInfoItem(items, 0)
	last := charInfoItem(items, nChars-1)
	tm := textObj.TextMatrix()
	firstO := tm.Transform(first.Origin)
	lastO := tm.Transform(last.Origin)
	dX := absf(lastO.X - firstO.X)
	dY := absf(lastO.Y - firstO.Y)
	if dX <= kEpsilon && dY <= kEpsilon {
		return orientUnknown
	}
	// CFX_VectorF(dX, dY).Normalize().
	length := hypotf(dX, dY)
	vx := dX
	vy := dY
	if length != 0 {
		vx = dX / length
		vy = dY / length
	}
	bXUnder := vx <= kThreshold
	if vy <= kThreshold {
		if bXUnder {
			return tp.textlineDir
		}
		return orientHorizontal
	}
	if bXUnder {
		return orientVertical
	}
	return tp.textlineDir
}

// charInfoItem returns the charIndex-th non-sentinel item (CPDF_TextObject::
// GetCharInfo / GetItemInfo over real chars).
func charInfoItem(items []page.TextItem, charIndex int) page.TextItem {
	count := 0
	for _, it := range items {
		if it.CharCode == page.InvalidCharCode {
			continue
		}
		if count == charIndex {
			return it
		}
		count++
	}
	return page.TextItem{}
}

// firstUnicode resolves item 0's leading unicode the way ProcessInsertObject
// does: font unicode, else the raw char code as a rune.
func firstUnicode(textObj *page.TextObject, charCode uint32) rune {
	f := textObj.Font()
	var u rune
	if f != nil {
		u = f.UnicodeFromCharCode(charCode)
	}
	if u == 0 {
		u = rune(charCode)
	}
	return u
}

// isHyphen ports CPDF_TextPage::IsHyphen (1164): a trailing hyphen at the end of
// the current temp/main line, joining an alpha run to an alnum next char.
func (tp *TextPage) isHyphen(curChar rune) bool {
	curText := tp.tempTextBuf
	if len(curText) == 0 {
		curText = tp.textBuf
	}
	if len(curText) == 0 {
		return false
	}
	// rbegin()..rend(): walk from the end skipping trailing spaces, but stop one
	// before the start (the (iter+1)!=rend() guard keeps at least one char).
	idx := len(curText) - 1
	for idx > 0 && curText[idx] == 0x20 {
		idx--
	}
	if !isHyphenCode(curText[idx]) {
		return false
	}
	if idx > 0 {
		prev := curText[idx-1]
		if isWAlpha(prev) && isWAlnum(curChar) {
			return true
		}
	}
	prev := tp.getPrevCharInfo()
	return prev != nil && prev.charType == charPiece && isHyphenCode(prev.unicode)
}

// processInsertObject ports ProcessInsertObject (1202): the kNone/kSpace/
// kLineBreak/kHyphen decision between prevTextObj and textObj.
func (tp *TextPage) processInsertObject(textObj *page.TextObject, formMatrix crt.Matrix) generateCharacter {
	tp.findPreviousTextObject()
	writingMode := tp.getTextObjectWritingMode(textObj)
	if writingMode == orientUnknown {
		writingMode = tp.getTextObjectWritingMode(tp.prevTextObj)
	}

	nItem := tp.prevTextObj.CountItems()
	if nItem == 0 {
		return genNone
	}

	prevItems := tp.prevTextObj.Items()
	prevItem := prevItems[nItem-1]
	item := textObj.Items()[0]
	thisRect := textObj.Rect()
	prevRect := tp.prevTextObj.Rect()
	curChar := firstUnicode(textObj, item.CharCode)

	switch writingMode {
	case orientHorizontal:
		if endHorizontalLine(thisRect, prevRect) {
			if tp.isHyphen(curChar) {
				return genHyphen
			}
			return genLineBreak
		}
	case orientVertical:
		if endVerticalLine(thisRect, prevRect, tp.curlineRect, textObj.FontSize(), tp.prevTextObj.FontSize()) {
			if tp.isHyphen(curChar) {
				return genHyphen
			}
			return genLineBreak
		}
	}

	lastPos := prevItem.Origin.X
	nLastWidth := getCharWidthInt(prevItem.CharCode, tp.prevTextObj.Font())
	lw := float32(nLastWidth) * tp.prevTextObj.FontSize()
	lastWidth := absf(lw / 1000)
	nThisWidth := getCharWidthInt(item.CharCode, textObj.Font())
	tw := float32(nThisWidth) * textObj.FontSize()
	thisWidth := absf(tw / 1000)
	threshold := maxf(lastWidth, thisWidth) / 4

	prevMatrix := tp.prevTextObj.TextMatrix().Multiply(tp.prevMatrix)
	prevReverse := prevMatrix.GetInverse()
	pos := prevReverse.Transform(formMatrix.Transform(objPos(textObj)))
	if lastWidth < thisWidth {
		threshold = prevReverse.TransformDistance(threshold)
	}

	bNewline := false
	if writingMode == orientHorizontal {
		rect := tp.prevTextObj.Rect()
		rectHeight := rect.Height()
		rect.Normalize()
		if (rect.IsEmpty() && rectHeight > 5) ||
			((pos.Y > threshold*2 || pos.Y < threshold*-3) &&
				(absf(pos.Y) >= 1 || absf(pos.Y) > absf(pos.X))) {
			bNewline = true
			if nItem > 1 {
				tempItem := prevItems[0]
				m := tp.prevTextObj.TextMatrix()
				if prevItem.Origin.X > tempItem.Origin.X &&
					tp.displayMatrix.A > 0.9 && tp.displayMatrix.B < 0.1 &&
					tp.displayMatrix.C < 0.1 && tp.displayMatrix.D < -0.9 &&
					m.B < 0.1 && m.C < 0.1 {
					pr := tp.prevTextObj.Rect()
					re := crt.NewFloatRect(0, pr.Bottom, 1000, pr.Top)
					if re.ContainsPoint(objPos(textObj)) {
						bNewline = false
					} else {
						tr := textObj.Rect()
						re2 := crt.NewFloatRect(0, tr.Bottom, 1000, tr.Top)
						if re2.ContainsPoint(objPos(tp.prevTextObj)) {
							bNewline = false
						}
					}
				}
			}
		}
	}
	if bNewline {
		if tp.isHyphen(curChar) {
			return genHyphen
		}
		return genLineBreak
	}

	if textObj.CountChars() == 1 && isHyphenCode(curChar) && tp.isHyphen(curChar) {
		return genHyphen
	}

	if curChar == ' ' {
		return genNone
	}

	preChar := lastUnicode(tp.prevTextObj, prevItem.CharCode)
	if preChar == ' ' {
		return genNone
	}

	matrix := textObj.TextMatrix().Multiply(formMatrix)
	threshold2 := float32(max(nLastWidth, nThisWidth))
	threshold2 = normalizeThreshold(threshold2, 400, 700, 800)
	if nLastWidth >= nThisWidth {
		threshold2 *= absf(tp.prevTextObj.FontSize())
	} else {
		threshold2 *= absf(textObj.FontSize())
		threshold2 = matrix.TransformDistance(threshold2)
		threshold2 = prevReverse.TransformDistance(threshold2)
	}
	threshold2 /= 1000
	if (threshold2 < 1.4881 && threshold2 > 1.4879) ||
		(threshold2 < 1.39001 && threshold2 > 1.38999) {
		threshold2 *= 1.5
	}
	if generateSpace(pos, lastPos, thisWidth, lastWidth, threshold2) {
		return genSpace
	}
	return genNone
}

// lastUnicode resolves PrevStr.Back() (the last unicode of prevItem's char).
func lastUnicode(textObj *page.TextObject, charCode uint32) rune {
	f := textObj.Font()
	if f == nil {
		return 0
	}
	s := f.UnicodeStringFromCharCode(charCode)
	if s == "" {
		// WideString::Back() on an empty string is undefined in C++; PDFium's
		// WideString returns 0 there in practice for the extraction path.
		return 0
	}
	r := []rune(s)
	return r[len(r)-1]
}

// normalizeThreshold ports NormalizeThreshold (46).
func normalizeThreshold(threshold float32, t1, t2, t3 int) float32 {
	if threshold < float32(t1) {
		return threshold / 2
	}
	if threshold < float32(t2) {
		return threshold / 4
	}
	if threshold < float32(t3) {
		return threshold / 5
	}
	return threshold / 6
}

// generateSpace ports GenerateSpace (234).
func generateSpace(pos crt.PointF, lastPos, thisWidth, lastWidth, threshold float32) bool {
	if absf(lastPos+lastWidth-pos.X) <= threshold {
		return false
	}
	thresholdPos := threshold + lastWidth
	posDifference := pos.X - lastPos
	if absf(posDifference) > thresholdPos {
		return true
	}
	if pos.X < 0 && -thresholdPos > posDifference {
		return true
	}
	return posDifference > thisWidth+lastWidth
}

// endHorizontalLine ports EndHorizontalLine (254).
func endHorizontalLine(thisRect, prevRect crt.FloatRect) bool {
	if thisRect.Height() <= 4.5 || prevRect.Height() <= 4.5 {
		return false
	}
	top := minf(thisRect.Top, prevRect.Top)
	bottom := maxf(thisRect.Bottom, prevRect.Bottom)
	return bottom >= top
}

// endVerticalLine ports EndVerticalLine (265).
func endVerticalLine(thisRect, prevRect, curlineRect crt.FloatRect, thisFontSize, prevFontSize float32) bool {
	if thisRect.Width() <= thisFontSize*0.1 || prevRect.Width() <= prevFontSize*0.1 {
		return false
	}
	left := maxf(thisRect.Left, curlineRect.Left)
	right := minf(thisRect.Right, curlineRect.Right)
	return right <= left
}

// processGenerateCharacter ports ProcessGenerateCharacter (1328). Returns
// whether the caller should continue (false drops a standalone hyphen object).
func (tp *TextPage) processGenerateCharacter(typ generateCharacter,
	textObj *page.TextObject, formMatrix crt.Matrix) bool {
	switch typ {
	case genNone:
		return true
	case genSpace:
		tp.appendGeneratedCharacter(' ', formMatrix, true)
		return true
	case genLineBreak:
		tp.closeTempLine()
		if len(tp.textBuf) != 0 {
			tp.appendGeneratedCharacter('\r', formMatrix, false)
			tp.appendGeneratedCharacter('\n', formMatrix, false)
		}
		return true
	case genHyphen:
		if textObj.CountChars() == 1 {
			item := charInfoItem(textObj.Items(), 0)
			curChar := firstUnicode(textObj, item.CharCode)
			if isHyphenCode(curChar) {
				return false
			}
		}
		for len(tp.tempTextBuf) > 0 && tp.tempTextBuf[len(tp.tempTextBuf)-1] == 0x20 {
			tp.tempTextBuf = tp.tempTextBuf[:len(tp.tempTextBuf)-1]
			tp.tempCharList = tp.tempCharList[:len(tp.tempCharList)-1]
		}
		// charinfo = temp_char_list_.back() (by reference); delete last temp char.
		tp.tempTextBuf = tp.tempTextBuf[:len(tp.tempTextBuf)-1]
		last := len(tp.tempCharList) - 1
		tp.tempCharList[last].charType = charHyphen
		tp.tempCharList[last].unicode = 0x2
		tp.tempTextBuf = append(tp.tempTextBuf, 0xfffe)
		return true
	}
	return true // NOTREACHED
}

// appendGeneratedCharacter ports AppendGeneratedCharacter (725).
func (tp *TextPage) appendGeneratedCharacter(unicode rune, formMatrix crt.Matrix, useTempBuffer bool) {
	ci, ok := tp.generateCharInfo(unicode, formMatrix)
	if !ok {
		return
	}
	if useTempBuffer {
		tp.tempTextBuf = append(tp.tempTextBuf, unicode)
		tp.tempCharList = append(tp.tempCharList, ci)
	} else {
		tp.textBuf = append(tp.textBuf, unicode)
		tp.charList = append(tp.charList, ci)
	}
}

// generateCharInfo ports GenerateCharInfo (1559). ok==false maps to nullopt.
func (tp *TextPage) generateCharInfo(unicode rune, formMatrix crt.Matrix) (charInfo, bool) {
	prev := tp.getPrevCharInfo()
	if prev == nil {
		return charInfo{}, false
	}
	preWidth := 0
	if prev.textObject != nil && prev.charCode != page.InvalidCharCode {
		preWidth = getCharWidthInt(prev.charCode, prev.textObject.Font())
	}
	var fFontSize float32
	if prev.textObject != nil {
		fFontSize = prev.textObject.FontSize()
	} else {
		fFontSize = prev.charBox.Height()
	}
	if fFontSize == 0 {
		fFontSize = kDefaultFontSize
	}
	prod := float32(preWidth) * fFontSize
	originX := prev.origin.X + prod/1000
	origin := crt.PointF{X: originX, Y: prev.origin.Y}
	box := crt.NewFloatRect(origin.X, origin.Y, origin.X, origin.Y)
	return newCharInfo(charGenerated, page.InvalidCharCode, unicode, origin, box, formMatrix, nil), true
}

// --- marked content (ActualText) ---

// preMarkedContent ports PreMarkedContent (951).
func (tp *TextPage) preMarkedContent(textObj *page.TextObject) markedContentState {
	marks := textObj.ContentMarks()
	nContentMarks := len(marks)
	if nContentMarks == 0 {
		return mcPass
	}

	var actualText string
	bExist := false
	var dict = (any)(nil)
	for i := range nContentMarks {
		params := marks[i].Params
		if params == nil {
			continue
		}
		dict = params
		if params.KeyExist("ActualText") {
			if s := params.GetStringFor("ActualText"); s != nil {
				bExist = true
				actualText = s.GetUnicodeText()
			}
		}
	}
	if !bExist {
		return mcPass
	}

	if tp.prevTextObj != nil {
		prevMarks := tp.prevTextObj.ContentMarks()
		if len(prevMarks) == nContentMarks && nContentMarks > 0 {
			lastParam := any(prevMarks[nContentMarks-1].Params)
			if lastParam == dict {
				return mcDone
			}
		}
	}

	if actualText == "" {
		return mcPass
	}

	bExist = false
	for _, wChar := range actualText {
		if (wChar > 0x80 && wChar < 0xFFFD) || (wChar <= 0x80 && isPrint(wChar)) {
			bExist = true
			break
		}
	}
	if !bExist {
		return mcDone
	}
	return mcDelay
}

// processMarkedContent ports ProcessMarkedContent (1015).
func (tp *TextPage) processMarkedContent(obj transformedTextObject) {
	textObj := obj.textObj
	marks := textObj.ContentMarks()
	var actualText string
	for _, m := range marks {
		if m.Params != nil {
			actualText = m.Params.GetUnicodeTextFor("ActualText")
		}
	}
	runes := []rune(actualText)
	if len(runes) == 0 {
		return
	}

	bR2L := isRightToLeft(textObj)
	matrix := textObj.TextMatrix().Multiply(obj.formMatrix)
	rect := markedContentSourceRect(textObj, matrix)
	var step float32
	n := float32(len(runes))
	if bR2L {
		rect.Left = rect.Right - (rect.Width() / n)
		step = -rect.Width()
	} else {
		rect.Right = rect.Left + (rect.Width() / n)
		step = rect.Width()
	}

	for k, wChar := range runes {
		if wChar <= 0x80 && !isPrint(wChar) {
			wChar = 0x20
		}
		if wChar >= 0xFFFD {
			continue
		}
		charBox := rect
		charBox.Translate(float32(k)*step, 0)
		tp.tempTextBuf = append(tp.tempTextBuf, wChar)
		ci := newCharInfo(charPiece, page.InvalidCharCode, wChar, objPos(textObj), charBox, matrix, textObj)
		tp.tempCharList = append(tp.tempCharList, ci)
	}
}

func markedContentSourceRect(textObj *page.TextObject, matrix crt.Matrix) crt.FloatRect {
	var rect crt.FloatRect
	haveRect := false
	for _, item := range textObj.Items() {
		if item.CharCode == page.InvalidCharCode {
			continue
		}
		charBox := textItemCharBox(textObj, item, matrix)
		if charBox.Width() < kSizeEpsilon || charBox.Height() < kSizeEpsilon {
			continue
		}
		charBox.Normalize()
		if !haveRect {
			rect = charBox
			haveRect = true
			continue
		}
		rect.Union(charBox)
	}
	if haveRect {
		return rect
	}
	rect = textObj.Rect()
	rect.Normalize()
	return rect
}

// --- per-char emission ---

// processTextObjectItems ports ProcessTextObjectItems (1373).
func (tp *TextPage) processTextObjectItems(textObj *page.TextObject, formMatrix, matrix crt.Matrix) {
	baseSpace := calculateBaseSpace(textObj, matrix) + calculateBaseSpaceAdjustment(textObj, matrix)
	f := textObj.Font()

	spacing := float32(0)
	items := textObj.Items()
	nItems := len(items)
	for i := range nItems {
		item := items[i]
		if item.CharCode == 0xffffffff {
			str := tp.tempTextBuf
			if len(str) == 0 {
				str = tp.textBuf
			}
			if len(str) != 0 && str[len(str)-1] != ' ' {
				fontsizeH := objFontSizeH(textObj)
				prod := -fontsizeH * item.Origin.X
				spacing = prod / 1000
			}
			continue
		}

		spacing -= baseSpace

		if spacing != 0 && i > 0 {
			threshold := calculateSpaceThreshold(f, objFontSizeH(textObj), item.CharCode)
			if threshold != 0 && spacing != 0 && spacing >= threshold {
				tp.tempTextBuf = append(tp.tempTextBuf, ' ')
				origin := matrix.Transform(item.Origin)
				box := crt.NewFloatRect(origin.X, origin.Y, origin.X, origin.Y)
				tp.tempCharList = append(tp.tempCharList,
					newCharInfo(charGenerated, page.InvalidCharCode, ' ', origin, box, formMatrix, textObj))
			}
			if item.CharCode == page.InvalidCharCode {
				continue
			}
		}

		spacing = 0
		var unicode []rune
		ct := charNormal
		if f != nil {
			s := f.UnicodeStringFromCharCode(item.CharCode)
			unicode = []rune(s)
		}
		if len(unicode) == 0 && item.CharCode != 0 {
			unicode = []rune{rune(item.CharCode)}
			ct = charNotUnicode
		}

		charBox := textItemCharBox(textObj, item, matrix)

		ci := newCharInfo(ct, item.CharCode, 0, matrix.Transform(item.Origin), charBox, matrix, textObj)
		if len(unicode) == 0 {
			tp.tempCharList = append(tp.tempCharList, ci)
			tp.tempTextBuf = append(tp.tempTextBuf, 0xfffe)
			continue
		}

		addUnicode := true
		count := min(len(tp.tempCharList), 7)
		threshold := ci.matrix.TransformXDistance(kTextCharRatioGapDelta * textObj.FontSize())
		for nn := len(tp.tempCharList); nn > len(tp.tempCharList)-count; nn-- {
			c1 := tp.tempCharList[nn-1]
			diffX := c1.origin.X - ci.origin.X
			diffY := c1.origin.Y - ci.origin.Y
			if c1.charCode == ci.charCode &&
				c1.textObject != nil && ci.textObject != nil &&
				c1.textObject.Font() == ci.textObject.Font() &&
				absf(diffX) < threshold && absf(diffY) < threshold {
				addUnicode = false
				break
			}
		}
		if addUnicode {
			for _, c := range unicode {
				ci.unicode = c
				if c != 0 {
					tp.tempTextBuf = append(tp.tempTextBuf, c)
				} else {
					tp.tempTextBuf = append(tp.tempTextBuf, 0xfffe)
				}
				tp.tempCharList = append(tp.tempCharList, ci)
			}
		} else if i == 0 {
			if n := len(tp.tempTextBuf); n != 0 && tp.tempTextBuf[n-1] == ' ' {
				tp.tempTextBuf = tp.tempTextBuf[:n-1]
				tp.tempCharList = tp.tempCharList[:len(tp.tempCharList)-1]
			}
		}
	}
}

func textItemCharBox(textObj *page.TextObject, item page.TextItem, matrix crt.Matrix) crt.FloatRect {
	var l, b, r, t int
	if f := textObj.Font(); f != nil {
		l, b, r, t = f.GetCharBBox(item.CharCode)
	}
	fFontSize := textObj.FontSize() / 1000
	// Each product into a local before adding origin (float32, no FMA).
	lProd := float32(l) * fFontSize
	bProd := float32(b) * fFontSize
	rProd := float32(r) * fFontSize
	tProd := float32(t) * fFontSize
	charBox := crt.NewFloatRect(
		lProd+item.Origin.X,
		bProd+item.Origin.Y,
		rProd+item.Origin.X,
		tProd+item.Origin.Y,
	)
	if absf(charBox.Top-charBox.Bottom) < kSizeEpsilon {
		charBox.Top = charBox.Bottom + fFontSize
	}
	if absf(charBox.Right-charBox.Left) < kSizeEpsilon {
		charBox.Right = charBox.Left + charWidthWithFallback(textObj, item.CharCode)
	}
	return matrix.TransformRect(charBox)
}

func charWidthWithFallback(textObj *page.TextObject, charCode uint32) float32 {
	width := objCharWidth(textObj, charCode)
	if absf(width) >= kSizeEpsilon {
		return width
	}
	fontSize := absf(textObj.FontSize())
	if fontSize < kSizeEpsilon {
		fontSize = kDefaultFontSize
	}
	return fontSize * 0.5
}

// calculateBaseSpace ports CalculateBaseSpace (62). Tc (GetCharSpace) is not
// reconstructible from the frozen page API; it is assumed 0 (see
// textobject_adapter.go), so this returns 0 — exactly what PDFium computes when
// char_space == 0.
func calculateBaseSpace(textObj *page.TextObject, matrix crt.Matrix) float32 {
	const charSpace = float32(0) // Tc unavailable; assume 0.
	nItems := textObj.CountItems()
	if charSpace == 0 || nItems < 3 {
		return 0
	}
	return 0 // unreachable while charSpace==0; kept structurally faithful.
}

// calculateBaseSpaceAdjustment ports CalculateBaseSpaceAdjustment (89). Tc==0.
func calculateBaseSpaceAdjustment(textObj *page.TextObject, matrix crt.Matrix) float32 {
	const charSpace = float32(0)
	if charSpace > 0.001 {
		return -matrix.TransformDistance(charSpace)
	}
	if charSpace < -0.001 {
		return matrix.TransformDistance(absf(charSpace))
	}
	return 0
}

// calculateSpaceThreshold ports CalculateSpaceThreshold (213). Note: PDFium's
// font.CharCodeFromUnicode returns kInvalidCharCode when ' ' is unmapped; our
// face-less font returns 0 (a valid code) for the predefined space mapping, so
// the threshold uses the space glyph's advance — the common, correct path.
func calculateSpaceThreshold(f *font.Font, fontsizeH float32, charCode uint32) float32 {
	if f == nil {
		return 0
	}
	spaceCharcode := f.CharCodeFromUnicode(' ')
	threshold := float32(0)
	if spaceCharcode != page.InvalidCharCode {
		threshold = fontsizeH * f.GetCharWidthF(spaceCharcode) / 1000
	}
	if threshold > fontsizeH/3 {
		threshold = 0
	} else {
		threshold /= 2
	}
	if threshold == 0 {
		threshold = float32(getCharWidthInt(charCode, f))
		threshold = normalizeThreshold(threshold, 300, 500, 700)
		threshold = fontsizeH * threshold / 1000
	}
	return threshold
}

// --- BiDi flush ---

// closeTempLine ports CloseTempLine (835).
func (tp *TextPage) closeTempLine() {
	if len(tp.tempCharList) == 0 {
		return
	}

	// MakeString then collapse consecutive spaces (keep first).
	str := append([]rune(nil), tp.tempTextBuf...)
	bPrevSpace := false
	for i := 0; i < len(str); i++ {
		if str[i] != ' ' {
			bPrevSpace = false
			continue
		}
		if bPrevSpace {
			tp.tempTextBuf = deleteRune(tp.tempTextBuf, i)
			tp.tempCharList = deleteCharInfo(tp.tempCharList, i)
			str = deleteRune(str, i)
			i--
		}
		bPrevSpace = true
	}

	bidi := newBidiString(str)
	if tp.rtl {
		bidi.setOverallDirectionRight()
	}
	eCurrentDirection := bidi.overallDir()
	for _, segment := range bidi.order {
		if segment.direction == dirRight ||
			(segment.direction == dirNeutral && eCurrentDirection == dirRight) {
			eCurrentDirection = dirRight
			for m := segment.start + segment.count; m > segment.start; m-- {
				tp.addCharInfoByRLDirection(str[m-1], tp.tempCharList[m-1])
			}
		} else {
			if segment.direction != dirLeftWeak {
				eCurrentDirection = dirLeft
			}
			for m := segment.start; m < segment.start+segment.count; m++ {
				tp.addCharInfoByLRDirection(str[m], tp.tempCharList[m])
			}
		}
	}
	tp.tempCharList = tp.tempCharList[:0]
	tp.tempTextBuf = tp.tempTextBuf[:0]
}

// addCharInfoByLRDirection ports AddCharInfoByLRDirection (786).
func (tp *TextPage) addCharInfoByLRDirection(wChar rune, info charInfo) {
	if !isNormalCharacter(info) {
		tp.charList = append(tp.charList, info)
		return
	}
	var normalized []rune
	if wChar >= 0xFB00 && wChar <= 0xFB06 {
		normalized = getUnicodeNormalization(wChar)
	}
	if len(normalized) == 0 {
		tp.textBuf = append(tp.textBuf, wChar)
		tp.charList = append(tp.charList, info)
		return
	}
	modified := info
	modified.charType = charPiece
	for _, nc := range normalized {
		modified.unicode = nc
		tp.textBuf = append(tp.textBuf, nc)
		tp.charList = append(tp.charList, modified)
	}
}

// addCharInfoByRLDirection ports AddCharInfoByRLDirection (811).
func (tp *TextPage) addCharInfoByRLDirection(wChar rune, info charInfo) {
	if !isNormalCharacter(info) {
		tp.charList = append(tp.charList, info)
		return
	}
	modified := info
	wChar = getMirrorChar(wChar)
	normalized := getUnicodeNormalization(wChar)
	if len(normalized) == 0 {
		modified.unicode = wChar
		tp.textBuf = append(tp.textBuf, wChar)
		tp.charList = append(tp.charList, modified)
		return
	}
	modified.charType = charPiece
	for _, nc := range normalized {
		modified.unicode = nc
		tp.textBuf = append(tp.textBuf, nc)
		tp.charList = append(tp.charList, modified)
	}
}

// --- rect / text consumers ---

// GetRectArray ports GetRectArray (433): group consecutive non-generated,
// non-degenerate CharInfos by text_object pointer; each group's Box is the
// normalised union of the members' tight char boxes. start=0, count<0 (or
// overshoot) clamps to all-from-start — the FPDFText_CountRects(tp,0,n) path.
//
// Box is in PDF USER SPACE, Y-UP (see the package doc).
func (tp *TextPage) GetRectArray() []Rect {
	return tp.getRectArray(0, -1)
}

// GetWordArray returns tight word-level rects over the same char stream as
// GetRectArray. It is intentionally lower-level than GetTextByRect: callers
// that need table tokens can keep word boxes separate instead of first merging
// them into line-level text cells.
func (tp *TextPage) GetWordArray() []Rect {
	words := make([]Rect, 0)
	var rect crt.FloatRect
	var text []rune
	var fontSize float64
	var fontName string
	var fontWeight int
	var fontFlags int
	inWord := false

	flush := func() {
		if !inWord {
			return
		}
		words = append(words, Rect{Text: string(text), Box: rect, FontSize: fontSize,
			FontName: fontName, FontWeight: fontWeight, FontFlags: fontFlags})
		text = text[:0]
		fontSize = 0
		fontName = ""
		fontWeight = 0
		fontFlags = 0
		inWord = false
	}

	for _, ci := range tp.charList {
		if wordBoundaryChar(ci) {
			flush()
			continue
		}
		if ci.charBox.Width() < kSizeEpsilon || ci.charBox.Height() < kSizeEpsilon {
			continue
		}

		charBox := ci.charBox
		charBox.Normalize()
		if !inWord {
			rect = charBox
			fontSize = rectFontSize(ci.textObject)
			fontName = rectFontName(ci.textObject)
			fontWeight = rectFontWeight(ci.textObject)
			fontFlags = rectFontFlags(ci.textObject)
			inWord = true
		} else {
			rect.Union(charBox)
		}
		if ci.unicode != 0 {
			text = append(text, ci.unicode)
		}
	}
	flush()
	return words
}

func wordBoundaryChar(ci charInfo) bool {
	if ci.charType == charGenerated || !isNormalCharacter(ci) {
		return true
	}
	return unicode.IsSpace(ci.unicode)
}

func (tp *TextPage) getRectArray(start, count int) []Rect {
	var rects []Rect
	if start < 0 || count == 0 {
		return rects
	}
	n := tp.CountChars()
	if start >= n {
		return rects
	}
	if count < 0 || start+count > n {
		count = n - start
	}

	var textObject *page.TextObject
	var rect crt.FloatRect
	var text []rune
	var fontSize float64
	var fontName string
	var fontWeight int
	var fontFlags int
	pos := start
	isNewRect := true
	for ; count > 0; count-- {
		ci := tp.charList[pos]
		pos++
		if ci.charType == charGenerated {
			continue
		}
		if unicode.IsSpace(ci.unicode) {
			continue
		}
		// RAW signed Width/Height (right-left, top-bottom), NOT normalised.
		if ci.charBox.Width() < kSizeEpsilon || ci.charBox.Height() < kSizeEpsilon {
			continue
		}
		if textObject == nil {
			textObject = ci.textObject
			fontSize = rectFontSize(textObject)
			fontName = rectFontName(textObject)
			fontWeight = rectFontWeight(textObject)
			fontFlags = rectFontFlags(textObject)
		}
		if textObject != ci.textObject {
			rects = append(rects, Rect{Text: string(text), Box: rect, FontSize: fontSize,
				FontName: fontName, FontWeight: fontWeight, FontFlags: fontFlags})
			text = text[:0]
			textObject = ci.textObject
			fontSize = rectFontSize(textObject)
			fontName = rectFontName(textObject)
			fontWeight = rectFontWeight(textObject)
			fontFlags = rectFontFlags(textObject)
			isNewRect = true
		}
		if isNewRect {
			isNewRect = false
			rect = ci.charBox
			rect.Normalize()
			if ci.unicode != 0 {
				text = append(text, ci.unicode)
			}
			continue
		}
		rect.Union(ci.charBox)
		if ci.unicode != 0 {
			text = append(text, ci.unicode)
		}
	}
	if !isNewRect {
		rects = append(rects, Rect{Text: string(text), Box: rect, FontSize: fontSize,
			FontName: fontName, FontWeight: fontWeight, FontFlags: fontFlags})
	}
	return rects
}

func rectFontSize(to *page.TextObject) float64 {
	if to == nil {
		return 0
	}
	size := objFontSizeH(to)
	if isFloatZero(size) {
		size = to.FontSize()
	}
	if size < 0 {
		size = -size
	}
	return float64(size)
}

// rectFontName returns the text object's font base name, or "" if none.
func rectFontName(to *page.TextObject) string {
	if to == nil {
		return ""
	}
	if f := to.Font(); f != nil {
		return f.BaseFontName()
	}
	return ""
}

// rectFontFlags returns the text object's font descriptor flags (PDF spec
// §5.7.1). Bit 6 (64) is Italic; bit 1 (1) is FixedPitch.
func rectFontFlags(to *page.TextObject) int {
	if to == nil {
		return 0
	}
	if f := to.Font(); f != nil {
		return f.Flags()
	}
	return 0
}

// rectFontWeight returns an inferred OpenType weight (100-900) from the
// font descriptor's /StemV. Returns 0 when no descriptor is present.
func rectFontWeight(to *page.TextObject) int {
	if to == nil {
		return 0
	}
	if f := to.Font(); f != nil {
		return f.FontWeight()
	}
	return 0
}

// CountRects ports CountRects (637): caches sel_rects_ and returns its length.
func (tp *TextPage) CountRects(start, count int) int {
	if start < 0 {
		return -1
	}
	rects := tp.getRectArray(start, count)
	tp.selRects = tp.selRects[:0]
	for _, r := range rects {
		tp.selRects = append(tp.selRects, r.Box)
	}
	return len(tp.selRects)
}

// GetRect ports GetRect (644): read the cached sel_rects_ (call CountRects
// first). The returned rect is in PDF USER SPACE, Y-UP.
func (tp *TextPage) GetRect(rectIndex int) (crt.FloatRect, bool) {
	if rectIndex < 0 || rectIndex >= len(tp.selRects) {
		return crt.FloatRect{}, false
	}
	return tp.selRects[rectIndex], true
}

// GetTextByRect ports GetTextByRect (560). box is in PDF USER SPACE, Y-UP — the
// SAME space as Rect.Box.
func (tp *TextPage) GetTextByRect(box crt.FloatRect) string {
	return tp.getTextByPredicate(func(ci charInfo) bool {
		return isRectIntersect(box, ci.charBox)
	})
}

// getTextByPredicate ports GetTextByPredicate (523).
func (tp *TextPage) getTextByPredicate(predicate func(charInfo) bool) string {
	posy := float32(0)
	isContainPreChar := false
	isAddLineFeed := false
	var strText []rune
	for _, ci := range tp.charList {
		if predicate(ci) {
			if absf(posy-ci.origin.Y) > 0 && !isContainPreChar && isAddLineFeed {
				posy = ci.origin.Y
				if len(strText) != 0 {
					strText = append(strText, '\r', '\n')
				}
			}
			isContainPreChar = true
			isAddLineFeed = false
			if ci.unicode != 0 {
				strText = append(strText, ci.unicode)
			}
		} else if ci.unicode == ' ' {
			if isContainPreChar {
				strText = append(strText, ' ')
				isContainPreChar = false
				isAddLineFeed = false
			}
		} else {
			isContainPreChar = false
			isAddLineFeed = true
		}
	}
	return string(strText)
}

// isRectIntersect ports IsRectIntersect (160).
func isRectIntersect(rect1, rect2 crt.FloatRect) bool {
	rect := rect1
	rect.Intersect(rect2)
	return !rect.IsEmpty()
}

// --- small helpers ---

func deleteRune(s []rune, i int) []rune {
	return append(s[:i], s[i+1:]...)
}

func deleteCharInfo(s []charInfo, i int) []charInfo {
	return append(s[:i], s[i+1:]...)
}
