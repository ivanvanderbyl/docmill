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
func PageRegionProposals(ctx context.Context, doc Document, splitColumns bool) ([]PageProposals, error) {
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

		lines := AssembleLineElements(cells, ParagraphOptions{}.withDefaults().LineTolerance)
		if splitColumns {
			lines = SplitLinesAtColumnGaps(lines, size)
		}
		out = append(out, PageProposals{
			Page:      index + 1,
			Size:      size,
			Proposals: ProposeRegions(lines, GroupInkClusters(drawn, size), size),
		})
	}
	return out, nil
}
