// Ported from core/fpdfapi/page/cpdf_pageobjectholder.{h,cpp} and
// cpdf_form.{h,cpp} @ pdfium 0db284a42.
//
// PageObjectHolder accumulates the parsed page objects and the per-stream CTM
// map, and drives the parse lifecycle (StartParse/ContinueParse). Form is a
// PageObjectHolder wrapping a Form XObject stream; the interpreter recurses into
// it on a Do of a /Form XObject. FormObject is the emitted page object.
package page

import (
	"slices"
	"sort"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/font"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// parseState mirrors CPDF_PageObjectHolder::ParseState.
type parseState int

const (
	stateNotParsed parseState = iota
	stateParsing
	stateParsed
)

// PageObjectHolder owns the parsed page-object list and the stream->CTM map.
// Page and Form embed it.
type PageObjectHolder struct {
	holder        objects.IndirectObjectHolder
	dict          *objects.Dictionary
	resources     *objects.Dictionary
	pageResources *objects.Dictionary

	pageObjectList []PageObject
	allCTMs        map[int32]crt.Matrix
	sortedCTMKeys  []int32

	// bbox is the holder's own box — the page box for a Page, the transformed
	// /BBox for a Form. It reaches the stream parser as the rcBBox argument.
	bbox crt.FloatRect

	state  parseState
	parser *ContentParser

	// Font caches shared across nested parsers (see findFont). They live here
	// so a font resolved by name keeps that name across the whole parse.
	fontCache         map[*objects.Dictionary]*font.Font
	fontResourceNames map[*font.Font]string
}

// AppendPageObject ports AppendPageObject: append a non-nil object.
func (h *PageObjectHolder) AppendPageObject(obj PageObject) {
	if obj == nil {
		return
	}
	h.pageObjectList = append(h.pageObjectList, obj)
}

// PageObjectCount returns the total number of parsed objects (active or not).
func (h *PageObjectHolder) PageObjectCount() int { return len(h.pageObjectList) }

// GetActivePageObjectCount ports GetActivePageObjectCount.
func (h *PageObjectHolder) GetActivePageObjectCount() int {
	n := 0
	for _, obj := range h.pageObjectList {
		if obj.IsActive() {
			n++
		}
	}
	return n
}

// Objects returns the parsed objects in draw order.
func (h *PageObjectHolder) Objects() []PageObject { return h.pageObjectList }

// --- parse lifecycle (page spec §4) ---

// startParse ports StartParse: require NotParsed; set parser; state=Parsing.
func (h *PageObjectHolder) startParse(p *ContentParser) {
	if h.state != stateNotParsed {
		return
	}
	h.parser = p
	h.state = stateParsing
}

// continueParse ports ContinueParse(nil): run to completion in one shot.
func (h *PageObjectHolder) continueParse() {
	if h.state == stateParsed {
		return
	}
	if h.state != stateParsing {
		return
	}
	if h.parser.Continue() {
		return // more to do (only with a pause indicator, which we never pass)
	}
	h.state = stateParsed
	h.allCTMs = h.parser.TakeAllCTMs()
	h.buildSortedCTMKeys()
	h.parser = nil
}

// parseContent ports ParseContent: start if needed, then drain.
func (h *PageObjectHolder) parseContent() {
	if h.state == stateParsed {
		return
	}
	if h.state == stateNotParsed {
		h.startParse(newContentParserForPage(h))
	}
	for h.state != stateParsed {
		h.continueParse()
	}
}

func (h *PageObjectHolder) buildSortedCTMKeys() {
	h.sortedCTMKeys = h.sortedCTMKeys[:0]
	for k := range h.allCTMs {
		h.sortedCTMKeys = append(h.sortedCTMKeys, k)
	}
	slices.Sort(h.sortedCTMKeys)
}

// kNoContentStreamSentinel mirrors the -1 sentinel used by the CTM lookups.
const kNoContentStreamSentinel int32 = -1

// GetCTMAtEndOfStream ports GetCTMAtEndOfStream(stream>=0): lower_bound on the
// sorted keys, else the last key's value, else identity.
func (h *PageObjectHolder) GetCTMAtEndOfStream(stream int32) crt.Matrix {
	if len(h.allCTMs) == 0 {
		return crt.IdentityMatrix()
	}
	// lower_bound: first key >= stream.
	idx := sort.Search(len(h.sortedCTMKeys), func(i int) bool { return h.sortedCTMKeys[i] >= stream })
	if idx < len(h.sortedCTMKeys) {
		return h.allCTMs[h.sortedCTMKeys[idx]]
	}
	return h.allCTMs[h.sortedCTMKeys[len(h.sortedCTMKeys)-1]]
}

// --- Form (cpdf_form.cpp) ---

// chooseResourcesDict ports CPDF_Form::ChooseResourcesDict: first non-nil of
// (form's own /Resources, parent resources, page resources).
func chooseResourcesDict(pResources, pParentResources, pPageResources *objects.Dictionary) *objects.Dictionary {
	if pResources != nil {
		return pResources
	}
	if pParentResources != nil {
		return pParentResources
	}
	return pPageResources
}

// Form is a PageObjectHolder wrapping a Form XObject content stream.
type Form struct {
	PageObjectHolder
	formStream     *objects.Stream
	recursionState *FormRecursionState
}

// newForm ports the CPDF_Form 4-arg ctor (cpdf_form.cpp:50).
func newForm(holder objects.IndirectObjectHolder, pageResources *objects.Dictionary, formStream *objects.Stream, parentResources *objects.Dictionary) *Form {
	dict := formStream.GetDict()
	resources := chooseResourcesDict(dict.GetDictFor("Resources"), parentResources, pageResources)
	f := &Form{
		PageObjectHolder: PageObjectHolder{
			holder:        holder,
			dict:          dict,
			resources:     resources,
			pageResources: pageResources,
		},
		formStream: formStream,
	}
	return f
}

// GetStream returns the form's content stream.
func (f *Form) GetStream() *objects.Stream { return f.formStream }

// parseContentForm ports CPDF_Form::ParseContent(graphicStates, parentMatrix,
// recursionState): start with the form-mode content parser, then drain.
func (f *Form) parseContentForm(graphicStates *AllStates, parentMatrix *crt.Matrix, recursionState *FormRecursionState) {
	if f.state == stateParsed {
		return
	}
	if f.state == stateNotParsed {
		rs := recursionState
		if rs == nil {
			rs = f.recursionState
		}
		f.startParse(newContentParserForForm(f, graphicStates, parentMatrix, rs))
	}
	for f.state != stateParsed {
		f.continueParse()
	}
}

// calcBoundingBoxRect ports CPDF_Form::CalcBoundingBox over active objects.
func (f *Form) calcBoundingBoxRect() crt.FloatRect {
	if f.GetActivePageObjectCount() == 0 {
		return crt.FloatRect{}
	}
	left := float32(1000000)
	right := float32(-1000000)
	bottom := float32(1000000)
	top := float32(-1000000)
	for _, obj := range f.pageObjectList {
		if !obj.IsActive() {
			continue
		}
		r := obj.Rect()
		if r.Left < left {
			left = r.Left
		}
		if r.Right > right {
			right = r.Right
		}
		if r.Bottom < bottom {
			bottom = r.Bottom
		}
		if r.Top > top {
			top = r.Top
		}
	}
	return crt.NewFloatRect(left, bottom, right, top)
}

// --- FormObject ---

// FormObject implements PageObject: a parsed Form XObject reference.
type FormObject struct {
	baseObject
	form       *Form
	formMatrix crt.Matrix
}

// newFormObject ports CPDF_FormObject(content_stream, form, matrix).
func newFormObject(contentStream int32, form *Form, matrix crt.Matrix) *FormObject {
	return &FormObject{
		baseObject: newBaseObject(contentStream),
		form:       form,
		formMatrix: matrix,
	}
}

func (o *FormObject) getType() objectType { return typeForm }

// Kind reports that this is a form XObject reference.
func (o *FormObject) Kind() ObjectKind { return KindForm }

// FormMatrix returns the form-space -> user-space matrix (ctm * contentToUser
// of the enclosing parser).
func (o *FormObject) FormMatrix() crt.Matrix { return o.formMatrix }

// Objects returns the form's nested page objects in draw order.
func (o *FormObject) Objects() []PageObject { return o.form.Objects() }

// calcBoundingBox ports CPDF_FormObject::CalcBoundingBox: form bbox transformed
// by the form matrix.
func (o *FormObject) calcBoundingBox() {
	formBox := o.form.calcBoundingBoxRect()
	o.SetRect(o.formMatrix.TransformRect(formBox))
}
