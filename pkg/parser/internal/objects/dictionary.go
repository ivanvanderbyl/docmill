// Ported from core/fpdfapi/parser/cpdf_dictionary.{h,cpp} @ pdfium 0db284a42.
//
// PDFium's std::map<ByteString,...> is an ORDERED map; GetKeys/Clone/WriteTo
// depend on ascending byte-lexicographic key order, so we sort on demand rather
// than rely on Go map iteration order. The ByteStringPool interning and the
// DictionaryLocker / destructor Leak() logic are dropped (GC, value strings).
package objects

import (
	"sort"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"
)

type Dictionary struct {
	baseObject
	m map[string]Object
}

// NewDictionary returns an empty dictionary.
func NewDictionary() *Dictionary { return &Dictionary{m: map[string]Object{}} }

func (d *Dictionary) Type() ObjectType     { return TypeDictionary }
func (d *Dictionary) GetDict() *Dictionary { return d }

// --- raw / resolved object access ---

// GetObjectFor returns the raw stored object (references not resolved), or nil.
func (d *Dictionary) GetObjectFor(key string) Object {
	if o, ok := d.m[key]; ok {
		return o
	}
	return nil
}

// GetDirectObjectFor resolves a reference one hop via the holder, or returns the
// stored object as-is; nil if absent or unresolvable.
func (d *Dictionary) GetDirectObjectFor(key string) Object {
	return Direct(d.GetObjectFor(key))
}

// KeyExist reports whether key is present.
func (d *Dictionary) KeyExist(key string) bool {
	_, ok := d.m[key]
	return ok
}

// GetKeys returns the keys in ascending byte-lexicographic order.
func (d *Dictionary) GetKeys() []string {
	keys := make([]string, 0, len(d.m))
	for k := range d.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- string-valued getters ---

// GetByteStringFor returns the value's string representation ("" if absent).
// References resolve inside the value's GetString.
func (d *Dictionary) GetByteStringFor(key string) string {
	if o := d.GetObjectFor(key); o != nil {
		return o.GetString()
	}
	return ""
}

// GetUnicodeTextFor explicitly resolves one reference level then returns the
// target's Unicode text ("" if absent).
func (d *Dictionary) GetUnicodeTextFor(key string) string {
	if o := Direct(d.GetObjectFor(key)); o != nil {
		return o.GetUnicodeText()
	}
	return ""
}

// GetNameFor returns the name only if the raw value is literally a Name.
func (d *Dictionary) GetNameFor(key string) string {
	if n := ToName(d.GetObjectFor(key)); n != nil {
		return n.GetString()
	}
	return ""
}

// GetStringFor returns the value only if it is literally a String.
func (d *Dictionary) GetStringFor(key string) *String { return ToString(d.GetObjectFor(key)) }

// --- numeric / boolean getters ---

// GetBooleanFor returns the boolean value only if the value is literally a
// Boolean; otherwise def.
func (d *Dictionary) GetBooleanFor(key string, def bool) bool {
	if b := ToBoolean(d.GetObjectFor(key)); b != nil {
		return b.GetInteger() != 0
	}
	return def
}

// GetIntegerFor returns the value's integer (0 if absent). References resolve.
func (d *Dictionary) GetIntegerFor(key string) int {
	if o := d.GetObjectFor(key); o != nil {
		return o.GetInteger()
	}
	return 0
}

// GetIntegerWithDefaultFor is GetIntegerFor with a fallback for absent keys.
func (d *Dictionary) GetIntegerWithDefaultFor(key string, def int) int {
	if o := d.GetObjectFor(key); o != nil {
		return o.GetInteger()
	}
	return def
}

// GetDirectIntegerFor returns the integer only if the raw value is literally a
// Number (no reference deref).
func (d *Dictionary) GetDirectIntegerFor(key string) int {
	if n := ToNumber(d.GetObjectFor(key)); n != nil {
		return n.GetInteger()
	}
	return 0
}

// GetFloatFor returns the value's float (0 if absent). References resolve.
func (d *Dictionary) GetFloatFor(key string) float32 {
	if o := d.GetObjectFor(key); o != nil {
		return o.GetNumber()
	}
	return 0
}

// GetNumberFor returns the value only if it is literally a Number.
func (d *Dictionary) GetNumberFor(key string) *Number { return ToNumber(d.GetObjectFor(key)) }

// --- container getters (resolve references) ---

// GetDictFor resolves references; a Stream value yields its dictionary.
func (d *Dictionary) GetDictFor(key string) *Dictionary {
	p := d.GetDirectObjectFor(key)
	if p == nil {
		return nil
	}
	return p.GetDict()
}

// GetArrayFor resolves references and returns an Array, or nil.
func (d *Dictionary) GetArrayFor(key string) *Array { return ToArray(d.GetDirectObjectFor(key)) }

// GetStreamFor resolves references and returns a Stream, or nil.
func (d *Dictionary) GetStreamFor(key string) *Stream { return ToStream(d.GetDirectObjectFor(key)) }

// GetRectFor returns the value array as a rect, or the zero rect.
func (d *Dictionary) GetRectFor(key string) crt.FloatRect {
	if arr := d.GetArrayFor(key); arr != nil {
		return arr.GetRect()
	}
	return crt.FloatRect{}
}

// GetMatrixFor returns the value array as a matrix, or the identity.
func (d *Dictionary) GetMatrixFor(key string) crt.Matrix {
	if arr := d.GetArrayFor(key); arr != nil {
		return arr.GetMatrix()
	}
	return crt.IdentityMatrix()
}

// --- mutators ---

// SetFor stores obj under key. A nil obj ERASES the key (dictionaries never hold
// nil for a valid key). PDFium additionally CHECKs the object is inline and not
// a Stream; on the parse path those invariants always hold, so we store directly.
func (d *Dictionary) SetFor(key string, obj Object) {
	if obj == nil {
		delete(d.m, key)
		return
	}
	d.m[key] = obj
}

// SetNewDictFor inserts and returns a fresh empty dictionary under key.
func (d *Dictionary) SetNewDictFor(key string) *Dictionary {
	v := NewDictionary()
	d.SetFor(key, v)
	return v
}

// SetNewArrayFor inserts and returns a fresh empty array under key.
func (d *Dictionary) SetNewArrayFor(key string) *Array {
	v := NewArray()
	d.SetFor(key, v)
	return v
}

// SetNewNumberFor inserts and returns an integer Number under key.
func (d *Dictionary) SetNewNumberFor(key string, value int32) *Number {
	v := NewNumberFromInt(value)
	d.SetFor(key, v)
	return v
}

// SetNewNameFor inserts and returns a Name under key.
func (d *Dictionary) SetNewNameFor(key, name string) *Name {
	v := NewName(name)
	d.SetFor(key, v)
	return v
}

// SetNewReferenceFor inserts and returns a Reference under key.
func (d *Dictionary) SetNewReferenceFor(key string, holder IndirectObjectHolder, objNum uint32) *Reference {
	v := NewReference(holder, objNum)
	d.SetFor(key, v)
	return v
}

// RemoveFor removes key and returns the removed object (nil if absent).
func (d *Dictionary) RemoveFor(key string) Object {
	if o, ok := d.m[key]; ok {
		delete(d.m, key)
		return o
	}
	return nil
}

// --- clone ---

func (d *Dictionary) Clone() Object { return cloneObjectNonCyclic(d, false) }

func (d *Dictionary) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	visited[d] = true
	out := NewDictionary()
	for _, key := range d.GetKeys() {
		value := d.m[key]
		if visited[value] {
			continue
		}
		childVisited := copyVisited(visited)
		if clone := value.cloneNonCyclic(direct, childVisited); clone != nil {
			out.m[key] = clone
		}
	}
	return out
}
