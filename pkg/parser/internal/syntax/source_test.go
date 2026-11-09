package syntax

import (
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/stretchr/testify/require"
)

func TestOriginalStringSourceRangeAndClone(t *testing.T) {
	input := []byte("prefix\n  (a\\(b\\)c)")
	p := NewWithOffset(input, 7)
	obj := objects.ToString(p.GetObjectBody(nil))
	require.NotNil(t, obj)
	start, end, ok := obj.SourceRange()
	require.True(t, ok)
	require.Equal(t, "(a\\(b\\)c)", string(input[start:end]))
	require.Equal(t, "a(b)c", obj.GetString())
	_, _, ok = objects.ToString(obj.Clone()).SourceRange()
	require.False(t, ok)
	decoded := New([]byte("(a)"))
	_, _, ok = objects.ToString(decoded.GetObjectBody(nil)).SourceRange()
	require.False(t, ok)
}

func TestSignatureIntegerSpellingPreserved(t *testing.T) {
	p := New([]byte("9007199254740993"))
	obj := objects.ToNumber(p.GetObjectBody(nil))
	require.NotNil(t, obj)
	require.Equal(t, "9007199254740993", obj.SourceLexeme())
}
