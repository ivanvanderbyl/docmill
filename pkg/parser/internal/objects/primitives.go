// Leaf object types ported from core/fpdfapi/parser/cpdf_boolean,cpdf_null,
// cpdf_name,cpdf_number,cpdf_string,cpdf_reference @ pdfium 0db284a42.
package objects

import (
	"strconv"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
)

// --- Boolean (CPDF_Boolean) ---

type Boolean struct {
	baseObject
	value bool
}

func NewBoolean(value bool) *Boolean { return &Boolean{value: value} }

func (b *Boolean) Type() ObjectType { return TypeBoolean }
func (b *Boolean) Value() bool      { return b.value }

func (b *Boolean) Clone() Object { return &Boolean{value: b.value} }
func (b *Boolean) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	return b.Clone()
}

func (b *Boolean) GetString() string {
	if b.value {
		return "true"
	}
	return "false"
}

// GetInteger is 1/0; note Boolean does NOT override GetNumber (stays 0).
func (b *Boolean) GetInteger() int {
	if b.value {
		return 1
	}
	return 0
}

// --- Null (CPDF_Null) ---

type Null struct {
	baseObject
}

func NewNull() *Null { return &Null{} }

func (n *Null) Type() ObjectType { return TypeNull }
func (n *Null) Clone() Object    { return &Null{} }
func (n *Null) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	return n.Clone()
}

// --- Name (CPDF_Name) ---

type Name struct {
	baseObject
	name string
}

func NewName(name string) *Name { return &Name{name: name} }

func (n *Name) Type() ObjectType { return TypeName }
func (n *Name) Clone() Object    { return &Name{name: n.name} }
func (n *Name) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	return n.Clone()
}

func (n *Name) GetString() string      { return n.name }
func (n *Name) GetUnicodeText() string { return decodePDFText(n.name) }

// --- Number (CPDF_Number wrapping FX_Number) ---

type Number struct {
	baseObject
	value crt.Number
}

func NewNumberFromInt(value int32) *Number { return &Number{value: crt.NumberFromInt(value)} }
func NewNumberFromFloat(value float32) *Number {
	return &Number{value: crt.NumberFromFloat(value)}
}
func NewNumberFromString(s string) *Number { return &Number{value: crt.NumberFromString(s)} }

func (n *Number) Type() ObjectType   { return TypeNumber }
func (n *Number) IsInteger() bool    { return n.value.IsInteger() }
func (n *Number) GetNumber() float32 { return n.value.GetFloat() }
func (n *Number) GetInteger() int    { return int(n.value.GetSigned()) }

// Clone of an integer number routes through GetSigned (so a large UNSIGNED
// value clones to its signed bit pattern, matching PDFium).
func (n *Number) Clone() Object {
	if n.value.IsInteger() {
		return &Number{value: crt.NumberFromInt(n.value.GetSigned())}
	}
	return &Number{value: crt.NumberFromFloat(n.value.GetFloat())}
}
func (n *Number) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	return n.Clone()
}

func (n *Number) GetString() string {
	if n.value.IsInteger() {
		return strconv.Itoa(int(n.value.GetSigned()))
	}
	return formatFloat(n.value.GetFloat())
}

// formatFloat is the float -> string formatter. PDFium uses SkFloatToDecimal
// (cpdf_contentstream_write_utils.cpp), which only matters for serialisation —
// out of scope for plan 009 (we parse, never write). This shortest-decimal form
// is adequate for the parse/text path; a faithful SkFloatToDecimal port is a
// follow-up if a write path is ever needed.
func formatFloat(f float32) string {
	return strconv.FormatFloat(float64(f), 'f', -1, 32)
}

// --- String (CPDF_String) ---

type String struct {
	baseObject
	data  string // raw decoded bytes (not the escaped literal)
	isHex bool
}

func NewString(data string, isHex bool) *String { return &String{data: data, isHex: isHex} }

func (s *String) Type() ObjectType { return TypeString }
func (s *String) IsHex() bool      { return s.isHex }
func (s *String) Clone() Object    { return &String{data: s.data, isHex: s.isHex} }
func (s *String) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	return s.Clone()
}

func (s *String) GetString() string      { return s.data }
func (s *String) GetUnicodeText() string { return decodePDFText(s.data) }

// --- Reference (CPDF_Reference) ---

type Reference struct {
	baseObject
	holder    IndirectObjectHolder
	refObjNum uint32
}

func NewReference(holder IndirectObjectHolder, refObjNum uint32) *Reference {
	return &Reference{holder: holder, refObjNum: refObjNum}
}

func (r *Reference) Type() ObjectType     { return TypeReference }
func (r *Reference) GetRefObjNum() uint32 { return r.refObjNum }

// getDirectInternal resolves the reference once; it does NOT unwrap chained
// references (it may return another Reference or nil).
func (r *Reference) getDirectInternal() Object {
	if r.holder == nil {
		return nil
	}
	return r.holder.GetOrParseIndirectObject(r.refObjNum)
}

// fastGetDirect resolves once but refuses reference-to-reference: if the
// resolved object is itself a Reference it returns nil. Used by the value
// accessors so a ref-to-ref yields empty/0/nil rather than chasing the chain.
func (r *Reference) fastGetDirect() Object {
	if r.holder == nil {
		return nil
	}
	obj := r.holder.GetOrParseIndirectObject(r.refObjNum)
	if obj != nil && obj.Type() != TypeReference {
		return obj
	}
	return nil
}

func (r *Reference) GetString() string {
	if obj := r.fastGetDirect(); obj != nil {
		return obj.GetString()
	}
	return ""
}

func (r *Reference) GetNumber() float32 {
	if obj := r.fastGetDirect(); obj != nil {
		return obj.GetNumber()
	}
	return 0
}

func (r *Reference) GetInteger() int {
	if obj := r.fastGetDirect(); obj != nil {
		return obj.GetInteger()
	}
	return 0
}

func (r *Reference) GetDict() *Dictionary {
	if obj := r.fastGetDirect(); obj != nil {
		return obj.GetDict()
	}
	return nil
}

// GetUnicodeText is deliberately NOT overridden (inherits ""), matching PDFium:
// a Reference's Unicode text is always empty even when it points at a String.

func (r *Reference) Clone() Object { return cloneObjectNonCyclic(r, false) }

func (r *Reference) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	visited[r] = true
	if !direct {
		return &Reference{holder: r.holder, refObjNum: r.refObjNum}
	}
	target := r.getDirectInternal()
	if target != nil && !visited[target] {
		return target.cloneNonCyclic(true, visited)
	}
	return nil
}
