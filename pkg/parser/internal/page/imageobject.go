// Ported from core/fpdfapi/page/cpdf_imageobject.{h,cpp} and
// cpdf_shadingobject.{h,cpp} @ pdfium 0db284a42, in geometry-only form.
//
// Neither object carries pixels or colour here. Layout analysis needs to know
// that ink was laid down and where, not what it looked like, and decoding image
// streams would cost far more than the answer is worth.
package page

import "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"

// unitSquare is the image space every PDF image is drawn into. The image
// dictionary's pixel dimensions do not place it: the content stream sets a
// matrix that maps the unit square to wherever the image belongs, and both
// `Do` on an image XObject and an inline `BI` obey the same rule (PDF 32000-1
// §8.9.5.2). So the rendered box is that matrix applied to these four corners,
// and no image data has to be touched to find it.
var unitSquare = []crt.PointF{
	{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 1, Y: 1}, {X: 0, Y: 1},
}

// ImageObject is a placed image — an image XObject drawn with `Do`, or an
// inline image between `BI` and `EI`.
type ImageObject struct {
	baseObject
	matrix crt.Matrix
	inline bool
}

func newImageObject(contentStream int32, matrix crt.Matrix, inline bool) *ImageObject {
	obj := &ImageObject{
		baseObject: newBaseObject(contentStream),
		matrix:     matrix,
		inline:     inline,
	}
	obj.calcBoundingBox()
	return obj
}

func (o *ImageObject) getType() objectType { return typeImage }

// Kind reports that this is a placed image.
func (o *ImageObject) Kind() ObjectKind { return KindImage }

// Matrix returns the image-space to user-space matrix.
func (o *ImageObject) Matrix() crt.Matrix { return o.matrix }

// IsInline reports whether this came from a BI/ID/EI inline image rather than
// an image XObject.
func (o *ImageObject) IsInline() bool { return o.inline }

// calcBoundingBox transforms the unit square. All four corners are transformed
// rather than two, because a rotated or skewed matrix makes the axis-aligned
// box of the corners larger than the box of any opposite pair.
func (o *ImageObject) calcBoundingBox() {
	points := make([]crt.PointF, 0, len(unitSquare))
	for _, corner := range unitSquare {
		points = append(points, o.matrix.Transform(corner))
	}
	o.SetRect(crt.GetBBox(points))
}

// ShadingObject is a `sh` shading fill.
//
// Unlike every other object it has no geometry of its own: `sh` paints the
// shading across the entire current clip region (PDF 32000-1 §8.7.4.2). So the
// clip is not a restriction on this object, it IS the object, and a shading
// drawn with no clip in force covers the whole page — which is exactly what an
// unclipped `sh` does.
type ShadingObject struct {
	baseObject
}

func newShadingObject(contentStream int32, area crt.FloatRect) *ShadingObject {
	obj := &ShadingObject{baseObject: newBaseObject(contentStream)}
	obj.SetRect(area)
	return obj
}

func (o *ShadingObject) getType() objectType { return typeShading }

// Kind reports that this is a shading fill.
func (o *ShadingObject) Kind() ObjectKind { return KindShading }
