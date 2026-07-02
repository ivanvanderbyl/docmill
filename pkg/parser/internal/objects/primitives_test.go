// Ported behaviour from core/fpdfapi/parser/cpdf_object_unittest.cpp and the
// leaf-type sources @ pdfium 0db284a42.
package objects

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoolean(t *testing.T) {
	b := NewBoolean(true)
	require.Equal(t, TypeBoolean, b.Type())
	require.Equal(t, "true", b.GetString())
	require.Equal(t, 1, b.GetInteger())
	require.Equal(t, float32(0), b.GetNumber()) // not overridden
	require.Equal(t, "false", NewBoolean(false).GetString())
}

func TestNumberInteger(t *testing.T) {
	n := NewNumberFromInt(-128)
	require.True(t, n.IsInteger())
	require.Equal(t, -128, n.GetInteger())
	require.Equal(t, float32(-128), n.GetNumber())
	require.Equal(t, "-128", n.GetString())
}

func TestNumberUnsignedWrapAndClone(t *testing.T) {
	n := NewNumberFromString("4294967295") // > INT32_MAX, stored unsigned
	require.True(t, n.IsInteger())
	require.Equal(t, -1, n.GetInteger()) // reinterpreted as signed
	clone := ToNumber(n.Clone())
	require.Equal(t, -1, clone.GetInteger())
}

func TestNumberFloat(t *testing.T) {
	n := NewNumberFromFloat(3.5)
	require.False(t, n.IsInteger())
	require.Equal(t, float32(3.5), n.GetNumber())
	require.Equal(t, 3, n.GetInteger())
}

func TestStringUnicode(t *testing.T) {
	s := NewString("hi", false)
	require.Equal(t, "hi", s.GetString())
	require.Equal(t, "hi", s.GetUnicodeText())

	utf16 := NewString(string([]byte{0xfe, 0xff, 0x00, 0x41}), false)
	require.Equal(t, "A", utf16.GetUnicodeText())
}

func TestName(t *testing.T) {
	n := NewName("Foo")
	require.Equal(t, "Foo", n.GetString())
	require.Equal(t, "Foo", n.GetUnicodeText())
}

func TestReferenceNullHolder(t *testing.T) {
	r := NewReference(nil, 5)
	require.Equal(t, "", r.GetString())
	require.Equal(t, 0, r.GetInteger())
	require.Equal(t, float32(0), r.GetNumber())
	require.Nil(t, r.GetDict())
}

func TestReferenceResolution(t *testing.T) {
	h := newTestHolder()
	h.add(6, NewNumberFromInt(42))
	r := NewReference(h, 6)
	require.Equal(t, 42, r.GetInteger())
	require.Equal(t, NewNumberFromInt(42).GetInteger(), Direct(r).GetInteger())
}

func TestReferenceToReferenceRejected(t *testing.T) {
	h := newTestHolder()
	h.objs[6] = NewNumberFromInt(42)
	h.objs[5] = NewReference(h, 6) // ref -> ref
	r := NewReference(h, 5)
	// fastGetDirect refuses a ref-to-ref, so value accessors yield defaults.
	require.Equal(t, 0, r.GetInteger())
	// But GetDirect returns the (one-hop) resolved reference.
	require.Equal(t, TypeReference, Direct(r).Type())
}

func TestReferenceUnicodeTextAlwaysEmpty(t *testing.T) {
	h := newTestHolder()
	h.add(7, NewString("hi", false))
	r := NewReference(h, 7)
	require.Equal(t, "", r.GetUnicodeText()) // not overridden -> empty
}
