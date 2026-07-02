// Package parser ports the PDFium parser layer (xref tables/streams, object
// streams, indirect-object resolution, linearization) from
// core/fpdfapi/parser/cpdf_parser* @ pdfium 0db284a42, over a whole-file []byte.
// PDFium's IFX_SeekableReadStream/CPDF_ReadValidator/CPDF_DataAvail streaming
// machinery is dropped; RetainPtr/UnownedPtr map to Go pointers + GC. See plan
// 009 Phase D.
package parser

import (
	"slices"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
)

// Constants (cpdf_parser.h / .cpp). kMaxObjectNumber was raised from 1M for
// crbug/910009; kMaxXRefSize stays exactly one larger.
const (
	kMaxObjectNumber       uint32 = 4 * 1024 * 1024 // 4194304
	kMaxXRefSize                  = int(kMaxObjectNumber) + 1
	kPDFHeaderSize         int64  = 9
	kMinFieldCount                = 3
	kNoTrailerObjectNumber uint32 = 0
	kEntrySize                    = 20
	kRebuildBufferSize            = 4096
	kFileBufSize                  = 32768
)

// kInvalidObjNum mirrors objects.InvalidObjNum.
const kInvalidObjNum = ^uint32(0)

// ObjectType is the kind of a cross-reference entry.
type ObjectType uint8

const (
	ObjectTypeFree       ObjectType = 0
	ObjectTypeNormal     ObjectType = 1
	ObjectTypeCompressed ObjectType = 2
)

// ObjectInfo is one cross-reference entry. Pos is read only when Type==Normal;
// ArchiveObjNum/ArchiveObjIndex only when Type==Compressed.
type ObjectInfo struct {
	Type            ObjectType
	IsObjectStream  bool
	GenNum          uint16
	Pos             int64
	ArchiveObjNum   uint32
	ArchiveObjIndex uint32
}

// CrossRefTable is a port of CPDF_CrossRefTable. The C++ ordered std::map is a
// Go map plus sorted-key helpers where iteration order / last-key is needed.
type CrossRefTable struct {
	trailer             *objects.Dictionary
	trailerObjectNumber uint32
	objectsInfo         map[uint32]ObjectInfo

	// maxKey caches the largest object number (PDFium gets this O(1) from the
	// ordered std::map's rbegin; a Go map would otherwise need an O(n) scan on
	// every GetLastObjNum, which dominates content parsing). It is recomputed
	// lazily when maxKeyDirty is set by a mutation.
	maxKey      uint32
	maxKeyDirty bool
}

// NewCrossRefTable returns an empty table.
func NewCrossRefTable() *CrossRefTable {
	return &CrossRefTable{objectsInfo: map[uint32]ObjectInfo{}}
}

// NewCrossRefTableWithTrailer returns a table seeded with a trailer.
func NewCrossRefTableWithTrailer(trailer *objects.Dictionary, objNum uint32) *CrossRefTable {
	return &CrossRefTable{trailer: trailer, trailerObjectNumber: objNum, objectsInfo: map[uint32]ObjectInfo{}}
}

// AddCompressed records objNum as living inside the archive object stream.
func (t *CrossRefTable) AddCompressed(objNum, archiveObjNum, archiveObjIndex uint32) {
	if objNum >= kMaxObjectNumber || archiveObjNum >= kMaxObjectNumber {
		return
	}
	info := t.objectsInfo[objNum]
	if info.GenNum > 0 {
		return
	}
	if info.IsObjectStream {
		return // don't add a known object stream into an object stream
	}
	info.Type = ObjectTypeCompressed
	info.ArchiveObjNum = archiveObjNum
	info.ArchiveObjIndex = archiveObjIndex
	info.GenNum = 0
	t.objectsInfo[objNum] = info

	archive := t.objectsInfo[archiveObjNum]
	archive.IsObjectStream = true
	t.objectsInfo[archiveObjNum] = archive
	t.maxKeyDirty = true
}

// AddNormal records objNum at the given file offset.
func (t *CrossRefTable) AddNormal(objNum uint32, genNum uint16, isObjectStream bool, pos int64) {
	if objNum >= kMaxObjectNumber {
		return
	}
	info := t.objectsInfo[objNum]
	if info.GenNum > genNum {
		return
	}
	info.Type = ObjectTypeNormal
	info.IsObjectStream = info.IsObjectStream || isObjectStream
	info.GenNum = genNum
	info.Pos = pos
	t.objectsInfo[objNum] = info
	t.maxKeyDirty = true
}

// SetFree marks objNum free.
func (t *CrossRefTable) SetFree(objNum uint32, genNum uint16) {
	if objNum >= kMaxObjectNumber {
		return
	}
	info := t.objectsInfo[objNum]
	info.Type = ObjectTypeFree
	info.GenNum = genNum
	info.Pos = 0
	t.objectsInfo[objNum] = info
	t.maxKeyDirty = true
}

// SetTrailer sets the trailer dictionary and its object number.
func (t *CrossRefTable) SetTrailer(trailer *objects.Dictionary, objNum uint32) {
	t.trailer = trailer
	t.trailerObjectNumber = objNum
}

// Trailer returns the trailer dictionary (may be nil).
func (t *CrossRefTable) Trailer() *objects.Dictionary { return t.trailer }

// GetObjectInfo returns the entry for objNum.
func (t *CrossRefTable) GetObjectInfo(objNum uint32) (ObjectInfo, bool) {
	info, ok := t.objectsInfo[objNum]
	return info, ok
}

// Empty reports whether the table has no entries.
func (t *CrossRefTable) Empty() bool { return len(t.objectsInfo) == 0 }

// LastKey returns the largest object number present (0 if empty), caching the
// result until the next mutation.
func (t *CrossRefTable) LastKey() uint32 {
	if t.maxKeyDirty {
		var max uint32
		first := true
		for k := range t.objectsInfo {
			if first || k > max {
				max = k
				first = false
			}
		}
		t.maxKey = max
		t.maxKeyDirty = false
	}
	return t.maxKey
}

// SortedKeys returns the object numbers in ascending order.
func (t *CrossRefTable) SortedKeys() []uint32 {
	keys := make([]uint32, 0, len(t.objectsInfo))
	for k := range t.objectsInfo {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// SetObjectMapSize trims entries >= size and inserts a free placeholder at
// size-1 if absent (matching CPDF_CrossRefTable::SetObjectMapSize).
func (t *CrossRefTable) SetObjectMapSize(size uint32) {
	t.maxKeyDirty = true
	if size == 0 {
		t.objectsInfo = map[uint32]ObjectInfo{}
		return
	}
	for k := range t.objectsInfo {
		if k >= size {
			delete(t.objectsInfo, k)
		}
	}
	if _, ok := t.objectsInfo[size-1]; !ok {
		t.objectsInfo[size-1] = ObjectInfo{Pos: 0}
	}
}

// Update merges newCRT into t (info then trailer).
func (t *CrossRefTable) Update(newCRT *CrossRefTable) {
	t.updateInfo(newCRT.objectsInfo)
	t.updateTrailer(newCRT.trailer)
}

// updateInfo merges newInfo (top) over t.objectsInfo, with the top winning but
// the object-stream flag OR-ed up when both entries are Normal.
func (t *CrossRefTable) updateInfo(newInfo map[uint32]ObjectInfo) {
	t.maxKeyDirty = true
	if len(newInfo) == 0 {
		return
	}
	if len(t.objectsInfo) == 0 {
		t.objectsInfo = newInfo
		return
	}
	for k, cur := range t.objectsInfo {
		if ni, ok := newInfo[k]; ok {
			if ni.Type == ObjectTypeNormal && cur.Type == ObjectTypeNormal && cur.IsObjectStream {
				ni.IsObjectStream = true
				newInfo[k] = ni
			}
		} else {
			newInfo[k] = cur
		}
	}
	t.objectsInfo = newInfo
}

// updateTrailer absorbs newTrailer's keys into the existing trailer, restoring
// the older trailer's XRefStm/Prev.
func (t *CrossRefTable) updateTrailer(newTrailer *objects.Dictionary) {
	if newTrailer == nil {
		return
	}
	if t.trailer == nil {
		t.trailer = newTrailer
		return
	}
	newTrailer.SetFor("XRefStm", t.trailer.RemoveFor("XRefStm"))
	newTrailer.SetFor("Prev", t.trailer.RemoveFor("Prev"))
	for _, k := range newTrailer.GetKeys() {
		t.trailer.SetFor(k, newTrailer.RemoveFor(k))
	}
}

// MergeUp overlays top onto current (current is the older layer). Despite the
// name, top wins where it has entries.
func MergeUp(current, top *CrossRefTable) *CrossRefTable {
	if current == nil {
		return top
	}
	if top == nil {
		return current
	}
	current.Update(top)
	return current
}
