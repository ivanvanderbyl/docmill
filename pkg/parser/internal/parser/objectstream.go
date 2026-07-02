// Ported from core/fpdfapi/parser/cpdf_object_stream.{h,cpp} @ pdfium
// 0db284a42: ISO 32000 §7.5.7 compressed object streams.
package parser

import (
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/syntax"
)

type osObjectInfo struct {
	objNum    uint32
	objOffset uint32
}

// ObjectStream holds the decoded body of an /ObjStm and its object index.
type ObjectStream struct {
	data              []byte
	firstObjectOffset int
	objectInfo        []osObjectInfo
}

// IsObjectStream reports whether stream is a valid object stream (the Create
// gate): /Type /ObjStm, /N an integer in [0, kMaxObjectNumber), /First an
// integer >= 0.
func IsObjectStream(stream *objects.Stream) bool {
	if stream == nil {
		return false
	}
	dict := stream.Dict()
	if dict.GetNameFor("Type") != "ObjStm" {
		return false
	}
	n := dict.GetNumberFor("N")
	if n == nil || !n.IsInteger() || n.GetInteger() < 0 || uint32(n.GetInteger()) >= kMaxObjectNumber {
		return false
	}
	first := dict.GetNumberFor("First")
	if first == nil || !first.IsInteger() || first.GetInteger() < 0 {
		return false
	}
	return true
}

// NewObjectStream returns the parsed object stream, or nil if stream is not a
// valid /ObjStm.
func NewObjectStream(stream *objects.Stream) *ObjectStream {
	if !IsObjectStream(stream) {
		return nil
	}
	os := &ObjectStream{firstObjectOffset: stream.Dict().GetIntegerFor("First")}
	acc := objects.NewStreamAcc(stream)
	acc.LoadAllDataFiltered()
	os.data = acc.GetSpan()
	os.init(stream.Dict().GetIntegerFor("N"))
	return os
}

func (os *ObjectStream) init(objectCount int) {
	sub := syntax.New(os.data)
	for i := objectCount; i > 0; i-- {
		if sub.GetPos() >= len(os.data) {
			break
		}
		objNum := sub.GetDirectNum()
		objOffset := sub.GetDirectNum()
		if objNum == 0 {
			continue // skip garbage object numbers
		}
		os.objectInfo = append(os.objectInfo, osObjectInfo{objNum, objOffset})
	}
}

// ParseObject returns the object at archiveIndex, requiring its recorded object
// number to equal objNumber.
func (os *ObjectStream) ParseObject(holder objects.IndirectObjectHolder, objNumber, archiveIndex uint32) objects.Object {
	if int(archiveIndex) >= len(os.objectInfo) {
		return nil
	}
	info := os.objectInfo[archiveIndex]
	if info.objNum != objNumber {
		return nil
	}
	result := os.parseObjectAtOffset(holder, info.objOffset)
	if result != nil {
		result.SetObjNum(objNumber)
	}
	return result
}

func (os *ObjectStream) parseObjectAtOffset(holder objects.IndirectObjectHolder, objectOffset uint32) objects.Object {
	offsetInStream := int64(os.firstObjectOffset) + int64(objectOffset)
	if offsetInStream < 0 || offsetInStream >= int64(len(os.data)) {
		return nil
	}
	sub := syntax.New(os.data)
	sub.SetPos(int(offsetInStream))
	return sub.GetObjectBody(holder)
}
