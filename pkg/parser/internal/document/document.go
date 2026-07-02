// Package document ports the page-tree half of core/fpdfapi/parser/cpdf_document
// @ pdfium 0db284a42: it drives the parser, implements the indirect-object
// holder, and counts pages via /Root /Pages /Count (with the /Kids traversal
// fallback). See plan 009 Phase D.
package document

import (
	"sync"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/objects"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/parser"
)

const kPageMaxNum = 0xFFFFF // 1048575

// Document is the parsed PDF: a parser plus the object holder and page list.
type Document struct {
	*parser.Holder
	prs      *parser.Parser
	rootDict *objects.Dictionary
	pageList []uint32
	// pageMu guards the lazy page-tree resolution in GetPageDict: the pageList
	// cache and getNodeType's in-place /Type normalisation of shared page-tree
	// dictionaries. Pages are extracted from many goroutines at once.
	pageMu sync.Mutex
}

// Open parses buf and returns the document (with the parser error code).
func Open(buf []byte) (*Document, parser.Error) {
	p := parser.New()
	h := parser.NewHolder(p)
	d := &Document{Holder: h, prs: p}
	err := p.StartParse(buf, d)
	return d, err
}

// TryInit is called by the parser once the xref is loaded: resolve the catalog
// and build the page list.
func (d *Document) TryInit() bool {
	d.SetLastObjNum(d.prs.GetLastObjNum())
	if rootObj := d.GetOrParseIndirectObject(d.prs.GetRootObjNum()); rootObj != nil {
		d.rootDict = rootObj.GetDict()
	}
	d.loadPages()
	return d.rootDict != nil && d.GetPageCount() > 0
}

// GetPageCount returns the number of pages.
func (d *Document) GetPageCount() int { return len(d.pageList) }

// GetPageDict returns the page dictionary for page index, resolving it via the
// /Kids leaf traversal (in document order) and caching the object number.
func (d *Document) GetPageDict(index int) *objects.Dictionary {
	d.pageMu.Lock()
	defer d.pageMu.Unlock()
	if index < 0 || index >= len(d.pageList) {
		return nil
	}
	if d.pageList[index] != 0 {
		if dict := objects.ToDictionary(d.GetOrParseIndirectObject(d.pageList[index])); dict != nil {
			return dict
		}
	}
	pages := d.getPagesDict()
	if pages == nil {
		return nil
	}
	var leaves []*objects.Dictionary
	d.collectLeaves(pages, map[*objects.Dictionary]struct{}{pages: {}}, &leaves)
	if index < len(leaves) {
		d.pageList[index] = leaves[index].GetObjNum()
		return leaves[index]
	}
	return nil
}

func (d *Document) collectLeaves(node *objects.Dictionary, visited map[*objects.Dictionary]struct{}, out *[]*objects.Dictionary) {
	if len(*out) >= len(d.pageList) {
		return
	}
	kids := node.GetArrayFor("Kids")
	if kids == nil {
		return
	}
	for i := 0; i < kids.Len(); i++ {
		kid := kids.GetDictAt(i)
		if kid == nil {
			continue
		}
		if _, seen := visited[kid]; seen {
			continue
		}
		if getNodeType(kid) == nodeBranch {
			visited[kid] = struct{}{}
			d.collectLeaves(kid, visited, out)
		} else {
			*out = append(*out, kid)
		}
	}
}

func (d *Document) loadPages() {
	lh := d.prs.GetLinearizedHeader()
	if lh == nil {
		d.pageList = make([]uint32, d.retrievePageCount())
		return
	}
	objnum := lh.GetFirstPageObjNum()
	if !isValidPageObject(d.GetOrParseIndirectObject(objnum)) {
		d.pageList = make([]uint32, d.retrievePageCount())
		return
	}
	d.pageList = make([]uint32, lh.GetPageCount())
	if int(lh.GetFirstPageNo()) < len(d.pageList) {
		d.pageList[lh.GetFirstPageNo()] = objnum
	}
}

func (d *Document) getPagesDict() *objects.Dictionary {
	if d.rootDict == nil {
		return nil
	}
	return d.rootDict.GetDictFor("Pages")
}

func (d *Document) retrievePageCount() int {
	pages := d.getPagesDict()
	if pages == nil {
		return 0
	}
	if !pages.KeyExist("Kids") {
		return 1
	}
	visited := map[*objects.Dictionary]struct{}{pages: {}}
	count, ok := d.countPages(pages, visited)
	if !ok {
		return 0
	}
	return count
}

const (
	nodeBranch = iota
	nodeLeaf
)

func getNodeType(dict *objects.Dictionary) int {
	switch dict.GetNameFor("Type") {
	case "Pages":
		return nodeBranch
	case "Page":
		return nodeLeaf
	}
	// Malformed node with no /Type: guess from /Kids and normalize in place.
	if dict.KeyExist("Kids") {
		dict.SetNewNameFor("Type", "Pages")
		return nodeBranch
	}
	dict.SetNewNameFor("Type", "Page")
	return nodeLeaf
}

func (d *Document) countPages(pagesDict *objects.Dictionary, visited map[*objects.Dictionary]struct{}) (int, bool) {
	countFromDict := pagesDict.GetIntegerFor("Count")
	if countFromDict > 0 && countFromDict < kPageMaxNum {
		return countFromDict, true // trust the declared /Count when in range
	}
	kids := pagesDict.GetArrayFor("Kids")
	if kids == nil {
		return 0, true
	}
	count := 0
	for i := 0; i < kids.Len(); i++ {
		kidDict := kids.GetDictAt(i)
		if kidDict == nil {
			continue
		}
		if _, seen := visited[kidDict]; seen {
			continue
		}
		if getNodeType(kidDict) == nodeBranch {
			visited[kidDict] = struct{}{}
			local, ok := d.countPages(kidDict, visited)
			if !ok {
				return 0, false
			}
			count += local
		} else {
			count++
		}
		if count >= kPageMaxNum {
			return 0, false
		}
	}
	pagesDict.SetFor("Count", objects.NewNumberFromInt(int32(count)))
	return count, true
}

func isValidPageObject(obj objects.Object) bool {
	dict := objects.ToDictionary(obj)
	return dict != nil && dict.GetNameFor("Type") == "Page"
}
