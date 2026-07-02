// Ported from the predictor logic in core/fxcodec/flate/flatemodule.cpp @
// pdfium 0db284a42: PNG predictors (10..15) and TIFF predictor 2, applied after
// Flate/LZW decode. All sample additions are intentionally modular (byte/uint16
// wraparound). See plan 009 Phase B.
package codec

import "math"

type predictorType int

const (
	predNone predictorType = iota
	predTIFF               // TIFF predictor 2 (PDFium's kFlate)
	predPNG                // PNG predictor group (10..15)
)

// getPredictor maps the /Predictor value to a predictor kind.
func getPredictor(predictor int) predictorType {
	if predictor >= 10 {
		return predPNG
	}
	if predictor == 2 {
		return predTIFF
	}
	return predNone
}

// calculatePitch8 = (bpc*components*width + 7) / 8, with per-step uint32
// overflow checks mirroring PDFium's FX_SAFE_UINT32 (a single uint64 product
// could wrap for adversarial inputs).
func calculatePitch8(bpc, components, width int) (uint32, bool) {
	if bpc < 0 || components < 0 || width < 0 {
		return 0, false
	}
	prod := uint64(bpc) * uint64(components)
	if prod > math.MaxUint32 {
		return 0, false
	}
	prod *= uint64(width)
	if prod > math.MaxUint32 {
		return 0, false
	}
	prod += 7
	if prod > math.MaxUint32 {
		return 0, false
	}
	return uint32(prod / 8), true
}

// tiffPredictor applies TIFF predictor 2 in place, row by row.
func tiffPredictor(colors, bpc, columns int, data []byte) bool {
	rowSize, ok := calculatePitch8(bpc, colors, columns)
	if !ok || rowSize == 0 {
		return false
	}
	off := 0
	for off < len(data) {
		n := min(int(rowSize), len(data)-off)
		tiffPredictLine(data[off:off+n], bpc, colors, columns)
		off += n
	}
	return true
}

func tiffPredictLine(dest []byte, bpc, colors, columns int) {
	if bpc == 1 {
		rowBits := min(bpc*colors*columns, len(dest)*8)
		indexPre, colPre := 0, 0
		for i := 1; i < rowBits; i++ {
			col := i % 8
			index := i / 8
			cur := (dest[index] >> (7 - col)) & 1
			prev := (dest[indexPre] >> (7 - colPre)) & 1
			if cur^prev != 0 {
				dest[index] |= byte(1 << (7 - col))
			} else {
				dest[index] &^= byte(1 << (7 - col))
			}
			indexPre, colPre = index, col
		}
		return
	}

	bytesPerPixel := bpc * colors / 8
	if bpc == 16 {
		for i := bytesPerPixel; i+1 < len(dest); i += 2 {
			pixel := uint16(dest[i-bytesPerPixel])<<8 | uint16(dest[i-bytesPerPixel+1])
			pixel += uint16(dest[i])<<8 | uint16(dest[i+1])
			dest[i] = byte(pixel >> 8)
			dest[i+1] = byte(pixel)
		}
		return
	}

	for i := bytesPerPixel; i < len(dest); i++ {
		dest[i] += dest[i-bytesPerPixel]
	}
}

// pngPredictor unfilters a PNG-predicted buffer, returning a new buffer.
func pngPredictor(colors, bpc, columns int, src []byte) ([]byte, bool) {
	rowSize, ok := calculatePitch8(bpc, colors, columns)
	if !ok || rowSize == 0 {
		return nil, false
	}
	srcRowSize := uint64(rowSize) + 1
	// row_count intentionally counts a trailing partial row.
	rowCount := (uint64(len(src)) + uint64(rowSize)) / srcRowSize
	if rowCount == 0 {
		return nil, false
	}
	lastRowSize := uint64(len(src)) % srcRowSize

	destSize := uint64(rowSize) * rowCount
	if destSize/uint64(rowSize) != rowCount { // Fx2DSize overflow guard
		return nil, false
	}
	if lastRowSize != 0 {
		destSize -= srcRowSize - lastRowSize
	}
	if destSize > math.MaxInt32 {
		return nil, false
	}

	dest := make([]byte, destSize)
	bytesPerPixel := (colors*bpc + 7) / 8
	srcPos, destPos := 0, 0
	var prevRow []byte
	for range rowCount {
		remaining := len(src) - srcPos
		if remaining <= 0 {
			break
		}
		remainingRowSize := min(int(rowSize), remaining-1)
		if remainingRowSize < 0 {
			break
		}
		if destPos+remainingRowSize > len(dest) {
			remainingRowSize = len(dest) - destPos
		}
		pngPredictLine(dest[destPos:destPos+remainingRowSize], src[srcPos:], prevRow, remainingRowSize, bytesPerPixel)
		srcPos += remainingRowSize + 1
		prevRow = dest[destPos : destPos+remainingRowSize]
		destPos += remainingRowSize
	}
	return dest, true
}

// pngPredictLine reconstructs one row in dest from the filtered src (whose first
// byte is the PNG filter tag) and the previous reconstructed row last.
func pngPredictLine(dest, src, last []byte, rowSize, bpp int) {
	if len(src) == 0 || rowSize <= 0 {
		return
	}
	tag := src[0]
	rs := src[1 : 1+rowSize]
	switch tag {
	case 1: // Sub
		for i := range rowSize {
			dest[i] = rs[i] + pngLeft(dest, i, bpp)
		}
	case 2: // Up
		for i := range rowSize {
			dest[i] = rs[i] + pngUp(last, i)
		}
	case 3: // Average
		for i := range rowSize {
			avg := (int(pngUp(last, i)) + int(pngLeft(dest, i, bpp))) / 2
			dest[i] = rs[i] + byte(avg)
		}
	case 4: // Paeth
		for i := range rowSize {
			dest[i] = rs[i] + paeth(pngLeft(dest, i, bpp), pngUp(last, i), pngUpperLeft(last, i, bpp))
		}
	default: // None (0) or unknown
		copy(dest[:rowSize], rs)
	}
}

func pngLeft(span []byte, i, bpp int) byte {
	if i >= bpp {
		return span[i-bpp]
	}
	return 0
}

func pngUp(span []byte, i int) byte {
	if len(span) == 0 || i >= len(span) {
		return 0
	}
	return span[i]
}

func pngUpperLeft(span []byte, i, bpp int) byte {
	if i >= bpp && len(span) > 0 && i-bpp < len(span) {
		return span[i-bpp]
	}
	return 0
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa := absInt(p - int(a))
	pb := absInt(p - int(b))
	pc := absInt(p - int(c))
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
