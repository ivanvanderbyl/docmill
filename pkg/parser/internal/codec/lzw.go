// Ported from CLZWDecoder in core/fxcodec/flate/flatemodule.cpp @ pdfium
// 0db284a42. This is PDF's variable-width, MSB-first LZW with the EarlyChange
// parameter — Go's compress/lzw cannot express early-change=0 nor PDFium's exact
// dictionary caps, so it is hand-rolled. Corrupt-input guards and the fixed
// 4000-byte decode stack / 5021-entry table are preserved bug-for-bug.
package codec

const (
	lzwDecodeStackSize = 4000
	lzwMaxCodes        = 5021
	lzwNoCode          = uint32(0xFFFFFFFF)
)

type lzwDecoder struct {
	src         []byte
	decodeStack [lzwDecodeStackSize]byte
	earlyChange uint32 // 0 or 1
	codes       [lzwMaxCodes]uint32
	srcBitPos   int
	destBytePos int
	stackLen    int
	codeLen     int
	currentCode uint32
	destBuf     []byte
}

func newLZWDecoder(src []byte, earlyChange bool) *lzwDecoder {
	d := &lzwDecoder{src: src, codeLen: 9}
	if earlyChange {
		d.earlyChange = 1
	}
	return d
}

func (d *lzwDecoder) getSrcSize() uint32 { return uint32((d.srcBitPos + 7) / 8) }

func (d *lzwDecoder) takeDestBuf() []byte { return d.destBuf[:d.destBytePos] }

func (d *lzwDecoder) addCode(prefixCode, appendChar uint32) {
	if d.currentCode+d.earlyChange == 4094 {
		return
	}
	d.codes[d.currentCode] = (prefixCode << 16) | appendChar
	d.currentCode++
	switch d.currentCode + d.earlyChange {
	case 512 - 258:
		d.codeLen = 10
	case 1024 - 258:
		d.codeLen = 11
	case 2048 - 258:
		d.codeLen = 12
	}
}

// decodeString walks the prefix chain for code onto decodeStack (in reverse).
func (d *lzwDecoder) decodeString(code uint32) {
	for {
		index := int(code) - 258
		if index < 0 || uint32(index) >= d.currentCode {
			break
		}
		data := d.codes[index]
		if d.stackLen >= len(d.decodeStack) {
			return
		}
		d.decodeStack[d.stackLen] = byte(data)
		d.stackLen++
		code = data >> 16
	}
	if d.stackLen >= len(d.decodeStack) {
		return
	}
	d.decodeStack[d.stackLen] = byte(code)
	d.stackLen++
}

func (d *lzwDecoder) expandDestBuf(additionalSize int) bool {
	newSize := max(additionalSize, len(d.destBuf)/2)
	total := newSize + len(d.destBuf)
	if total < newSize { // overflow
		d.destBuf = nil
		return false
	}
	grown := make([]byte, total)
	copy(grown, d.destBuf)
	d.destBuf = grown
	return true
}

// decode runs the LZW main loop; returns true iff it produced any output.
func (d *lzwDecoder) decode() bool {
	oldCode := lzwNoCode
	var lastChar byte
	d.destBuf = make([]byte, 512)

	for {
		if d.srcBitPos+d.codeLen > len(d.src)*8 {
			break
		}

		// MSB-first variable-width read of codeLen bits.
		bytePos := d.srcBitPos / 8
		bitPos := d.srcBitPos % 8
		bitLeft := d.codeLen
		var code uint32
		if bitPos != 0 {
			bitLeft -= 8 - bitPos
			code = uint32(d.src[bytePos]&byte((1<<(8-bitPos))-1)) << uint(bitLeft)
			bytePos++
		}
		if bitLeft < 8 {
			code |= uint32(d.src[bytePos] >> uint(8-bitLeft))
		} else {
			bitLeft -= 8
			code |= uint32(d.src[bytePos]) << uint(bitLeft)
			bytePos++
			if bitLeft != 0 {
				code |= uint32(d.src[bytePos] >> uint(8-bitLeft))
			}
		}
		d.srcBitPos += d.codeLen

		if code < 256 { // literal
			if d.destBytePos >= len(d.destBuf) {
				if !d.expandDestBuf(d.destBytePos - len(d.destBuf) + 1) {
					return false
				}
			}
			d.destBuf[d.destBytePos] = byte(code)
			d.destBytePos++
			lastChar = byte(code)
			if oldCode != lzwNoCode {
				d.addCode(oldCode, uint32(lastChar))
			}
			oldCode = code
			continue
		}

		if code == 256 { // clear table
			d.codeLen = 9
			d.currentCode = 0
			oldCode = lzwNoCode
			continue
		}

		if code == 257 { // EOD
			break
		}

		// code >= 258
		if oldCode == lzwNoCode {
			return false // first real code cannot be a dictionary reference
		}

		d.stackLen = 0
		if code-258 >= d.currentCode { // KwKwK: code not yet in table
			if d.stackLen < len(d.decodeStack) {
				d.decodeStack[d.stackLen] = lastChar
				d.stackLen++
			}
			d.decodeString(oldCode)
		} else {
			d.decodeString(code)
		}

		requiredSize := d.destBytePos + d.stackLen
		if requiredSize > len(d.destBuf) {
			if !d.expandDestBuf(requiredSize - len(d.destBuf)) {
				return false
			}
		}
		for i := 0; i < d.stackLen; i++ {
			d.destBuf[d.destBytePos+i] = d.decodeStack[d.stackLen-i-1]
		}
		d.destBytePos += d.stackLen
		lastChar = d.decodeStack[d.stackLen-1]

		if oldCode >= 258 && oldCode-258 >= d.currentCode {
			break // corrupt state
		}
		d.addCode(oldCode, uint32(lastChar))
		oldCode = code
	}
	return d.destBytePos != 0
}
