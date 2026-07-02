// Ported from A85Decode/HexDecode/RunLengthDecode in
// core/fpdfapi/parser/fpdf_parser_decode.cpp @ pdfium 0db284a42.
//
// Each returns (data, bytesConsumed); bytesConsumed == InvalidOffset signals a
// decode failure (so StreamAcc falls back to raw). The malformed-input
// tolerance (whitespace skipping, odd final nibble, truncated runs zero-filled)
// is preserved bug-for-bug.
package codec

import "math"

// A85Decode decodes an ASCII85 stream.
func A85Decode(src []byte) ([]byte, uint32) {
	if len(src) == 0 {
		return []byte{}, 0
	}

	// Count legal characters and 'z' run-of-zero markers.
	var zcount, pos uint32
	for pos < uint32(len(src)) {
		ch := src[pos]
		if ch == 'z' {
			zcount++
		} else if (ch < '!' || ch > 'u') && !isLineEnding(ch) && ch != ' ' && ch != '\t' {
			break
		}
		pos++
	}
	if pos == 0 {
		return []byte{}, 0
	}

	// Encoding ratio of ASCII85 is 4:5.
	spaceForNonZeroes := (pos-zcount)/5*4 + 4
	size := uint64(zcount)*4 + uint64(spaceForNonZeroes)
	if size > math.MaxUint32 {
		return []byte{}, InvalidOffset
	}

	dest := make([]byte, size)
	di := 0
	state := 0
	var res uint32
	pos = 0
	for pos < uint32(len(src)) {
		ch := src[pos]
		pos++
		if isLineEnding(ch) || ch == ' ' || ch == '\t' {
			continue
		}
		if ch == 'z' {
			dest[di], dest[di+1], dest[di+2], dest[di+3] = 0, 0, 0, 0
			di += 4
			state = 0
			res = 0
			continue
		}
		if ch < '!' || ch > 'u' {
			break
		}
		res = res*85 + uint32(ch) - 33
		if state < 4 {
			state++
			continue
		}
		for i := range 4 {
			dest[di] = a85Result(res, i)
			di++
		}
		state = 0
		res = 0
	}
	// Flush a partial final group, padding with the maximum digit ('u').
	if state != 0 {
		for i := state; i < 5; i++ {
			res = res*85 + 84
		}
		for i := 0; i < state-1; i++ {
			dest[di] = a85Result(res, i)
			di++
		}
	}
	if pos < uint32(len(src)) && src[pos] == '>' {
		pos++
	}
	return dest[:di], pos
}

// HexDecode decodes an ASCIIHex stream, tolerating whitespace and an odd final
// nibble.
func HexDecode(src []byte) ([]byte, uint32) {
	if len(src) == 0 {
		return []byte{}, 0
	}

	end := 0
	for end < len(src) && src[end] != '>' {
		end++
	}

	dest := make([]byte, end/2+1)
	di := 0
	isFirst := true
	i := 0
	for ; i < len(src); i++ {
		ch := src[i]
		if isLineEnding(ch) || ch == ' ' || ch == '\t' {
			continue
		}
		if ch == '>' {
			i++
			break
		}
		if !isHexDigit(ch) {
			continue
		}
		digit := hexCharToInt(ch)
		if isFirst {
			dest[di] = byte(digit * 16)
		} else {
			dest[di] += byte(digit)
			di++
		}
		isFirst = !isFirst
	}
	destSize := di
	if !isFirst {
		destSize++ // dangling high nibble
	}
	return dest[:destSize], uint32(i)
}

// RunLengthDecode decodes a RunLength stream, with the 20 MB output cap and
// truncated-run tolerance (missing bytes zero-filled).
func RunLengthDecode(src []byte) ([]byte, uint32) {
	var destSize uint32
	i := 0
	for i < len(src) {
		if src[i] == 128 {
			break
		}
		old := destSize
		if src[i] < 128 {
			destSize += uint32(src[i]) + 1
			if destSize < old {
				return []byte{}, InvalidOffset
			}
			i += int(src[i]) + 2
		} else {
			destSize += 257 - uint32(src[i])
			if destSize < old {
				return []byte{}, InvalidOffset
			}
			i += 2
		}
	}
	if destSize >= maxStreamSize {
		return []byte{}, InvalidOffset
	}

	dest := make([]byte, destSize) // zero-initialised; covers truncated-run fill
	i = 0
	destCount := 0
	for i < len(src) {
		if src[i] == 128 {
			break
		}
		if src[i] < 128 {
			copyLen := int(src[i]) + 1
			bufLeft := len(src) - i - 1
			if bufLeft < copyLen {
				copyLen = bufLeft // remaining bytes already zero in dest
			}
			copy(dest[destCount:], src[i+1:i+1+copyLen])
			destCount += int(src[i]) + 1
			i += int(src[i]) + 2
		} else {
			var fill byte
			if i+1 < len(src) {
				fill = src[i+1]
			}
			fillSize := 257 - int(src[i])
			for k := range fillSize {
				dest[destCount+k] = fill
			}
			destCount += fillSize
			i += 2
		}
	}
	consumed := min(i+1, len(src))
	return dest, uint32(consumed)
}
