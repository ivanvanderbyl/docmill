package image

import (
	"bytes"
	"fmt"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/syntax"
)

// InlineImages parses BI dictionaries and their binary data without treating
// operator-looking bytes in strings or image samples as content operators.
func InlineImages(data []byte, resources *objects.Dictionary) ([]*objects.Stream, error) {
	p := syntax.New(data)
	var out []*objects.Stream
	for p.GetPos() < len(data) {
		word := p.PeekNextWord()
		if word == "" {
			break
		}
		if word == "(" || word == "[" || word == "<<" || word == "<" {
			old := p.GetPos()
			p.GetObjectBody(nil)
			if p.GetPos() == old {
				return nil, fmt.Errorf("image: malformed content object")
			}
			continue
		}
		word, _ = p.GetNextWord()
		if word != "BI" {
			continue
		}
		dict := objects.NewDictionary()
		for {
			key, _ := p.GetNextWord()
			if key == "ID" {
				break
			}
			if len(key) < 2 || key[0] != '/' {
				return nil, fmt.Errorf("image: malformed inline dictionary")
			}
			value := p.GetObjectBody(nil)
			if value == nil {
				return nil, fmt.Errorf("image: invalid inline value")
			}
			key = expandKey(key[1:])
			if key == "ColorSpace" || key == "Filter" {
				value = expandValue(value)
			}
			dict.SetFor(key, value)
		}
		start := p.GetPos()
		if start >= len(data) || !isSpace(data[start]) {
			return nil, fmt.Errorf("image: inline ID lacks whitespace")
		}
		start++
		if data[start-1] == '\r' && start < len(data) && data[start] == '\n' {
			start++
		}
		end, after, err := inlineEnd(data, start, dict, resources)
		if err != nil {
			return nil, err
		}
		out = append(out, objects.NewStreamFromData(append([]byte(nil), data[start:end]...), dict))
		p.SetPos(after)
	}
	return out, nil
}

func inlineEnd(data []byte, start int, d, resources *objects.Dictionary) (int, int, error) {
	if !d.KeyExist("Filter") {
		components := 1
		bpc := d.GetIntegerWithDefaultFor("BitsPerComponent", 1)
		if !d.GetBooleanFor("ImageMask", false) {
			cs, err := colorSpace(d.GetObjectFor("ColorSpace"), resources, make(map[objects.Object]bool))
			if err != nil {
				return 0, 0, err
			}
			components = cs.components
			bpc = d.GetIntegerFor("BitsPerComponent")
		}
		w, h := d.GetIntegerFor("Width"), d.GetIntegerFor("Height")
		if w <= 0 || h <= 0 || w > 131071 || h > 131071 || bpc <= 0 || bpc > 16 {
			return 0, 0, fmt.Errorf("image: invalid inline dimensions")
		}
		length := (uint64(w)*uint64(components)*uint64(bpc) + 7) / 8 * uint64(h)
		if length > uint64(len(data)-start) {
			return 0, 0, fmt.Errorf("image: truncated inline image")
		}
		end := start + int(length)
		p := syntax.New(data)
		p.SetPos(end)
		word, _ := p.GetNextWord()
		if word != "EI" {
			return 0, 0, fmt.Errorf("image: missing inline EI")
		}
		return end, p.GetPos(), nil
	}
	// Filtered payloads may contain the bytes EI. Accept a boundary only if the
	// complete candidate decodes to the declared image dimensions.
	cursor := start
	attempts := 0
	for cursor < len(data) {
		rel := bytes.Index(data[cursor:], []byte("EI"))
		if rel < 0 {
			break
		}
		ei := cursor + rel
		cursor = ei + 2
		if ei <= start || !isSpace(data[ei-1]) || (ei+2 < len(data) && !isSpace(data[ei+2])) {
			continue
		}
		attempts++
		if attempts > 256 {
			return 0, 0, fmt.Errorf("image: too many candidate inline boundaries")
		}
		end := ei - 1
		s := objects.NewStreamFromData(data[start:end], d)
		if _, _, err := Decode(s, resources); err == nil {
			return end, ei + 2, nil
		}
	}
	return 0, 0, fmt.Errorf("image: invalid or unsupported filtered inline image")
}
func isSpace(b byte) bool { return b == 0 || b == 9 || b == 10 || b == 12 || b == 13 || b == 32 }
func expandKey(k string) string {
	switch k {
	case "W":
		return "Width"
	case "H":
		return "Height"
	case "BPC":
		return "BitsPerComponent"
	case "CS":
		return "ColorSpace"
	case "D":
		return "Decode"
	case "DP":
		return "DecodeParms"
	case "F":
		return "Filter"
	case "IM":
		return "ImageMask"
	case "I":
		return "Interpolate"
	}
	return k
}
func expandValue(o objects.Object) objects.Object {
	if n := objects.ToName(o); n != nil {
		switch n.GetString() {
		case "G":
			return objects.NewName("DeviceGray")
		case "RGB":
			return objects.NewName("DeviceRGB")
		case "CMYK":
			return objects.NewName("DeviceCMYK")
		case "I":
			return objects.NewName("Indexed")
		}
	}
	return o
}
