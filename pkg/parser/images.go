package parser

import (
	"bytes"
	"context"
	"fmt"
	"image/png"

	pdfimage "github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/image"
	"github.com/ivanvanderbyl/docmill/v2/pkg/parser/internal/objects"
)

// ExtractedImage is an image resource, independent of its painted position.
// Data contains JPEG bytes when preservation is possible, otherwise PNG bytes.
type ExtractedImage struct {
	PageIndex    int
	ObjectNumber uint32
	Name         string
	Format       string
	Data         []byte
	Width        int
	Height       int
}

// ExtractImages returns images from selected zero-based pages, including nested
// forms and inline images. Stencils use black paint with their mask alpha.
// A nil selection means all pages. Shared resources
// appear once per page. This extracts images; it does not render whole pages.
func (d *Document) ExtractImages(ctx context.Context, pages []int) ([]ExtractedImage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d == nil || d.doc == nil {
		return nil, fmt.Errorf("image: nil document")
	}
	if pages == nil {
		pages = make([]int, d.doc.GetPageCount())
		for i := range pages {
			pages[i] = i
		}
	}
	for _, index := range pages {
		if index < 0 || index >= d.doc.GetPageCount() {
			return nil, fmt.Errorf("image: page index %d out of range", index)
		}
	}
	var out []ExtractedImage
	for _, index := range pages {
		page := d.doc.GetPageDict(index)
		if page == nil {
			return nil, fmt.Errorf("image: page %d dictionary missing", index)
		}
		resources := inheritedImageResources(page)
		c := imageCollector{
			ctx:         ctx,
			index:       index,
			seen:        make(map[*objects.Stream]bool),
			contentSeen: make(map[objects.Object]bool),
		}
		if err := c.resources(resources, ""); err != nil {
			return nil, err
		}
		if err := c.contentObject(page.GetDirectObjectFor("Contents"), resources, "inline"); err != nil {
			return nil, err
		}
		out = append(out, c.images...)
	}
	if err := d.doc.DecryptionError(); err != nil {
		return nil, err
	}
	return out, nil
}

func inheritedImageResources(page *objects.Dictionary) *objects.Dictionary {
	seen := make(map[*objects.Dictionary]bool)
	for page != nil && !seen[page] {
		seen[page] = true
		if r := page.GetDictFor("Resources"); r != nil {
			return r
		}
		page = page.GetDictFor("Parent")
	}
	return nil
}

type imageCollector struct {
	ctx         context.Context
	index       int
	seen        map[*objects.Stream]bool
	contentSeen map[objects.Object]bool
	images      []ExtractedImage
	depth       int
}

func (c *imageCollector) resources(resources *objects.Dictionary, prefix string) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if c.depth >= 64 {
		return fmt.Errorf("image: form nesting exceeds 64")
	}
	c.depth++
	defer func() {
		c.depth--
	}()
	if resources == nil {
		return nil
	}
	xobjects := resources.GetDictFor("XObject")
	if xobjects == nil {
		return nil
	}
	for _, name := range xobjects.GetKeys() {
		s := xobjects.GetStreamFor(name)
		if s == nil || c.seen[s] {
			continue
		}
		c.seen[s] = true
		full := prefix + name
		switch s.Dict().GetNameFor("Subtype") {
		case "Image":
			if err := c.add(s, resources, full); err != nil {
				return err
			}
		case "Form":
			nested := s.Dict().GetDictFor("Resources")
			if nested == nil {
				nested = resources
			}
			if err := c.resources(nested, full+"/"); err != nil {
				return err
			}
			if err := c.contentObject(s, nested, full+"/inline"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *imageCollector) contentObject(obj objects.Object, resources *objects.Dictionary, name string) error {
	if c.contentSeen[obj] {
		return fmt.Errorf("image: cyclic content array")
	}
	if obj != nil {
		c.contentSeen[obj] = true
		defer delete(c.contentSeen, obj)
	}
	if len(c.contentSeen) > 64 {
		return fmt.Errorf("image: content nesting exceeds 64")
	}
	if a := objects.ToArray(obj); a != nil {
		for i := 0; i < a.Len(); i++ {
			if err := c.contentObject(a.GetDirectObjectAt(i), resources, fmt.Sprintf("%s/%d", name, i)); err != nil {
				return err
			}
		}
		return nil
	}
	s := objects.ToStream(obj)
	if s == nil {
		return nil
	}
	data, terminal, err := objects.DecodeImageData(s)
	if err != nil {
		return err
	}
	if terminal != "" {
		return fmt.Errorf("image: invalid content stream filter %s", terminal)
	}
	streams, err := pdfimage.InlineImages(data, resources)
	if err != nil {
		return err
	}
	for i, stream := range streams {
		if err := c.add(stream, resources, fmt.Sprintf("%s/%d", name, i)); err != nil {
			return err
		}
	}
	return nil
}

func (c *imageCollector) add(s *objects.Stream, resources *objects.Dictionary, name string) error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	pixels, jpeg, err := pdfimage.Decode(s, resources)
	if err != nil {
		return fmt.Errorf("page %d image %s: %w", c.index, name, err)
	}
	var data []byte
	format := "jpeg"
	if jpeg != nil {
		data = jpeg
	} else {
		format = "png"
		var buf bytes.Buffer
		if err := png.Encode(&buf, pixels); err != nil {
			return err
		}
		data = buf.Bytes()
	}
	size := pixels.Bounds().Size()
	c.images = append(c.images, ExtractedImage{
		PageIndex:    c.index,
		ObjectNumber: s.GetObjNum(),
		Name:         name,
		Format:       format,
		Data:         data,
		Width:        size.X,
		Height:       size.Y,
	})
	return nil
}
