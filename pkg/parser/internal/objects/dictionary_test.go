// Ported from core/fpdfapi/parser/cpdf_dictionary_unittest.cpp @ pdfium
// 0db284a42, plus behavioural vectors implied by the C++ code paths.
package objects

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDictionaryIterators(t *testing.T) {
	d := NewDictionary()
	d.SetNewDictFor("the-dictionary")
	d.SetNewArrayFor("the-array")
	d.SetNewNumberFor("the-number", 42)

	// Keys come out sorted ascending by byte, regardless of insertion order.
	keys := d.GetKeys()
	require.Equal(t, []string{"the-array", "the-dictionary", "the-number"}, keys)

	_, ok := d.GetObjectFor("the-array").(*Array)
	require.True(t, ok)
	_, ok = d.GetObjectFor("the-dictionary").(*Dictionary)
	require.True(t, ok)
	_, ok = d.GetObjectFor("the-number").(*Number)
	require.True(t, ok)
	require.Equal(t, 42, d.GetIntegerFor("the-number"))
	require.Equal(t, 42, d.GetDirectIntegerFor("the-number"))
}

func TestDictionaryAbsentKeyDefaults(t *testing.T) {
	d := NewDictionary()
	require.Equal(t, "", d.GetByteStringFor("x"))
	require.Equal(t, 0, d.GetIntegerFor("x"))
	require.Equal(t, 7, d.GetIntegerWithDefaultFor("x", 7))
	require.Equal(t, float32(0), d.GetFloatFor("x"))
	require.True(t, d.GetBooleanFor("x", true))
	require.Nil(t, d.GetDictFor("x"))
	require.Nil(t, d.GetArrayFor("x"))
	require.Nil(t, d.GetStreamFor("x"))
	require.Equal(t, "", d.GetNameFor("x"))
	require.False(t, d.KeyExist("x"))
	require.Equal(t, float32(0), d.GetRectFor("x").Right)
	require.True(t, d.GetMatrixFor("x").IsIdentity())
}

func TestDictionaryRectAndMatrix(t *testing.T) {
	d := NewDictionary()
	rect := d.SetNewArrayFor("rect")
	appendInts(rect, 1, 2, 3, 4)
	got := d.GetRectFor("rect")
	require.Equal(t, float32(1), got.Left)
	require.Equal(t, float32(4), got.Top)

	mat := d.SetNewArrayFor("mat")
	appendInts(mat, 1, 0, 0, 1, 5, 6)
	m := d.GetMatrixFor("mat")
	require.Equal(t, float32(5), m.E)
	require.Equal(t, float32(6), m.F)
}

func TestDictionarySetNilErasesKey(t *testing.T) {
	d := NewDictionary()
	d.SetNewNumberFor("the-number", 42)
	require.True(t, d.KeyExist("the-number"))
	d.SetFor("the-number", nil)
	require.False(t, d.KeyExist("the-number"))
}

func TestDictionaryRemoveFor(t *testing.T) {
	d := NewDictionary()
	arr := d.SetNewArrayFor("the-array")
	removed := d.RemoveFor("the-array")
	require.Equal(t, Object(arr), removed)
	require.False(t, d.KeyExist("the-array"))
	require.Nil(t, d.RemoveFor("absent"))
}

func TestDictionaryLiteralTypeGetters(t *testing.T) {
	d := NewDictionary()
	d.SetNewNumberFor("num", 5)
	d.SetFor("bool", NewBoolean(true))
	// GetNameFor requires a literal Name.
	require.Equal(t, "", d.GetNameFor("num"))
	// GetDirectIntegerFor requires a literal Number.
	require.Equal(t, 0, d.GetDirectIntegerFor("bool"))
	require.Equal(t, 5, d.GetDirectIntegerFor("num"))
}
