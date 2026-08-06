package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// fixture is the Python-side ground truth written by train.py: feature vectors
// and the scores LightGBM itself produced for them. Replaying it through leaves
// is the check that the Go predictor and the Python trainer agree — the plan
// calls this out as the test that catches the two realistic porting failures,
// an index shift in the feature vector and float drift in the tree walk.
type fixture struct {
	Features []string `json:"features"`
	Cases    []struct {
		F     []float64 `json:"f"`
		Score float64   `json:"score"`
	} `json:"cases"`
}

// scoreTolerance is a float-comparison epsilon, not a tuned quality bar: leaves
// and LightGBM sum the same leaf values in the same order, so anything beyond
// double-rounding noise means the port is wrong.
const scoreTolerance = 1e-9

func runVerify(args []string) error {
	path := "benchmarks/layout/spike/out/fixture.json"
	if len(args) > 0 {
		path = args[0]
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var fx fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return err
	}

	if len(fx.Features) != len(featureNames) {
		return fmt.Errorf("fixture declares %d features, emitter declares %d", len(fx.Features), len(featureNames))
	}
	for i, name := range fx.Features {
		if name != featureNames[i] {
			return fmt.Errorf("feature %d: fixture says %q, emitter says %q", i, name, featureNames[i])
		}
	}

	model, err := layoutModel()
	if err != nil {
		return fmt.Errorf("load embedded model: %w", err)
	}

	worst := 0.0
	for i, c := range fx.Cases {
		got := model.PredictSingle(c.F, 0)
		delta := math.Abs(got - c.Score)
		if delta > worst {
			worst = delta
		}
		if delta > scoreTolerance {
			return fmt.Errorf("case %d: leaves=%.17g lightgbm=%.17g (delta %.3g)", i, got, c.Score, delta)
		}
	}
	fmt.Printf("ok: %d fixture cases agree, worst delta %.3g\n", len(fx.Cases), worst)
	return nil
}
