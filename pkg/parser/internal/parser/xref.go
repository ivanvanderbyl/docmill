// Ported from the xref-loading + indirect-object resolution + RebuildCrossRef
// machinery of core/fpdfapi/parser/cpdf_parser.cpp and the indirect-object
// holder from cpdf_indirect_object_holder.cpp @ pdfium 0db284a42.
package parser

import (
	"sync"
	"sync/atomic"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/syntax"
)

// Holder is the default indirect-object resolver (CPDF_IndirectObjectHolder):
// it memoizes parsed objects and serializes lazy parser misses.
type Holder struct {
	parseMu    sync.Mutex
	parser     *Parser
	objs       sync.Map
	lastObjNum atomic.Uint32
}

// NewHolder returns a holder delegating to p.
func NewHolder(p *Parser) *Holder {
	return &Holder{parser: p}
}

// GetOrParseIndirectObject returns the cached object or parses it, guarding
// re-entrancy by inserting a nil placeholder before parsing.
func (h *Holder) GetOrParseIndirectObject(objnum uint32) objects.Object {
	if objnum == 0 || objnum == kInvalidObjNum {
		return nil
	}
	if existing, present := h.objs.Load(objnum); present {
		return cachedObject(existing)
	}

	h.parseMu.Lock()
	defer h.parseMu.Unlock()

	if existing, present := h.objs.Load(objnum); present {
		return cachedObject(existing)
	}

	obj := h.parser.ParseIndirectObject(objnum)
	if obj == nil {
		return nil
	}
	obj.SetObjNum(objnum)
	for {
		last := h.lastObjNum.Load()
		if objnum <= last || h.lastObjNum.CompareAndSwap(last, objnum) {
			break
		}
	}
	h.objs.Store(objnum, obj)
	return obj
}

func cachedObject(value any) objects.Object {
	obj, ok := value.(objects.Object)
	if !ok || obj == nil || obj.GetObjNum() == kInvalidObjNum {
		return nil
	}
	return obj
}

// TryInit is the stub holder's no-op success (the Document overrides it).
func (h *Holder) TryInit() bool { return true }

// SetLastObjNum records the highest object number.
func (h *Holder) SetLastObjNum(n uint32) {
	h.lastObjNum.Store(n)
}

// GetLastObjNum returns the highest object number seen.
func (h *Holder) GetLastObjNum() uint32 {
	return h.lastObjNum.Load()
}

// Parser returns the underlying parser (used by the document layer).
func (h *Holder) Parser() *Parser { return h.parser }

// --- xref walk ---

type crossRefObjData struct {
	objNum uint32
	info   ObjectInfo
}

func (p *Parser) loadAllCrossRefTablesAndStreams(xrefOffset int64) bool {
	isXRefStream := !p.loadCrossRefTable(xrefOffset, true)
	if isXRefStream {
		cpy := xrefOffset
		if !p.loadCrossRefStream(&cpy, true) {
			return false
		}
	} else {
		trailer := p.loadTrailer()
		if trailer == nil {
			return false
		}
		p.crossRefTable.SetTrailer(trailer, kNoTrailerObjectNumber)
		xrefsize := p.GetTrailer().GetDirectIntegerFor("Size")
		if xrefsize > 0 && xrefsize <= kMaxXRefSize {
			p.crossRefTable.SetObjectMapSize(uint32(xrefsize))
		}
	}

	var xrefList, xrefStreamList []int64
	if isXRefStream {
		xrefList = []int64{0}
		xrefStreamList = []int64{xrefOffset}
	} else {
		xrefList = []int64{xrefOffset}
		xrefStreamList = []int64{int64(p.GetTrailer().GetDirectIntegerFor("XRefStm"))}
	}
	if !p.findAllCrossReferenceTablesAndStream(xrefOffset, &xrefList, &xrefStreamList) {
		return false
	}
	if xrefList[0] > 0 {
		if !p.loadCrossRefTable(xrefList[0], false) {
			return false
		}
		if !p.verifyCrossRefTable() {
			return false
		}
	}
	for i := 1; i < len(xrefList); i++ {
		if xrefStreamList[i] > 0 {
			pos := xrefStreamList[i]
			if !p.loadCrossRefStream(&pos, false) {
				return false
			}
		}
		if xrefList[i] > 0 {
			if !p.loadCrossRefTable(xrefList[i], false) {
				return false
			}
		}
	}
	if isXRefStream {
		p.objectStreamMap = map[uint32]*ObjectStream{}
		p.xrefStream = true
	}
	return true
}

func prependInt64(s []int64, v int64) []int64 { return append([]int64{v}, s...) }

func (p *Parser) findAllCrossReferenceTablesAndStream(mainXrefOffset int64, xrefList, xrefStreamList *[]int64) bool {
	seen := map[int64]struct{}{mainXrefOffset: {}}
	xrefOffset := int64(p.GetTrailer().GetDirectIntegerFor("Prev"))
	for xrefOffset > 0 {
		if _, ok := seen[xrefOffset]; ok {
			return false // circular /Prev
		}
		seen[xrefOffset] = struct{}{}
		cpy := xrefOffset
		if p.loadCrossRefStream(&cpy, false) {
			*xrefList = prependInt64(*xrefList, 0)
			*xrefStreamList = prependInt64(*xrefStreamList, xrefOffset)
			xrefOffset = cpy
		} else {
			p.loadCrossRefTable(xrefOffset, true)
			trailerDict := p.loadTrailer()
			if trailerDict == nil {
				return false
			}
			*xrefList = prependInt64(*xrefList, xrefOffset)
			*xrefStreamList = prependInt64(*xrefStreamList, int64(trailerDict.GetIntegerFor("XRefStm")))
			xrefOffset = int64(trailerDict.GetDirectIntegerFor("Prev"))
			p.crossRefTable = MergeUp(NewCrossRefTableWithTrailer(trailerDict, kNoTrailerObjectNumber), p.crossRefTable)
		}
	}
	return true
}

func (p *Parser) loadCrossRefTable(pos int64, skip bool) bool {
	p.syntax.SetPos(int(pos))
	var objs []crossRefObjData
	var out *[]crossRefObjData
	if !skip {
		out = &objs
	}
	if !p.parseCrossRefTable(out) {
		return false
	}
	p.mergeCrossRefObjectsData(objs)
	return true
}

func (p *Parser) parseCrossRefTable(out *[]crossRefObjData) bool {
	if out != nil {
		*out = nil
	}
	if p.syntax.GetKeyword() != "xref" {
		return false
	}
	var result []crossRefObjData
	for {
		savedPos := p.syntax.GetPos()
		word, isNum := p.syntax.GetNextWord()
		if word == "" {
			return false
		}
		if !isNum {
			p.syntax.SetPos(savedPos)
			break
		}
		startObjNum := syntax.Atoui(word)
		if startObjNum >= kMaxObjectNumber {
			return false
		}
		count := p.syntax.GetDirectNum()
		p.syntax.ToNextWord()
		var sink *[]crossRefObjData
		if out != nil {
			sink = &result
		}
		if !p.parseAndAppendCrossRefSubsectionData(startObjNum, count, sink) {
			return false
		}
	}
	if out != nil {
		*out = result
	}
	return true
}

func (p *Parser) parseAndAppendCrossRefSubsectionData(startObjNum, count uint32, out *[]crossRefObjData) bool {
	if count == 0 {
		return true
	}
	if out == nil {
		adv := int64(count) * kEntrySize
		if adv < 0 {
			return false
		}
		p.syntax.SetPos(p.syntax.GetPos() + int(adv))
		return true
	}
	startObjIndex := len(*out)
	newSize := int64(startObjIndex) + int64(count)
	if newSize < 0 || newSize > int64(kMaxXRefSize) {
		return false
	}
	maxEntriesInFile := p.syntax.GetDocumentSize() / kEntrySize
	if newSize > int64(maxEntriesInFile) {
		return false
	}
	for len(*out) < int(newSize) {
		*out = append(*out, crossRefObjData{})
	}
	entriesToRead := int(count)
	for entriesToRead > 0 {
		entriesInBlock := min(entriesToRead, 1024)
		block := make([]byte, entriesInBlock*kEntrySize)
		if !p.syntax.ReadBlock(block) {
			return false
		}
		for i := range entriesInBlock {
			objIndex := int(count) - entriesToRead + i
			objnum := startObjNum + uint32(objIndex)
			entry := block[i*kEntrySize : i*kEntrySize+kEntrySize]
			var info ObjectInfo
			if entry[17] == 'f' {
				info.Pos = 0
				info.Type = ObjectTypeFree
			} else {
				offset, ok := syntax.Atoi64(string(entry[:10]))
				if !ok {
					return false
				}
				if offset == 0 {
					for c := range 10 {
						if entry[c] < '0' || entry[c] > '9' {
							return false
						}
					}
				}
				info.Pos = offset
				info.GenNum = uint16(stringToInt(entry[11:]))
				info.Type = ObjectTypeNormal
			}
			(*out)[startObjIndex+objIndex] = crossRefObjData{objNum: objnum, info: info}
		}
		entriesToRead -= entriesInBlock
	}
	return true
}

func (p *Parser) mergeCrossRefObjectsData(objs []crossRefObjData) {
	for _, o := range objs {
		switch o.info.Type {
		case ObjectTypeFree:
			if o.info.GenNum > 0 {
				p.crossRefTable.SetFree(o.objNum, o.info.GenNum)
			}
		case ObjectTypeNormal:
			p.crossRefTable.AddNormal(o.objNum, o.info.GenNum, o.info.IsObjectStream, o.info.Pos)
		case ObjectTypeCompressed:
			p.crossRefTable.AddCompressed(o.objNum, o.info.ArchiveObjNum, o.info.ArchiveObjIndex)
		}
	}
}

// --- xref streams ---

type indexEntry struct {
	startObjNum uint32
	objCount    uint32
}

func (p *Parser) loadCrossRefStream(pos *int64, isMainXref bool) bool {
	stream := objects.ToStream(p.parseIndirectObjectAt(*pos, 0))
	if stream == nil || stream.GetObjNum() == 0 {
		return false
	}
	dict := stream.Dict()
	prev := dict.GetIntegerFor("Prev")
	if prev < 0 {
		return false
	}
	size := dict.GetIntegerFor("Size")
	if size < 0 {
		return false
	}
	*pos = int64(prev)
	newCRT := NewCrossRefTableWithTrailer(objects.ToDictionary(dict.Clone()), stream.GetObjNum())
	if isMainXref {
		p.crossRefTable = newCRT
		p.crossRefTable.SetObjectMapSize(uint32(size))
	} else {
		p.crossRefTable = MergeUp(newCRT, p.crossRefTable)
	}

	indices := getCrossRefStreamIndices(dict.GetArrayFor("Index"), uint32(size))
	fieldWidths := getFieldWidths(dict.GetArrayFor("W"))
	if len(fieldWidths) < kMinFieldCount {
		return false
	}
	var totalWidth uint32
	for _, w := range fieldWidths {
		if totalWidth > ^uint32(0)-w {
			return false
		}
		totalWidth += w
	}

	acc := objects.NewStreamAcc(stream)
	acc.LoadAllDataFiltered()
	data := acc.GetSpan()

	var segindex uint32
	for _, idx := range indices {
		segObjs := uint64(segindex) + uint64(idx.objCount)
		segEnd := segObjs * uint64(totalWidth)
		if segEnd > uint64(len(data)) {
			continue
		}
		segStart := uint64(segindex) * uint64(totalWidth)
		seg := data[segStart:segEnd]
		safeNewSize := uint64(idx.startObjNum) + uint64(idx.objCount)
		if safeNewSize > uint64(^uint32(0)) {
			continue
		}
		var currentSize uint32
		if !p.crossRefTable.Empty() {
			currentSize = p.GetLastObjNum() + 1
		}
		newSz := uint32(safeNewSize)
		if int(newSz) > kMaxXRefSize {
			newSz = uint32(kMaxXRefSize)
		}
		if newSz > currentSize {
			p.crossRefTable.SetObjectMapSize(newSz)
		}
		for i := uint32(0); i < idx.objCount; i++ {
			objnum := idx.startObjNum + i
			if objnum >= kMaxObjectNumber {
				break
			}
			entry := seg[uint64(i)*uint64(totalWidth) : uint64(i+1)*uint64(totalWidth)]
			p.processCrossRefStreamEntry(entry, fieldWidths, objnum)
		}
		segindex += idx.objCount
	}
	return true
}

func (p *Parser) processCrossRefStreamEntry(entry []byte, fieldWidths []uint32, objnum uint32) {
	var typ ObjectType
	if fieldWidths[0] != 0 {
		switch getVarInt(safeSlice(entry, 0, int(fieldWidths[0]))) {
		case 0:
			typ = ObjectTypeFree
		case 1:
			typ = ObjectTypeNormal
		case 2:
			typ = ObjectTypeCompressed
		default:
			return
		}
	} else {
		typ = ObjectTypeNormal // ISO 32000 table 17 default when W[0]==0
	}
	switch typ {
	case ObjectTypeFree:
		gen := getThirdXRefStreamEntry(entry, fieldWidths)
		if gen <= 0xFFFF {
			p.crossRefTable.SetFree(objnum, uint16(gen))
		}
	case ObjectTypeNormal:
		offset := getSecondXRefStreamEntry(entry, fieldWidths)
		gen := getThirdXRefStreamEntry(entry, fieldWidths)
		if gen <= 0xFFFF {
			p.crossRefTable.AddNormal(objnum, uint16(gen), false, int64(offset))
		}
	case ObjectTypeCompressed:
		archiveObjNum := getSecondXRefStreamEntry(entry, fieldWidths)
		if !p.IsValidObjectNumber(archiveObjNum) {
			return
		}
		archiveObjIndex := getThirdXRefStreamEntry(entry, fieldWidths)
		p.crossRefTable.AddCompressed(objnum, archiveObjNum, archiveObjIndex)
	}
}

func getVarInt(span []byte) uint32 {
	var r uint32
	for _, b := range span {
		r = r*256 + uint32(b)
	}
	return r
}

func getSecondXRefStreamEntry(entry []byte, w []uint32) uint32 {
	return getVarInt(safeSlice(entry, int(w[0]), int(w[0])+int(w[1])))
}

func getThirdXRefStreamEntry(entry []byte, w []uint32) uint32 {
	lo := int(w[0]) + int(w[1])
	return getVarInt(safeSlice(entry, lo, lo+int(w[2])))
}

func safeSlice(b []byte, lo, hi int) []byte {
	if lo < 0 {
		lo = 0
	}
	if hi > len(b) {
		hi = len(b)
	}
	if lo > hi {
		return nil
	}
	return b[lo:hi]
}

func getCrossRefStreamIndices(array *objects.Array, size uint32) []indexEntry {
	var result []indexEntry
	if array != nil {
		count := array.Len() / 2
		for i := range count {
			start := array.GetNumberAt(i * 2)
			cnt := array.GetNumberAt(i*2 + 1)
			if start == nil || cnt == nil {
				continue
			}
			nStart := start.GetInteger()
			nCount := cnt.GetInteger()
			if nStart < 0 || nCount <= 0 {
				continue
			}
			result = append(result, indexEntry{uint32(nStart), uint32(nCount)})
		}
	}
	if len(result) == 0 {
		result = append(result, indexEntry{0, size})
	}
	return result
}

func getFieldWidths(array *objects.Array) []uint32 {
	if array == nil {
		return nil
	}
	widths := make([]uint32, 0, array.Len())
	for i := 0; i < array.Len(); i++ {
		widths = append(widths, uint32(array.GetIntegerAt(i)))
	}
	return widths
}

func (p *Parser) verifyCrossRefTable() bool {
	for _, k := range p.crossRefTable.SortedKeys() {
		info := p.crossRefTable.objectsInfo[k]
		if info.Pos <= 0 {
			continue
		}
		saved := p.syntax.GetPos()
		p.syntax.SetPos(int(info.Pos))
		word, isNum := p.syntax.GetNextWord()
		p.syntax.SetPos(saved)
		if !isNum || word == "" || syntax.Atoui(word) != k {
			return false
		}
		break
	}
	return true
}

// --- indirect object resolution ---

// ParseIndirectObject resolves objnum from the cross-reference table.
func (p *Parser) ParseIndirectObject(objnum uint32) objects.Object {
	if !p.IsValidObjectNumber(objnum) {
		return nil
	}
	if _, in := p.parsingObjNums[objnum]; in {
		return nil // cycle guard
	}
	p.parsingObjNums[objnum] = struct{}{}
	defer delete(p.parsingObjNums, objnum)

	info, ok := p.crossRefTable.GetObjectInfo(objnum)
	if !ok {
		return nil
	}
	switch info.Type {
	case ObjectTypeFree:
		return nil
	case ObjectTypeNormal:
		if info.Pos <= 0 {
			return nil
		}
		return p.parseIndirectObjectAt(info.Pos, objnum)
	case ObjectTypeCompressed:
		os := p.getObjectStream(info.ArchiveObjNum)
		if os == nil {
			return nil
		}
		return os.ParseObject(p.objectsHolder, objnum, info.ArchiveObjIndex)
	}
	return nil
}

func (p *Parser) parseIndirectObjectAt(pos int64, objnum uint32) objects.Object {
	saved := p.syntax.GetPos()
	p.syntax.SetPos(int(pos))
	result := p.syntax.GetIndirectObject(p.objectsHolder)
	p.syntax.SetPos(saved)
	if result != nil && objnum != 0 && result.GetObjNum() != objnum {
		return nil
	}
	return result
}

func (p *Parser) getObjectStream(objectNumber uint32) *ObjectStream {
	if _, in := p.parsingObjNums[objectNumber]; in {
		return nil
	}
	if s, ok := p.objectStreamMap[objectNumber]; ok {
		return s
	}
	info, ok := p.crossRefTable.GetObjectInfo(objectNumber)
	if !ok || !info.IsObjectStream || info.Pos <= 0 {
		return nil
	}
	p.parsingObjNums[objectNumber] = struct{}{}
	defer delete(p.parsingObjNums, objectNumber)
	obj := p.parseIndirectObjectAt(info.Pos, objectNumber)
	if obj == nil {
		return nil
	}
	os := NewObjectStream(objects.ToStream(obj))
	p.objectStreamMap[objectNumber] = os
	return os
}

// --- rebuild recovery ---

type numPos struct {
	val uint32
	pos int64
}

func (p *Parser) rebuildCrossRef() bool {
	cross := NewCrossRefTable()
	p.syntax.SetReadBufferSize(kRebuildBufferSize)
	p.syntax.SetPos(0)
	var numbers []numPos
	for {
		word, isNum := p.syntax.GetNextWord()
		if word == "" {
			break
		}
		if isNum {
			numbers = append(numbers, numPos{syntax.Atoui(word), int64(p.syntax.GetPos() - len(word))})
			if len(numbers) > 2 {
				numbers = numbers[1:]
			}
			continue
		}
		switch word {
		case "(":
			p.syntax.ReadString()
		case "<":
			p.syntax.ReadHexString()
		case "trailer":
			tr := p.syntax.GetObjectBody(nil)
			if tr != nil {
				trailerObjNum := tr.GetObjNum()
				var trailerDict *objects.Dictionary
				if s := objects.ToStream(tr); s != nil {
					trailerDict = s.Dict()
				} else {
					trailerDict = objects.ToDictionary(tr)
				}
				cross = MergeUp(cross, NewCrossRefTableWithTrailer(trailerDict, trailerObjNum))
			}
		case "obj":
			if len(numbers) == 2 {
				objPos := numbers[0].pos
				objNum := numbers[0].val
				genNum := numbers[1].val
				p.syntax.SetPos(int(objPos))
				stream := objects.ToStream(p.syntax.GetIndirectObjectStrict(nil))
				if stream != nil && stream.Dict().GetNameFor("Type") == "XRef" {
					cross = MergeUp(cross, NewCrossRefTableWithTrailer(objects.ToDictionary(stream.Dict().Clone()), stream.GetObjNum()))
				}
				if objNum < kMaxObjectNumber {
					cross.AddNormal(objNum, uint16(genNum), false, objPos)
					if os := NewObjectStream(stream); os != nil {
						for i, info := range os.objectInfo {
							if info.objNum < kMaxObjectNumber {
								cross.AddCompressed(info.objNum, objNum, uint32(i))
							}
						}
					}
				}
			}
		}
		numbers = numbers[:0]
	}
	p.crossRefTable = MergeUp(p.crossRefTable, cross)
	p.syntax.SetReadBufferSize(kFileBufSize)
	return p.GetTrailer() != nil && !p.crossRefTable.Empty()
}

// stringToInt parses a leading signed decimal (FXSYS StringToInt), stopping at
// the first non-digit.
func stringToInt(b []byte) int {
	i := 0
	for i < len(b) && b[i] == ' ' {
		i++
	}
	neg := false
	if i < len(b) && (b[i] == '+' || b[i] == '-') {
		neg = b[i] == '-'
		i++
	}
	n := 0
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		n = n*10 + int(b[i]-'0')
		i++
	}
	if neg {
		n = -n
	}
	return n
}
