// Package objects is the PDF object model ported from
// core/fpdfapi/parser/cpdf_object* @ pdfium 0db284a42.
//
// PDFium's CPDF_Object hierarchy (an abstract Retainable with final leaf
// subclasses and virtual AsArray()/AsDictionary() downcasts) becomes a sealed
// Go interface with concrete pointer types and type switches. Strings are Go
// string/[]byte (no ByteString/WideString); ownership is GC (no RetainPtr).
// Serialisation (WriteTo and the PDF_Encode* helpers) is intentionally omitted:
// plan 009 parses, it never writes. See plan 009 Phase B.
package objects

import "github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"

// ObjectType mirrors CPDF_Object::Type (1-based, in this exact order).
type ObjectType int

const (
	TypeBoolean ObjectType = iota + 1
	TypeNumber
	TypeString
	TypeName
	TypeArray
	TypeDictionary
	TypeStream
	TypeNull
	TypeReference
)

// InvalidObjNum is CPDF_Object::kInvalidObjNum.
const InvalidObjNum = ^uint32(0)

// Object is the sealed PDF object interface. Only types in this package
// implement it (via the unexported isObject marker).
type Object interface {
	Type() ObjectType
	// Clone deep-copies the object.
	Clone() Object
	// GetString returns the byte-string view: literal/hex content for String,
	// the name for Name, "true"/"false" for Boolean, the formatted number for
	// Number, "" otherwise.
	GetString() string
	// GetUnicodeText returns the Unicode (UTF-8) text for String/Name, "" else.
	GetUnicodeText() string
	GetNumber() float32
	GetInteger() int
	// GetDict returns the associated dictionary (self for Dictionary, the
	// stream dict for Stream, the resolved target for Reference), else nil.
	GetDict() *Dictionary

	GetObjNum() uint32
	SetObjNum(uint32)
	GetGenNum() uint32
	SetGenNum(uint32)
	IsInline() bool

	cloneNonCyclic(direct bool, visited map[Object]bool) Object
	isObject()
}

// IndirectObjectHolder resolves an object number to its parsed object. The
// parser (Phase D) implements it; Reference holds one to resolve GetDirect.
type IndirectObjectHolder interface {
	GetOrParseIndirectObject(objNum uint32) Object
}

// baseObject carries the object number/generation and the value-accessor
// defaults that leaves override only where they differ.
type baseObject struct {
	objNum uint32
	genNum uint32
}

func (b *baseObject) GetObjNum() uint32      { return b.objNum }
func (b *baseObject) SetObjNum(n uint32)     { b.objNum = n }
func (b *baseObject) GetGenNum() uint32      { return b.genNum }
func (b *baseObject) SetGenNum(n uint32)     { b.genNum = n }
func (b *baseObject) IsInline() bool         { return b.objNum == 0 }
func (b *baseObject) GetString() string      { return "" }
func (b *baseObject) GetUnicodeText() string { return "" }
func (b *baseObject) GetNumber() float32     { return 0 }
func (b *baseObject) GetInteger() int        { return 0 }
func (b *baseObject) GetDict() *Dictionary   { return nil }
func (b *baseObject) isObject()              {}

// Direct resolves a reference once (CPDF_Object::GetDirect): a Reference returns
// whatever its holder yields (which may itself be a Reference or nil); any other
// object returns itself. nil in, nil out.
func Direct(o Object) Object {
	if o == nil {
		return nil
	}
	if r, ok := o.(*Reference); ok {
		return r.getDirectInternal()
	}
	return o
}

// CloneDirectObject deep-copies o with references replaced by copies of their
// targets (CPDF_Object::CloneDirectObject).
func CloneDirectObject(o Object) Object {
	return cloneObjectNonCyclic(o, true)
}

func cloneObjectNonCyclic(o Object, direct bool) Object {
	if o == nil {
		return nil
	}
	return o.cloneNonCyclic(direct, map[Object]bool{})
}

// Null-safe casts mirroring PDFium's ToArray/ToDictionary/... free functions.
func ToBoolean(o Object) *Boolean       { v, _ := o.(*Boolean); return v }
func ToNumber(o Object) *Number         { v, _ := o.(*Number); return v }
func ToString(o Object) *String         { v, _ := o.(*String); return v }
func ToName(o Object) *Name             { v, _ := o.(*Name); return v }
func ToArray(o Object) *Array           { v, _ := o.(*Array); return v }
func ToDictionary(o Object) *Dictionary { v, _ := o.(*Dictionary); return v }
func ToStream(o Object) *Stream         { v, _ := o.(*Stream); return v }
func ToReference(o Object) *Reference   { v, _ := o.(*Reference); return v }
func ToNull(o Object) *Null             { v, _ := o.(*Null); return v }

// decodePDFText is the shared boundary decoder used by Name/String.
func decodePDFText(data string) string {
	return crt.DecodePDFTextString([]byte(data))
}
