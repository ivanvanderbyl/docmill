// Ported from core/fpdfapi/parser/cpdf_array.{h,cpp} @ pdfium 0db284a42.
//
// CPDF_Array's RetainPtr vector becomes a Go []Object. The C++ ArrayLocker /
// lock_count_ iteration guard and the cyclic-reference destructor Leak() loop
// are not ported (Go has GC and range-over-slice). The bounds-tolerant accessor
// semantics and the strict GetRect/GetMatrix size checks are preserved exactly.
package objects

import "github.com/ivanvanderbyl/docmill/pkg/parser/internal/crt"

type Array struct {
	baseObject
	objects []Object
}

// NewArray returns an empty array.
func NewArray() *Array { return &Array{} }

func (a *Array) Type() ObjectType { return TypeArray }
func (a *Array) Len() int         { return len(a.objects) }
func (a *Array) IsEmpty() bool    { return len(a.objects) == 0 }

// GetObjectAt returns the raw element (references not resolved), or nil if the
// index is out of bounds.
func (a *Array) GetObjectAt(index int) Object {
	if index < 0 || index >= len(a.objects) {
		return nil
	}
	return a.objects[index]
}

// GetDirectObjectAt returns the element with references resolved, or nil for an
// out-of-bounds index or a reference that does not resolve to a live object.
func (a *Array) GetDirectObjectAt(index int) Object {
	obj := a.GetObjectAt(index)
	if obj == nil {
		return nil
	}
	return Direct(obj)
}

// GetByteStringAt returns the raw element's string value ("" for OOB or for
// objects whose GetString is empty). References are NOT resolved.
func (a *Array) GetByteStringAt(index int) string {
	if index < 0 || index >= len(a.objects) {
		return ""
	}
	return a.objects[index].GetString()
}

// GetStringAt returns the element only if it is a String, else nil.
func (a *Array) GetStringAt(index int) *String { return ToString(a.GetObjectAt(index)) }

// GetNumberAt returns the element only if it is a Number, else nil.
func (a *Array) GetNumberAt(index int) *Number { return ToNumber(a.GetObjectAt(index)) }

// GetBooleanAt returns the boolean value only if the element is a Boolean;
// otherwise (including OOB or a Number element) it returns def.
func (a *Array) GetBooleanAt(index int, def bool) bool {
	if index < 0 || index >= len(a.objects) {
		return def
	}
	if b := ToBoolean(a.objects[index]); b != nil {
		return b.GetInteger() != 0
	}
	return def
}

// GetIntegerAt returns the raw element's integer value (0 for OOB or
// non-numeric). References are NOT resolved.
func (a *Array) GetIntegerAt(index int) int {
	if index < 0 || index >= len(a.objects) {
		return 0
	}
	return a.objects[index].GetInteger()
}

// GetFloatAt returns the raw element's float value (0 for OOB or non-numeric).
func (a *Array) GetFloatAt(index int) float32 {
	if index < 0 || index >= len(a.objects) {
		return 0
	}
	return a.objects[index].GetNumber()
}

// GetDictAt resolves references; if the element is a Stream it returns the
// stream's dictionary, else the dictionary itself, else nil.
func (a *Array) GetDictAt(index int) *Dictionary {
	p := a.GetDirectObjectAt(index)
	switch v := p.(type) {
	case *Dictionary:
		return v
	case *Stream:
		return v.Dict()
	default:
		return nil
	}
}

// GetRect returns the 4-element rect [left bottom right top], or the zero rect
// unless the array has exactly 4 elements.
func (a *Array) GetRect() crt.FloatRect {
	if len(a.objects) != 4 {
		return crt.FloatRect{}
	}
	return crt.NewFloatRect(a.GetFloatAt(0), a.GetFloatAt(1), a.GetFloatAt(2), a.GetFloatAt(3))
}

// GetMatrix returns the 6-element matrix [a b c d e f], or the identity unless
// the array has exactly 6 elements.
func (a *Array) GetMatrix() crt.Matrix {
	if len(a.objects) != 6 {
		return crt.IdentityMatrix()
	}
	return crt.NewMatrix(a.GetFloatAt(0), a.GetFloatAt(1), a.GetFloatAt(2),
		a.GetFloatAt(3), a.GetFloatAt(4), a.GetFloatAt(5))
}

// Find returns the index of the element whose resolved direct object is that
// (by identity), or (-1, false).
func (a *Array) Find(that Object) (int, bool) {
	for i := range a.objects {
		if a.GetDirectObjectAt(i) == that {
			return i, true
		}
	}
	return -1, false
}

// Contains reports whether that is present (by resolved identity).
func (a *Array) Contains(that Object) bool {
	_, ok := a.Find(that)
	return ok
}

// Append adds obj to the end.
func (a *Array) Append(obj Object) { a.objects = append(a.objects, obj) }

// SetAt overwrites the element at index. It never grows the array: an
// out-of-range index is a no-op.
func (a *Array) SetAt(index int, obj Object) {
	if index < 0 || index >= len(a.objects) {
		return
	}
	a.objects[index] = obj
}

// InsertAt inserts obj before index. index == Len() appends; index > Len() (or
// negative) is a no-op.
func (a *Array) InsertAt(index int, obj Object) {
	if index < 0 || index > len(a.objects) {
		return
	}
	a.objects = append(a.objects, nil)
	copy(a.objects[index+1:], a.objects[index:])
	a.objects[index] = obj
}

// RemoveAt removes the element at index; an out-of-range index is a no-op.
func (a *Array) RemoveAt(index int) {
	if index < 0 || index >= len(a.objects) {
		return
	}
	a.objects = append(a.objects[:index], a.objects[index+1:]...)
}

// Clear empties the array.
func (a *Array) Clear() { a.objects = a.objects[:0] }

func (a *Array) Clone() Object { return cloneObjectNonCyclic(a, false) }

func (a *Array) cloneNonCyclic(direct bool, visited map[Object]bool) Object {
	visited[a] = true
	out := NewArray()
	for _, value := range a.objects {
		if visited[value] {
			continue // already visited: drop the back-edge element (cycle break)
		}
		childVisited := copyVisited(visited)
		if clone := value.cloneNonCyclic(direct, childVisited); clone != nil {
			out.objects = append(out.objects, clone)
		}
	}
	return out
}

// copyVisited duplicates the visited set so sibling recursions stay independent
// (only the ancestor chain is shared), matching CloneNonCyclic.
func copyVisited(visited map[Object]bool) map[Object]bool {
	out := make(map[Object]bool, len(visited))
	for k := range visited {
		out[k] = true
	}
	return out
}
