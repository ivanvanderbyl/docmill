// Ported from GetDecoderArray / ValidateDecoderPipeline / PDF_DataDecode in
// core/fpdfapi/parser/fpdf_parser_decode.cpp @ pdfium 0db284a42.
//
// This is the dict-aware decode dispatch that StreamAcc drives: it reads
// /Filter and /DecodeParms from the stream dictionary, builds the decoder
// pipeline, and applies each stage via the pure-byte codec package. It lives in
// objects (not codec) so codec stays free of the PDF object model.
package objects

import "github.com/ivanvanderbyl/docmill/pkg/parser/internal/codec"

type decoderEntry struct {
	name  string
	param *Dictionary
}

// validIntermediateDecoders are the only filter names allowed before the last
// stage of a multi-filter pipeline (ValidateDecoderPipeline).
var validIntermediateDecoders = map[string]bool{
	"FlateDecode": true, "Fl": true,
	"LZWDecode": true, "LZW": true,
	"ASCII85Decode": true, "A85": true,
	"ASCIIHexDecode": true, "AHx": true,
	"RunLengthDecode": true, "RL": true,
}

// getDecoderArray builds the filter pipeline from a stream dictionary. ok=false
// is PDFium's nullopt (malformed /Filter) — StreamAcc then serves raw.
func getDecoderArray(dict *Dictionary) ([]decoderEntry, bool) {
	pFilter := dict.GetDirectObjectFor("Filter")
	if pFilter == nil {
		return nil, true // no filter -> empty pipeline, ok
	}
	pParams := dict.GetDirectObjectFor("DecodeParms")

	switch filter := pFilter.(type) {
	case *Array:
		if !validateDecoderPipeline(filter) {
			return nil, false
		}
		paramsArray := ToArray(pParams)
		out := make([]decoderEntry, 0, filter.Len())
		for i := 0; i < filter.Len(); i++ {
			var param *Dictionary
			if paramsArray != nil {
				param = paramsArray.GetDictAt(i)
			}
			out = append(out, decoderEntry{name: filter.GetByteStringAt(i), param: param})
		}
		return out, true
	case *Name:
		var param *Dictionary
		if pParams != nil {
			param = pParams.GetDict()
		}
		return []decoderEntry{{name: filter.GetString(), param: param}}, true
	default:
		return nil, false // /Filter is neither Array nor Name
	}
}

// validateDecoderPipeline checks every element is a Name and every non-last
// filter is a known intermediate decoder.
func validateDecoderPipeline(filter *Array) bool {
	count := filter.Len()
	if count == 0 {
		return true
	}
	for i := range count {
		if _, ok := filter.GetDirectObjectAt(i).(*Name); !ok {
			return false
		}
	}
	if count == 1 {
		return true
	}
	for i := 0; i < count-1; i++ {
		if !validIntermediateDecoders[filter.GetByteStringAt(i)] {
			return false
		}
	}
	return true
}

type pdfDataDecodeResult struct {
	data []byte
}

// pdfDataDecode runs the pipeline sequentially. ok=false (a non-image stage
// failed) signals StreamAcc to serve raw. The final-stage-only size estimate,
// the Crypt skip, and the terminal-image early-return (an unrecognised filter
// name is a terminal image filter; decoding stops with the pipeline output so
// far) are all preserved. PDFium's image-accessor mode and the which-filter
// bookkeeping are not ported — no caller consumes them.
func pdfDataDecode(srcSpan []byte, lastEstimatedSize uint32, decoders []decoderEntry) (pdfDataDecodeResult, bool) {
	var result pdfDataDecodeResult
	lastSpan := srcSpan
	nSize := len(decoders)

	for i := range nSize {
		var estimatedSize uint32
		if i == nSize-1 {
			estimatedSize = lastEstimatedSize
		}
		decoder := decoders[i].name
		param := decoders[i].param

		var newBuf []byte
		bytesConsumed := codec.InvalidOffset

		switch decoder {
		case "Crypt":
			continue
		case "FlateDecode", "Fl":
			newBuf, bytesConsumed = flateDecodeWithParams(false, lastSpan, param, estimatedSize)
		case "LZWDecode", "LZW":
			newBuf, bytesConsumed = flateDecodeWithParams(true, lastSpan, param, estimatedSize)
		case "ASCII85Decode", "A85":
			newBuf, bytesConsumed = codec.A85Decode(lastSpan)
		case "ASCIIHexDecode", "AHx":
			newBuf, bytesConsumed = codec.HexDecode(lastSpan)
		case "RunLengthDecode", "RL":
			newBuf, bytesConsumed = codec.RunLengthDecode(lastSpan)
		default:
			// Any other name is a (terminal) image filter: stop decoding and
			// serve what the pipeline produced so far.
			return result, true
		}

		if bytesConsumed == codec.InvalidOffset {
			return pdfDataDecodeResult{}, false
		}
		lastSpan = newBuf
		result.data = newBuf
	}

	return result, true
}

// flateDecodeWithParams reads the Flate/LZW DecodeParms (with PDFium defaults)
// and decodes.
func flateDecodeWithParams(useLZW bool, src []byte, param *Dictionary, estimatedSize uint32) ([]byte, uint32) {
	// With no DecodeParms, PDFium passes predictor/Colors/BPC/Columns = 0 (the
	// predictor is then None, so the dimensions are never read).
	predictor := 0
	earlyChange := true
	colors := 0
	bpc := 0
	columns := 0
	if param != nil {
		predictor = param.GetIntegerFor("Predictor")
		earlyChange = param.GetIntegerWithDefaultFor("EarlyChange", 1) != 0
		colors = param.GetIntegerWithDefaultFor("Colors", 1)
		bpc = param.GetIntegerWithDefaultFor("BitsPerComponent", 8)
		columns = param.GetIntegerWithDefaultFor("Columns", 1)
	}
	return codec.FlateOrLZWDecode(useLZW, src, earlyChange, predictor, colors, bpc, columns, estimatedSize)
}
