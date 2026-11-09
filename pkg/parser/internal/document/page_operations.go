package document

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// PageSelection identifies zero-based pages in their output order.
type PageSelection struct {
	Document *Document
	Pages    []int
}

// WritePages writes only the selected pages into a new PDF. The source is not
// modified. Catalog-level structures such as outlines are not imported.
func (d *Document) WritePages(ctx context.Context, w io.Writer, pages []int) error {
	return WritePageSelections(ctx, w, []PageSelection{{Document: d, Pages: pages}})
}

// WritePageSelections concatenates selections into a new document. The object
// graph import follows PDFium's CPDF_PageExporter and CPDF_PageOrganizer; page
// attributes are made explicit before references are remapped. Reference:
// PDFium 6688c576fd851f4159c6fae39396da50939de110, core/fpdfapi/edit/.
func WritePageSelections(ctx context.Context, w io.Writer, selections []PageSelection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if w == nil {
		return fmt.Errorf("write pages: nil writer")
	}
	total, err := validatePageSelections(selections)
	if err != nil {
		return err
	}
	graph := &pageGraph{objects: make(map[uint32]objects.Object)}
	root := objects.NewDictionary()
	root.SetNewNameFor("Type", "Catalog")
	pages := objects.NewDictionary()
	pages.SetNewNameFor("Type", "Pages")
	rootID := graph.add(root)
	pagesID := graph.add(pages)
	root.SetFor("Pages", objects.NewReference(graph, pagesID))
	kids := objects.NewArray()
	pages.SetFor("Kids", kids)
	pages.SetFor("Count", objects.NewNumberFromInt(int32(total)))
	version, err := graph.copySelections(ctx, pagesID, kids, selections, total)
	if err != nil {
		return err
	}
	trailer := objects.NewDictionary()
	trailer.SetFor("Size", objects.NewNumberFromInt(int32(len(graph.objects)+1)))
	trailer.SetFor("Root", objects.NewReference(graph, rootID))
	if err := ctx.Err(); err != nil {
		return err
	}
	return writePDFObjects(w, graph.objects, uint32(len(graph.objects)), version, trailer)
}

type pageGraph struct{ objects map[uint32]objects.Object }

func (g *pageGraph) GetOrParseIndirectObject(id uint32) objects.Object { return g.objects[id] }
func (g *pageGraph) add(obj objects.Object) uint32 {
	id := uint32(len(g.objects) + 1)
	obj.SetObjNum(id)
	g.objects[id] = obj
	return id
}

type pageCopier struct {
	ctx     context.Context
	graph   *pageGraph
	mapped  map[objects.Object]uint32
	active  map[objects.Object]bool
	pagesID uint32
}

func (c *pageCopier) copyPage(dest, src *objects.Dictionary) error {
	for _, key := range src.GetKeys() {
		if key == "Type" || key == "Parent" {
			continue
		}
		v, err := c.clone(src.GetObjectFor(key), 0)
		if err != nil {
			return err
		}
		dest.SetFor(key, v)
	}
	for _, key := range []string{"MediaBox", "Resources", "CropBox", "Rotate"} {
		if dest.KeyExist(key) {
			continue
		}
		v, err := c.clone(inheritedPageValue(src, key), 0)
		if err != nil {
			return err
		}
		if v != nil {
			dest.SetFor(key, v)
		}
	}
	if !dest.KeyExist("MediaBox") {
		if crop := dest.GetObjectFor("CropBox"); crop != nil {
			dest.SetFor("MediaBox", crop)
		} else {
			box := objects.NewArray()
			for _, n := range []int32{0, 0, 612, 792} {
				box.Append(objects.NewNumberFromInt(n))
			}
			dest.SetFor("MediaBox", box)
		}
	}
	if !dest.KeyExist("Resources") {
		dest.SetFor("Resources", objects.NewDictionary())
	}
	return nil
}

func inheritedPageValue(page *objects.Dictionary, key string) objects.Object {
	seen := make(map[*objects.Dictionary]bool)
	for node := page; node != nil && !seen[node]; node = node.GetDictFor("Parent") {
		seen[node] = true
		if node.KeyExist(key) {
			return node.GetObjectFor(key)
		}
	}
	return nil
}

func (c *pageCopier) clone(src objects.Object, depth int) (objects.Object, error) {
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, nil
	}
	if depth > 512 {
		return nil, fmt.Errorf("object graph exceeds depth limit")
	}
	if ref := objects.ToReference(src); ref != nil {
		direct := objects.Direct(ref)
		if direct == nil {
			return nil, nil
		}
		return c.indirect(direct, depth)
	}
	if !src.IsInline() {
		return c.indirect(src, depth)
	}
	return c.body(src, depth)
}

func (c *pageCopier) indirect(src objects.Object, depth int) (objects.Object, error) {
	if id, ok := c.mapped[src]; ok {
		return objects.NewReference(c.graph, id), nil
	}
	if dict := objects.ToDictionary(src); dict != nil {
		switch strings.ToLower(dict.GetNameFor("Type")) {
		case "pages":
			return objects.NewReference(c.graph, c.pagesID), nil
		case "page", "catalog":
			return nil, nil
		}
	}
	// Reserve the number before following references so indirect cycles resolve.
	id := c.graph.add(objects.NewNull())
	c.mapped[src] = id
	copied, err := c.body(src, depth+1)
	if err != nil {
		return nil, err
	}
	if copied == nil {
		copied = objects.NewNull()
	}
	copied.SetObjNum(id)
	c.graph.objects[id] = copied
	return objects.NewReference(c.graph, id), nil
}

func (c *pageCopier) body(src objects.Object, depth int) (objects.Object, error) {
	if c.active[src] {
		return nil, fmt.Errorf("cyclic direct object")
	}
	c.active[src] = true
	defer delete(c.active, src)
	switch v := src.(type) {
	case *objects.Array:
		out := objects.NewArray()
		for i := 0; i < v.Len(); i++ {
			item, err := c.clone(v.GetObjectAt(i), depth+1)
			if err != nil {
				return nil, err
			}
			if item == nil {
				item = objects.NewNull()
			}
			out.Append(item)
		}
		return out, nil
	case *objects.Dictionary:
		return c.dictionary(v, depth)
	case *objects.Stream:
		dict, err := c.dictionary(v.Dict(), depth+1)
		if err != nil {
			return nil, err
		}
		acc := objects.NewStreamAcc(v)
		acc.LoadAllDataRaw()
		return objects.NewStreamFromData(append([]byte(nil), acc.GetSpan()...), dict), nil
	default:
		out := src.Clone()
		out.SetObjNum(0)
		out.SetGenNum(0)
		return out, nil
	}
}

func (c *pageCopier) dictionary(src *objects.Dictionary, depth int) (*objects.Dictionary, error) {
	out := objects.NewDictionary()
	for _, key := range src.GetKeys() {
		item, err := c.clone(src.GetObjectFor(key), depth+1)
		if err != nil {
			return nil, err
		}
		if item != nil {
			out.SetFor(key, item)
		}
	}
	return out, nil
}

func validatePageSelections(selections []PageSelection) (int, error) {
	total := 0
	for _, s := range selections {
		if s.Document == nil || s.Document.prs == nil {
			return 0, fmt.Errorf("write pages: nil document")
		}
		for _, i := range s.Pages {
			if i < 0 || i >= s.Document.GetPageCount() {
				return 0, fmt.Errorf("write pages: page index %d out of range", i)
			}
			total++
			if total >= kPageMaxNum {
				return 0, fmt.Errorf("write pages: too many pages")
			}
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("write pages: empty selection")
	}
	return total, nil
}

func (graph *pageGraph) copySelections(ctx context.Context, pagesID uint32, kids *objects.Array, selections []PageSelection, total int) (int, error) {
	type selectedPage struct {
		source   *objects.Dictionary
		target   *objects.Dictionary
		document *Document
		index    int
	}
	selected := make([]selectedPage, 0, total)
	pageMappings := make(map[*Document]map[objects.Object]uint32)
	version := 14
	for _, s := range selections {
		if v := s.Document.prs.FileVersion(); v > version {
			version = v
		}
		if pageMappings[s.Document] == nil {
			pageMappings[s.Document] = make(map[objects.Object]uint32)
		}
		for _, index := range s.Pages {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			src := s.Document.GetPageDict(index)
			if src == nil {
				return 0, fmt.Errorf("write pages: page %d dictionary missing", index)
			}
			dest := objects.NewDictionary()
			dest.SetNewNameFor("Type", "Page")
			dest.SetFor("Parent", objects.NewReference(graph, pagesID))
			id := graph.add(dest)
			kids.Append(objects.NewReference(graph, id))
			if _, exists := pageMappings[s.Document][src]; !exists {
				pageMappings[s.Document][src] = id
			}
			selected = append(selected, selectedPage{src, dest, s.Document, index})
		}
	}
	for _, page := range selected {
		mapping := make(map[objects.Object]uint32)
		for src, id := range pageMappings[page.document] {
			mapping[src] = id
		}
		mapping[page.source] = page.target.GetObjNum()
		copier := &pageCopier{ctx: ctx, graph: graph, mapped: mapping, active: make(map[objects.Object]bool), pagesID: pagesID}
		if err := copier.copyPage(page.target, page.source); err != nil {
			return 0, fmt.Errorf("write pages: page %d: %w", page.index, err)
		}
	}
	return version, nil
}
