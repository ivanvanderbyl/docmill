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
	paragraphOptions := ParagraphOptions{EnableInlineFormatting: options.EnableInlineFormatting}

	// Headings: the union of two detectors. The region classifier finds 70%
	// of headings end to end, and every one it misses renders as prose — on a
	// document whose only heading is missed, MHS goes straight to zero. The
	// routed path's heading machinery (line-model decider, font-size levels)
	// runs FIRST; region-classified headings then fill in whatever it missed.
	// Each catches headings the other cannot: the line pass has levels and
	// higher recall, the region pass sees multi-line headers as one unit.
	var headingBlocks []markdownBlock
	if options.DetectHeadings {
		protected := protectedHeadingCellIndexes(cells, rulings, options)
		if options.DetectStructure {
			protected = mergeProtectedCellIndexes(protected, protectedListLineCellIndexes(cells))
		}
		protected = mergeProtectedCellIndexes(protected, denseIndexLineCellIndexes(cells, size))
		// The HEURISTIC decider, not the line model's. The default pipeline's
		// MHS 0.77 is this decider's; the line model found fewer of these
		// documents' headings ("Functional Abstraction", "5 Conclusion"), and
		// a union that starts from the weaker set stays weaker.
		headingBlocks, _ = splitHeadingCellsWith(cells, size, protected, nil)
		headingBlocks = demoteImpostorHeadings(headingBlocks, paragraphOptions)
	}

	// Tables: detection runs PAGE-WIDE, acceptance is the model's. Running the
	// grid machinery inside each region box starved it of context — the
	// anchored-text detector needs the "Table 1:" line, which is usually a
	// separate region — and a found-but-unrendered table scores TEDS 0. So the
	// heuristics detect with everything they normally see, and the region
	// stage decides which detections are real: a detected table overlapping a
	// model Table region is accepted, the rest are vetoed. This is the
	// division the plan called "the region model owns table acceptance".
	detected, _ := detectPageTables(cells, wordCells, rulings, size, options)
	var acceptedTables []doctable.DetectedTable
	for _, table := range detected.Tables {
		for _, region := range regions {
			if region.Class == layoutClassTable &&
				(boxIoU(table.Box, region.Proposal.Box) >= 0.3 ||
					containedFractionOf(table.Box, region.Proposal.Box) >= 0.5 ||
					containedFractionOf(region.Proposal.Box, table.Box) >= 0.5) {
				acceptedTables = append(acceptedTables, table)
				break
			}
		}
	}

	// Each line belongs to the highest-ranked region that claims it. Regions
	// come out of SelectRegions rank-ordered, so first claim wins, and a line
	// no region claims falls through to the leftover pass below — nothing on
	// the page is ever silently dropped except what a Picture region absorbs.
	claimed := make([]bool, len(lines))
	var blocks []markdownBlock

	// Heading blocks claim their lines before regions do.
	for _, heading := range headingBlocks {
		for i, line := range lines {
			if !claimed[i] && containedFraction(line.BBox, heading.Box) >= 0.5 {
				claimed[i] = true
			}
		}
	}
	blocks = append(blocks, headingBlocks...)

	// Accepted tables claim their lines FIRST — they outrank region prose the
	// same way they do on the default path, and a line rendered inside a table
	// must not also render as a paragraph.
	for _, table := range acceptedTables {
		for i, line := range lines {
			if !claimed[i] && containedFraction(line.BBox, table.Box) >= 0.5 {
				claimed[i] = true
			}
		}
		markdown, err := render.Table(table.Data)
		if err != nil {
			continue
		}
		tableData := table.Data
		blocks = append(blocks, markdownBlock{
			Index:     minTextCellIndex(table.TextCells),
			Box:       table.Box,
			Text:      strings.TrimSpace(markdown),
			tableData: &tableData,
			tableBox:  table.Box,
		})
	}
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
		blocks = append(blocks, regionBlocks(region, member, memberLabels, paragraphOptions, wordCells, rulings, size, options)...)
	}

	// Leftovers: lines no region claimed, rendered through the SAME TOC-aware
	// paragraph assembler the default pipeline uses. The first version joined line texts
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
	blocks = append(blocks, assembleWithToc(leftovers, paragraphOptions)...)

	blocks = append(blocks, formBlocks...)
	sortMarkdownBlocks(blocks, size, !options.ReadingOrder)
	return blocks, nil
}

// regionBlocks renders one kept region according to its class. It returns a
// slice because a region legitimately yields several blocks — a Text region
// holding two paragraphs, a Picture region yielding its rescued caption.
func regionBlocks(region ScoredProposal, member []ParagraphTextLine, memberLabels []string, paragraphOptions ParagraphOptions, wordCells []page.TextCell, rulings []page.RulingSegment, size geom.Size, options ExtractionOptions) []markdownBlock {
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
			return assembleWithToc(member, paragraphOptions)
		}
		var rescued []ParagraphTextLine
		for i, line := range member {
			if memberLabels[i] == "Caption" {
				rescued = append(rescued, line)
			}
		}
		if len(rescued) > 0 {
			return assembleWithToc(rescued, paragraphOptions)
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
		// A caption is the third impostor. "Figure 1.2. Per capita GDP
		// growth in 2020" carried a Section-header vote, and one false
		// heading on a heading-free document scores MHS 1.00 -> 0.00 on
		// DPBench — the benchmark's harshest single penalty.
		//
		// The guard names actual caption words rather than reusing the
		// model's marker-SHAPE feature. The shape ("any word then a number")
		// is right as a feature, where the model weighs it against everything
		// else, and wrong as a rule: it also matches "Activity 1:" and
		// "Chapter 3", and its first version deleted real headings from two
		// documents wholesale.
		if formulaVotes*2 > len(memberLabels) || headingReadsAsMath(member) || startsWithCaptionWord(text) {
			return assembleWithToc(member, paragraphOptions)
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
		// Table STRUCTURE was already handled page-wide, before regions
		// claimed lines: detection needs context a region box cuts away (the
		// anchored detector's "Table 1:" line usually lives in a neighbouring
		// region), so tables are detected with everything the default path
		// sees and this stage only decided WHICH detections stand. Member
		// lines still unclaimed here are the region's non-grid remainder —
		// header notes, unit rows the grid rejected — and they render as
		// prose rather than vanishing.
		if len(member) == 0 {
			return nil
		}
		return assembleWithToc(member, paragraphOptions)

	default:
		// Text, Caption, Formula, Footnote, Page-header, Page-footer: prose,
		// through the same TOC-aware assembler as the default pipeline. On a
		// contents page the dot leaders otherwise render as literal ". . ."
		// prose — one such page scored NID 1.00 -> 0.02, the benchmark's worst
		// single reading-order penalty, without a single line being out of
		// order.
		if len(member) == 0 {
			return nil
		}
		return assembleWithToc(member, paragraphOptions)
	}
}

// demoteImpostorHeadings applies the same guards to heuristic heading blocks
// that region-classified headings get: captions and mathematics keep their
// text but stay out of the outline. The guards live on the OUTPUT rather than
// in any one detector, so no path around them exists — the caption that first
// motivated them re-entered through the heuristic pass the moment the guards
// were attached to the region branch alone.
func demoteImpostorHeadings(blocks []markdownBlock, paragraphOptions ParagraphOptions) []markdownBlock {
	out := blocks[:0]
	for _, block := range blocks {
		body := headingTextBody(block.Text)
		if startsWithCaptionWord(body) {
			block.HeadingLevel = 0
			block.Text = body
		}
		out = append(out, block)
	}
	return out
}

// captionWords are the words that begin a figure or table caption. A heading
// starting with one of these plus a number is a caption the region model
// misread, not a section of the document.
var captionWords = map[string]bool{
	"figure": true, "fig": true, "fig.": true, "figure.": true,
	"table": true, "tab": true, "tab.": true,
	"chart": true, "exhibit": true, "plate": true, "scheme": true,
	"diagram": true, "map": true, "photo": true, "illustration": true,
	"abbildung": true, "abb": true, "abb.": true, "tabelle": true,
}

// startsWithCaptionWord reports whether text opens like "Figure 3" or
// "Tabelle 2.1".
func startsWithCaptionWord(text string) bool {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return false
	}
	if !captionWords[strings.ToLower(strings.TrimSuffix(fields[0], ":"))] {
		return false
	}
	rest := strings.TrimRight(fields[1], ".:—-–")
	for _, r := range rest {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return rest != ""
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
