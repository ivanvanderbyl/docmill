// Ported from core/fpdfapi/parser/cpdf_parser.{h,cpp} and
// cpdf_indirect_object_holder.{h,cpp} @ pdfium 0db284a42.
package parser

import (
	"bytes"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/syntax"
)

// Error mirrors CPDF_Parser::Error.
type Error int

const (
	Success Error = iota
	FileError
	FormatError
	PasswordError
	HandlerError
)

// ParsedObjectsHolder is the holder the parser drives (CPDF_Document /
// ParsedObjectsHolder in C++): an indirect-object resolver plus TryInit.
type ParsedObjectsHolder interface {
	objects.IndirectObjectHolder
	TryInit() bool
}

// Parser ports CPDF_Parser.
type Parser struct {
	syntax           *syntax.SyntaxParser
	objectsHolder    ParsedObjectsHolder
	hasParsed        bool
	xrefStream       bool
	xrefTableRebuilt bool
	fileVersion      int
	crossRefTable    *CrossRefTable
	lastXRefOffset   int64
	linearized       *LinearizedHeader
	objectStreamMap  map[uint32]*ObjectStream
	parsingObjNums   map[uint32]struct{}
}

// New returns an empty parser.
func New() *Parser {
	return &Parser{
		crossRefTable:   NewCrossRefTable(),
		objectStreamMap: map[uint32]*ObjectStream{},
		parsingObjNums:  map[uint32]struct{}{},
	}
}

// XRefTableRebuilt reports whether the xref had to be reconstructed.
func (p *Parser) XRefTableRebuilt() bool { return p.xrefTableRebuilt }

// GetLinearizedHeader returns the linearization header, or nil.
func (p *Parser) GetLinearizedHeader() *LinearizedHeader { return p.linearized }

// FileVersion returns the parsed PDF header version encoded as major*10+minor.
func (p *Parser) FileVersion() int { return p.fileVersion }

// --- accessors ---

// GetLastObjNum returns the highest object number in the table (0 if empty).
func (p *Parser) GetLastObjNum() uint32 {
	if p.crossRefTable.Empty() {
		return 0
	}
	return p.crossRefTable.LastKey()
}

// IsValidObjectNumber reports whether objnum is within the table's range.
func (p *Parser) IsValidObjectNumber(objnum uint32) bool { return objnum <= p.GetLastObjNum() }

// GetTrailer returns the trailer dictionary.
func (p *Parser) GetTrailer() *objects.Dictionary { return p.crossRefTable.Trailer() }

// GetRootObjNum returns the /Root object number, or kInvalidObjNum.
func (p *Parser) GetRootObjNum() uint32 {
	tr := p.GetTrailer()
	if tr == nil {
		return kInvalidObjNum
	}
	if ref := objects.ToReference(tr.GetObjectFor("Root")); ref != nil {
		return ref.GetRefObjNum()
	}
	return kInvalidObjNum
}

// GetRoot resolves the document catalog dictionary.
func (p *Parser) GetRoot() *objects.Dictionary {
	obj := p.objectsHolder.GetOrParseIndirectObject(p.GetRootObjNum())
	if obj == nil {
		return nil
	}
	return obj.GetDict()
}

func (p *Parser) getEncryptDict() *objects.Dictionary {
	tr := p.GetTrailer()
	if tr == nil {
		return nil
	}
	obj := tr.GetObjectFor("Encrypt")
	if obj == nil {
		return nil
	}
	if d := objects.ToDictionary(obj); d != nil {
		return d
	}
	if ref := objects.ToReference(obj); ref != nil {
		return objects.ToDictionary(p.objectsHolder.GetOrParseIndirectObject(ref.GetRefObjNum()))
	}
	return nil
}

// setEncryptHandler is a Phase-D stub: encryption is unsupported until Phase E,
// so an /Encrypt entry is a hard failure (no corpus PDF is encrypted).
func (p *Parser) setEncryptHandler() Error {
	if p.GetTrailer() == nil {
		return FormatError
	}
	if p.getEncryptDict() != nil {
		return HandlerError
	}
	return Success
}

// --- init / version ---

// getHeaderOffset finds "%PDF" within the first 1024 bytes.
func getHeaderOffset(buf []byte) (int, bool) {
	for off := 0; off <= 1024; off++ {
		if off+4 > len(buf) {
			return 0, false
		}
		if bytes.Equal(buf[off:off+4], []byte("%PDF")) {
			return off, true
		}
	}
	return 0, false
}

func (p *Parser) initSyntaxParser(buf []byte) bool {
	off, ok := getHeaderOffset(buf)
	if !ok {
		return false
	}
	if int64(len(buf)) < int64(off)+kPDFHeaderSize {
		return false
	}
	p.syntax = syntax.NewWithOffset(buf, off)
	return p.parseFileVersion()
}

func (p *Parser) parseFileVersion() bool {
	p.fileVersion = 0
	ch5, ok := p.syntax.CharAt(5)
	if !ok {
		return false
	}
	if ch5 >= '0' && ch5 <= '9' {
		p.fileVersion = int(ch5-'0') * 10
	}
	ch7, ok := p.syntax.CharAt(7)
	if !ok {
		return false
	}
	if ch7 >= '0' && ch7 <= '9' {
		p.fileVersion += int(ch7 - '0')
	}
	return true
}

// --- top-level parse ---

// StartParse opens buf using holder as the indirect-object resolver.
func (p *Parser) StartParse(buf []byte, holder ParsedObjectsHolder) Error {
	p.objectsHolder = holder
	if !p.initSyntaxParser(buf) {
		return FormatError
	}
	return p.startParseInternal()
}

// StartLinearizedParse opens a (possibly) linearized buf.
func (p *Parser) StartLinearizedParse(buf []byte, holder ParsedObjectsHolder) Error {
	p.objectsHolder = holder
	p.xrefStream = false
	p.lastXRefOffset = 0
	if !p.initSyntaxParser(buf) {
		return FormatError
	}
	p.linearized = ParseLinearizedHeader(p.syntax, p.syntax.GetDocumentSize())
	if p.linearized == nil {
		return p.startParseInternal()
	}
	p.hasParsed = true
	p.lastXRefOffset = p.linearized.GetLastXRefOffset()
	first := p.lastXRefOffset
	loadedTable := p.loadCrossRefTable(first, false)
	if !loadedTable && !p.loadCrossRefStream(&first, true) {
		if !p.rebuildCrossRef() {
			return FormatError
		}
		p.xrefTableRebuilt = true
		p.lastXRefOffset = 0
	}
	if loadedTable {
		trailer := p.loadTrailer()
		if trailer == nil {
			return Success
		}
		p.crossRefTable.SetTrailer(trailer, kNoTrailerObjectNumber)
		xrefsize := p.GetTrailer().GetDirectIntegerFor("Size")
		if xrefsize > 0 {
			expectedLast := uint32(xrefsize) - 1
			if p.GetLastObjNum() != expectedLast && !p.rebuildCrossRef() {
				return FormatError
			}
		}
	}
	if eRet := p.finishParse(); eRet != Success {
		return eRet
	}
	return Success
}

func (p *Parser) startParseInternal() Error {
	p.hasParsed = true
	p.xrefStream = false
	p.lastXRefOffset = p.parseStartXRef()
	if p.lastXRefOffset >= kPDFHeaderSize {
		if !p.loadAllCrossRefTablesAndStreams(p.lastXRefOffset) {
			if !p.rebuildCrossRef() {
				return FormatError
			}
			p.xrefTableRebuilt = true
			p.lastXRefOffset = 0
		}
	} else {
		if !p.rebuildCrossRef() {
			return FormatError
		}
		p.xrefTableRebuilt = true
	}
	return p.finishParse()
}

// finishParse runs the encrypt-handler + Root validation with rebuild fallbacks
// shared by StartParseInternal and StartLinearizedParse.
func (p *Parser) finishParse() Error {
	if eRet := p.setEncryptHandler(); eRet != Success {
		return eRet
	}
	if p.GetRoot() == nil || !p.objectsHolder.TryInit() {
		if p.xrefTableRebuilt {
			return FormatError
		}
		if !p.rebuildCrossRef() {
			return FormatError
		}
		if eRet := p.setEncryptHandler(); eRet != Success {
			return eRet
		}
		p.objectsHolder.TryInit()
		if p.GetRoot() == nil {
			return FormatError
		}
	}
	if p.GetRootObjNum() == kInvalidObjNum {
		if !p.rebuildCrossRef() || p.GetRootObjNum() == kInvalidObjNum {
			return FormatError
		}
		if eRet := p.setEncryptHandler(); eRet != Success {
			return eRet
		}
	}
	return Success
}

func (p *Parser) parseStartXRef() int64 {
	const kw = "startxref"
	p.syntax.SetPos(p.syntax.GetDocumentSize() - len(kw))
	if !p.syntax.BackwardsSearchToWord(kw, 4096) {
		return 0
	}
	p.syntax.GetKeyword() // skip "startxref"
	word, isNum := p.syntax.GetNextWord()
	if !isNum || word == "" {
		return 0
	}
	v, ok := syntax.Atoi64(word)
	if !ok || v >= int64(p.syntax.GetDocumentSize()) {
		return 0
	}
	return v
}

func (p *Parser) loadTrailer() *objects.Dictionary {
	if p.syntax.GetKeyword() != "trailer" {
		return nil
	}
	return objects.ToDictionary(p.syntax.GetObjectBody(p.objectsHolder))
}
