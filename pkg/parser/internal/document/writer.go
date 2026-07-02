package document

import (
	"bytes"
	"fmt"
	"io"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// WritePDF serialises the parsed document as a fresh, non-incremental PDF.
// It follows PDFium's CPDF_Creator save-as-copy shape: write every live
// indirect object, then a new xref table, trailer, startxref, and EOF marker.
func (d *Document) WritePDF(w io.Writer) error {
	if d == nil || d.prs == nil {
		return fmt.Errorf("write PDF: nil document")
	}
	if w == nil {
		return fmt.Errorf("write PDF: nil writer")
	}

	objs, lastObjNum := d.objectsForWrite()
	if lastObjNum == 0 {
		return fmt.Errorf("write PDF: no indirect objects")
	}

	var buf bytes.Buffer
	version := d.prs.FileVersion()
	if version <= 0 {
		version = 14
	}
	fmt.Fprintf(&buf, "%%PDF-%d.%d\n", version/10, version%10)

	offsets := make([]int64, int(lastObjNum)+1)
	for objNum := uint32(1); objNum <= lastObjNum; objNum++ {
		obj := objs[objNum]
		if obj == nil {
			continue
		}
		offsets[objNum] = int64(buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\r\n", objNum)
		writePDFObjectBody(&buf, obj, map[objects.Object]bool{})
		buf.WriteString("\r\nendobj\r\n")
	}

	xrefOffset := buf.Len()
	fmt.Fprintf(&buf, "xref\r\n0 %d\r\n", lastObjNum+1)
	buf.WriteString("0000000000 65535 f \r\n")
	for objNum := uint32(1); objNum <= lastObjNum; objNum++ {
		if offsets[objNum] == 0 {
			buf.WriteString("0000000000 65535 f \r\n")
			continue
		}
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[objNum])
	}

	trailer := d.trailerForWrite(lastObjNum)
	buf.WriteString("trailer\r\n")
	writePDFObjectBody(&buf, trailer, map[objects.Object]bool{})
	fmt.Fprintf(&buf, "\r\nstartxref\r\n%d\r\n%%%%EOF\r\n", xrefOffset)

	_, err := w.Write(buf.Bytes())
	return err
}

func (d *Document) objectsForWrite() (map[uint32]objects.Object, uint32) {
	lastObjNum := d.prs.GetLastObjNum()
	if holderLast := d.GetLastObjNum(); holderLast > lastObjNum {
		lastObjNum = holderLast
	}

	out := make(map[uint32]objects.Object, lastObjNum)
	for objNum := uint32(1); objNum <= lastObjNum; objNum++ {
		obj := d.GetOrParseIndirectObject(objNum)
		if obj == nil {
			continue
		}
		obj.SetObjNum(objNum)
		out[objNum] = obj
	}
	return out, lastObjNum
}

func (d *Document) trailerForWrite(lastObjNum uint32) *objects.Dictionary {
	trailer := objects.NewDictionary()
	trailer.SetFor("Size", objects.NewNumberFromInt(int32(lastObjNum+1)))
	if rootObjNum := d.prs.GetRootObjNum(); rootObjNum != objects.InvalidObjNum {
		trailer.SetFor("Root", objects.NewReference(d, rootObjNum))
	}

	if original := d.prs.GetTrailer(); original != nil {
		for _, key := range []string{"Info", "ID"} {
			if obj := original.GetObjectFor(key); obj != nil {
				trailer.SetFor(key, obj)
			}
		}
	}
	return trailer
}

func writePDFObject(buf *bytes.Buffer, obj objects.Object, visited map[objects.Object]bool) {
	if obj == nil {
		buf.WriteString("null")
		return
	}
	if !obj.IsInline() {
		fmt.Fprintf(buf, "%d 0 R", obj.GetObjNum())
		return
	}
	writePDFObjectBody(buf, obj, visited)
}

func writePDFObjectBody(buf *bytes.Buffer, obj objects.Object, visited map[objects.Object]bool) {
	if obj == nil {
		buf.WriteString("null")
		return
	}

	switch obj.Type() {
	case objects.TypeBoolean:
		buf.WriteString(obj.GetString())
	case objects.TypeNumber:
		buf.WriteString(obj.GetString())
	case objects.TypeString:
		writePDFString(buf, obj.GetString())
	case objects.TypeName:
		buf.WriteByte('/')
		writePDFName(buf, obj.GetString())
	case objects.TypeArray:
		writePDFArray(buf, objects.ToArray(obj), visited)
	case objects.TypeDictionary:
		writePDFDictionary(buf, objects.ToDictionary(obj), visited)
	case objects.TypeStream:
		writePDFStream(buf, objects.ToStream(obj), visited)
	case objects.TypeReference:
		ref := objects.ToReference(obj)
		if ref == nil || ref.GetRefObjNum() == 0 {
			buf.WriteString("null")
			return
		}
		fmt.Fprintf(buf, "%d 0 R", ref.GetRefObjNum())
	default:
		buf.WriteString("null")
	}
}

func writePDFArray(buf *bytes.Buffer, arr *objects.Array, visited map[objects.Object]bool) {
	if arr == nil {
		buf.WriteString("[]")
		return
	}
	if visited[arr] {
		buf.WriteString("[]")
		return
	}
	visited[arr] = true
	defer delete(visited, arr)

	buf.WriteByte('[')
	for i := 0; i < arr.Len(); i++ {
		if i > 0 {
			buf.WriteByte(' ')
		}
		writePDFObject(buf, arr.GetObjectAt(i), visited)
	}
	buf.WriteByte(']')
}

func writePDFDictionary(buf *bytes.Buffer, dict *objects.Dictionary, visited map[objects.Object]bool) {
	if dict == nil {
		buf.WriteString("<<>>")
		return
	}
	if visited[dict] {
		buf.WriteString("<<>>")
		return
	}
	visited[dict] = true
	defer delete(visited, dict)

	buf.WriteString("<<")
	for _, key := range dict.GetKeys() {
		buf.WriteByte(' ')
		buf.WriteByte('/')
		writePDFName(buf, key)
		buf.WriteByte(' ')
		writePDFObject(buf, dict.GetObjectFor(key), visited)
	}
	buf.WriteString(" >>")
}

func writePDFStream(buf *bytes.Buffer, stream *objects.Stream, visited map[objects.Object]bool) {
	if stream == nil {
		buf.WriteString("<<>>\r\nstream\r\n\r\nendstream")
		return
	}
	acc := objects.NewStreamAcc(stream)
	acc.LoadAllDataRaw()
	data := acc.GetSpan()
	stream.Dict().SetFor("Length", objects.NewNumberFromInt(int32(len(data))))

	writePDFDictionary(buf, stream.Dict(), visited)
	buf.WriteString("\r\nstream\r\n")
	buf.Write(data)
	buf.WriteString("\r\nendstream")
}

func writePDFString(buf *bytes.Buffer, value string) {
	buf.WriteByte('(')
	for i := 0; i < len(value); i++ {
		b := value[i]
		switch b {
		case '\\', '(', ')':
			buf.WriteByte('\\')
			buf.WriteByte(b)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		default:
			if b < 0x20 || b >= 0x7f {
				buf.WriteByte('\\')
				buf.WriteByte('0' + ((b >> 6) & 7))
				buf.WriteByte('0' + ((b >> 3) & 7))
				buf.WriteByte('0' + (b & 7))
				continue
			}
			buf.WriteByte(b)
		}
	}
	buf.WriteByte(')')
}

func writePDFName(buf *bytes.Buffer, value string) {
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(value); i++ {
		b := value[i]
		if needsNameEscape(b) {
			buf.WriteByte('#')
			buf.WriteByte(hex[b>>4])
			buf.WriteByte(hex[b&0x0f])
			continue
		}
		buf.WriteByte(b)
	}
}

func needsNameEscape(b byte) bool {
	if b <= 0x20 || b >= 0x7f || b == '#' {
		return true
	}
	switch b {
	case '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return true
	default:
		return false
	}
}
