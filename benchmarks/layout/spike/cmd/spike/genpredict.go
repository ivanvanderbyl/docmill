package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// The generated-model predictor. Same arithmetic as leaves, no dependency and
// no parse at start-up: the trees are compile-time constant arrays.

// genDecision reproduces LightGBM's numerical split exactly, and by extension
// leaves' numericalDecision. Two details matter and both are load-bearing:
//
//   - LightGBM compares with `<=`, not `<` (XGBoost uses `<`).
//   - With missing_type None — which "spike gen" has verified holds for every
//     split in this model — a NaN feature is treated as 0.0 rather than sent
//     down the default branch.
//
// Getting either wrong produces a model that runs fine and scores subtly
// differently, which is the failure mode the plan warns about.
func genDecision(node int, value float64) bool {
	if math.IsNaN(value) {
		value = 0
	}
	return value <= genNodeThreshold[node]
}

// genNode is the interleaved node record: one cache line holds two of them, so
// a node visit costs one memory fetch rather than one per parallel array.
type genNode struct {
	Threshold   float64
	Left, Right int32
	Feature     int32
}

// genPredictRawPacked is genPredictRaw over the interleaved layout. Both walk
// the identical trees and predict_test.go asserts they agree; they differ only
// in memory layout, which is what the benchmark measures.
//
// MEASURED: it does not pay. 17.8 µs/op packed against 17.4 µs/op flat — a
// wash, marginally worse. So the walk is not bound by cache lines per node; it
// is bound by branch misprediction on a data-dependent traversal, which no
// layout change fixes. Kept as the evidence for that conclusion, so Task 4 does
// not re-run the experiment. genPredict uses the flat layout.
func genPredictRawPacked(features []float64) float64 {
	sum := 0.0
	for tree := 0; tree < genTreeCount; tree++ {
		node := &genNodes[genTreeRoot[tree]]
		for {
			value := features[node.Feature]
			if math.IsNaN(value) {
				value = 0
			}
			next := node.Right
			if value <= node.Threshold {
				next = node.Left
			}
			if next < 0 {
				sum += genLeafValue[-next-1]
				break
			}
			node = &genNodes[next]
		}
	}
	return sum
}

// genPredictRaw sums the leaf values across every tree — the raw score, before
// the logistic transform.
func genPredictRaw(features []float64) float64 {
	sum := 0.0
	for tree := 0; tree < genTreeCount; tree++ {
		node := genTreeRoot[tree]
		for {
			var next int32
			if genDecision(int(node), features[genNodeFeature[node]]) {
				next = genNodeLeft[node]
			} else {
				next = genNodeRight[node]
			}
			if next < 0 {
				sum += genLeafValue[-next-1]
				break
			}
			node = next
		}
	}
	return sum
}

// genPredict returns P(formula), matching the sigmoid LightGBM's
// "binary sigmoid:1" objective applies and that leaves applies via
// transformation.TransformLogistic.
func genPredict(features []float64) float64 {
	return 1 / (1 + math.Exp(-genPredictRaw(features)))
}

// genExplain returns a human-readable decision path for one feature vector.
//
// This is the capability the plan could not get from leaves: Task 4 step 4 asks
// what introspection it exposes and says to record a gap against AGENTS.md
// ("deterministic, repeatable, and explainable") if a decision path cannot be
// reported. leaves exposes only scores. Generated code can walk the same trees
// and say WHY, so the gap closes rather than being recorded.
//
// The full path across 300 trees is unreadable, so this reports the trees that
// moved the score most, plus how often each feature was tested overall.
func genExplain(features []float64, names []string, topTrees int) string {
	type contribution struct {
		tree int
		leaf float64
		path []string
	}

	contributions := make([]contribution, 0, genTreeCount)
	featureUses := map[int]int{}

	for tree := 0; tree < genTreeCount; tree++ {
		node := genTreeRoot[tree]
		var path []string
		for {
			feature := int(genNodeFeature[node])
			threshold := genNodeThreshold[node]
			value := features[feature]
			featureUses[feature]++
			goLeft := genDecision(int(node), value)
			comparison := ">"
			if goLeft {
				comparison = "<="
			}
			path = append(path, fmt.Sprintf("%s=%.4g %s %.4g", names[feature], value, comparison, threshold))
			var next int32
			if goLeft {
				next = genNodeLeft[node]
			} else {
				next = genNodeRight[node]
			}
			if next < 0 {
				contributions = append(contributions, contribution{tree: tree, leaf: genLeafValue[-next-1], path: path})
				break
			}
			node = next
		}
	}

	sort.SliceStable(contributions, func(i, j int) bool {
		return math.Abs(contributions[i].leaf) > math.Abs(contributions[j].leaf)
	})

	var out strings.Builder
	raw := genPredictRaw(features)
	fmt.Fprintf(&out, "score=%.6f (raw %.6f, %d trees)\n", genPredict(features), raw, genTreeCount)
	fmt.Fprintf(&out, "top %d trees by absolute leaf contribution:\n", topTrees)
	for i, c := range contributions {
		if i >= topTrees {
			break
		}
		fmt.Fprintf(&out, "  tree %3d  leaf %+.5f  %s\n", c.tree, c.leaf, strings.Join(c.path, "  |  "))
	}
	out.WriteString("most-tested features on this vector:\n")
	for i, feature := range sortedFeatureIndexes(featureUses) {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&out, "  %-22s tested %d times, value %.4g\n", names[feature], featureUses[feature], features[feature])
	}
	return out.String()
}
