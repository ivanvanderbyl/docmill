// Ported from core/fpdfapi/parser/cpdf_linearized_header.{h,cpp} @ pdfium
// 0db284a42.
package parser

import (
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/syntax"
)

const kLinearizedHeaderOffset = 9
const maxInt32 = int64(2147483647)

// LinearizedHeader is the parsed /Linearized dictionary.
type LinearizedHeader struct {
	fileSize                      int64
	firstPageNo                   uint32
	mainXRefTableFirstEntryOffset int64
	pageCount                     uint32
	firstPageEndOffset            int64
	firstPageObjNum               uint32
	lastXRefOffset                int64
	hintStart                     int64
	hintLength                    uint32
}

// GetFileSize returns /L.
func (h *LinearizedHeader) GetFileSize() int64 { return h.fileSize }

// GetFirstPageNo returns /P.
func (h *LinearizedHeader) GetFirstPageNo() uint32 { return h.firstPageNo }

// GetPageCount returns /N.
func (h *LinearizedHeader) GetPageCount() uint32 { return h.pageCount }

// GetFirstPageObjNum returns /O.
func (h *LinearizedHeader) GetFirstPageObjNum() uint32 { return h.firstPageObjNum }

// GetLastXRefOffset returns the position just past the linearization dict.
func (h *LinearizedHeader) GetLastXRefOffset() int64 { return h.lastXRefOffset }

func validNumInt64(dict *objects.Dictionary, key string, min int64, mustExist bool) bool {
	if !dict.KeyExist(key) {
		return !mustExist
	}
	num := dict.GetNumberFor(key)
	if num == nil || !num.IsInteger() {
		return false
	}
	return int64(num.GetInteger()) >= min
}

func validNumUint32(dict *objects.Dictionary, key string, min uint32, mustExist bool) bool {
	if !dict.KeyExist(key) {
		return !mustExist
	}
	num := dict.GetNumberFor(key)
	if num == nil || !num.IsInteger() {
		return false
	}
	raw := num.GetInteger()
	if raw < 0 || int64(raw) > int64(^uint32(0)) {
		return false
	}
	return uint32(raw) >= min
}

// ParseLinearizedHeader parses the first indirect object as a linearization
// header, or returns nil (not linearized / invalid).
func ParseLinearizedHeader(sp *syntax.SyntaxParser, docSize int) *LinearizedHeader {
	sp.SetPos(kLinearizedHeaderOffset)
	dict := objects.ToDictionary(sp.GetIndirectObject(nil))
	if dict == nil || !dict.KeyExist("Linearized") {
		return nil
	}
	if !validNumInt64(dict, "L", 1, true) || !validNumUint32(dict, "P", 0, false) ||
		!validNumInt64(dict, "T", 1, true) || !validNumUint32(dict, "N", 1, true) ||
		!validNumInt64(dict, "E", 1, true) || !validNumUint32(dict, "O", 1, true) {
		return nil
	}
	if w, _ := sp.GetNextWord(); w != "endobj" {
		return nil
	}
	h := &LinearizedHeader{
		fileSize:                      int64(dict.GetIntegerFor("L")),
		firstPageNo:                   uint32(dict.GetIntegerFor("P")),
		mainXRefTableFirstEntryOffset: int64(dict.GetIntegerFor("T")),
		pageCount:                     uint32(dict.GetIntegerFor("N")),
		firstPageEndOffset:            int64(dict.GetIntegerFor("E")),
		firstPageObjNum:               uint32(dict.GetIntegerFor("O")),
		lastXRefOffset:                int64(sp.GetPos()),
	}
	if hint := dict.GetArrayFor("H"); hint != nil && (hint.Len() == 2 || hint.Len() == 4) {
		hs := max(hint.GetIntegerAt(0), 0)
		h.hintStart = int64(hs)
		hl := hint.GetIntegerAt(1)
		if hl >= 0 && int64(hl) <= int64(^uint32(0)) {
			h.hintLength = uint32(hl)
		}
	}
	if !isLinearizedHeaderValid(h, int64(docSize)) {
		return nil
	}
	return h
}

// isLinearizedHeaderValid enforces the /L-vs-file-length and bounds checks.
func isLinearizedHeaderValid(h *LinearizedHeader, docSize int64) bool {
	return h.fileSize == docSize &&
		int64(h.firstPageNo) < maxInt32 &&
		h.firstPageNo < h.pageCount &&
		h.mainXRefTableFirstEntryOffset < docSize &&
		h.firstPageEndOffset < docSize &&
		h.firstPageObjNum < kMaxObjectNumber &&
		h.lastXRefOffset < docSize &&
		h.hintStart < docSize
}
