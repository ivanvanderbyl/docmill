package pdf

import (
	"context"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// PageProposals is one page's candidate regions, for the training harness.
type PageProposals struct {
	Page      int
	Size      geom.Size
	Proposals []RegionProposal
	// Features is parallel to Proposals, empty when the line model is
	// unavailable (the frac_* features describe the line model's output, so a
	// vector computed without it would be a different vector wearing the same
	// contract).
	Features [][]float64
	// Classes and Scores are parallel to Proposals and set only when the
	// caller asked for selection. With selection on, Proposals holds ONLY the
	// candidates that survived non-max suppression, so the three stay aligned.
	Classes []string
	Scores  []float64
}

// PageRegionProposals runs the proposer over a document and returns every
// candidate it offers.
//
// It exists so the harness measures the SHIPPING proposer rather than a
// reimplementation of it. Every earlier ceiling number in this project came
// from Python standing in for Go, and the whole class of bug that costs a
// retrain is the two quietly disagreeing.
//
// The lines here are assembled class-agnostically — no figure drops, no table
// carve-outs — because that is the line set the classifier sees at inference.
func PageRegionProposals(ctx context.Context, doc Document, splitColumns, selected, suppress bool) ([]PageProposals, error) {
	count, err := doc.PageCount(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]PageProposals, 0, count)
	for index := range count {
		pdfPage, err := doc.Page(ctx, index)
		if err != nil {
			return nil, err
		}
		size, err := pdfPage.Size(ctx)
		if err != nil {
			return nil, err
		}
		cells, err := pdfPage.TextCells(ctx)
		if err != nil {
			return nil, err
		}

		var drawn []page.DrawnObject
		if provider, ok := pdfPage.(drawnObjectProvider); ok {
			if drawn, err = provider.DrawnObjects(ctx); err != nil {
				return nil, err
			}
		}

		var rulings []page.RulingSegment
		if provider, ok := pdfPage.(rulingSegmentProvider); ok {
			if rulings, err = provider.RulingSegments(ctx); err != nil {
				return nil, err
			}
		}
		// Word cells give the column-gap features their granularity; line-level
		// cells span whole rows and erase every internal gutter.
		gapCells := cells
		if provider, ok := pdfPage.(wordTextCellProvider); ok {
			if words, err := provider.WordTextCells(ctx); err == nil && len(words) > 0 {
				gapCells = words
			}
		}

		lines := AssembleLineElements(cells, ParagraphOptions{}.withDefaults().LineTolerance)
		if splitColumns {
			lines = SplitLinesAtColumnGaps(lines, size)
		}
		proposals := ProposeRegions(lines, GroupInkClusters(drawn, size), size)

		result := PageProposals{Page: index + 1, Size: size, Proposals: proposals}
		labeller := newLineLabeller(lines, cells, size, rulings)
		if !labeller.ok && selected {
			// With selection asked for, an unavailable line model means no
			// classification happened. Emitting the raw proposals here would
			// hand the caller hundreds of unclassified boxes wearing the same
			// shape as a decomposition.
			result.Proposals = nil
			out = append(out, result)
			continue
		}
		if labeller.ok {
			in := ProposalFeatureInput{
				Lines:   lines,
				Labels:  labeller.labels,
				Cells:   gapCells,
				Rulings: rulings,
				Size:    size,
			}
			if selected {
				scored := ClassifyProposals(proposals, in)
				kept := scored
				if suppress {
					kept = SelectRegions(scored)
				} else {
					// Classified but not suppressed: everything the model did
					// not call Background. Comparing the two isolates whether
					// recall is lost by the classifier or by suppression.
					kept = kept[:0]
					for _, candidate := range scored {
						if candidate.Class != "" && candidate.Class != layoutClassBackground {
							kept = append(kept, candidate)
						}
					}
				}
				result.Proposals = make([]RegionProposal, 0, len(kept))
				result.Classes = make([]string, 0, len(kept))
				result.Scores = make([]float64, 0, len(kept))
				for _, region := range kept {
					result.Proposals = append(result.Proposals, region.Proposal)
					result.Classes = append(result.Classes, region.Class)
					result.Scores = append(result.Scores, region.Score)
				}
			} else {
				result.Features = make([][]float64, 0, len(proposals))
				for _, proposal := range proposals {
					result.Features = append(result.Features, ProposalFeatures(proposal, in))
				}
			}
		}
		out = append(out, result)
	}
	return out, nil
}
