package parser

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"io"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/document"
)

// ErrSignedDocument means stamping would invalidate an existing digital signature.
var ErrSignedDocument = document.ErrSignedDocument

// ImageStamp places an image on a zero-based page. Rect uses points in the
// displayed, cropped and rotated page; an empty Origin means TopLeft.
type ImageStamp struct {
	PageIndex int
	Rect      geom.Box
	Image     image.Image
}

// WriteImageStamps writes a full-document copy with images painted over page
// content, beneath annotations. It leaves the source unchanged and rejects
// signed documents. Both coordinate origins are accepted; rectangles must fit
// within the visible page. Images are stretched to fit Rect without cropping.
// A call accepts up to 256 stamps, 16 megapixels per image, and 64 megapixels in
// total. The output is PDF 1.4 or later and unencrypted, as for WritePDF; this
// is not digital signing.
func (d *Document) WriteImageStamps(ctx context.Context, w io.Writer, stamps []ImageStamp) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d == nil || d.doc == nil {
		return fmt.Errorf("stamp images: nil document")
	}
	if w == nil {
		return fmt.Errorf("stamp images: nil writer")
	}
	if len(stamps) == 0 || len(stamps) > 256 {
		return fmt.Errorf("stamp images: expected 1 to 256 stamps")
	}
	encoded := make([]document.ImageStamp, len(stamps))
	var total uint64
	for i, stamp := range stamps {
		raster, err := rasterizeStamp(ctx, stamp.Image)
		if err != nil {
			return fmt.Errorf("stamp %d: %w", i, err)
		}
		total += uint64(raster.Width) * uint64(raster.Height)
		if total > 64*1024*1024 {
			return fmt.Errorf("stamp images: total image size exceeds 64 megapixels")
		}
		raster.PageIndex = stamp.PageIndex
		raster.Rect = stamp.Rect
		encoded[i] = raster
	}
	return d.doc.WriteImageStamps(ctx, w, encoded)
}

func rasterizeStamp(ctx context.Context, im image.Image) (document.ImageStamp, error) {
	if im == nil {
		return document.ImageStamp{}, fmt.Errorf("nil image")
	}
	bounds := im.Bounds()
	if bounds.Empty() {
		return document.ImageStamp{}, fmt.Errorf("empty image")
	}
	width := uint64(bounds.Max.X) - uint64(bounds.Min.X)
	height := uint64(bounds.Max.Y) - uint64(bounds.Min.Y)
	if width > 16*1024*1024 || height > 16*1024*1024 || width*height > 16*1024*1024 {
		return document.ImageStamp{}, fmt.Errorf("image exceeds 16 megapixels")
	}
	count := int(width * height)
	raster := document.ImageStamp{
		Width:  int(width),
		Height: int(height),
		RGB:    make([]byte, count*3),
		Alpha:  make([]byte, count),
	}
	opaque := true
	for y := range raster.Height {
		if err := ctx.Err(); err != nil {
			return document.ImageStamp{}, err
		}
		for x := range raster.Width {
			pixel := color.NRGBAModel.Convert(im.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			at := y*raster.Width + x
			raster.RGB[at*3] = pixel.R
			raster.RGB[at*3+1] = pixel.G
			raster.RGB[at*3+2] = pixel.B
			raster.Alpha[at] = pixel.A
			if pixel.A != 255 {
				opaque = false
			}
		}
	}
	if opaque {
		raster.Alpha = nil
	}
	return raster, nil
}
