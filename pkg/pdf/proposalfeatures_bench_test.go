package pdf

import (
	"fmt"
	doctable "github.com/ivanvanderbyl/docmill/v2/pkg/table"
	"testing"

	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// The region stage runs ProposalFeatures once per candidate, and the proposer
// offers ~375 candidates per page. Anything inside that function is multiplied
// by 375, so this measures the per-page cost rather than the per-call cost —
// the per-call number is the one that looks harmless.
func benchmarkPage(lines, cells int) ProposalFeatureInput {
	in := ProposalFeatureInput{Size: geom.Size{Width: 612, Height: 792}}
	for i := range lines {
		top := 60 + float64(i)*11
		line := ParagraphTextLine{
			BBox:     geom.Box{L: 60, T: top, R: 550, B: top + 9, Origin: geom.TopLeft},
			FontSize: 9,
		}
		for c := range cells {
			left := 60 + float64(c)*(490/float64(cells))
			cell := page.TextCell{
				Text: fmt.Sprintf("w%d", c),
				Box:  geom.Box{L: left, T: top, R: left + 30, B: top + 9, Origin: geom.TopLeft},
			}
			line.Cells = append(line.Cells, cell)
			in.Cells = append(in.Cells, cell)
		}
		in.Lines = append(in.Lines, line)
		in.Labels = append(in.Labels, "Text")
	}
	return in
}

func BenchmarkProposalFeaturesPage(b *testing.B) {
	in := benchmarkPage(60, 8)
	proposals := ProposeRegions(in.Lines, nil, in.Size)
	b.ReportMetric(float64(len(proposals)), "proposals/page")

	b.ResetTimer()
	for range b.N {
		for _, proposal := range proposals {
			ProposalFeatures(proposal, in)
		}
	}
}

func BenchmarkProposalFeaturesSingle(b *testing.B) {
	in := benchmarkPage(60, 8)
	proposals := ProposeRegions(in.Lines, nil, in.Size)
	if len(proposals) == 0 {
		b.Skip("no proposals")
	}
	proposal := proposals[len(proposals)/2]

	b.ResetTimer()
	for range b.N {
		ProposalFeatures(proposal, in)
	}
}

// BenchmarkColumnGapCandidates isolates the suspected cost. ProposalFeatures
// calls it once per candidate, so its per-call cost is multiplied by the
// proposal count before it reaches a page.
func BenchmarkColumnGapCandidates(b *testing.B) {
	in := benchmarkPage(60, 8)
	box := geom.Box{L: 60, T: 60, R: 550, B: 720, Origin: geom.TopLeft}
	b.ResetTimer()
	for range b.N {
		doctable.ColumnGapCandidates(in.Cells, in.Rulings, box)
	}
}
