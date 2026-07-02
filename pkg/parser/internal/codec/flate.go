// Ported from FlateModule::FlateOrLZWDecode / FlateUncompress in
// core/fxcodec/flate/flatemodule.cpp and the CheckFlateDecodeParams /
// FlateOrLZWDecode wrapper in fpdf_parser_decode.cpp @ pdfium 0db284a42.
//
// Underneath we use Go's compress/zlib for the inflate; PDFium's manual zlib
// buffer-chaining is replaced with a streaming read. bytesConsumed is tracked
// via a byte-granular source reader so it matches PDFium's total_in for the
// no-trailing-garbage cases the corpus exercises.
package codec

import (
	"compress/zlib"
	"fmt"
	"io"
	"math"
	"sync"
)

type pooledZlibReader interface {
	io.ReadCloser
	zlib.Resetter
}

var (
	zlibReaderPool sync.Pool
	flateBufPool   = sync.Pool{
		New: func() any {
			return new([32 * 1024]byte)
		},
	}
)

// FlateOrLZWDecode decodes a Flate or LZW stream and applies the predictor.
// It returns (data, bytesConsumed); bytesConsumed == InvalidOffset on failure.
func FlateOrLZWDecode(useLZW bool, src []byte, earlyChange bool, predictor, colors, bpc, columns int, estimatedSize uint32) ([]byte, uint32) {
	if !checkFlateDecodeParams(colors, bpc, columns) {
		return []byte{}, InvalidOffset
	}

	var destBuf []byte
	bytesConsumed := InvalidOffset
	pt := getPredictor(predictor)

	if useLZW {
		d := newLZWDecoder(src, earlyChange)
		if !d.decode() {
			return []byte{}, InvalidOffset
		}
		destBuf = d.takeDestBuf()
		bytesConsumed = d.getSrcSize()
	} else {
		destBuf, bytesConsumed = flateUncompress(src, estimatedSize)
	}

	switch pt {
	case predNone:
		return destBuf, bytesConsumed
	case predPNG:
		result, ok := pngPredictor(colors, bpc, columns, destBuf)
		if !ok {
			return destBuf, InvalidOffset
		}
		return result, bytesConsumed
	case predTIFF:
		if tiffPredictor(colors, bpc, columns, destBuf) {
			return destBuf, bytesConsumed
		}
		return destBuf, InvalidOffset
	}
	return destBuf, bytesConsumed
}

// checkFlateDecodeParams rejects negative dimensions and an overflowing
// Columns*Colors*BitsPerComponent product (must be <= INT_MAX-7). The product
// is built with a per-multiply int32-overflow check, mirroring PDFium's
// FX_SAFE_INT32 — a single int64 expression would wrap for adversarial
// ~2^30 params and wrongly accept input PDFium rejects.
func checkFlateDecodeParams(colors, bpc, columns int) bool {
	if colors < 0 || bpc < 0 || columns < 0 {
		return false
	}
	prod := int64(columns) * int64(colors)
	if prod > math.MaxInt32 {
		return false
	}
	prod *= int64(bpc)
	if prod > math.MaxInt32 {
		return false
	}
	return prod <= int64(math.MaxInt32-7)
}

// byteSource is a source reader that counts bytes consumed and implements
// io.ByteReader, so compress/zlib reads it directly (no bufio read-ahead) and
// pos stays byte-accurate against PDFium's total_in.
type byteSource struct {
	src []byte
	pos int
}

func (b *byteSource) Read(p []byte) (int, error) {
	if b.pos >= len(b.src) {
		return 0, io.EOF
	}
	n := copy(p, b.src[b.pos:])
	b.pos += n
	return n, nil
}

func (b *byteSource) ReadByte() (byte, error) {
	if b.pos >= len(b.src) {
		return 0, io.EOF
	}
	c := b.src[b.pos]
	b.pos++
	return c, nil
}

// flateUncompress inflates a zlib stream, keeping any partial output produced
// before an error (bug-for-bug with PDFium, which retains partial inflate
// output). estimatedSize sizes the initial buffer only.
func flateUncompress(src []byte, estimatedSize uint32) ([]byte, uint32) {
	source := &byteSource{src: src}
	zr, err := acquireZlibReader(source)
	if err != nil {
		// Bad zlib header: PDFium's inflate fails after consuming the header.
		return []byte{}, uint32(source.pos)
	}
	defer releaseZlibReader(zr)

	out := make([]byte, 0, estimateFlateBufferSize(estimatedSize, len(src)))
	bufp := flateBufPool.Get().(*[32 * 1024]byte)
	defer flateBufPool.Put(bufp)
	buf := bufp[:]
	for {
		n, rerr := zr.Read(buf)
		out = append(out, buf[:n]...)
		if len(out) >= maxTotalOutSize {
			out = out[:maxTotalOutSize]
			break
		}
		if rerr != nil {
			break // EOF or corrupt: keep the partial output already produced
		}
	}
	return out, uint32(source.pos)
}

func acquireZlibReader(source *byteSource) (pooledZlibReader, error) {
	if pooled := zlibReaderPool.Get(); pooled != nil {
		zr := pooled.(pooledZlibReader)
		if err := zr.Reset(source, nil); err != nil {
			zlibReaderPool.Put(zr)
			return nil, err
		}
		return zr, nil
	}
	rc, err := zlib.NewReader(source)
	if err != nil {
		return nil, err
	}
	zr, ok := rc.(pooledZlibReader)
	if !ok {
		_ = rc.Close()
		return nil, fmt.Errorf("zlib reader does not implement Reset")
	}
	return zr, nil
}

func releaseZlibReader(zr pooledZlibReader) {
	_ = zr.Close()
	zlibReaderPool.Put(zr)
}

// estimateFlateBufferSize mirrors EstimateFlateUncompressBufferSize: capped at
// 10 MB so a bogus /Length hint cannot force a huge initial allocation.
func estimateFlateBufferSize(origSize uint32, srcSize int) int {
	guess := uint64(origSize)
	if origSize == 0 {
		guess = uint64(srcSize) * 2
	}
	if guess > maxInitialAllocSize {
		guess = maxInitialAllocSize
	}
	return int(guess)
}
