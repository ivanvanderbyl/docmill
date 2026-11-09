package objects

import "fmt"

// DecodeImageData decodes ordinary stream filters and returns any terminal
// image codec separately. Unlike StreamAcc, it never returns encoded bytes as
// a successful fallback after a malformed or failed filter.
func DecodeImageData(stream *Stream) ([]byte, string, error) {
	if stream == nil {
		return nil, "", fmt.Errorf("image: missing stream")
	}
	filters, ok := getDecoderArray(stream.Dict())
	if !ok {
		return nil, "", fmt.Errorf("image: malformed filter pipeline")
	}
	data := append([]byte(nil), stream.rawData()...)
	for i, filter := range filters {
		if !validIntermediateDecoders[filter.name] {
			if i != len(filters)-1 {
				return nil, "", fmt.Errorf("image: invalid terminal filter")
			}
			switch filter.name {
			case "DCTDecode", "DCT", "JPXDecode", "JBIG2Decode", "CCITTFaxDecode", "CCF":
				return data, filter.name, nil
			default:
				return nil, "", fmt.Errorf("image: unsupported filter %q", filter.name)
			}
		}
		result, ok := pdfDataDecode(data, 0, []decoderEntry{filter})
		if !ok {
			return nil, "", fmt.Errorf("image: failed %s decoding", filter.name)
		}
		data = result.data
	}
	return data, "", nil
}
