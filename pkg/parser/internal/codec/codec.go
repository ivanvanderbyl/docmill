// Package codec ports PDFium's stream filters from
// core/fpdfapi/parser/fpdf_parser_decode.cpp and core/fxcodec/flate @ pdfium
// 0db284a42: Flate, LZW, ASCII85, ASCIIHex, RunLength, and the PNG/TIFF
// predictors. It is a pure byte-transform library — it has no PDF object-model
// dependency. The dict-aware dispatch (GetDecoderArray/PDF_DataDecode) lives in
// the objects package, which reads /Filter and /DecodeParms and calls these
// functions; this layering avoids an objects<->codec import cycle. See plan 009
// Phase B.
package codec

// InvalidOffset is FX_INVALID_OFFSET (core/fxcrt/fx_extension.h): the sentinel
// "decode failed / no valid bytes consumed".
const InvalidOffset = ^uint32(0) // 0xFFFFFFFF

// maxStreamSize is kMaxStreamSize, the RunLengthDecode output cap (20 MB).
const maxStreamSize = 20 * 1024 * 1024 // 20971520

// maxInitialAllocSize caps the initial Flate output allocation (10 MB).
const maxInitialAllocSize = 10000000

// maxTotalOutSize clamps Flate total output (1 GiB).
const maxTotalOutSize = 1024 * 1024 * 1024

func isLineEnding(ch byte) bool { return ch == '\r' || ch == '\n' }

func isHexDigit(ch byte) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func hexCharToInt(ch byte) int {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0')
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10
	default:
		return int(ch-'A') + 10
	}
}

// a85Result extracts byte i (0..3) of the 32-bit ASCII85 group, big-endian.
func a85Result(res uint32, i int) byte {
	return byte(res >> uint((3-i)*8))
}
