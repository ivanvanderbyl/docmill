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
	paragraphOptions := ParagraphOptions{EnableInlineFormatting: options.EnableInlineFormatting}
	claimed := make([]bool, len(lines))
	var blocks []markdownBlock
	for _, region := range regions {
		member := make([]ParagraphTextLine, 0, len(region.Proposal.Lines))
		memberLabels := make([]string, 0, len(region.Proposal.Lines))
		for _, index := range region.Proposal.Lines {
			if index >= 0 && index < len(lines) && !claimed[index] {
				claimed[index] = true
				member = append(member, lines[index])
				memberLabels = append(memberLabels, labeller.labels[index])
			}
		}
		if len(member) == 0 && region.Class != layoutClassPicture {
			continue
		}
		blocks = append(blocks, regionBlocks(region, member, memberLabels, paragraphOptions, wordCells, rulings, options)...)
	}

	// Leftovers: lines no region claimed, rendered through the SAME paragraph
	// assembler the default pipeline uses. The first version joined line texts
	// with spaces, and the seams showed: "Vol. 27, pp." came out "Vol . 27 ,
	// pp ." because the assembler's spacing decisions were being redone badly,
	// and hyphenated line breaks lost their hyphens. One text builder, both
	// paths.
	var leftovers []ParagraphTextLine
	for i, line := range lines {
		if !claimed[i] {
			leftovers = append(leftovers, line)
		}
	}
	blocks = append(blocks, assembleParagraphs(leftovers, paragraphOptions)...)

	blocks = append(blocks, formBlocks...)
	sortMarkdownBlocks(blocks, size, !options.ReadingOrder)
	return blocks, nil
}

// regionBlocks renders one kept region according to its class. It returns a
// slice because a region legitimately yields several blocks — a Text region
// holding two paragraphs, a Picture region yielding its rescued caption.
func regionBlocks(region ScoredProposal, member []ParagraphTextLine, memberLabels []string, paragraphOptions ParagraphOptions, wordCells []page.TextCell, rulings []page.RulingSegment, options ExtractionOptions) []markdownBlock {
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
		// Two guards temper that. A "Picture" with no INK is a
		// misclassification wearing a green box, and its text survives as
		// prose. And a member line the LINE model calls Caption is rescued:
		// on the held-out Shannon sample the picture box over-reached by one
		// line and silently ate "Fig. 1 — Schematic diagram of a general
		// communication system". The region model owns the figure; it does
		// not own the caption's deletion when a second model disagrees.
		if region.Proposal.Ink.Ink == 0 {
			if len(member) == 0 {
				return nil
			}
			return assembleParagraphs(member, paragraphOptions)
		}
		var rescued []ParagraphTextLine
		for i, line := range member {
			if memberLabels[i] == "Caption" {
				rescued = append(rescued, line)
			}
		}
		if len(rescued) > 0 {
			return assembleParagraphs(rescued, paragraphOptions)
		}
		return nil

	case layoutClassSectionHeader, layoutClassTitle:
		// Two guards before anything becomes part of the document outline.
		//
		// If the LINE model calls the lines Formula, the region model has been
		// fooled by geometry — short, centred, isolated is the shape of a
		// title, worn by mathematics.
		//
		// And if the TEXT is dense with digits and operators, both models have
		// been fooled: on the held-out Shannon sample "log2 M log10 M log10 2"
		// carried Section-header votes from both, because nothing geometric
		// distinguishes it from a heading. A third of its characters are
		// digits, and no real heading reads like that. This is a renderer
		// guard, not a classification: the text stays, only its promotion into
		// the outline is refused.
		formulaVotes := 0
		for _, label := range memberLabels {
			if label == "Formula" {
				formulaVotes++
			}
		}
		if text == "" {
			return nil
		}
		if formulaVotes*2 > len(memberLabels) || headingReadsAsMath(member) {
			return assembleParagraphs(member, paragraphOptions)
		}
		level := 2
		if region.Class == layoutClassTitle {
			level = 1
		}
		return []markdownBlock{{
			Index: index, Box: box, LineCount: len(member),
			HeadingLevel: level,
			Text:         strings.Repeat("#", level) + " " + text,
		}}

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
			return nil
		}
		return []markdownBlock{{
			Index: index, Box: box, LineCount: len(member),
			Text: strings.Join(items, "\n"),
		}}

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
					return []markdownBlock{{
						Index: index, Box: box,
						Text:      strings.Join(parts, "\n\n"),
						tableData: firstData,
						tableBox:  firstBox,
					}}
				}
			}
		}
		// No grid found inside the box: the region keeps its content as prose.
		if len(member) == 0 {
			return nil
		}
		return assembleParagraphs(member, paragraphOptions)

	default:
		// Text, Caption, Formula, Footnote, Page-header, Page-footer: prose,
		// through the same paragraph assembler as the default pipeline.
		if len(member) == 0 {
			return nil
		}
		return assembleParagraphs(member, paragraphOptions)
	}
}

// headingReadsAsMath reports whether a would-be heading's characters are
// dominated by digits and mathematical symbols.
func headingReadsAsMath(member []ParagraphTextLine) bool {
	mathChars, digitChars, totalChars := 0, 0, 0
	for _, line := range member {
		counts := countLineRunes(line)
		mathChars += counts.math
		digitChars += counts.digit
		totalChars += counts.chars
	}
	if totalChars == 0 {
		return false
	}
	return float64(mathChars+digitChars)/float64(totalChars) > 0.2
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
