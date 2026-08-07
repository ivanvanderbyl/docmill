package pdf

import (
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// Model-owned routing: the end state Task 6 works towards, where the learned
// classifier decides what every line is and no hand-tuned classification
// threshold remains.
//
// What the model takes over here:
//
//	Section-header, Title  -> heading  (replaces isHeadingLine)
//	List-item              -> list     (replaces isListBlockCandidate)
//	Picture                -> dropped from prose (replaces the figure-label filter)
//	Formula                -> never a table cell (already migrated)
//
// What it deliberately does NOT take over, per the plan:
//
//	heading LEVEL assignment  — the teacher is flat on headings, so
//	                            assignDocumentHeadingLevels survives
//	table STRUCTURE           — a class label cannot build a grid; pkg/table
//	                            still builds every cell
//	line assembly, columns, reading order — out of scope for this project
//
// The unit is the class-agnostically assembled line, so every decision below is
// made on the same lines the model was trained on.

// The model's label set, which is DocLayNet's vocabulary rather than docmill's.
// The mapping onto docmill routing is the plan's label table.
const (
	layoutClassSectionHeader = "Section-header"
	layoutClassTitle         = "Title"
	layoutClassListItem      = "List-item"
	layoutClassPicture       = "Picture"
	layoutClassTable         = "Table"
)

// lineLabeller predicts a label per class-agnostic line and answers questions
// about a box. Built once per page so the model runs over each line exactly
// once, however many routing decisions consult it.
type lineLabeller struct {
	lines  []ParagraphTextLine
	labels []string
	ok     bool

	// gated holds the candidate regions the REGION model accepted, by class.
	// Nil until gateRegions runs; an empty slice means every candidate of that
	// class was rejected.
	gated map[string][]geom.Box
}

// gateRegions groups the labelled lines into candidate regions and asks the
// REGION model which stand. Only the classes named are gated; everything else
// keeps its line labels untouched.
//
// This is the cascade's second stage. A rejected candidate costs nothing beyond
// the region: its lines fall back to their own labels, which is what they had
// before the region existed.
func (l *lineLabeller) gateRegions(cells []page.TextCell, rulings []page.RulingSegment, size geom.Size, classes ...string) {
	if !l.ok {
		return
	}
	l.gated = map[string][]geom.Box{}
	wanted := map[string]bool{}
	for _, class := range classes {
		wanted[class] = true
		l.gated[class] = nil
	}
	for _, region := range GroupLineRegions(l.lines, l.labels) {
		if !wanted[region.Class] {
			continue
		}
		if acceptRegion(RegionFeatures(region, l.lines, l.labels, cells, rulings, size)) {
			l.gated[region.Class] = append(l.gated[region.Class], region.Box)
		}
	}
}

// inAcceptedRegion reports whether box lies inside a candidate of this class
// that the region model accepted. When the class was never gated it reports
// true, so ungated classes behave exactly as before.
func (l *lineLabeller) inAcceptedRegion(box geom.Box, class string) bool {
	if l.gated == nil {
		return true
	}
	accepted, gated := l.gated[class]
	if !gated {
		return true
	}
	for _, region := range accepted {
		if lineContainment(box, region) >= 0.5 {
			return true
		}
	}
	return false
}

func newLineLabeller(lines []ParagraphTextLine, cells []page.TextCell, size geom.Size, rulings []page.RulingSegment) *lineLabeller {
	out := &lineLabeller{lines: lines, labels: make([]string, len(lines))}
	model, err := layoutModel()
	if err != nil || model == nil || len(lines) == 0 {
		return out
	}

	// repeat_frac is 0 here, matching how the model was trained: DocLayNet is
	// 81k single-page PDFs, so it never saw a non-zero value. Feeding page
	// context in would be the skew this project keeps guarding against.
	ctx := NewPageLayoutContext(size, cells, lines, rulings, 0, 1)
	for i := range lines {
		var prev, next *ParagraphTextLine
		if i > 0 {
			prev = &lines[i-1]
		}
		if i+1 < len(lines) {
			next = &lines[i+1]
		}
		out.labels[i], _ = model.PredictLineClass(LineLayoutFeatures(lines[i], prev, next, ctx))
	}
	out.ok = true
	return out
}

// labelOf returns the label for a box by a PLURALITY VOTE over the
// class-agnostic lines inside it.
//
// A vote rather than the single best-matching line, because a block is usually
// several lines: a three-line paragraph fully contains three lines, all scoring
// containment 1.0, so "the best match" would pick one of them arbitrarily and
// the label would depend on iteration order. This is the same
// argmax-over-the-line-label-distribution rule the Formula veto uses, for the
// same reason.
func (l *lineLabeller) labelOf(box geom.Box) (string, bool) {
	if !l.ok {
		return "", false
	}
	votes := map[string]int{}
	for i, line := range l.lines {
		if lineContainment(line.BBox, box) >= 0.5 {
			votes[l.labels[i]]++
		}
	}
	winner, best := "", 0
	for label, count := range votes {
		if count > best || (count == best && label < winner) {
			winner, best = label, count
		}
	}
	if best == 0 {
		return "", false
	}
	return winner, true
}

// isHeading reports whether the model calls this line a heading. Section-header
// and Title collapse to one destination because docmill has a single heading
// concept and levels are assigned later from the document outline.
func (l *lineLabeller) isHeading(line ParagraphTextLine) bool {
	label, ok := l.labelOf(line.BBox)
	return ok && (label == layoutClassSectionHeader || label == layoutClassTitle)
}

// isClass reports whether the model gives box exactly this label.
func (l *lineLabeller) isClass(box geom.Box, class string) bool {
	label, ok := l.labelOf(box)
	return ok && label == class
}

// labelOfFirstLine returns the label of the TOPMOST class-agnostic line inside
// box, rather than the plurality.
//
// This exists for list items, where the plurality vote is actively wrong. A
// four-line list item carries its marker on the first line only; the other
// three read as ordinary prose and the model labels them Text. The vote is then
// 3-1 against and the item renders as a paragraph — which is what was
// happening, and it accounted for 88% of the list items the routing missed.
//
// "Is this block a list item?" is really "does this block START a list item?",
// so the first line is the right thing to ask.
func (l *lineLabeller) labelOfFirstLine(box geom.Box) (string, bool) {
	if !l.ok {
		return "", false
	}
	best, label := 0.0, ""
	found := false
	for i, line := range l.lines {
		if lineContainment(line.BBox, box) < 0.5 {
			continue
		}
		top := topEdgeOf(line.BBox)
		if !found || top < best {
			best, label, found = top, l.labels[i], true
		}
	}
	return label, found
}

// dropPictureBlocks removes blocks the model calls Picture — figure innards
// such as axis ticks, node labels and legend text, which are text on the page
// but not part of the prose flow.
//
// This replaces filterFigureInternalLabelBlocks, whose measured recall on
// DocLayNet was 0.074: it fired on one figure line in thirteen. Captions are
// NOT dropped; the model gives them their own class and they route as prose.
func dropPictureBlocks(blocks []markdownBlock, labeller *lineLabeller) []markdownBlock {
	if !labeller.ok {
		return blocks
	}
	out := blocks[:0]
	for _, block := range blocks {
		// Both stages must agree: the line model calls it figure innards AND
		// the region model accepts the run it belongs to. Picture candidates
		// are correct only 5.7% of the time on their own, so the gate is doing
		// most of the work here.
		if block.tableData == nil &&
			labeller.isClass(block.Box, layoutClassPicture) &&
			labeller.inAcceptedRegion(block.Box, layoutClassPicture) {
			continue
		}
		out = append(out, block)
	}
	return out
}

// applyLearnedListItems rewrites the blocks the model calls List-item, using the
// existing marker-stripping writer.
//
// It replaces isListBlockCandidate and listBlockHasContext — the run-context
// rule that gave DetectStructure 0.126 recall, because a list item with no
// neighbouring list item was never recognised. The model has no such
// requirement: it judges each line on its own geometry.
func applyLearnedListItems(blocks []markdownBlock, labeller *lineLabeller) []markdownBlock {
	if !labeller.ok {
		return blocks
	}
	out := append([]markdownBlock(nil), blocks...)
	for i := range out {
		if out[i].tableData != nil {
			continue
		}
		label, ok := labeller.labelOfFirstLine(out[i].Box)
		if !ok || label != layoutClassListItem {
			continue
		}
		if rewritten, ok := rewriteListItem(out[i].Text); ok {
			out[i].Text = rewritten
			continue
		}
		// The model recognised a list item whose marker is not a character we
		// can strip — it may be a glyph the font maps oddly, or drawn rather
		// than typed. Render it as a list item anyway: the classification is the
		// evidence, and refusing because no literal bullet is present is the
		// renderer overruling the classifier.
		if text := strings.TrimSpace(out[i].Text); text != "" {
			out[i].Text = "- " + text
		}
	}
	return out
}
