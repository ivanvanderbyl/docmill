package document

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	pdfpage "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/parser"
)

// ErrSignedDocument prevents rewriting an existing digital signature.
var ErrSignedDocument = errors.New("signed documents cannot be rewritten")

// ImageStamp contains unpremultiplied RGB pixels and an optional alpha plane.
type ImageStamp struct {
	PageIndex     int
	Rect          geom.Box
	Width, Height int
	RGB, Alpha    []byte
}

// WriteImageStamps preserves the full document by editing a private, parsed
// copy. Image XObjects and drawing matrices follow PDFium 6688c576's
// FPDFImageObj_SetBitmap and CPDF_PageContentGenerator::ProcessImage.
func (d *Document) WriteImageStamps(ctx context.Context, w io.Writer, stamps []ImageStamp) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d == nil || d.prs == nil {
		return fmt.Errorf("stamp images: nil document")
	}
	if w == nil {
		return fmt.Errorf("stamp images: nil writer")
	}
	signatures, err := d.Signatures()
	if err != nil {
		return fmt.Errorf("stamp images: inspect signatures: %w", err)
	}
	if len(signatures) > 0 {
		return ErrSignedDocument
	}
	if len(stamps) == 0 {
		return fmt.Errorf("stamp images: empty selection")
	}
	var snapshot bytes.Buffer
	if err := d.WritePDF(&snapshot); err != nil {
		return err
	}
	copied, status := Open(snapshot.Bytes())
	if status != parser.Success {
		return fmt.Errorf("stamp images: cannot reopen document copy")
	}
	all, last := copied.objectsForWrite()
	writer := stampWriter{ctx: ctx, doc: copied, objects: all, last: last, pages: make(map[int]*stampPage)}
	for i, stamp := range stamps {
		if err := writer.addStamp(stamp); err != nil {
			return fmt.Errorf("stamp %d: %w", i, err)
		}
	}
	if err := copied.DecryptionError(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return writePDFObjects(w, writer.objects, writer.last, max(14, copied.prs.FileVersion()), copied.trailerForWrite(writer.last))
}

type stampPage struct {
	dict     *objects.Dictionary
	xobjects *objects.Dictionary
	content  *objects.Stream
	drawing  bytes.Buffer
	display  *pdfpage.Page
}

type stampWriter struct {
	ctx     context.Context
	doc     *Document
	objects map[uint32]objects.Object
	last    uint32
	pages   map[int]*stampPage
}

func (w *stampWriter) addObject(obj objects.Object) *objects.Reference {
	w.last++
	obj.SetObjNum(w.last)
	w.objects[w.last] = obj
	return objects.NewReference(w, w.last)
}
func (w *stampWriter) GetOrParseIndirectObject(n uint32) objects.Object { return w.objects[n] }

func (w *stampWriter) addStamp(stamp ImageStamp) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if stamp.Width <= 0 || stamp.Height <= 0 || stamp.Width > 16*1024*1024 || stamp.Height > 16*1024*1024 || int64(stamp.Width)*int64(stamp.Height) > 16*1024*1024 {
		return fmt.Errorf("invalid image dimensions")
	}
	count := stamp.Width * stamp.Height
	if len(stamp.RGB) != count*3 || (stamp.Alpha != nil && len(stamp.Alpha) != count) {
		return fmt.Errorf("invalid image pixels")
	}
	page, err := w.stampPage(stamp.PageIndex)
	if err != nil {
		return err
	}
	matrix, err := stampMatrix(page.display, stamp.Rect)
	if err != nil {
		return err
	}
	image, err := w.imageObject(stamp)
	if err != nil {
		return err
	}
	name := ""
	for i := 1; name == ""; i++ {
		candidate := fmt.Sprintf("DMStamp%d", i)
		if !page.xobjects.KeyExist(candidate) {
			name = candidate
		}
	}
	page.xobjects.SetFor(name, image)
	fmt.Fprintf(&page.drawing, "q\n%.9g %.9g %.9g %.9g %.9g %.9g cm\n/%s Do\nQ\n", matrix.A, matrix.B, matrix.C, matrix.D, matrix.E, matrix.F, name)
	page.content.SetData(page.drawing.Bytes())
	return nil
}

func (w *stampWriter) stampPage(index int) (*stampPage, error) {
	if existing := w.pages[index]; existing != nil {
		return existing, nil
	}
	if index < 0 || index >= w.doc.GetPageCount() {
		return nil, fmt.Errorf("page index %d out of range", index)
	}
	dict := w.doc.GetPageDict(index)
	if dict == nil {
		return nil, fmt.Errorf("page %d dictionary missing", index)
	}
	resources := objects.ToDictionary(objects.Direct(inheritedPageValue(dict, "Resources")))
	if resources == nil {
		resources = objects.NewDictionary()
	} else {
		resources = objects.ToDictionary(resources.Clone())
	}
	xobjects := resources.GetDictFor("XObject")
	if xobjects == nil {
		xobjects = objects.NewDictionary()
	} else {
		xobjects = objects.ToDictionary(xobjects.Clone())
	}
	resources.SetFor("XObject", xobjects)
	dict.SetFor("Resources", resources)
	contents := objects.NewArray()
	contents.Append(w.addObject(objects.NewStreamFromSpan([]byte("q\n"))))
	original := dict.GetObjectFor("Contents")
	if array := objects.ToArray(objects.Direct(original)); array != nil {
		for i := 0; i < array.Len(); i++ {
			contents.Append(array.GetObjectAt(i))
		}
	} else if original != nil {
		contents.Append(original)
	}
	page := &stampPage{dict: dict, xobjects: xobjects, content: objects.NewStreamFromSpan(nil), display: pdfpage.LoadPage(dict, w.doc)}
	page.drawing.WriteString("\nQ\n")
	contents.Append(w.addObject(page.content))
	dict.SetFor("Contents", contents)
	w.pages[index] = page
	return page, nil
}

func normalizedStampRect(page *pdfpage.Page, rect geom.Box) (geom.Box, error) {
	width, height := float64(page.Width()), float64(page.Height())
	if rect.Origin == "" {
		rect.Origin = geom.TopLeft
	}
	if rect.Origin != geom.TopLeft && rect.Origin != geom.BottomLeft {
		return geom.Box{}, fmt.Errorf("invalid rectangle origin")
	}
	rect = rect.WithOrigin(geom.TopLeft, height)
	for _, value := range []float64{width, height, rect.L, rect.T, rect.R, rect.B} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return geom.Box{}, fmt.Errorf("nonfinite rectangle or page size")
		}
	}
	if width <= 0 || height <= 0 || rect.L < 0 || rect.T < 0 || rect.R > width || rect.B > height || rect.R <= rect.L || rect.B <= rect.T {
		return geom.Box{}, fmt.Errorf("rectangle must fit inside visible page")
	}
	return rect, nil
}

func stampMatrix(page *pdfpage.Page, rect geom.Box) (crt.Matrix, error) {
	var err error
	rect, err = normalizedStampRect(page, rect)
	if err != nil {
		return crt.Matrix{}, err
	}
	inverse := page.DisplayMatrix().GetInverse()
	bottomLeft := inverse.Transform(crt.PointF{X: float32(rect.L), Y: float32(rect.B)})
	bottomRight := inverse.Transform(crt.PointF{X: float32(rect.R), Y: float32(rect.B)})
	topLeft := inverse.Transform(crt.PointF{X: float32(rect.L), Y: float32(rect.T)})
	matrix := crt.NewMatrix(bottomRight.X-bottomLeft.X, bottomRight.Y-bottomLeft.Y, topLeft.X-bottomLeft.X, topLeft.Y-bottomLeft.Y, bottomLeft.X, bottomLeft.Y)
	if (matrix.A == 0 && matrix.B == 0) || (matrix.C == 0 && matrix.D == 0) {
		return crt.Matrix{}, fmt.Errorf("rectangle collapses at page coordinate precision")
	}
	return matrix, nil
}

func (w *stampWriter) imageObject(stamp ImageStamp) (*objects.Reference, error) {
	rgb, err := flateStamp(stamp.RGB)
	if err != nil {
		return nil, err
	}
	dict := stampImageDictionary(stamp.Width, stamp.Height, "DeviceRGB")
	if stamp.Alpha != nil {
		alpha, err := flateStamp(stamp.Alpha)
		if err != nil {
			return nil, err
		}
		mask := objects.NewStreamFromData(alpha, stampImageDictionary(stamp.Width, stamp.Height, "DeviceGray"))
		dict.SetFor("SMask", w.addObject(mask))
	}
	return w.addObject(objects.NewStreamFromData(rgb, dict)), nil
}

func stampImageDictionary(width, height int, space string) *objects.Dictionary {
	dict := objects.NewDictionary()
	dict.SetNewNameFor("Type", "XObject")
	dict.SetNewNameFor("Subtype", "Image")
	dict.SetNewNumberFor("Width", int32(width))
	dict.SetNewNumberFor("Height", int32(height))
	dict.SetNewNumberFor("BitsPerComponent", 8)
	dict.SetNewNameFor("ColorSpace", space)
	dict.SetNewNameFor("Filter", "FlateDecode")
	return dict
}

func flateStamp(data []byte) ([]byte, error) {
	var out bytes.Buffer
	compressed := zlib.NewWriter(&out)
	if _, err := compressed.Write(data); err != nil {
		return nil, err
	}
	if err := compressed.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
