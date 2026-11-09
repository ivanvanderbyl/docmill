package image

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/stretchr/testify/require"
)

func stream(w, h, bpc int, cs string, data []byte) *objects.Stream {
	d := objects.NewDictionary()
	d.SetFor("Width", objects.NewNumberFromInt(int32(w)))
	d.SetFor("Height", objects.NewNumberFromInt(int32(h)))
	d.SetFor("BitsPerComponent", objects.NewNumberFromInt(int32(bpc)))
	d.SetNewNameFor("ColorSpace", cs)
	return objects.NewStreamFromData(data, d)
}
func TestSamples(t *testing.T) {
	for _, tc := range []struct {
		name string
		bpc  int
		data []byte
		want []uint8
	}{
		{"one bit row padding", 1, []byte{0x80, 0x40}, []uint8{255, 0, 0, 255}},
		{"two bit", 2, []byte{0x30, 0xc0}, []uint8{0, 255, 255, 0}},
		{"four bit", 4, []byte{0x0f, 0xf0}, []uint8{0, 255, 255, 0}},
		{"eight bit", 8, []byte{0, 255, 255, 0}, []uint8{0, 255, 255, 0}},
		{"sixteen bit big endian", 16, []byte{0, 0, 255, 255, 255, 255, 0, 0}, []uint8{0, 255, 255, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			im, _, err := Decode(stream(2, 2, tc.bpc, "DeviceGray", tc.data), nil)
			require.NoError(t, err)
			for i, v := range tc.want {
				require.Equal(t, v, im.NRGBAAt(i%2, i/2).R)
			}
		})
	}
}
func TestDecodeInversionAndMask(t *testing.T) {
	s := stream(2, 1, 8, "DeviceRGB", []byte{255, 0, 0, 0, 255, 0})
	decode := objects.NewArray()
	for _, v := range []int32{1, 0, 1, 0, 1, 0} {
		decode.Append(objects.NewNumberFromInt(v))
	}
	s.Dict().SetFor("Decode", decode)
	mask := objects.NewArray()
	for _, v := range []int32{255, 255, 0, 0, 0, 0} {
		mask.Append(objects.NewNumberFromInt(v))
	}
	s.Dict().SetFor("Mask", mask)
	im, _, err := Decode(s, nil)
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{0, 255, 255, 0}, im.NRGBAAt(0, 0))
	require.Equal(t, color.NRGBA{255, 0, 255, 255}, im.NRGBAAt(1, 0))
}
func TestIndexedPalette(t *testing.T) {
	s := stream(2, 1, 1, "", []byte{0x40})
	a := objects.NewArray()
	a.Append(objects.NewName("Indexed"))
	a.Append(objects.NewName("DeviceRGB"))
	a.Append(objects.NewNumberFromInt(1))
	a.Append(objects.NewString(string([]byte{255, 0, 0, 0, 255, 0}), false))
	s.Dict().SetFor("ColorSpace", a)
	im, _, err := Decode(s, nil)
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{255, 0, 0, 255}, im.NRGBAAt(0, 0))
	require.Equal(t, color.NRGBA{0, 255, 0, 255}, im.NRGBAAt(1, 0))
}
func TestFlateAndSoftMask(t *testing.T) {
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	_, err := z.Write([]byte{255, 0, 0})
	require.NoError(t, err)
	require.NoError(t, z.Close())
	s := stream(1, 1, 8, "DeviceRGB", b.Bytes())
	s.Dict().SetNewNameFor("Filter", "FlateDecode")
	s.Dict().SetFor("SMask", stream(1, 1, 8, "DeviceGray", []byte{127}))
	im, raw, err := Decode(s, nil)
	require.NoError(t, err)
	require.Nil(t, raw)
	require.Equal(t, color.NRGBA{255, 0, 0, 127}, im.NRGBAAt(0, 0))
}
func TestJPEGPreserved(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil))
	s := stream(1, 1, 8, "DeviceRGB", b.Bytes())
	s.Dict().SetNewNameFor("Filter", "DCTDecode")
	_, raw, err := Decode(s, nil)
	require.NoError(t, err)
	require.Equal(t, b.Bytes(), raw)
}
func TestMalformedImages(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*objects.Stream)
	}{
		{"truncated", func(s *objects.Stream) { s.SetData(nil) }},
		{"oversize", func(s *objects.Stream) { s.Dict().SetFor("Width", objects.NewNumberFromInt(131072)) }},
		{"bad depth", func(s *objects.Stream) { s.Dict().SetFor("BitsPerComponent", objects.NewNumberFromInt(3)) }},
		{"bad filter", func(s *objects.Stream) { s.Dict().SetNewNameFor("Filter", "Invalid") }},
		{"bad compressed bytes", func(s *objects.Stream) { s.Dict().SetNewNameFor("Filter", "FlateDecode") }},
		{"cyclic mask", func(s *objects.Stream) { s.Dict().SetFor("SMask", s) }},
		{"unsupported color space", func(s *objects.Stream) { s.Dict().SetNewNameFor("ColorSpace", "Separation") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := stream(1, 1, 8, "DeviceGray", []byte{255})
			tc.mutate(s)
			_, _, err := Decode(s, nil)
			require.Error(t, err)
		})
	}
}
func TestInlineBinaryAndStrings(t *testing.T) {
	// The pixel contains an EI byte sequence; raw length, not a delimiter search,
	// identifies the end. BI inside a literal string is not an image operator.
	data := append([]byte("(BI is text) Tj BI /W 3 /H 1 /BPC 8 /CS /G ID "), []byte{'E', 'I', ' '}...)
	data = append(data, []byte(" EI")...)
	images, err := InlineImages(data, nil)
	require.NoError(t, err)
	require.Len(t, images, 1)
	im, _, err := Decode(images[0], nil)
	require.NoError(t, err)
	require.Equal(t, uint8('E'), im.NRGBAAt(0, 0).R)
}

func TestNamedColorSpaceAndCMYK(t *testing.T) {
	r := objects.NewDictionary()
	cs := objects.NewDictionary()
	cs.SetNewNameFor("Named", "DeviceCMYK")
	r.SetFor("ColorSpace", cs)
	s := stream(1, 1, 8, "Named", []byte{0, 255, 255, 0})
	im, _, err := Decode(s, r)
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{255, 0, 0, 255}, im.NRGBAAt(0, 0))
	cs.SetNewNameFor("Named", "Named")
	_, _, err = Decode(s, r)
	require.Error(t, err)
}
func TestFilteredInlineAndMalformedEnd(t *testing.T) {
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	_, err := z.Write([]byte{255})
	require.NoError(t, err)
	require.NoError(t, z.Close())
	data := append([]byte("BI /W 1 /H 1 /BPC 8 /CS /G /F /Fl ID "), b.Bytes()...)
	data = append(data, []byte(" EI")...)
	images, err := InlineImages(data, nil)
	require.NoError(t, err)
	require.Len(t, images, 1)
	for _, data := range []string{"BI /W 1 /H 1 /BPC 8 /CS /G ID X", "BI /W 1 /H 1 /BPC 8 /CS /G ID X no", "BI nonsense"} {
		_, err := InlineImages([]byte(data), nil)
		require.Error(t, err)
	}
}
func TestMaskDimensionsAndDecodeLength(t *testing.T) {
	s := stream(1, 1, 8, "DeviceGray", []byte{255})
	s.Dict().SetFor("SMask", stream(2, 1, 8, "DeviceGray", []byte{255, 255}))
	_, _, err := Decode(s, nil)
	require.ErrorContains(t, err, "dimensions")
	s = stream(1, 1, 8, "DeviceGray", []byte{255})
	s.Dict().SetFor("Decode", objects.NewArray())
	_, _, err = Decode(s, nil)
	require.ErrorContains(t, err, "decode array")
}

func TestGroup4Fax(t *testing.T) {
	// The vertical-zero code '1' copies an all-white reference row. Three
	// consecutive rows therefore decode from 111, without an end marker.
	for _, black := range []bool{false, true} {
		s := stream(8, 3, 1, "DeviceGray", []byte{0xe0})
		s.Dict().SetNewNameFor("Filter", "CCITTFaxDecode")
		p := objects.NewDictionary()
		p.SetFor("K", objects.NewNumberFromInt(-1))
		p.SetFor("Columns", objects.NewNumberFromInt(8))
		p.SetFor("BlackIs1", objects.NewBoolean(black))
		s.Dict().SetFor("DecodeParms", p)
		im, _, err := Decode(s, nil)
		require.NoError(t, err)
		want := uint8(255)
		if black {
			want = 0
		}
		require.Equal(t, want, im.NRGBAAt(7, 2).R)
	}
}

func TestGroup3FaxEOL(t *testing.T) {
	bitstring := "000000000001" + "10011" + "000000000001000000000001000000000001000000000001000000000001000000000001"
	data := make([]byte, (len(bitstring)+7)/8)
	for i, b := range bitstring {
		if b == '1' {
			data[i/8] |= 1 << uint(7-i%8)
		}
	}
	s := stream(8, 1, 1, "DeviceGray", data)
	s.Dict().SetNewNameFor("Filter", "CCITTFaxDecode")
	p := objects.NewDictionary()
	p.SetFor("Columns", objects.NewNumberFromInt(8))
	p.SetFor("EndOfLine", objects.NewBoolean(true))
	s.Dict().SetFor("DecodeParms", p)
	im, _, err := Decode(s, nil)
	require.NoError(t, err)
	require.Equal(t, uint8(255), im.NRGBAAt(7, 0).R)
}
func TestPNGImagePredictor(t *testing.T) {
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	_, err := z.Write([]byte{0, 255, 0, 0})
	require.NoError(t, err)
	require.NoError(t, z.Close())
	s := stream(1, 1, 8, "DeviceRGB", b.Bytes())
	s.Dict().SetNewNameFor("Filter", "FlateDecode")
	p := objects.NewDictionary()
	p.SetFor("Predictor", objects.NewNumberFromInt(12))
	p.SetFor("Colors", objects.NewNumberFromInt(3))
	p.SetFor("Columns", objects.NewNumberFromInt(1))
	s.Dict().SetFor("DecodeParms", p)
	im, _, err := Decode(s, nil)
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{255, 0, 0, 255}, im.NRGBAAt(0, 0))
}

func TestStencilAndExplicitMask(t *testing.T) {
	mask := stream(2, 1, 1, "", []byte{0x40})
	mask.Dict().SetFor("ImageMask", objects.NewBoolean(true))
	mask.Dict().RemoveFor("ColorSpace")
	im, _, err := Decode(mask, nil)
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{0, 0, 0, 255}, im.NRGBAAt(0, 0))
	require.Equal(t, uint8(0), im.NRGBAAt(1, 0).A)
	s := stream(2, 1, 8, "DeviceRGB", []byte{255, 0, 0, 0, 255, 0})
	s.Dict().SetFor("Mask", mask)
	im, _, err = Decode(s, nil)
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{255, 0, 0, 255}, im.NRGBAAt(0, 0))
	require.Equal(t, uint8(0), im.NRGBAAt(1, 0).A)
}
func TestJPEGDecodeArray(t *testing.T) {
	var b bytes.Buffer
	src := image.NewGray(image.Rect(0, 0, 1, 1))
	src.SetGray(0, 0, color.Gray{Y: 0})
	require.NoError(t, jpeg.Encode(&b, src, nil))
	s := stream(1, 1, 8, "DeviceGray", b.Bytes())
	s.Dict().SetNewNameFor("Filter", "DCTDecode")
	a := objects.NewArray()
	a.Append(objects.NewNumberFromInt(1))
	a.Append(objects.NewNumberFromInt(0))
	s.Dict().SetFor("Decode", a)
	im, raw, err := Decode(s, nil)
	require.NoError(t, err)
	require.Nil(t, raw)
	require.Equal(t, color.NRGBA{255, 255, 255, 255}, im.NRGBAAt(0, 0))
}

func TestInlineStencilDecodeInversion(t *testing.T) {
	images, err := InlineImages([]byte("BI /W 2 /H 1 /IM true /D [1 0] ID \x40 EI"), nil)
	require.NoError(t, err)
	require.Len(t, images, 1)
	im, _, err := Decode(images[0], nil)
	require.NoError(t, err)
	require.Equal(t, uint8(0), im.NRGBAAt(0, 0).A)
	require.Equal(t, uint8(255), im.NRGBAAt(1, 0).A)
}

func TestSoftMaskMatte(t *testing.T) {
	s := stream(1, 1, 8, "DeviceRGB", []byte{127, 127, 127})
	mask := stream(1, 1, 8, "DeviceGray", []byte{128})
	a := objects.NewArray()
	for range 3 {
		a.Append(objects.NewNumberFromInt(1))
	}
	mask.Dict().SetFor("Matte", a)
	s.Dict().SetFor("SMask", mask)
	im, _, err := Decode(s, nil)
	require.NoError(t, err)
	require.Equal(t, color.NRGBA{0, 0, 0, 128}, im.NRGBAAt(0, 0))
}
