// Ported from core/fpdfapi/page/cpdf_pageobject.{h,cpp} @ pdfium 0db284a42.
//
// The base PageObject carries the shared state the interpreter touches: the
// graphic states snapshot at emit time, the bounding box in user space, the
// content-stream index, the resource name, the marked-content snapshot, and the
// active flag. For text extraction only TextObject and FormObject are
// materialised; path/image/shading objects are accepted-and-discarded so the
// stream parses.
package page

import (
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
)

// kNoContentStream is CPDF_PageObject::kNoContentStream.
const kNoContentStream int32 = -1

// objectType mirrors CPDF_PageObject::Type (1-based, this order).
type objectType int

const (
	typeText    objectType = 1
	typePath    objectType = 2
	typeImage   objectType = 3
	typeShading objectType = 4
	typeForm    objectType = 5
)

// PageObject is the sealed base interface every parsed object implements.
type PageObject interface {
	// Rect returns the object's bounding box in user space.
	Rect() crt.FloatRect
	// IsActive reports whether the object is active (always true on the read
	// path; the mutation APIs that toggle it are not ported).
	IsActive() bool

	getType() objectType
}

// ContentMark is one BDC/BMC marked-content entry (snapshot). The textpage
// reads /ActualText out of these. It mirrors CPDF_ContentMarks::Item: a tag
// plus either a direct property dict (BDC <dict>) or a named-resource dict
// (BDC /name resolved from /Properties); nil for BMC.
type ContentMark struct {
	// Tag is the marked-content tag (e.g. "Span", "ReversedChars").
	Tag string
	// PropertyName is the /Properties resource key, "" for an inline dict or BMC.
	PropertyName string
	// Params is the property dictionary, or nil for a bare BMC mark.
	Params *objects.Dictionary
}

// baseObject is embedded by the concrete page objects.
type baseObject struct {
	graphicStates GraphicStates
	rect          crt.FloatRect
	contentMarks  []ContentMark
	contentStream int32
	resourceName  string
	isActive      bool
}

func newBaseObject(contentStream int32) baseObject {
	return baseObject{
		graphicStates: newGraphicStates(),
		contentStream: contentStream,
		isActive:      true,
	}
}

// Rect returns the object's user-space bounding box.
func (b *baseObject) Rect() crt.FloatRect { return b.rect }

// SetRect sets the bounding box (the interpreter writes it from CalcPositionData).
func (b *baseObject) SetRect(r crt.FloatRect) { b.rect = r }

// IsActive reports whether the object is active.
func (b *baseObject) IsActive() bool { return b.isActive }

// SetIsActive toggles the active flag.
func (b *baseObject) SetIsActive(v bool) { b.isActive = v }

// GetContentStream returns the /Contents array index that produced the object
// (kNoContentStream for synthetic).
func (b *baseObject) GetContentStream() int32 { return b.contentStream }

// GetResourceName returns the /Name resource key (font/xobject name).
func (b *baseObject) GetResourceName() string { return b.resourceName }

// SetResourceName sets the resource name.
func (b *baseObject) SetResourceName(name string) { b.resourceName = name }

// ContentMarks returns the marked-content snapshot at emit time.
func (b *baseObject) ContentMarks() []ContentMark { return b.contentMarks }
