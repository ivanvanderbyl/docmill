package pdf

import (
	"context"
	"strings"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
	"github.com/ivanvanderbyl/docmill/v2/pkg/render"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
)

// Region-routed Markdown: the learned region stage drives the whole page.
//
// The classify-then-route path (reroute.go) keeps the heuristics' structure and
// lets the line model overrule individual decisions. This path inverts that:
// the region stage decomposes the page first — propose, classify, suppress —
// and every kept region becomes Markdown according to its class. Tables run
// the existing grid machinery, but only inside boxes the MODEL said are
// tables; text the model claims for a picture is dropped; headings and lists
// come straight from region classes.
//
// It is behind its own flag because it is honest about what it is: the first
// end-to-end "the model owns the page" renderer, measured at weighted F1 0.55
// against DocLayNet regions. That is not better than the tuned pipeline on
// clean documents yet. The flag exists so the difference can be SEEN on real
// documents rather than argued from benchmark deltas.
//
// When the region models are unavailable this falls back to the routed path:
// a missing model degrades to yesterday's behaviour, never to an empty page.

func pageMarkdownBlocksRegionRouted(ctx context.Context, cells []page.TextCell, wordCells []page.TextCell, rulings []page.RulingSegment, formFields []page.FormField, size geom.Size, options ExtractionOptions) ([]markdownBlock, error) {
	if available, _ := ProposalModelAvailable(); !available {
		return pageMarkdownBlocksRouted(ctx, cells, wordCells, rulings, formFields, size, options)
	}

	if options.ReadingOrder {
		cells = orderCells(cells, size)
		if len(wordCells) > 0 {
			wordCells = orderCells(wordCells, size)
		}
	}
	var formBlocks []markdownBlock
	if len(formFields) > 0 {
		cells = reserveCellIndexGaps(cells)
		if len(wordCells) > 0 {
			wordCells = reserveCellIndexGaps(wordCells)
		}
		formBlocks = formFieldMarkdownBlocks(formFields, cells, size)
	}

	lines := AssembleLineElements(cells, ParagraphOptions{}.withDefaults().LineTolerance)
	labeller := newLineLabeller(lines, cells, size, rulings)
	if !labeller.ok {
		return pageMarkdownBlocksRouted(ctx, cells, wordCells, rulings, formFields, size, options)
	}

	gapCells := wordCells
	if len(gapCells) == 0 {
		gapCells = cells
	}
	regions := PageRegions(lines, options.drawn, labeller.labels, gapCells, rulings, size)

	// Each line belongs to the highest-ranked region that claims it. Regions
	// come out of SelectRegions rank-ordered, so first claim wins, and a line
	// no region claims falls through to the leftover pass below — nothing on
	// the page is ever silently dropped except what a Picture region absorbs.
	claimed := make([]bool, len(lines))
	var blocks []markdownBlock
	for _, region := range regions {
		member := make([]ParagraphTextLine, 0, len(region.Proposal.Lines))
		for _, index := range region.Proposal.Lines {
			if index >= 0 && index < len(lines) && !claimed[index] {
				claimed[index] = true
				member = append(member, lines[index])
			}
		}
		if len(member) == 0 && region.Class != layoutClassPicture {
			continue
		}
		block, ok := regionBlock(region, member, wordCells, rulings, size, options)
		if ok {
			blocks = append(blocks, block)
		}
	}

	// Leftovers: lines no region claimed. Grouped on geometry alone — the same
	// atomic grouping the proposer uses — and rendered as plain paragraphs.
	var leftovers []ParagraphTextLine
	for i, line := range lines {
		if !claimed[i] {
			leftovers = append(leftovers, line)
		}
	}
	for _, group := range atomicLineGroups(leftovers, coarseGapRatio) {
		var texts []string
		box := geom.Box{}
		index := int(^uint(0) >> 1)
		for j, lineIndex := range group.lines {
			line := leftovers[lineIndex]
			texts = append(texts, strings.TrimSpace(line.Text))
			if j == 0 {
				box = line.BBox
			} else {
				box = unionBoxes(box, line.BBox)
			}
			if line.MinIndex < index {
				index = line.MinIndex
			}
		}
		if text := strings.TrimSpace(strings.Join(texts, " ")); text != "" {
			blocks = append(blocks, markdownBlock{
				Index: index, Text: text, Box: box,
				LineCount: len(group.lines),
			})
		}
	}

	blocks = append(blocks, formBlocks...)
	sortMarkdownBlocks(blocks, size, !options.ReadingOrder)
	return blocks, nil
}

// regionBlock renders one kept region according to its class.
func regionBlock(region ScoredProposal, member []ParagraphTextLine, wordCells []page.TextCell, rulings []page.RulingSegment, size geom.Size, options ExtractionOptions) (markdownBlock, bool) {
	box := region.Proposal.Box
	index := int(^uint(0) >> 1)
	var texts []string
	for _, line := range member {
		texts = append(texts, strings.TrimSpace(line.Text))
		if line.MinIndex < index {
			index = line.MinIndex
		}
	}
	text := strings.TrimSpace(strings.Join(texts, " "))

	switch region.Class {
	case layoutClassPicture:
		// The region IS the picture; the lines inside it are its innards —
		// axis labels, legend fragments — and dropping them is the point.
		// Captions are their own regions and survive on their own merits.
		//
		// But only when there is INK. A real figure contains an image, paths,
		// or a shading; a "Picture" made of nothing but text lines is a
		// misclassification wearing a green box, and honouring it deletes a
		// paragraph. Measured on DPBench, region-routed output was keeping
		// 90.7% of the default path's characters, and text-only Picture
		// regions were the largest hole. The classification is the model's
		// call; destroying text on its say-so alone is not.
		if region.Proposal.Ink.Ink > 0 {
			return markdownBlock{}, false
		}
		if text == "" {
			return markdownBlock{}, false
		}
		return markdownBlock{Index: index, Box: box, LineCount: len(member), Text: text}, true

	case layoutClassSectionHeader, layoutClassTitle:
		level := 2
		if region.Class == layoutClassTitle {
			level = 1
		}
		if text == "" {
			return markdownBlock{}, false
		}
		return markdownBlock{
			Index: index, Box: box, LineCount: len(member),
			HeadingLevel: level,
			Text:         strings.Repeat("#", level) + " " + text,
		}, true

	case layoutClassListItem:
		// One region may hold several visual items; each LINE that starts
		// with a marker starts an item, continuation lines join the previous
		// one. The existing rewriteListItem strips the literal marker.
		var items []string
		for _, line := range member {
			lineText := strings.TrimSpace(line.Text)
			if lineText == "" {
				continue
			}
			if rewritten, ok := rewriteListItem(lineText); ok {
				items = append(items, rewritten)
			} else if len(items) > 0 {
				items[len(items)-1] += " " + lineText
			} else {
				items = append(items, "- "+lineText)
			}
		}
		if len(items) == 0 {
			return markdownBlock{}, false
		}
		return markdownBlock{
			Index: index, Box: box, LineCount: len(member),
			Text: strings.Join(items, "\n"),
		}, true

	case layoutClassTable:
		// The model owns WHERE the table is; the existing grid machinery owns
		// its structure, running only on the cells inside the model's box.
		// This is the division of labour the whole cascade was built for.
		//
		// Two rules here exist because their absence LOST text, measured as a
		// per-character drop in the output. Every table the detector finds in
		// the box is rendered, not just the first. And the cells the detector
		// hands back as NOT part of any grid are re-attached as prose — a cell
		// inside the model's box but outside the detector's grid belongs to
		// the document, and a wrong table is bad but vanished text is worse.
		inside := cellsInsideRegion(wordCells, box)
		if len(inside) >= 4 {
			detected := doctable.DetectTables(inside, rulings, options.TableDetection)
			if len(detected.Tables) > 0 {
				parts := make([]string, 0, len(detected.Tables)+1)
				var firstData *doctable.Data
				var firstBox geom.Box
				for _, table := range detected.Tables {
					markdown, err := render.Table(table.Data)
					if err != nil {
						continue
					}
					parts = append(parts, strings.TrimSpace(markdown))
					if firstData == nil {
						tableData := table.Data
						firstData = &tableData
						firstBox = table.Box
					}
				}
				if leftover := strings.TrimSpace(joinCellTexts(detected.TextCells)); leftover != "" {
					parts = append(parts, leftover)
				}
				if len(parts) > 0 {
					return markdownBlock{
						Index: index, Box: box,
						Text:      strings.Join(parts, "\n\n"),
						tableData: firstData,
						tableBox:  firstBox,
					}, true
				}
			}
		}
		// No grid found inside the box: the region keeps its content as prose.
		if text == "" {
			return markdownBlock{}, false
		}
		return markdownBlock{Index: index, Box: box, LineCount: len(member), Text: text}, true

	default:
		// Text, Caption, Formula, Footnote, Page-header, Page-footer: prose.
		if text == "" {
			return markdownBlock{}, false
		}
		return markdownBlock{Index: index, Box: box, LineCount: len(member), Text: text}, true
	}
}

// joinCellTexts flattens leftover cells into reading-order prose.
func joinCellTexts(cells []page.TextCell) string {
	texts := make([]string, 0, len(cells))
	for _, cell := range cells {
		if t := strings.TrimSpace(cell.Text); t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, " ")
}

// cellsInsideRegion returns the cells at least half inside box.
func cellsInsideRegion(cells []page.TextCell, box geom.Box) []page.TextCell {
	var out []page.TextCell
	for _, cell := range cells {
		if containedFraction(cell.Box, box) >= 0.5 {
			out = append(out, cell)
		}
	}
	return out
}
