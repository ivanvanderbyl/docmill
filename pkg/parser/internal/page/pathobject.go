package page

import "github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"

type PathSegment struct {
	From crt.PointF
	To   crt.PointF
}

type PathObject struct {
	baseObject
	segments    []PathSegment
	strokeWidth float32
}

func newPathObject(contentStream int32, segments []PathSegment, strokeWidth float32) *PathObject {
	obj := &PathObject{
		baseObject:  newBaseObject(contentStream),
		segments:    append([]PathSegment(nil), segments...),
		strokeWidth: strokeWidth,
	}
	obj.calcBoundingBox()
	return obj
}

func (p *PathObject) getType() objectType { return typePath }

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
