// Ported from core/fpdfapi/page/cpdf_page.{h,cpp} @ pdfium 0db284a42.
//
// Page loads a page dict, inherits /Resources up the page tree, computes the
// page size + page matrix from /MediaBox,/CropBox,/Rotate, and on first call to
// Objects() runs the content-stream interpreter to produce the ordered list of
// text/form objects. DisplayMatrix maps PDF user space to device space
// (origin top-left).
package page

import (
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// Page is a parsed PDF page. It embeds the PageObjectHolder accumulator.
type Page struct {
	PageObjectHolder

	pageWidth  float32
	pageHeight float32
	pageMatrix crt.Matrix
	parsed     bool
}

// LoadPage ports the CPDF_Page constructor sequence: resolve inherited
// /Resources, then UpdateDimensions. holder resolves indirect objects (a
// *document.Document satisfies it). The /Group transparency info PDFium also
// loads here is inert for text extraction and is not tracked.
func LoadPage(pageDict *objects.Dictionary, holder objects.IndirectObjectHolder) *Page {
	if pageDict == nil {
		return nil
	}
	p := &Page{
		PageObjectHolder: PageObjectHolder{
			holder: holder,
			dict:   pageDict,
		},
	}
	// page_size_ initial {100,100}.
	p.pageWidth = 100
	p.pageHeight = 100

	// resources_ = GetPageAttr("Resources"); page_resources_ = resources_.
	if res := objects.ToDictionary(p.getPageAttr("Resources")); res != nil {
		p.resources = res
		p.pageResources = res
	}

	p.updateDimensions()
	return p
}

// getPageAttr ports GetPageAttr: inherited lookup up /Parent, returning the
// first direct object found, guarding against cycles.
func (p *Page) getPageAttr(name string) objects.Object {
	visited := map[*objects.Dictionary]struct{}{}
	d := p.dict
	for d != nil {
		if _, seen := visited[d]; seen {
			break
		}
		if obj := d.GetDirectObjectFor(name); obj != nil {
			return obj
		}
		visited[d] = struct{}{}
		d = d.GetDictFor("Parent")
	}
	return nil
}

// getBox ports GetBox(name): the named box as a normalized rect, or zero.
func (p *Page) getBox(name string) crt.FloatRect {
	box := crt.FloatRect{}
	if arr := objects.ToArray(p.getPageAttr(name)); arr != nil {
		box = arr.GetRect()
		box.Normalize()
	}
	return box
}

// getPageRotation ports GetPageRotation: (Rotate/90) mod 4, normalized to 0..3.
func (p *Page) getPageRotation() int {
	n := 0
	if rot := p.getPageAttr("Rotate"); rot != nil {
		n = (rot.GetInteger() / 90) % 4
	}
	if n < 0 {
		n += 4
	}
	return n
}

// updateDimensions ports UpdateDimensions (cpdf_page.cpp). EXACT.
func (p *Page) updateDimensions() {
	mediabox := p.getBox("MediaBox")
	if mediabox.IsEmpty() {
		mediabox = crt.NewFloatRect(0, 0, 612, 792) // US Letter default
	}

	bbox := p.getBox("CropBox")
	if bbox.IsEmpty() {
		bbox = mediabox
	} else {
		bbox.Intersect(mediabox)
	}
	p.pageWidth = bbox.Width()
	p.pageHeight = bbox.Height()
	// CPDF_Page::GetBBox, which the content parser passes through as rcBBox.
	// It stays in unrotated user space: page_matrix_ is applied downstream, and
	// the interpreter it feeds works in user space too.
	p.bbox = bbox

	switch p.getPageRotation() {
	case 0:
		p.pageMatrix = crt.NewMatrix(1, 0, 0, 1, -bbox.Left, -bbox.Bottom)
	case 1:
		p.pageWidth, p.pageHeight = p.pageHeight, p.pageWidth
		p.pageMatrix = crt.NewMatrix(0, -1, 1, 0, -bbox.Bottom, bbox.Right)
	case 2:
		p.pageMatrix = crt.NewMatrix(-1, 0, 0, -1, bbox.Right, bbox.Top)
	case 3:
		p.pageWidth, p.pageHeight = p.pageHeight, p.pageWidth
		p.pageMatrix = crt.NewMatrix(0, 1, -1, 0, bbox.Top, -bbox.Left)
	}
}

// Width ports GetPageWidth.
func (p *Page) Width() float32 { return p.pageWidth }

// Height ports GetPageHeight.
func (p *Page) Height() float32 { return p.pageHeight }

// DisplayMatrix ports GetDisplayMatrix (cpdf_page.cpp): PDF user space ->
// device (origin top-left), for FloatRect{0,0,Width,Height} at rotation 0.
func (p *Page) DisplayMatrix() crt.Matrix {
	return p.displayMatrixForFloatRect(crt.NewFloatRect(0, 0, p.Width(), p.Height()), 0)
}

// displayMatrixForFloatRect ports GetDisplayMatrixForFloatRect. Every quotient
// is a single float32 division (no FMA-sensitive fusion); the final compose is
// page_matrix_ * m.
func (p *Page) displayMatrixForFloatRect(rect crt.FloatRect, rotation int) crt.Matrix {
	if p.pageWidth == 0 || p.pageHeight == 0 {
		return crt.IdentityMatrix()
	}
	var x0, y0, x1, y1, x2, y2 float32
	switch rotation % 4 {
	case 0:
		x0, y0 = rect.Left, rect.Top
		x1, y1 = rect.Left, rect.Bottom
		x2, y2 = rect.Right, rect.Top
	case 1:
		x0, y0 = rect.Left, rect.Bottom
		x1, y1 = rect.Right, rect.Bottom
		x2, y2 = rect.Left, rect.Top
	case 2:
		x0, y0 = rect.Right, rect.Bottom
		x1, y1 = rect.Right, rect.Top
		x2, y2 = rect.Left, rect.Bottom
	case 3:
		x0, y0 = rect.Right, rect.Top
		x1, y1 = rect.Left, rect.Top
		x2, y2 = rect.Right, rect.Bottom
	default:
		// Negative rotation: return the all-zero matrix (public-API quirk).
		return crt.NewMatrix(0, 0, 0, 0, 0, 0)
	}
	a := (x2 - x0) / p.pageWidth
	b := (y2 - y0) / p.pageWidth
	c := (x1 - x0) / p.pageHeight
	d := (y1 - y0) / p.pageHeight
	m := crt.NewMatrix(a, b, c, d, x0, y0)
	return p.pageMatrix.Multiply(m)
}

// Objects parses the page content on first call and returns the ordered list of
// page objects (text and form, recursively) as drawn.
func (p *Page) Objects() []PageObject {
	if !p.parsed {
		p.parseContent()
		p.parsed = true
	}
	return p.PageObjectHolder.Objects()
}
