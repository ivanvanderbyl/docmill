// Ported from core/fpdfapi/page/cpdf_streamcontentparser.{h,cpp} @ pdfium
// 0db284a42 — the operator table + dispatcher + AddTextObject, the heart of
// text extraction.
//
// Path/color/image/shading operators are accepted no-ops: their params are
// consumed by ClearAllParams after each dispatch so the stream stays in sync,
// but we do not evaluate colour or build path/image objects. Only the Form
// branch of Do recurses (into nested content). Geometry is float32 with every
// product stored in a local before summing.
package page

import (
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/font"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

const (
	kParamBufSize = 16
	kMaxFormLevel = 40
)

// paramKind discriminates the std::variant<Object, FX_Number, ByteString>.
type paramKind int

const (
	paramObject paramKind = iota
	paramNumber
	paramName
)

type contentParam struct {
	kind paramKind
	obj  objects.Object
	num  crt.Number
	name string
}

// FormRecursionState is CPDF_Form::RecursionState: the identity set of stream
// data already being parsed, shared across nested forms to break cycles.
type FormRecursionState struct {
	// parsed keys on the decoded content-stream byte identity (the *Stream the
	// parser is currently consuming). Insert before recursing; refuse to
	// re-enter if already present.
	parsed map[*objects.Stream]struct{}
}

func newFormRecursionState() *FormRecursionState {
	return &FormRecursionState{parsed: map[*objects.Stream]struct{}{}}
}

// StreamContentParser is the operator interpreter.
type StreamContentParser struct {
	holder          objects.IndirectObjectHolder
	pageResources   *objects.Dictionary
	resources       *objects.Dictionary
	objectHolder    *PageObjectHolder
	recursionState  *FormRecursionState
	mtContentToUser crt.Matrix

	paramStartPos uint32
	paramCount    uint32
	paramBuf      [kParamBufSize]contentParam

	syntax    *StreamParser
	curStates *AllStates

	contentMarksStack [][]ContentMark // index 0 is the sentinel, never popped
	clipTextList      []*TextObject

	stateStack []AllStates
	allCTMs    map[int32]crt.Matrix

	streamStartOffsets []uint32
	startParseOffset   uint32

	// formParser, when non-nil, is invoked by Handle_ExecuteXObject's Form
	// branch to parse a nested form XObject. It is wired by the holder so this
	// file stays free of the page/form construction logic.
	parseForm func(stream *objects.Stream, name string, graphicStates *AllStates) *Form
}

// newStreamContentParser ports the constructor (cpdf_streamcontentparser.cpp:
// 389). resources = first non-nil of (pResources, page); both callers hand the
// parent resources to the holder, never here, so PDFium's middle candidate is
// always nil.
func newStreamContentParser(
	holder objects.IndirectObjectHolder,
	pageResources *objects.Dictionary,
	contentToUser *crt.Matrix,
	objectHolder *PageObjectHolder,
	pResources *objects.Dictionary,
	states *AllStates,
	recursionState *FormRecursionState,
) *StreamContentParser {
	p := &StreamContentParser{
		holder:          holder,
		pageResources:   pageResources,
		resources:       chooseResourcesDict(pResources, nil, pageResources),
		objectHolder:    objectHolder,
		recursionState:  recursionState,
		mtContentToUser: crt.IdentityMatrix(),
		allCTMs:         map[int32]crt.Matrix{},
	}
	if contentToUser != nil {
		p.mtContentToUser = *contentToUser
	}
	if states != nil {
		p.curStates = states.clone()
	} else {
		p.curStates = newAllStates()
	}
	// Sentinel content marks.
	p.contentMarksStack = append(p.contentMarksStack, nil)
	// Seed all_ctms_[0].
	p.allCTMs[0] = p.curStates.CTM()
	return p
}

// GetCurStates exposes the live interpreter state (the driver sets the color
// default and the form ctor overrides the CTM).
func (p *StreamContentParser) GetCurStates() *AllStates { return p.curStates }

// GraphicStatesRefDefault ports the page driver's
// curStates.mutable_color_state.SetDefault() seed (cpdf_contentparser.cpp Parse).
func (p *StreamContentParser) GraphicStatesRefDefault() {
	p.curStates.graphicStates.SetDefaultStates()
}

// TakeAllCTMs moves the CTM map out (the holder captures it at end of parse).
func (p *StreamContentParser) TakeAllCTMs() map[int32]crt.Matrix {
	m := p.allCTMs
	p.allCTMs = map[int32]crt.Matrix{}
	return m
}

// --- param ring (cpdf_streamcontentparser.cpp:433) ---

// getNextParamPos ports GetNextParamPos — the 16-slot ring with wrap-around.
func (p *StreamContentParser) getNextParamPos() uint32 {
	if p.paramCount == kParamBufSize {
		p.paramStartPos++
		if p.paramStartPos == kParamBufSize {
			p.paramStartPos = 0
		}
		p.paramBuf[p.paramStartPos] = contentParam{}
		return p.paramStartPos
	}
	index := p.paramStartPos + p.paramCount
	if index >= kParamBufSize {
		index -= kParamBufSize
	}
	p.paramCount++
	return index
}

func (p *StreamContentParser) addNameParam(name []byte) {
	p.paramBuf[p.getNextParamPos()] = contentParam{kind: paramName, name: nameDecode(name)}
}

func (p *StreamContentParser) addNumberParam(str []byte) {
	p.paramBuf[p.getNextParamPos()] = contentParam{kind: paramNumber, num: crt.NumberFromBytes(str)}
}

func (p *StreamContentParser) addObjectParam(obj objects.Object) {
	p.paramBuf[p.getNextParamPos()] = contentParam{kind: paramObject, obj: obj}
}

func (p *StreamContentParser) clearAllParams() {
	p.paramStartPos = 0
	p.paramCount = 0
}

// realIndex maps a reverse param index (0 = most recent) to a ring slot.
func (p *StreamContentParser) realIndex(index uint32) int {
	ri := int(p.paramStartPos+p.paramCount-index) - 1
	if ri >= kParamBufSize {
		ri -= kParamBufSize
	}
	return ri
}

// getObject ports GetObject(index): lazily converting number/name to an Object.
func (p *StreamContentParser) getObject(index uint32) objects.Object {
	if index >= p.paramCount {
		return nil
	}
	param := &p.paramBuf[p.realIndex(index)]
	switch param.kind {
	case paramNumber:
		var obj objects.Object
		if param.num.IsInteger() {
			obj = objects.NewNumberFromInt(param.num.GetSigned())
		} else {
			obj = objects.NewNumberFromFloat(param.num.GetFloat())
		}
		*param = contentParam{kind: paramObject, obj: obj}
		return obj
	case paramName:
		obj := objects.NewName(param.name)
		*param = contentParam{kind: paramObject, obj: obj}
		return obj
	default:
		return param.obj
	}
}

// getString ports GetString(index).
func (p *StreamContentParser) getString(index uint32) string {
	if index >= p.paramCount {
		return ""
	}
	param := &p.paramBuf[p.realIndex(index)]
	switch param.kind {
	case paramName:
		return param.name
	case paramObject:
		if param.obj != nil {
			return param.obj.GetString()
		}
	}
	return ""
}

// getNumber ports GetNumber(index).
func (p *StreamContentParser) getNumber(index uint32) float32 {
	if index >= p.paramCount {
		return 0
	}
	param := &p.paramBuf[p.realIndex(index)]
	switch param.kind {
	case paramNumber:
		return param.num.GetFloat()
	case paramObject:
		if param.obj != nil {
			return param.obj.GetNumber()
		}
	}
	return 0
}

// getInteger ports GetInteger(index).
func (p *StreamContentParser) getInteger(index uint32) int { return int(p.getNumber(index)) }

// getNumbers ports GetNumbers(count): forward (stream) order.
func (p *StreamContentParser) getNumbers(count uint32) []float32 {
	values := make([]float32, count)
	for i := range count {
		values[i] = p.getNumber(count - i - 1)
	}
	return values
}

// getPoint ports GetPoint(index) = {GetNumber(index+1), GetNumber(index)}.
func (p *StreamContentParser) getPoint(index uint32) crt.PointF {
	return crt.PointF{X: p.getNumber(index + 1), Y: p.getNumber(index)}
}

// getMatrix ports GetMatrix().
func (p *StreamContentParser) getMatrix() crt.Matrix {
	return crt.NewMatrix(p.getNumber(5), p.getNumber(4), p.getNumber(3),
		p.getNumber(2), p.getNumber(1), p.getNumber(0))
}

// --- the driver: Parse (cpdf_streamcontentparser.cpp:1633) ---

// Parse runs the operator interpreter over data[startOffset:], up to maxCost
// emitted objects (0 = unlimited), and returns the number of bytes consumed.
// stream is the *Stream backing the data (for the recursion cycle guard).
func (p *StreamContentParser) Parse(data []byte, startOffset, maxCost uint32, streamStartOffsets []uint32, stream *objects.Stream) uint32 {
	if startOffset >= uint32(len(data)) {
		return 0
	}
	dataStart := data[startOffset:]
	p.startParseOffset = startOffset

	// Recursion guard (cpdf_streamcontentparser.cpp:1644).
	if len(p.recursionState.parsed) > kMaxFormLevel {
		return uint32(len(dataStart))
	}
	if stream != nil {
		if _, seen := p.recursionState.parsed[stream]; seen {
			return uint32(len(dataStart))
		}
	}

	p.streamStartOffsets = streamStartOffsets

	if stream != nil {
		p.recursionState.parsed[stream] = struct{}{}
		defer delete(p.recursionState.parsed, stream)
	}
	initObjCount := p.objectHolder.PageObjectCount()
	p.syntax = newStreamParser(dataStart)
	defer func() { p.syntax = nil }()

	for {
		cost := uint32(p.objectHolder.PageObjectCount() - initObjCount)
		if maxCost != 0 && cost >= maxCost {
			break
		}
		switch p.syntax.ParseNextElement() {
		case ElemEndOfData:
			return p.syntax.GetPos()
		case ElemKeyword:
			p.onOperator(p.syntax.GetWord())
			p.clearAllParams()
		case ElemNumber:
			p.addNumberParam(p.syntax.GetWord())
		case ElemName:
			p.addNameParam(p.syntax.GetWord()[1:])
		default: // ElemOther
			p.addObjectParam(p.syntax.GetObject())
		}
	}
	return p.syntax.GetPos()
}

// getCurrentStreamIndex ports GetCurrentStreamIndex
// (cpdf_streamcontentparser.cpp:1374): upper_bound(offsets, pos+startOffset)-1.
func (p *StreamContentParser) getCurrentStreamIndex() int32 {
	target := p.syntax.GetPos() + p.startParseOffset
	// upper_bound: first index whose offset > target.
	ub := sort.Search(len(p.streamStartOffsets), func(i int) bool {
		return p.streamStartOffsets[i] > target
	})
	return int32(ub) - 1
}

// --- operator dispatch ---

// onOperator ports OnOperator: switch on the operator string. Only listed
// operators are handled; everything else is silently ignored (params already
// cleared by the caller).
func (p *StreamContentParser) onOperator(op []byte) {
	switch string(op) {
	// Text-showing.
	case "Tj":
		p.handleShowText()
	case "TJ":
		p.handleShowTextPositioning()
	case "'":
		p.handleNextLineShowText()
	case "\"":
		p.handleNextLineShowTextSpace()
	// Text-state.
	case "Tf":
		p.handleSetFont()
	case "Tm":
		p.handleSetTextMatrix()
	case "Td":
		p.handleMoveTextPoint()
	case "TD":
		p.handleMoveTextPointSetLeading()
	case "T*":
		p.handleMoveToNextLine()
	case "TL":
		p.handleSetTextLeading()
	case "Tc":
		p.handleSetCharSpace()
	case "Tw":
		p.handleSetWordSpace()
	case "Tz":
		p.handleSetHorzScale()
	case "Ts":
		p.handleSetTextRise()
	case "Tr":
		p.handleSetTextRenderMode()
	// Text block.
	case "BT":
		p.handleBeginText()
	case "ET":
		p.handleEndText()
	// Graphics state.
	case "q":
		p.handleSaveGraphState()
	case "Q":
		p.handleRestoreGraphState()
	case "cm":
		p.handleConcatMatrix()
	case "gs":
		p.handleSetExtendGraphState()
	case "w":
		p.curStates.MutableGraphState().SetLineWidth(p.getNumber(0))
	// Marked content.
	case "BDC":
		p.handleBeginMarkedContentDictionary()
	case "BMC":
		p.handleBeginMarkedContent()
	case "EMC":
		p.handleEndMarkedContent()
	// XObject + inline image.
	case "Do":
		p.handleExecuteXObject()
	case "BI":
		p.handleBeginImage()
	// Path subpath start: must greedily consume the path run so the tokenizer
	// stays in sync (other path ops just clear params).
	case "m":
		p.handleMoveTo()
	// All other path/color/shading/state/Type3-metric operators (l c v y h re
	// n f f* F S s B B* b b* W W* J j M d ri i g G rg RG k K cs CS sc SC scn
	// SCN sh d0 d1) are accepted no-ops: their params are consumed by
	// clearAllParams.
	default:
	}
}

// --- text operators (cpdf_streamcontentparser.cpp) ---

func (p *StreamContentParser) handleSetCharSpace() {
	p.curStates.MutableTextState().SetCharSpace(p.getNumber(0))
}

func (p *StreamContentParser) handleSetWordSpace() {
	p.curStates.MutableTextState().SetWordSpace(p.getNumber(0))
}

func (p *StreamContentParser) handleSetHorzScale() {
	if p.paramCount != 1 {
		return
	}
	p.curStates.SetTextHorzScale(p.getNumber(0) / 100)
	p.onChangeTextMatrix()
}

func (p *StreamContentParser) handleSetTextRise() {
	p.curStates.SetTextRise(p.getNumber(0))
}

func (p *StreamContentParser) handleSetTextLeading() {
	p.curStates.SetTextLeading(p.getNumber(0))
}

func (p *StreamContentParser) handleSetTextRenderMode() {
	if mode, ok := SetTextRenderingModeFromInt(p.getInteger(0)); ok {
		p.curStates.MutableTextState().SetTextMode(mode)
	}
}

func (p *StreamContentParser) handleSetFont() {
	p.curStates.MutableTextState().SetFontSize(p.getNumber(0))
	f := p.findFont(p.getString(1))
	if f != nil {
		p.curStates.MutableTextState().SetFont(f)
	}
}

func (p *StreamContentParser) handleMoveTextPoint() {
	p.curStates.MoveTextPoint(p.getPoint(0))
}

func (p *StreamContentParser) handleMoveTextPointSetLeading() {
	p.handleMoveTextPoint()
	p.curStates.SetTextLeading(-p.getNumber(0))
}

func (p *StreamContentParser) handleMoveToNextLine() {
	p.curStates.MoveTextToNextLine()
}

func (p *StreamContentParser) handleSetTextMatrix() {
	p.curStates.SetTextMatrix(p.getMatrix())
	p.onChangeTextMatrix()
	p.curStates.ResetTextPosition()
}

func (p *StreamContentParser) handleShowText() {
	str := []byte(p.getString(0))
	if len(str) != 0 {
		p.addTextObject([][]byte{str}, nil, 0.0)
	}
}

func (p *StreamContentParser) handleShowTextPositioning() {
	arr := objects.ToArray(p.getObject(0))
	if arr == nil {
		return
	}
	n := arr.Len()
	nsegs := 0
	for i := range n {
		if obj := arr.GetDirectObjectAt(i); obj != nil && obj.Type() == objects.TypeString {
			nsegs++
		}
	}
	if nsegs == 0 {
		for i := range n {
			kerning := arr.GetFloatAt(i)
			if kerning != 0 {
				p.curStates.IncrementTextPositionX(-p.getHorizontalTextSize(kerning))
			}
		}
		return
	}
	strs := make([][]byte, nsegs)
	kernings := make([]float32, nsegs)
	iSegment := 0
	var fInitKerning float32
	for i := range n {
		obj := arr.GetDirectObjectAt(i)
		if obj == nil {
			continue
		}
		if obj.Type() == objects.TypeString {
			s := obj.GetString()
			if len(s) == 0 {
				continue
			}
			strs[iSegment] = []byte(s)
			kernings[iSegment] = 0
			iSegment++
		} else {
			num := obj.GetNumber()
			if iSegment == 0 {
				fInitKerning += num
			} else {
				kernings[iSegment-1] += num
			}
		}
	}
	p.addTextObject(strs[:iSegment], kernings, fInitKerning)
}

func (p *StreamContentParser) handleNextLineShowText() {
	p.handleMoveToNextLine()
	p.handleShowText()
}

func (p *StreamContentParser) handleNextLineShowTextSpace() {
	p.curStates.MutableTextState().SetWordSpace(p.getNumber(2))
	p.curStates.MutableTextState().SetCharSpace(p.getNumber(1))
	p.handleNextLineShowText()
}

func (p *StreamContentParser) handleBeginText() {
	p.curStates.SetTextMatrix(crt.IdentityMatrix())
	p.onChangeTextMatrix()
	p.curStates.ResetTextPosition()
}

func (p *StreamContentParser) handleEndText() {
	// Clip-mode text application is deferred (ClipPath has no path data); just
	// clear the list so mismatched ET tolerates.
	if len(p.clipTextList) == 0 {
		return
	}
	p.clipTextList = nil
}

// --- AddTextObject (cpdf_streamcontentparser.cpp:1306) ---

func (p *StreamContentParser) getVerticalTextSize(kerning float32) float32 {
	product := kerning * p.curStates.TextState().GetFontSize()
	return product / 1000
}

func (p *StreamContentParser) getHorizontalTextSize(kerning float32) float32 {
	return p.getVerticalTextSize(kerning) * p.curStates.TextHorzScale()
}

func (p *StreamContentParser) addTextObject(strings [][]byte, kernings []float32, initialKerning float32) {
	f := p.curStates.TextState().GetFont()
	if f == nil {
		return
	}
	if initialKerning != 0 {
		if f.IsVertWriting() {
			p.curStates.IncrementTextPositionY(-p.getVerticalTextSize(initialKerning))
		} else {
			p.curStates.IncrementTextPositionX(-p.getHorizontalTextSize(initialKerning))
		}
	}
	if len(strings) == 0 {
		return
	}

	pText := newTextObject(p.getCurrentStreamIndex())
	pText.SetResourceName(p.fontResourceName(f))
	p.setGraphicStates(pText)

	textMode := pText.graphicStates.textState.GetTextMode()
	if f.IsType3() {
		textMode = ModeFill
	}
	if TextRenderingModeIsStrokeMode(textMode) {
		ctm := p.curStates.CTM()
		c := pText.graphicStates.textState.MutableCTM()
		c[0] = ctm.A
		c[1] = ctm.C
		c[2] = ctm.B
		c[3] = ctm.D
	}

	pText.setSegments(strings, kernings)
	pText.setPosition(p.mtContentToUser.Transform(p.curStates.GetTransformedTextPosition()))

	position := pText.calcPositionData(p.curStates.TextHorzScale())
	p.curStates.IncrementTextPositionX(position.X)
	p.curStates.IncrementTextPositionY(position.Y)

	// Clip-mode text capture is deferred (Clone unused on read path).
	p.objectHolder.AppendPageObject(pText)

	if len(kernings) > 0 && kernings[len(kernings)-1] != 0 {
		last := kernings[len(kernings)-1]
		if f.IsVertWriting() {
			p.curStates.IncrementTextPositionY(-p.getVerticalTextSize(last))
		} else {
			p.curStates.IncrementTextPositionX(-p.getHorizontalTextSize(last))
		}
	}
}

// onChangeTextMatrix ports OnChangeTextMatrix (cpdf_streamcontentparser.cpp:
// 1449): recompute the cached transposed text->device 2x2 whenever Tm/cm/Tz/BT
// change it. The translation (e,f) is discarded; the object position comes from
// SetPosition.
func (p *StreamContentParser) onChangeTextMatrix() {
	tm := crt.NewMatrix(p.curStates.TextHorzScale(), 0, 0, 1, 0, 0)
	tm.Concat(p.curStates.TextMatrix())
	tm.Concat(p.curStates.CTM())
	tm.Concat(p.mtContentToUser)
	m := p.curStates.MutableTextState().MutableMatrix()
	m[0] = tm.A
	m[1] = tm.C
	m[2] = tm.B
	m[3] = tm.D
}

// setGraphicStates ports SetGraphicStates(pObj, color, text, graph) for the
// text path (all three true): copy general/clip/contentMarks + color/graph/text
// onto the object.
func (p *StreamContentParser) setGraphicStates(obj *TextObject) {
	obj.graphicStates = p.curStates.graphicStates // value copy of all five sub-states
	obj.contentMarks = append([]ContentMark(nil), p.topContentMarks()...)
}

// --- graphics state operators ---

func (p *StreamContentParser) handleSaveGraphState() {
	p.stateStack = append(p.stateStack, *p.curStates)
}

func (p *StreamContentParser) handleRestoreGraphState() {
	if len(p.stateStack) == 0 {
		return
	}
	last := len(p.stateStack) - 1
	*p.curStates = p.stateStack[last]
	p.stateStack = p.stateStack[:last]
	p.allCTMs[p.getCurrentStreamIndex()] = p.curStates.CTM()
}

func (p *StreamContentParser) handleConcatMatrix() {
	p.curStates.PrependToCTM(p.getMatrix())
	p.allCTMs[p.getCurrentStreamIndex()] = p.curStates.CTM()
	p.onChangeTextMatrix()
}

func (p *StreamContentParser) handleSetExtendGraphState() {
	name := p.getString(0)
	gs := objects.ToDictionary(p.findResourceObj("ExtGState", name))
	if gs == nil {
		return
	}
	p.processExtGS(gs)
}

// processExtGS ports CPDF_AllStates::ProcessExtGS for the only text-relevant
// branch: /Font -> [fontRef, size]. Other keys are accepted no-ops (graph/
// general state is inert for extraction).
func (p *StreamContentParser) processExtGS(gs *objects.Dictionary) {
	fontArr := gs.GetArrayFor("Font")
	if fontArr != nil && fontArr.Len() >= 2 {
		p.curStates.MutableTextState().SetFontSize(fontArr.GetFloatAt(1))
		if f := p.findFont(fontArr.GetByteStringAt(0)); f != nil {
			p.curStates.MutableTextState().SetFont(f)
		}
	}
}

// --- marked content ---

func (p *StreamContentParser) topContentMarks() []ContentMark {
	return p.contentMarksStack[len(p.contentMarksStack)-1]
}

func (p *StreamContentParser) handleBeginMarkedContent() {
	marks := append([]ContentMark(nil), p.topContentMarks()...)
	marks = append(marks, ContentMark{Tag: p.getString(0)})
	p.contentMarksStack = append(p.contentMarksStack, marks)
}

func (p *StreamContentParser) handleBeginMarkedContentDictionary() {
	prop := p.getObject(0)
	if prop == nil {
		return
	}
	tag := p.getString(1)
	marks := append([]ContentMark(nil), p.topContentMarks()...)

	switch prop.Type() {
	case objects.TypeName:
		propName := prop.GetString()
		holder := p.findResourceHolder("Properties")
		if holder == nil {
			return
		}
		dict := holder.GetDictFor(propName)
		if dict == nil {
			return
		}
		marks = append(marks, ContentMark{Tag: tag, PropertyName: propName, Params: dict})
	case objects.TypeDictionary:
		marks = append(marks, ContentMark{Tag: tag, Params: objects.ToDictionary(prop)})
	default:
		return
	}
	p.contentMarksStack = append(p.contentMarksStack, marks)
}

func (p *StreamContentParser) handleEndMarkedContent() {
	// Never pop the sentinel (index 0): tolerates mismatched EMC.
	if len(p.contentMarksStack) > 1 {
		p.contentMarksStack = p.contentMarksStack[:len(p.contentMarksStack)-1]
	}
}

// --- inline image (BI..ID..EI) ---

// handleBeginImage ports Handle_BeginImage (cpdf_streamcontentparser.cpp:642)
// far enough to keep the tokenizer in sync: parse the dict name/value pairs
// until ID, then re-scan for the EI keyword. No image is created.
func (p *StreamContentParser) handleBeginImage() {
	savePos := p.syntax.GetPos()
	for {
		typ := p.syntax.ParseNextElement()
		if typ == ElemKeyword {
			if string(p.syntax.GetWord()) != "ID" {
				p.syntax.SetPos(savePos)
				return
			}
		}
		if typ != ElemName {
			break
		}
		// Consume the value object (advances past it).
		p.syntax.readNextObject(false, false, 0)
	}
	// Skip to EI.
	for {
		typ := p.syntax.ParseNextElement()
		if typ == ElemEndOfData {
			return
		}
		if typ == ElemKeyword && string(p.syntax.GetWord()) == "EI" {
			break
		}
	}
}

// --- path: m + ParsePathObject ---

type pathNumberBuffer struct {
	values [6]float32
	count  int
}

func (b *pathNumberBuffer) append(value float32) {
	if b.count >= len(b.values) {
		return
	}
	b.values[b.count] = value
	b.count++
}

func (b *pathNumberBuffer) reset() { b.count = 0 }

func (b *pathNumberBuffer) valuesSlice() []float32 { return b.values[:b.count] }

// handleMoveTo ports Handle_MoveTo: only valid with 2 params; then greedily
// consume the path run via parsePathObject so the tokenizer position is exact.
func (p *StreamContentParser) handleMoveTo() {
	if p.paramCount != 2 {
		return
	}
	p.parsePathObject()
}

// parsePathObject ports ParsePathObject (cpdf_streamcontentparser.cpp:1685): a
// greedy sub-loop consuming a run of path operators (m l c v y h re) and their
// numeric params, stopping (rewinding to last_pos) at the first non-path
// operator. The native Markdown pipeline only needs ruling geometry, so this
// materialises straight line segments from m/l/h/re and tracks curve endpoints.
func (p *StreamContentParser) parsePathObject() {
	lastPos := p.syntax.GetPos()
	matrix := p.curStates.CTM().Multiply(p.mtContentToUser)
	transform := func(point crt.PointF) crt.PointF {
		return matrix.Transform(point)
	}
	current := transform(p.getPoint(0))
	subpathStart := current
	var segments []PathSegment
	var numbers pathNumberBuffer
	appendObject := func() {
		if len(segments) == 0 {
			return
		}
		obj := newPathObject(p.getCurrentStreamIndex(), segments, p.curStates.graphicStates.graphState.GetLineWidth())
		obj.graphicStates = p.curStates.graphicStates
		obj.contentMarks = append([]ContentMark(nil), p.topContentMarks()...)
		p.objectHolder.AppendPageObject(obj)
		segments = nil
	}
	pointFromNumbers := func(values []float32) crt.PointF {
		return transform(crt.PointF{X: values[len(values)-2], Y: values[len(values)-1]})
	}
	for {
		typ := p.syntax.ParseNextElement()
		bProcessed := true
		var paintOp []byte
		switch typ {
		case ElemEndOfData:
			return
		case ElemKeyword:
			word := p.syntax.GetWord()
			values := numbers.valuesSlice()
			if len(word) == 1 {
				switch word[0] {
				case 'm':
					if len(values) >= 2 {
						current = pointFromNumbers(values)
						subpathStart = current
					}
					numbers.reset()
				case 'l':
					if len(values) >= 2 {
						next := pointFromNumbers(values)
						segments = append(segments, PathSegment{From: current, To: next})
						current = next
					}
					numbers.reset()
				case 'c':
					if len(values) >= 6 {
						current = pointFromNumbers(values)
					}
					numbers.reset()
				case 'v':
					if len(values) >= 4 {
						current = pointFromNumbers(values)
					}
					numbers.reset()
				case 'y':
					if len(values) >= 4 {
						current = pointFromNumbers(values)
					}
					numbers.reset()
				case 'h':
					segments = append(segments, PathSegment{From: current, To: subpathStart})
					current = subpathStart
					numbers.reset()
				case 'W':
					numbers.reset()
				default:
					bProcessed = false
				}
			} else if len(word) == 2 {
				if word[0] == 'r' && word[1] == 'e' {
					if len(values) >= 4 {
						x, y, width, height := values[0], values[1], values[2], values[3]
						p1 := transform(crt.PointF{X: x, Y: y})
						p2 := transform(crt.PointF{X: x + width, Y: y})
						p3 := transform(crt.PointF{X: x + width, Y: y + height})
						p4 := transform(crt.PointF{X: x, Y: y + height})
						segments = append(segments,
							PathSegment{From: p1, To: p2},
							PathSegment{From: p2, To: p3},
							PathSegment{From: p3, To: p4},
							PathSegment{From: p4, To: p1},
						)
						current = p1
						subpathStart = p1
					}
					numbers.reset()
				} else if word[0] == 'W' && word[1] == '*' {
					numbers.reset()
				} else {
					bProcessed = false
				}
			} else {
				bProcessed = false
			}
			if !bProcessed {
				paintOp = word
			}
			if bProcessed {
				lastPos = p.syntax.GetPos()
			}
		case ElemNumber:
			if numbers.count == len(numbers.values) {
				break
			}
			numbers.append(crt.NumberFromBytes(p.syntax.GetWord()).GetFloat())
		default:
			bProcessed = false
		}
		if !bProcessed {
			if pathPaintsStroke(paintOp) {
				if pathPaintOperatorCloses(paintOp) && current != subpathStart {
					segments = append(segments, PathSegment{From: current, To: subpathStart})
				}
				appendObject()
			}
			p.syntax.SetPos(lastPos)
			return
		}
	}
}

func pathPaintsStroke(op []byte) bool {
	switch string(op) {
	case "S", "s", "B", "B*", "b", "b*":
		return true
	default:
		return false
	}
}

func pathPaintOperatorCloses(op []byte) bool {
	switch string(op) {
	case "s", "b", "b*":
		return true
	default:
		return false
	}
}

// --- Do / form recursion (cpdf_streamcontentparser.cpp:770) ---

func (p *StreamContentParser) handleExecuteXObject() {
	name := p.getString(0)
	xobj := objects.ToStream(p.findResourceObj("XObject", name))
	if xobj == nil {
		return
	}
	subtype := xobj.GetDict().GetByteStringFor("Subtype")
	if subtype == "Form" {
		p.addForm(xobj, name)
		return
	}
	// Image (and other) subtypes are out of scope for text extraction.
}

// addForm ports AddForm (cpdf_streamcontentparser.cpp:810): snapshot the
// sub-states, recurse via the wired parseForm callback (sharing the recursion
// state), then emit a FormObject with matrix ctm * mt_content_to_user_.
func (p *StreamContentParser) addForm(stream *objects.Stream, name string) {
	if p.parseForm == nil {
		return
	}
	status := newAllStates()
	status.graphicStates = p.curStates.graphicStates

	form := p.parseForm(stream, name, status)
	if form == nil {
		return
	}

	matrix := p.curStates.CTM().Multiply(p.mtContentToUser)
	formObj := newFormObject(p.getCurrentStreamIndex(), form, matrix)
	formObj.SetResourceName(name)
	formObj.calcBoundingBox()
	formObj.graphicStates = p.curStates.graphicStates
	formObj.contentMarks = append([]ContentMark(nil), p.topContentMarks()...)
	p.objectHolder.AppendPageObject(formObj)
}

// --- resource lookup ---

// findResourceHolder ports FindResourceHolder (cpdf_streamcontentparser.cpp:
// 1218): resources_[type], falling back to page_resources_[type].
func (p *StreamContentParser) findResourceHolder(typ string) *objects.Dictionary {
	if p.resources == nil {
		return nil
	}
	if dict := p.resources.GetDictFor(typ); dict != nil {
		return dict
	}
	if p.resources == p.pageResources || p.pageResources == nil {
		return nil
	}
	return p.pageResources.GetDictFor(typ)
}

// findResourceObj ports FindResourceObj.
func (p *StreamContentParser) findResourceObj(typ, name string) objects.Object {
	holder := p.findResourceHolder(typ)
	if holder == nil {
		return nil
	}
	return holder.GetDirectObjectFor(name)
}

// findFont ports FindFont (cpdf_streamcontentparser.cpp:1226), adapted for the
// face-less, cache-less font port: load the /Font/<name> dict via font.Load and
// remember the resource name for the text object. No stock-font fallback exists
// here (the face-less port has no default ANSI face), so a missing font dict
// yields nil and the text run is skipped (no font => no text object).
func (p *StreamContentParser) findFont(name string) *font.Font {
	fontDict := objects.ToDictionary(p.findResourceObj("Font", name))
	if fontDict == nil {
		return nil
	}
	if cached, ok := p.fontCache()[fontDict]; ok {
		return cached
	}
	f := font.Load(fontDict, p.holder)
	if f != nil {
		p.fontResourceNames()[f] = name
		p.fontCache()[fontDict] = f
	}
	return f
}

// fontCache/fontResourceNames lazily back the per-parser font caches. They are
// stored on the object holder so nested form parsers share them (and so a font
// resolved under one name keeps that name).
func (p *StreamContentParser) fontCache() map[*objects.Dictionary]*font.Font {
	if p.objectHolder.fontCache == nil {
		p.objectHolder.fontCache = map[*objects.Dictionary]*font.Font{}
	}
	return p.objectHolder.fontCache
}

func (p *StreamContentParser) fontResourceNames() map[*font.Font]string {
	if p.objectHolder.fontResourceNames == nil {
		p.objectHolder.fontResourceNames = map[*font.Font]string{}
	}
	return p.objectHolder.fontResourceNames
}

func (p *StreamContentParser) fontResourceName(f *font.Font) string {
	if names := p.objectHolder.fontResourceNames; names != nil {
		if n, ok := names[f]; ok {
			return n
		}
	}
	return ""
}
