package page

import "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/crt"

type PathSegment struct {
	From crt.PointF
	To   crt.PointF
}

type PathObject struct {
	baseObject
	segments    []PathSegment
	strokeWidth float32
	stroked     bool
	filled      bool
}

func newPathObject(contentStream int32, segments []PathSegment, strokeWidth float32, stroked, filled bool) *PathObject {
	obj := &PathObject{
		baseObject:  newBaseObject(contentStream),
		segments:    append([]PathSegment(nil), segments...),
		strokeWidth: strokeWidth,
		stroked:     stroked,
		filled:      filled,
	}
	obj.calcBoundingBox()
	return obj
}

// IsStroked reports whether the path was painted with a stroking operator.
//
// Ruling extraction depends on this. A stroked segment is a drawn line; a
// filled one is a shape whose outline is not ink, and treating a filled
// rectangle's four edges as four rules would invent a grid around every solid
// block on the page.
func (p *PathObject) IsStroked() bool { return p.stroked }

// IsFilled reports whether the path was painted with a filling operator.
func (p *PathObject) IsFilled() bool { return p.filled }

func (p *PathObject) getType() objectType { return typePath }

// Kind reports that this is a path.
func (p *PathObject) Kind() ObjectKind { return KindPath }

func (p *PathObject) Segments() []PathSegment {
	return append([]PathSegment(nil), p.segments...)
}

func (p *PathObject) StrokeWidth() float32 { return p.strokeWidth }

func (p *PathObject) calcBoundingBox() {
	if len(p.segments) == 0 {
		p.SetRect(crt.FloatRect{})
		return
	}
	points := make([]crt.PointF, 0, len(p.segments)*2)
	for _, segment := range p.segments {
		points = append(points, segment.From, segment.To)
	}
	p.SetRect(crt.GetBBox(points))
}
