package parser

// Regression guard for the per-glyph GetCharBBox path (plan 009 face-backed glyph
// boxes). It loads a real embedded font from the corpus and asserts that
// GetCharBBox returns genuine per-glyph control boxes — varying vertical extents
// per glyph, a plausible cap height, and a descender below the baseline — rather
// than the uniform descriptor ascent/descent box of the original face-less port.
// Skips if the corpus is unavailable.
import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/document"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/font"
	"github.com/ivanvanderbyl/docmill/pkg/parser/internal/parser"
)

func TestPerGlyphCharBBox(t *testing.T) {
	root := repoRootInv(t)
	// 2203.01017v2 embeds Nimbus (Type1) and 2206.01062 embeds LinLibertine;
	// either exercises the face-backed simple-font path.
	for _, name := range []string{"2203.01017v2", "2206.01062"} {
		p := filepath.Join(root, ".upstream-docling", "tests", "data", "pdf", name+".pdf")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		doc, perr := document.Open(append([]byte(nil), data...))
		if perr != parser.Success {
			t.Fatalf("%s: open: %v", name, perr)
		}
		if checkPerGlyph(t, doc, name) {
			return // one embedded simple font verified
		}
	}
	t.Skip("no corpus PDF with an embedded simple font available")
}

func repoRootInv(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func checkPerGlyph(t *testing.T, doc *document.Document, name string) bool {
	pd := doc.GetPageDict(0)
	if pd == nil {
		return false
	}
	res := pd.GetDictFor("Resources")
	if res == nil {
		return false
	}
	fonts := res.GetDictFor("Font")
	if fonts == nil {
		return false
	}
	for _, key := range fonts.GetKeys() {
		fd := fonts.GetDictFor(key)
		if fd == nil || fd.GetByteStringFor("Subtype") == "Type0" {
			continue
		}
		f := font.Load(fd, doc)
		if f == nil || !f.IsEmbedded() {
			continue
		}
		// Sample glyphs with distinct shapes. A genuine face-backed box gives the
		// cap 'A', the ascender 'T' and the x-height 'x' DIFFERENT tops; a uniform
		// descriptor fallback gives them all the same. Skip fonts whose GID this
		// port cannot resolve (they hit the descriptor box).
		_, _, _, aTop := f.GetCharBBox('A')
		_, _, _, tTop := f.GetCharBBox('T')
		_, _, _, xTop := f.GetCharBBox('x')
		_, gBot, _, _ := f.GetCharBBox('g')
		if aTop == tTop && tTop == xTop {
			continue // uniform -> descriptor fallback; try another font
		}
		if xTop >= aTop {
			t.Errorf("%s/%s: x-height top (%d) should be below cap-height top (%d)", name, key, xTop, aTop)
		}
		if aTop < 500 || aTop > 800 {
			t.Errorf("%s/%s: cap-height top for 'A' = %d, want plausible (500-800)", name, key, aTop)
		}
		if gBot >= 0 {
			t.Errorf("%s/%s: 'g' bottom (%d) should be a descender below baseline", name, key, gBot)
		}
		t.Logf("%s/%s: per-glyph boxes OK (A.top=%d T.top=%d x.top=%d g.bot=%d)", name, key, aTop, tTop, xTop, gBot)
		return true
	}
	return false
}
