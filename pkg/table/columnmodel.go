package table

import (
	_ "embed"
	"fmt"
	"sort"
	"sync"

	"github.com/ivanvanderbyl/docmill/v2/pkg/gbm"
	"github.com/ivanvanderbyl/docmill/v2/pkg/geom"
	"github.com/ivanvanderbyl/docmill/v2/pkg/page"
)

// The learned column-boundary model, trained on FinTabNet's human-annotated
// grids. It replaces the densest-row heuristic in reconstructColumns, which
// assumes the row with the most cells has exactly one cell per column — true
// for a clean header row, wrong whenever the widest row wraps or merges.
//
// Held out on FinTabNet's own val split: 0.865 precision and 0.887 recall per
// boundary, with 65.4% of tables recovered with EVERY boundary correct.

//go:embed columnmodel.bin
var columnModelBlob []byte

var columnModel = sync.OnceValues(func() (*gbm.Ensemble, error) {
	model, err := gbm.Decode(columnModelBlob)
	if err != nil {
		return nil, err
	}
	// A model trained against a different feature vector must fail loudly
	// rather than score confidently wrong.
	if model.NumFeatures() != len(ColumnGapFeatureNames) {
		return nil, fmt.Errorf("column model expects %d features, pkg/table defines %d — the feature contract has drifted",
			model.NumFeatures(), len(ColumnGapFeatureNames))
	}
	return model, nil
})

// ColumnModelAvailable reports whether the embedded column model decoded, and
// why not if it did not.
func ColumnModelAvailable() (bool, error) {
	model, err := columnModel()
	return model != nil && err == nil, err
}

// learnedColumnBoxes derives column boxes by asking the model which candidate
// gaps are real boundaries.
//
// It returns nil when the model is unavailable or accepts too few boundaries,
// so callers fall back to the existing derivation rather than losing the table
// entirely. A wrong grid is bad; no table at all is worse.
func learnedColumnBoxes(cells []page.TextCell, rulings []page.RulingSegment, tableBox geom.Box) []geom.Box {
	model, err := columnModel()
	if err != nil || model == nil || tableBox.Width() <= 0 {
		return nil
	}
	candidates := ColumnGapCandidates(cells, rulings, tableBox)
	if len(candidates) == 0 {
		return nil
	}

	splits := make([]float64, 0, len(candidates))
	for _, candidate := range candidates {
		// >= 0.5 is argmax over the two classes, not a tuned cutoff: the
		// sigmoid is monotonic, so this says "boundary outscores not-boundary".
		if model.PredictBinary(candidate.Features) >= 0.5 {
			splits = append(splits, candidate.Center())
		}
	}
	if len(splits) == 0 {
		return nil
	}
	sort.Float64s(splits)

	boxes := make([]geom.Box, 0, len(splits)+1)
	left := tableBox.L
	for _, split := range splits {
		if split <= left || split >= tableBox.R {
			continue
		}
		boxes = append(boxes, geom.Box{L: left, T: tableBox.T, R: split, B: tableBox.B, Origin: tableBox.Origin})
		left = split
	}
	boxes = append(boxes, geom.Box{L: left, T: tableBox.T, R: tableBox.R, B: tableBox.B, Origin: tableBox.Origin})
	if len(boxes) < gridMinCols {
		return nil
	}
	return boxes
}

// ExplainColumnGap reports why the model accepted or rejected one candidate
// boundary — the explainability requirement, for the structure model.
func ExplainColumnGap(features []float64, topTrees int) string {
	model, err := columnModel()
	if err != nil || model == nil {
		return ""
	}
	score := model.PredictBinary(features)
	verdict := "rejected"
	if score >= 0.5 {
		verdict = "accepted"
	}
	return fmt.Sprintf("%s (p=%.4f)\n%s", verdict, score, model.Explain(features, ColumnGapFeatureNames, 0, topTrees))
}
