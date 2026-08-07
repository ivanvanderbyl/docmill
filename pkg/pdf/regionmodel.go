package pdf

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/ivanvanderbyl/docmill/v2/pkg/gbm"
)

// The REGION model: does this candidate region stand?
//
// Second stage of the cascade. The line model proposes runs of same-label
// lines; this accepts or rejects them using features no line can express —
// gutter persistence, column-count stability, row regularity, ruling coverage,
// and the distribution of line labels inside the run.
//
// It exists because two measurements demanded it. Handing table routing to the
// line model moved Table F1 by 0.008, and the learned column model turned a
// display equation into a four-column table because nothing had asked "is this
// a table at all". Both are region questions.
//
// On DocLayNet val the gate is worth most exactly where the line model is worst:
// Picture candidates are correct only 5.7% of the time and the gate identifies
// them at 0.782 precision / 0.710 recall; Table candidates are correct 15.6% of
// the time, gated at 0.671 / 0.688.

//go:embed regionmodel.bin
var regionModelBlob []byte

var regionModel = sync.OnceValues(func() (*gbm.Ensemble, error) {
	model, err := gbm.Decode(regionModelBlob)
	if err != nil {
		return nil, err
	}
	if model.NumFeatures() != len(RegionFeatureNames) {
		return nil, fmt.Errorf("region model expects %d features, pkg/pdf defines %d — the feature contract has drifted",
			model.NumFeatures(), len(RegionFeatureNames))
	}
	return model, nil
})

// RegionModelAvailable reports whether the embedded region model decoded.
func RegionModelAvailable() (bool, error) {
	model, err := regionModel()
	return model != nil && err == nil, err
}

// acceptRegion reports whether the candidate should stand.
//
// >= 0.5 is argmax over accept/reject, not a tuned cutoff: the sigmoid is
// monotonic, so this says "accept outscores reject". When the model is
// unavailable it accepts everything, which reproduces the ungated behaviour
// rather than silently discarding regions.
func acceptRegion(features []float64) bool {
	model, err := regionModel()
	if err != nil || model == nil {
		return true
	}
	return model.PredictBinary(features) >= 0.5
}
