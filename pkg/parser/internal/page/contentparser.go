// Ported from core/fpdfapi/page/cpdf_contentparser.{h,cpp} @ pdfium 0db284a42.
//
// ContentParser concatenates a page's /Contents (single stream or array, each
// followed by a single 0x20 separator) into one buffer, records each stream's
// start offset, and runs the StreamContentParser in bounded steps. Form mode
// builds the parser with the form's /Matrix and parent matrix, and seeds the
// interpreter's clip with the form's /BBox — the C++ ctor does this too
// (cpdf_contentparser.cpp:78-95), and it is load-bearing rather than
// decorative: a form XObject's /BBox clips its content, so a figure drawn from
// an oversized form reports the form's extent instead of the visible one
// without it.
package page

import (
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

type stage int

const (
	stageGetContent stage = iota
	stagePrepareContent
	stageParse
	stageCheckClip
	stageComplete
)

const kParseStepLimit = 100

// ContentParser drives the per-stream parse for a PageObjectHolder.
type ContentParser struct {
	holder *PageObjectHolder
	parser *StreamContentParser
	stage  stage

	singleStream         *objects.StreamAcc
	streamArray          []*objects.StreamAcc
	streamSegmentOffsets []uint32
	streams              uint32
	currentOffset        uint32

	data           []byte
	streamForParse *objects.Stream // backing stream for the recursion cycle guard

	recursionState *FormRecursionState
	formMatrix     crt.Matrix
	parentMatrix   *crt.Matrix
	formStates     *AllStates
	isForm         bool

	// bbox is the rcBBox ctor argument: the page box for a page, the
	// transformed /BBox for a form. Handle_ShadeFill needs it, because an
	// unclipped `sh` covers the whole of it.
	bbox crt.FloatRect
	// formClip is the form's /BBox as a clip, nil when the form has no /BBox.
	formClip *crt.FloatRect
}

// newContentParserForPage ports CPDF_ContentParser(CPDF_Page*)
// (cpdf_contentparser.cpp:31).
func newContentParserForPage(holder *PageObjectHolder) *ContentParser {
	cp := &ContentParser{holder: holder, stage: stageGetContent, bbox: holder.bbox}
	content := holder.dict.GetDirectObjectFor("Contents")
	if content == nil {
		cp.stage = stageComplete
		return cp
	}
	switch v := content.(type) {
	case *objects.Stream:
		cp.singleStream = objects.NewStreamAcc(v)
		cp.singleStream.LoadAllDataFiltered()
		cp.streamForParse = v
		cp.stage = stagePrepareContent
	case *objects.Array:
		if v.Len() == 0 {
			cp.stage = stageComplete
			return cp
		}
		cp.streams = uint32(v.Len())
		cp.streamArray = make([]*objects.StreamAcc, cp.streams)
		// stays in stageGetContent
	default:
		cp.stage = stageComplete
	}
	return cp
}

// newContentParserForForm ports the form-mode CPDF_ContentParser
// (cpdf_contentparser.cpp:61).
func newContentParserForForm(form *Form, graphicStates *AllStates, parentMatrix *crt.Matrix, recursionState *FormRecursionState) *ContentParser {
	cp := &ContentParser{
		holder:         &form.PageObjectHolder,
		stage:          stageParse,
		recursionState: recursionState,
		parentMatrix:   parentMatrix,
		formStates:     graphicStates,
		isForm:         true,
	}
	dict := form.dict
	formMatrix := dict.GetMatrixFor("Matrix")
	if graphicStates != nil {
		formMatrix.Concat(graphicStates.CTM())
	}
	cp.formMatrix = formMatrix

	// The /BBox becomes both the parser's bbox and its initial clip, each
	// transformed by the form matrix and then the parent matrix — the same
	// two transforms the C++ ctor applies, in the same order.
	if bboxArray := dict.GetArrayFor("BBox"); bboxArray != nil {
		formBBox := bboxArray.GetRect()
		formBBox = formMatrix.TransformRect(formBBox)
		if parentMatrix != nil {
			formBBox = parentMatrix.TransformRect(formBBox)
		}
		cp.bbox = formBBox
		clip := formBBox
		cp.formClip = &clip
	}

	cp.singleStream = objects.NewStreamAcc(form.formStream)
	cp.singleStream.LoadAllDataFiltered()
	cp.streamForParse = form.formStream
	cp.data = cp.singleStream.GetSpan()
	return cp
}

// Continue ports CPDF_ContentParser::Continue(nil): drains to kComplete in one
// call (we never pass a pause indicator). Returns false when complete.
func (cp *ContentParser) Continue() bool {
	for cp.stage == stageGetContent {
		cp.stage = cp.getContent()
	}
	if cp.stage == stagePrepareContent {
		cp.stage = cp.prepareContent()
	}
	for cp.stage == stageParse {
		cp.stage = cp.parse()
	}
	if cp.stage == stageCheckClip {
		cp.stage = cp.checkClip()
	}
	return false
}

// TakeAllCTMs forwards to the stream parser's CTM map.
func (cp *ContentParser) TakeAllCTMs() map[int32]crt.Matrix {
	if cp.parser != nil {
		return cp.parser.TakeAllCTMs()
	}
	return map[int32]crt.Matrix{}
}

// getContent ports GetContent (array mode): resolve and load the i-th stream.
func (cp *ContentParser) getContent() stage {
	content := cp.holder.dict.GetArrayFor("Contents")
	var streamObj *objects.Stream
	if content != nil {
		streamObj = objects.ToStream(content.GetDirectObjectAt(int(cp.currentOffset)))
	}
	acc := objects.NewStreamAcc(streamObj)
	acc.LoadAllDataFiltered()
	cp.streamArray[cp.currentOffset] = acc
	cp.currentOffset++
	if cp.currentOffset == cp.streams {
		return stagePrepareContent
	}
	return stageGetContent
}

// prepareContent ports PrepareContent (cpdf_contentparser.cpp): concatenate the
// streams into one buffer with a single 0x20 separator after each, recording
// each stream's start offset. EXACT.
func (cp *ContentParser) prepareContent() stage {
	cp.currentOffset = 0
	if len(cp.streamArray) == 0 {
		cp.data = cp.singleStream.GetSpan()
		return stageParse
	}

	var safeSize uint32
	for _, s := range cp.streamArray {
		cp.streamSegmentOffsets = append(cp.streamSegmentOffsets, safeSize)
		safeSize += uint32(s.GetSize())
		safeSize++ // +1 for the separator byte
	}
	buffer := make([]byte, safeSize)
	dst := buffer
	for _, s := range cp.streamArray {
		span := s.GetSpan()
		copy(dst, span)
		dst = dst[len(span):]
		dst[0] = ' ' // overwrite the next byte with a SPACE separator
		dst = dst[1:]
	}
	cp.streamArray = nil
	cp.data = buffer
	// For a concatenated array the "stream backing" identity for the recursion
	// guard is the buffer itself; the page-level parse never recurses on its own
	// /Contents, so a nil stream key (no cycle guard) is correct here.
	cp.streamForParse = nil
	return stageParse
}

// parse ports Parse: build the stream parser once, then step it.
func (cp *ContentParser) parse() stage {
	if cp.parser == nil {
		if cp.isForm {
			cp.startFormParser()
		} else {
			cp.startPageParser()
		}
	}
	if cp.currentOffset >= uint32(len(cp.data)) {
		return stageCheckClip
	}
	if len(cp.streamSegmentOffsets) == 0 {
		cp.streamSegmentOffsets = append(cp.streamSegmentOffsets, 0)
	}
	consumed := cp.parser.Parse(cp.data, cp.currentOffset, kParseStepLimit, cp.streamSegmentOffsets, cp.streamForParse)
	cp.currentOffset += consumed
	if consumed == 0 {
		// Defensive: a step that consumes nothing (e.g. recursion-guard early
		// return on a re-entered form) would loop forever; advance to clip.
		return stageCheckClip
	}
	return stageParse
}

func (cp *ContentParser) startPageParser() {
	cp.recursionState = newFormRecursionState()
	cp.parser = newStreamContentParser(
		cp.holder.holder,
		cp.holder.pageResources,
		nil, // contentToUser
		cp.holder,
		cp.holder.resources,
		cp.bbox,
		nil, // graphicStates
		cp.recursionState,
	)
	cp.parser.GraphicStatesRefDefault()
	cp.wireForm(cp.parser)
}

func (cp *ContentParser) startFormParser() {
	cp.parser = newStreamContentParser(
		cp.holder.holder,
		cp.holder.pageResources,
		cp.parentMatrix,
		cp.holder,
		cp.holder.resources,
		cp.bbox,
		cp.formStates,
		cp.recursionState,
	)
	cp.parser.GetCurStates().SetCTM(cp.formMatrix)
	cp.parser.GetCurStates().SetParentMatrix(cp.formMatrix)
	if cp.formClip != nil {
		cp.parser.GetCurStates().MutableClipPath().IntersectRect(*cp.formClip)
	}
	cp.wireForm(cp.parser)
}

// wireForm installs the Form-XObject recursion callback so the stream parser
// can recurse without importing the form/page construction logic.
func (cp *ContentParser) wireForm(p *StreamContentParser) {
	p.parseForm = func(stream *objects.Stream, name string, graphicStates *AllStates) *Form {
		form := newForm(p.holder, p.pageResources, stream, p.resources)
		// Share the font caches across nested parsers.
		form.fontCache = cp.holder.fontCache
		form.fontResourceNames = cp.holder.fontResourceNames
		form.parseContentForm(graphicStates, nil, p.recursionState)
		// Propagate any newly-populated caches back up.
		cp.holder.fontCache = form.fontCache
		cp.holder.fontResourceNames = form.fontResourceNames
		return form
	}
}

// checkClip ports CheckClip: the redundant-rect-clip stripping is deferred
// (ClipPath carries no path data; HasRef is always false), so this is a no-op.
func (cp *ContentParser) checkClip() stage { return stageComplete }
