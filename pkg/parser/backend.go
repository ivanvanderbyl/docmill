package parser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	docpage "github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/document"
	pdfpage "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
	pdftext "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/text"
	docpdf "github.com/ivanvanderbyl/docmill/v2/pkg/pdf"
)

// Backend is the native pure-Go pkg/pdf.Backend (plan 009): document loading,
// page sizing, and text extraction with no cgo or external PDFium dependency.
type Backend struct{}

// NewBackend returns the native backend.
func NewBackend() docpdf.Backend { return &Backend{} }

// OpenBytes parses data into a Document.
func (b *Backend) OpenBytes(ctx context.Context, data []byte) (docpdf.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	buf := append([]byte(nil), data...)
	doc, perr := document.Open(buf)
	if perr != parser.Success {
		return nil, parserError(perr)
	}
	return &Document{doc: doc}, nil
}

// Close releases the backend (no shared resources).
func (b *Backend) Close() error { return nil }

// Document wraps the native document.
type Document struct {
	doc *document.Document
}

// PageCount returns the number of pages.
func (d *Document) PageCount(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return d.doc.GetPageCount(), nil
}

// Page returns a handle to page index.
func (d *Document) Page(ctx context.Context, index int) (docpdf.Page, error) {
	count := d.doc.GetPageCount()
	if index < 0 || index >= count {
		return nil, fmt.Errorf("page index %d out of range 0..%d", index, count-1)
	}
	return &Page{doc: d.doc, index: index}, nil
}

// Close releases the document.
func (d *Document) Close() error { return nil }

// AcroFormValues returns every terminal AcroForm field as field name -> value.
func (d *Document) AcroFormValues(ctx context.Context) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return d.doc.AcroFormFieldValues(), nil
}

// SetAcroFormValues updates terminal AcroForm field values by field name.
func (d *Document) SetAcroFormValues(ctx context.Context, values map[string]string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return d.doc.SetAcroFormFieldValues(values)
}

// WritePDF serialises the current native document state to w.
func (d *Document) WritePDF(ctx context.Context, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.doc.WritePDF(w)
}

// Page is a native page handle.
type Page struct {
	doc   *document.Document
	index int

	loadOnce sync.Once
	page     *pdfpage.Page
	size     geom.Size
	loadErr  error

	textOnce sync.Once
	text     *pdftext.TextPage
	textErr  error
}

// load resolves the page dict and builds the native page interpreter result.
func (p *Page) load() (*pdfpage.Page, geom.Size, error) {
	p.loadOnce.Do(func() {
		dict := p.doc.GetPageDict(p.index)
		if dict == nil {
			p.loadErr = fmt.Errorf("native pkg/parser: page %d dictionary not found", p.index)
			return
		}
		p.page = pdfpage.LoadPage(dict, p.doc)
		p.size = geom.Size{Width: float64(p.page.Width()), Height: float64(p.page.Height())}
	})
	return p.page, p.size, p.loadErr
}

func (p *Page) textPage() (*pdftext.TextPage, geom.Size, error) {
	p.textOnce.Do(func() {
		pg, _, err := p.load()
		if err != nil {
			p.textErr = err
			return
		}
		p.text = pdftext.New(pg, false)
	})
	return p.text, p.size, p.textErr
}

// Size returns the page dimensions in points.
func (p *Page) Size(ctx context.Context) (geom.Size, error) {
	if err := ctx.Err(); err != nil {
		return geom.Size{}, err
	}
	_, size, err := p.load()
	return size, err
}

// TextCells extracts the page's text rects, mirroring PDFium: build
// the textpage, segment into rects, convert to top-left cells, then merge
// fragmented rects (re-reading each merged region's text). Rects from the native
// textpage are in PDF user space (y-up), the same convention TextRectsToCells
// expects.
func (p *Page) TextCells(ctx context.Context) ([]docpage.TextCell, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tp, size, err := p.textPage()
	if err != nil {
		return nil, err
	}

	rects := tp.GetRectArray()
	rectBoxes := make([]crt.FloatRect, len(rects))
	for i, r := range rects {
		rectBoxes[i] = r.Box
	}
	rectTexts := tp.GetTextByRects(rectBoxes)
	textRects := make([]docpdf.TextRect, 0, len(rects))
	for i, r := range rects {
		// Re-extract each rect's text via the bounded-text path rather than using
		// the raw GetRectArray text: the rect text omits generated/degenerate
		// chars (so inter-word spaces are absent once spaces carry PDFium's zero
		// box), whereas GetTextByRect reinserts them — matching PDFium, which
		// fills rect text from FPDFText_GetBoundedText.
		textRects = append(textRects, docpdf.TextRect{
			Text:       rectTexts[i],
			Left:       float64(r.Box.Left),
			Top:        float64(r.Box.Top),
			Right:      float64(r.Box.Right),
			Bottom:     float64(r.Box.Bottom),
			FontSize:   r.FontSize,
			FontName:   r.FontName,
			FontWeight: r.FontWeight,
			FontFlags:  r.FontFlags,
		})
	}
	cells := docpdf.TextRectsToCells(textRects, size.Height)

	// Batched exclusive re-extraction: every merged group's box is handed to
	// the textpage at once, and each character is emitted into exactly one
	// cell — the one whose box covers it best. Without exclusivity, a glyph
	// whose charBox straddles two cell regions (big-operator limits and
	// sub/superscripts routinely overlap the neighbouring line's rect) is
	// swept into both rect queries and appears twice; with sequential
	// first-query-wins claiming, a tall math delimiter's rect grazing the
	// next prose line stole letters out of that line's own re-extraction.
	reextractAll := func(boxes []geom.Box) []string {
		rects := make([]crt.FloatRect, len(boxes))
		for i, box := range boxes {
			bounds := docpdf.TopLeftBoxToPDFiumBounds(box, size.Height)
			rects[i] = crt.NewFloatRect(
				float32(bounds.Left), float32(bounds.Bottom),
				float32(bounds.Right), float32(bounds.Top))
		}
		return tp.GetTextByRectsExclusive(rects)
	}
	cells = docpdf.MergeFragmentedCellsExclusive(cells, reextractAll, docpdf.MergeOptions{})
	return cells, nil
}

// WordTextCells extracts unmerged word-level text cells. The Markdown pipeline
// uses these as table-structure tokens while retaining TextCells for prose.
func (p *Page) WordTextCells(ctx context.Context) ([]docpage.TextCell, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tp, size, err := p.textPage()
	if err != nil {
		return nil, err
	}
	return textRectsToCells(tp.GetWordArray(), size.Height), nil
}

// FormFieldBox describes one AcroForm widget's placement on a page. Unlike
// FormFields (which feeds Markdown extraction with filled values only, one
// entry per field), this keeps unfilled fields and reports every widget of a
// multi-widget field.
type FormFieldBox struct {
	Name    string   // fully-qualified field name (the /T chain)
	Label   string   // human-readable label (/TU alternate name); "" when absent
	Type    string   // field type (/FT): Tx, Btn, Ch, or Sig
	Value   string   // current value; "" when unfilled
	OnState string   // checkbox/radio checked appearance-state name (e.g. "Yes"); "" otherwise
	Flags   int      // /Ff field flags (PDF 32000-1 Table 221/226/228), inherited
	Box     geom.Box // widget rectangle in top-left-origin page points
}

// FormFieldBoxes returns the bounding box and labelling of every AcroForm
// widget on this page, for layout consumers.
func (p *Page) FormFieldBoxes(ctx context.Context) ([]FormFieldBox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, size, err := p.load()
	if err != nil {
		return nil, err
	}
	fields := p.doc.PageFormFieldWidgets(p.doc.GetPageDict(p.index))
	if len(fields) == 0 {
		return nil, nil
	}

	out := make([]FormFieldBox, 0, len(fields))
	for _, field := range fields {
		box := geom.Box{
			L:      float64(field.Rect.Left),
			T:      float64(field.Rect.Top),
			R:      float64(field.Rect.Right),
			B:      float64(field.Rect.Bottom),
			Origin: geom.BottomLeft,
		}
		out = append(out, FormFieldBox{
			Name:    field.Name,
			Label:   field.AlternateName,
			Type:    field.Type,
			Value:   field.Value,
			OnState: field.OnState,
			Flags:   field.Flags,
			Box:     box.WithOrigin(geom.TopLeft, size.Height),
		})
	}
	return out, nil
}

// FormFields extracts filled AcroForm widget values positioned on this page.
func (p *Page) FormFields(ctx context.Context) ([]docpage.FormField, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, size, err := p.load()
	if err != nil {
		return nil, err
	}
	fields := p.doc.PageFormFields(p.doc.GetPageDict(p.index))
	if len(fields) == 0 {
		return nil, nil
	}

	out := make([]docpage.FormField, 0, len(fields))
	for _, field := range fields {
		name := field.AlternateName
		if name == "" {
			name = field.Name
		}
		box := geom.Box{
			L:      float64(field.Rect.Left),
			T:      float64(field.Rect.Top),
			R:      float64(field.Rect.Right),
			B:      float64(field.Rect.Bottom),
			Origin: geom.BottomLeft,
		}
		out = append(out, docpage.FormField{
			Name:  name,
			Value: field.Value,
			Box:   box.WithOrigin(geom.TopLeft, size.Height),
		})
	}
	return out, nil
}

func textRectsToCells(rects []pdftext.Rect, pageHeight float64) []docpage.TextCell {
	textRects := make([]docpdf.TextRect, 0, len(rects))
	for _, r := range rects {
		textRects = append(textRects, docpdf.TextRect{
			Text:       r.Text,
			Left:       float64(r.Box.Left),
			Top:        float64(r.Box.Top),
			Right:      float64(r.Box.Right),
			Bottom:     float64(r.Box.Bottom),
			FontSize:   r.FontSize,
			FontName:   r.FontName,
			FontWeight: r.FontWeight,
			FontFlags:  r.FontFlags,
		})
	}
	return docpdf.TextRectsToCells(textRects, pageHeight)
}

// TextInRect returns the text whose glyphs fall within box.
func (p *Page) TextInRect(ctx context.Context, box geom.Box) (string, error) {
	tp, size, err := p.textPage()
	if err != nil {
		return "", err
	}
	bounds := docpdf.TopLeftBoxToPDFiumBounds(box, size.Height)
	return tp.GetTextByRect(crt.NewFloatRect(
		float32(bounds.Left), float32(bounds.Bottom),
		float32(bounds.Right), float32(bounds.Top))), nil
}

// RulingSegments returns stroked straight path segments in top-left page
// coordinates. These are used by the Markdown table detector to reconstruct
// ruled table grids.
func (p *Page) RulingSegments(ctx context.Context) ([]docpage.RulingSegment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pg, size, err := p.load()
	if err != nil {
		return nil, err
	}

	var segments []docpage.RulingSegment
	var collect func([]pdfpage.PageObject)
	collect = func(objects []pdfpage.PageObject) {
		for _, obj := range objects {
			switch v := obj.(type) {
			case *pdfpage.PathObject:
				// Stroked paths only. The interpreter now also emits FILLED
				// paths, which it previously dropped, and a filled shape's
				// outline is not a drawn line — letting the four edges of every
				// solid block through here would invent a table grid around it.
				// Ruling geometry is unchanged by that new capability.
				if !v.IsStroked() {
					continue
				}
				for _, segment := range v.Segments() {
					segments = append(segments, docpage.RulingSegment{
						FromX:  float64(segment.From.X),
						FromY:  size.Height - float64(segment.From.Y),
						ToX:    float64(segment.To.X),
						ToY:    size.Height - float64(segment.To.Y),
						Width:  float64(v.StrokeWidth()),
						Origin: geom.TopLeft,
					})
				}
			case *pdfpage.FormObject:
				collect(v.Objects())
			}
		}
	}
	collect(pg.Objects())
	return segments, nil
}

// DrawnObjects returns everything the page draws, in draw order, with each
// object's VISIBLE box in top-left page coordinates.
//
// RulingSegments answers a narrow question — where are the ruled lines — and
// discards every other kind of ink. This is the whole picture: images,
// shadings, filled shapes and text, each clipped to what actually shows.
//
// Nested form content is emitted after the form that holds it, at Depth+1, with
// the form's clip intersected down. The interpreter deliberately does not push
// the enclosing clip into a form's own states (matching the C++, which copies
// the other four sub-states and not the clip), so the intersection has to
// happen here, on the way down.
func (p *Page) DrawnObjects(ctx context.Context) ([]docpage.DrawnObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pg, size, err := p.load()
	if err != nil {
		return nil, err
	}

	var out []docpage.DrawnObject
	var collect func(objects []pdfpage.PageObject, inherited pdfpage.ClipPath, depth int)
	collect = func(objects []pdfpage.PageObject, inherited pdfpage.ClipPath, depth int) {
		for _, obj := range objects {
			if !obj.IsActive() {
				continue
			}
			visible, shown := obj.VisibleRect()
			if shown {
				if clipped, ok := inherited.Clip(visible); ok {
					visible = clipped
				} else {
					shown = false
				}
			}

			if shown {
				drawn := docpage.DrawnObject{
					Kind:  drawnKindOf(obj),
					Box:   pdfiumRectToTopLeftBox(visible, size.Height),
					Depth: depth,
				}
				if path, ok := obj.(*pdfpage.PathObject); ok {
					drawn.Stroked = path.IsStroked()
					drawn.Filled = path.IsFilled()
				}
				if image, ok := obj.(*pdfpage.ImageObject); ok {
					drawn.Inline = image.IsInline()
				}
				out = append(out, drawn)
			}

			if form, ok := obj.(*pdfpage.FormObject); ok {
				// Descend even when the form itself was clipped away, so the
				// child clip decides per object rather than the form's union
				// box discarding content that does show.
				child := inherited
				if box, bounded := obj.ClipPath().Box(); bounded {
					child.IntersectRect(box)
				}
				collect(form.Objects(), child, depth+1)
			}
		}
	}
	collect(pg.Objects(), pdfpage.ClipPath{}, 0)
	return out, nil
}

func drawnKindOf(obj pdfpage.PageObject) docpage.DrawnKind {
	switch obj.Kind() {
	case pdfpage.KindText:
		return docpage.DrawnText
	case pdfpage.KindPath:
		return docpage.DrawnPath
	case pdfpage.KindImage:
		return docpage.DrawnImage
	case pdfpage.KindShading:
		return docpage.DrawnShading
	default:
		return docpage.DrawnForm
	}
}

// pdfiumRectToTopLeftBox converts a bottom-left user-space rect to docmill's
// top-left box, which is the same flip RulingSegments applies to its endpoints.
func pdfiumRectToTopLeftBox(rect crt.FloatRect, pageHeight float64) geom.Box {
	return geom.Box{
		L:      float64(rect.Left),
		R:      float64(rect.Right),
		T:      pageHeight - float64(rect.Top),
		B:      pageHeight - float64(rect.Bottom),
		Origin: geom.TopLeft,
	}
}

func parserError(e parser.Error) error {
	switch e {
	case parser.PasswordError:
		return errors.New("native pkg/parser: password required")
	case parser.HandlerError:
		return errors.New("native pkg/parser: encrypted PDF unsupported (plan 009 Phase E)")
	default:
		return errors.New("native pkg/parser: failed to parse PDF (FORMAT_ERROR)")
	}
}
