// Ported from core/fpdfapi/parser/cpdf_array_unittest.cpp @ pdfium 0db284a42.
package objects

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func appendInts(a *Array, vals ...int32) {
	for _, v := range vals {
		a.Append(NewNumberFromInt(v))
	}
}

func TestArrayGetBooleanAt(t *testing.T) {
	a := NewArray()
	a.Append(NewBoolean(true))
	a.Append(NewBoolean(false))
	a.Append(NewNumberFromInt(100))
	a.Append(NewNumberFromInt(0))

	require.True(t, a.GetBooleanAt(0, true))
	require.True(t, a.GetBooleanAt(0, false))
	require.False(t, a.GetBooleanAt(1, true))
	require.False(t, a.GetBooleanAt(1, false))
	// A Number is not a Boolean, so the default is echoed.
	require.True(t, a.GetBooleanAt(2, true))
	require.False(t, a.GetBooleanAt(2, false))
	require.True(t, a.GetBooleanAt(3, true))
	require.False(t, a.GetBooleanAt(3, false))
	// Out of bounds.
	require.True(t, a.GetBooleanAt(99, true))
	require.False(t, a.GetBooleanAt(99, false))
}

func TestArrayRemoveAt(t *testing.T) {
	a := NewArray()
	appendInts(a, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	a.RemoveAt(3)
	a.RemoveAt(3)
	a.RemoveAt(3)
	require.Equal(t, 7, a.Len())
	require.Equal(t, []int{1, 2, 3, 7, 8, 9, 10}, ints(a))
	a.RemoveAt(4)
	a.RemoveAt(4)
	require.Equal(t, []int{1, 2, 3, 7, 10}, ints(a))

	b := NewArray()
	appendInts(b, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	b.RemoveAt(11) // OOB no-op
	require.Equal(t, 10, b.Len())
}

func TestArrayClear(t *testing.T) {
	a := NewArray()
	appendInts(a, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	require.Equal(t, 10, a.Len())
	a.Clear()
	require.Equal(t, 0, a.Len())
}

func TestArraySetAtBeyond(t *testing.T) {
	a := NewArray()
	a.SetAt(0, NewNumberFromInt(0))
	require.Equal(t, 0, a.Len()) // SetAt never grows
	a.InsertAt(0, NewNumberFromInt(0))
	require.Equal(t, 1, a.Len()) // InsertAt at end ok
	a.SetAt(1, NewNumberFromInt(0))
	require.Equal(t, 1, a.Len())
}

func TestArrayInsertAt(t *testing.T) {
	a := NewArray()
	for i := range 10 {
		a.InsertAt(i, NewNumberFromInt(int32(i+1)))
	}
	require.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, ints(a))
	a.InsertAt(3, NewNumberFromInt(33))
	a.InsertAt(6, NewNumberFromInt(55))
	a.InsertAt(12, NewNumberFromInt(12))
	require.Equal(t, []int{1, 2, 3, 33, 4, 5, 55, 6, 7, 8, 9, 10, 12}, ints(a))
}

func TestArrayInsertAtBeyond(t *testing.T) {
	a := NewArray()
	a.InsertAt(1, NewNumberFromInt(0)) // index > size: no-op
	require.Equal(t, 0, a.Len())
	a.InsertAt(0, NewNumberFromInt(0)) // index == size: ok
	require.Equal(t, 1, a.Len())
	a.InsertAt(2, NewNumberFromInt(0)) // index > size: no-op
	require.Equal(t, 1, a.Len())
}

func TestArrayFindAndContains(t *testing.T) {
	d0, d1, d2 := NewDictionary(), NewDictionary(), NewDictionary()
	a := NewArray()
	a.Append(d0)
	a.Append(d1)

	_, ok := a.Find(nil)
	require.False(t, ok)
	i, ok := a.Find(d0)
	require.True(t, ok)
	require.Equal(t, 0, i)
	i, ok = a.Find(d1)
	require.True(t, ok)
	require.Equal(t, 1, i)
	_, ok = a.Find(d2)
	require.False(t, ok)

	require.True(t, a.Contains(d0))
	require.True(t, a.Contains(d1))
	require.False(t, a.Contains(d2))
}

func TestArrayRectAndMatrix(t *testing.T) {
	rectArr := NewArray()
	appendInts(rectArr, 1, 2, 3, 4)
	rect := rectArr.GetRect()
	require.Equal(t, float32(1), rect.Left)
	require.Equal(t, float32(2), rect.Bottom)
	require.Equal(t, float32(3), rect.Right)
	require.Equal(t, float32(4), rect.Top)

	// Wrong length -> zero rect.
	short := NewArray()
	appendInts(short, 1, 2, 3)
	require.Equal(t, float32(0), short.GetRect().Right)

	matArr := NewArray()
	appendInts(matArr, 1, 0, 0, 1, 5, 6)
	m := matArr.GetMatrix()
	require.Equal(t, float32(1), m.A)
	require.Equal(t, float32(5), m.E)
	require.Equal(t, float32(6), m.F)

	// Wrong length -> identity.
	short2 := NewArray()
	appendInts(short2, 1, 0, 0, 1, 5)
	require.True(t, short2.GetMatrix().IsIdentity())
}

func TestArrayCloneBasic(t *testing.T) {
	a := NewArray()
	for i := range 10 {
		a.InsertAt(i, NewNumberFromInt(int32(i+1)))
	}
	a2 := ToArray(a.Clone())
	require.Equal(t, a.Len(), a2.Len())
	for i := 0; i < a.Len(); i++ {
		require.NotSame(t, a.GetObjectAt(i), a2.GetObjectAt(i)) // new objects
		require.Equal(t, a.GetIntegerAt(i), a2.GetIntegerAt(i)) // same values
	}
}

func TestArrayCloneReferences(t *testing.T) {
	holder := newTestHolder()
	elems := [3][5]int32{{1, 2, 3, 4, 5}, {10, 9, 8, 7, 6}, {11, 12, 13, 14, 15}}
	outer := NewArray()
	for i := range 3 {
		row := NewArray()
		for j := range 5 {
			objNum := uint32(i*5 + j + 1)
			holder.add(objNum, NewNumberFromInt(elems[i][j]))
			row.InsertAt(j, NewReference(holder, objNum))
		}
		outer.Append(row)
	}

	arr1 := ToArray(outer.Clone())            // bDirect=false: references stay references
	arr2 := ToArray(CloneDirectObject(outer)) // bDirect=true: references deep-copied to targets
	require.Equal(t, 3, arr1.Len())
	require.Equal(t, 3, arr2.Len())

	for i := range 3 {
		row1 := ToArray(arr1.GetObjectAt(i))
		row2 := ToArray(arr2.GetObjectAt(i))
		require.NotNil(t, row1)
		require.NotNil(t, row2)
		for j := range 5 {
			elem1 := row1.GetObjectAt(j)
			require.Equal(t, TypeReference, elem1.Type())
			require.Equal(t, int(elems[i][j]), elem1.GetInteger())

			elem2 := row2.GetObjectAt(j)
			require.Equal(t, TypeNumber, elem2.Type()) // reference deep-copied to its target
			require.Equal(t, int(elems[i][j]), elem2.GetInteger())
			// The original element is an inline reference (objNum 0) and the
			// deep clone is a fresh Number (objNum 0): both 0, matching PDFium.
			require.Equal(t, elem1.GetObjNum(), elem2.GetObjNum())
		}
	}
}
