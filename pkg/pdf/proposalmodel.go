package pdf

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/ivanvanderbyl/docmill/v2/pkg/gbm"
	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// The region CLASSIFIER: what is this candidate, and how sure are we?
//
// Its predecessor was a gate. Candidates arrived already carrying a class,
// because they were built by grouping lines that shared a predicted label, so
// the model only had to answer "keep it?". That framing is why it could never
// fix an extent: a table proposed one line short and the same table proposed
// correctly are each plausible in isolation, and a gate only ever sees one at a
// time.
//
// The new proposer splits on geometry and clusters ink, so a candidate arrives
// with no opinion about what it is. Assigning the class is therefore part of the
// job — and the useful side effect is a confidence that is comparable ACROSS
// candidates, which is what lets SelectRegions run non-max suppression over the
// ~375 proposals a page produces.

//go:embed proposalmodel.bin
var proposalModelBlob []byte

var proposalModel = sync.OnceValues(func() (*gbm.Ensemble, error) {
	model, err := gbm.Decode(proposalModelBlob)
	if err != nil {
		return nil, err
	}
	// A model trained against a different feature vector must fail loudly
	// rather than score confidently wrong.
	if model.NumFeatures() != len(ProposalFeatureNames) {
		return nil, fmt.Errorf("proposal model expects %d features, pkg/pdf defines %d — the feature contract has drifted",
			model.NumFeatures(), len(ProposalFeatureNames))
	}
	if model.NumClasses() != len(proposalLabelOrder) {
		return nil, fmt.Errorf("proposal model has %d classes, pkg/pdf names %d",
			model.NumClasses(), len(proposalLabelOrder))
	}
	return model, nil
})

// ProposalModelAvailable reports whether the embedded region classifier
// decoded, and why not if it did not.
func ProposalModelAvailable() (bool, error) {
	model, err := proposalModel()
	return model != nil && err == nil, err
}

// ClassifyProposals scores every candidate and returns them with their class.
//
// It returns nil when the model is unavailable, which callers must read as "no
// opinion" rather than "no regions" — the same contract the column model uses,
// so a missing model degrades to the previous behaviour instead of erasing the
// page.
func ClassifyProposals(proposals []RegionProposal, in ProposalFeatureInput) []ScoredProposal {
	model, err := proposalModel()
	if err != nil || model == nil || len(proposals) == 0 {
		return nil
	}

	backgroundIndex := 0 // proposalLabelOrder[0]
	scored := make([]ScoredProposal, 0, len(proposals))
	for _, proposal := range proposals {
		probabilities := model.PredictProbabilities(ProposalFeatures(proposal, in))
		if len(probabilities) != len(proposalLabelOrder) {
			return nil
		}
		best := 0
		for i, p := range probabilities {
			if p > probabilities[best] {
				best = i
			}
		}
		scored = append(scored, ScoredProposal{
			Proposal:   proposal,
			Class:      proposalLabelOrder[best],
			Score:      probabilities[best],
			Background: probabilities[backgroundIndex],
		})
	}
	return scored
}

// PageRegions is the end-to-end region stage: propose, classify, suppress.
//
// One function so that training and inference cannot drift. The emitter calls
// the same proposer and the same feature extractor, and this adds only the two
// steps a trained model makes possible.
func PageRegions(lines []ParagraphTextLine, drawn []page.DrawnObject, labels []string, cells []page.TextCell, rulings []page.RulingSegment, size geom.Size) []ScoredProposal {
	proposals := ProposeRegions(lines, GroupInkClusters(drawn, size), size)
	scored := ClassifyProposals(proposals, ProposalFeatureInput{
		Lines:   lines,
		Labels:  labels,
		Cells:   cells,
		Rulings: rulings,
		Size:    size,
	})
	return SelectRegions(scored)
}

// ExplainProposal reports why the classifier called a candidate what it did —
// the explainability requirement, for the region stage.
func ExplainProposal(features []float64, topTrees int) string {
	model, err := proposalModel()
	if err != nil || model == nil {
		return ""
	}
	probabilities := model.PredictProbabilities(features)
	best := 0
	for i, p := range probabilities {
		if p > probabilities[best] {
			best = i
		}
	}
	return fmt.Sprintf("%s (p=%.4f)\n%s", proposalLabelOrder[best], probabilities[best],
		model.Explain(features, ProposalFeatureNames, best, topTrees))
}
