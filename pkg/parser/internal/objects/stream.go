// Ported from core/fpdfapi/parser/cpdf_stream.{h,cpp} and
// cpdf_stream_acc.{h,cpp} @ pdfium 0db284a42.
//
// A Stream is a dictionary plus a body of bytes. Because plan 009 loads the
// whole PDF into memory (design §5), only the memory-based backing is modelled
// here; the file-based variant is deferred. StreamAcc lazily materialises and
// optionally filter-decodes the body, with PDFium's three "no usable decoder /
// decode failed / terminal image filter -> serve raw" fall-backs. The borrowed
// span lifetime concern (crbug.com/1361849) is automatic under Go's GC.
package objects

import "github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"

type Stream struct {
	baseObject
	dict *Dictionary
	data []byte // memory-based raw (still-encoded) bytes
}

// NewStreamFromData wraps data and dict; /Length is set to the actual size.
func NewStreamFromData(data []byte, dict *Dictionary) *Stream {
	if dict == nil {
		dict = NewDictionary()
	}
	s := &Stream{dict: dict, data: data}
	s.setLengthInDict(len(data))
	return s
}

// NewStreamFromSpan makes a memory-based stream with a fresh dict, copying span.
func NewStreamFromSpan(span []byte) *Stream {
	s := &Stream{dict: NewDictionary()}
	s.SetData(span)
	return s
}

func (s *Stream) Type() ObjectType     { return TypeStream }
func (s *Stream) Dict() *Dictionary    { return s.dict }
func (s *Stream) GetDict() *Dictionary { return s.dict }
func (s *Stream) HasFilter() bool      { return s.dict.KeyExist("Filter") }
func (s *Stream) GetRawSize() int      { return len(s.data) }
func (s *Stream) rawData() []byte      { return s.data }

// SetData copies span as the new body and overwrites /Length.
func (s *Stream) SetData(span []byte) {
	d := make([]byte, len(span))
	copy(d, span)
	s.data = d
	s.setLengthInDict(len(d))
}

// SetDataAndRemoveFilter sets the body and drops /Filter and /DecodeParms.
func (s *Stream) SetDataAndRemoveFilter(span []byte) {
	s.SetData(span)
	s.dict.RemoveFor("Filter")
	s.dict.RemoveFor("DecodeParms")
}

func (s *Stream) setLengthInDict(length int) {
	s.dict.SetFor("Length", NewNumberFromInt(int32(length)))
}

// GetUnicodeText decodes the filtered body as a PDF text string.
func (s *Stream) GetUnicodeText() string {
	acc := NewStreamAcc(s)
	acc.LoadAllDataFiltered()
	return crt.DecodePDFTextString(acc.GetSpan())
}

func (s *Stream) Clone() Object { return cloneObjectNonCyclic(s, false) }

func (s *Stream) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	visited[s] = true
	acc := NewStreamAcc(s)
	acc.LoadAllDataRaw() // clones preserve the raw (undecoded) bytes
	var newDict *Dictionary
	if !visited[s.dict] {
		if dc := s.dict.cloneNonCyclic(direct, visited); dc != nil {
			newDict = ToDictionary(dc)
		}
	}
	// A stream dict is never itself in a cycle at clone time; fall back to an
	// empty dict rather than passing nil (NewStreamFromData requires non-nil).
	if newDict == nil {
		newDict = NewDictionary()
	}
	return NewStreamFromData(acc.DetachData(), newDict)
}

// StreamAcc materialises (and optionally filter-decodes) a stream's body.
type StreamAcc struct {
	stream *Stream
	data   []byte
}

// NewStreamAcc returns an accessor over stream (which may be nil).
func NewStreamAcc(stream *Stream) *StreamAcc { return &StreamAcc{stream: stream} }

// LoadAllDataFiltered decodes the body through its filter pipeline.
func (a *StreamAcc) LoadAllDataFiltered() { a.loadAllData(false, 0) }

// LoadAllDataRaw exposes the raw (undecoded) body.
func (a *StreamAcc) LoadAllDataRaw() { a.loadAllData(true, 0) }

func (a *StreamAcc) loadAllData(rawAccess bool, estimatedSize uint32) {
	if a.stream == nil {
		return
	}
	if rawAccess || !a.stream.HasFilter() {
		a.processRawData()
		return
	}
	a.processFilteredData(estimatedSize)
}

func (a *StreamAcc) processRawData() {
	if a.stream.GetRawSize() == 0 {
		return
	}
	a.data = a.stream.rawData() // borrowed view; survives the stream under GC
}

func (a *StreamAcc) processFilteredData(estimatedSize uint32) {
	if a.stream.GetRawSize() == 0 {
		return
	}
	srcSpan := a.stream.rawData()
	decoders, ok := getDecoderArray(a.stream.Dict())
	if !ok || len(decoders) == 0 {
		a.data = srcSpan // no usable decoder -> serve raw
		return
	}
	result, ok := pdfDataDecode(srcSpan, estimatedSize, decoders)
	if !ok {
		a.data = srcSpan // decode failed -> serve raw
		return
	}
	if len(result.data) == 0 {
		a.data = srcSpan // terminal image filter (or empty output) -> serve raw
		return
	}
	a.data = result.data
}

// GetSpan returns the materialised bytes (empty if not loaded).
func (a *StreamAcc) GetSpan() []byte {
	if a.data == nil {
		return []byte{}
	}
	return a.data
}

// GetData is an alias for GetSpan.
func (a *StreamAcc) GetData() []byte { return a.GetSpan() }

// GetSize returns the materialised byte count.
func (a *StreamAcc) GetSize() int { return len(a.GetSpan()) }

// DetachData returns an owned, independent copy of the materialised bytes.
func (a *StreamAcc) DetachData() []byte {
	span := a.GetSpan()
	out := make([]byte, len(span))
	copy(out, span)
	return out
}

// GetStream returns the source stream.
func (a *StreamAcc) GetStream() *Stream { return a.stream }
