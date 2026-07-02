// Ported from the PDFStreamTest and StreamAccTest cases in
// cpdf_object_unittest.cpp / cpdf_stream_acc_unittest.cpp @ pdfium 0db284a42,
// plus a Flate filter-pipeline integration test.
package objects

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamSetData(t *testing.T) {
	stream := NewStreamFromData(make([]byte, 100), NewDictionary())
	require.Equal(t, 100, stream.Dict().GetIntegerFor("Length"))

	stream.Dict().SetFor("Filter", NewString("SomeFilter", false))
	stream.Dict().SetFor("DecodeParms", NewString("SomeParams", false))

	stream.SetData(make([]byte, 200))
	require.Equal(t, 200, stream.Dict().GetIntegerFor("Length"))
	// Filter/DecodeParms are untouched by SetData.
	require.Equal(t, "SomeFilter", stream.Dict().GetByteStringFor("Filter"))
	require.Equal(t, "SomeParams", stream.Dict().GetByteStringFor("DecodeParms"))
}

func TestStreamSetDataAndRemoveFilter(t *testing.T) {
	stream := NewStreamFromData(make([]byte, 100), NewDictionary())
	stream.Dict().SetFor("Filter", NewString("SomeFilter", false))
	stream.Dict().SetFor("DecodeParms", NewString("SomeParams", false))

	stream.SetDataAndRemoveFilter(make([]byte, 200))
	require.Equal(t, 200, stream.Dict().GetIntegerFor("Length"))
	require.False(t, stream.Dict().KeyExist("Filter"))
	require.False(t, stream.Dict().KeyExist("DecodeParms"))
}

func TestStreamLengthCorrectedOnCreate(t *testing.T) {
	// A wrong pre-existing /Length is overwritten with the actual size.
	dict := NewDictionary()
	dict.SetNewNumberFor("Length", 30000)
	stream := NewStreamFromData(make([]byte, 100), dict)
	require.Equal(t, 100, stream.Dict().GetIntegerFor("Length"))
}

func TestStreamAccRawDataLifetime(t *testing.T) {
	stream := NewStreamFromSpan([]byte{0x61, 0x62, 0x63})
	acc := NewStreamAcc(stream)
	acc.LoadAllDataRaw()
	require.Equal(t, []byte{0x61, 0x62, 0x63}, acc.GetSpan())
}

func TestStreamAccNoFilterServesRaw(t *testing.T) {
	stream := NewStreamFromSpan([]byte("hello"))
	acc := NewStreamAcc(stream)
	acc.LoadAllDataFiltered() // no /Filter -> raw bytes
	require.Equal(t, []byte("hello"), acc.GetSpan())
}

func TestStreamAccFlateFilter(t *testing.T) {
	// zlib("123") from the flatemodule decode vectors.
	encoded := []byte{0x78, 0x9c, 0x33, 0x34, 0x32, 0x06, 0x00, 0x01, 0x2d, 0x00, 0x97}
	dict := NewDictionary()
	dict.SetNewNameFor("Filter", "FlateDecode")
	stream := NewStreamFromData(encoded, dict)

	acc := NewStreamAcc(stream)
	acc.LoadAllDataFiltered()
	require.True(t, bytes.Equal([]byte("123"), acc.GetSpan()))
}

func TestStreamAccFlateArrayFilter(t *testing.T) {
	// /Filter as a single-element array [ /FlateDecode ].
	encoded := []byte{0x78, 0x9c, 0x33, 0x34, 0x32, 0x06, 0x00, 0x01, 0x2d, 0x00, 0x97}
	dict := NewDictionary()
	filter := dict.SetNewArrayFor("Filter")
	filter.Append(NewName("FlateDecode"))
	stream := NewStreamFromData(encoded, dict)

	acc := NewStreamAcc(stream)
	acc.LoadAllDataFiltered()
	require.Equal(t, []byte("123"), acc.GetSpan())
}
