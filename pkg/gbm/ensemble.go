// Package gbm runs gradient-boosted tree ensembles compiled into docmill.
//
// It exists because two packages need the same runtime — pkg/pdf classifies
// lines, pkg/table finds column boundaries — and pkg/table cannot import
// pkg/pdf (the dependency runs the other way). Keeping one decoder here rather
// than a copy in each is what stops the two drifting apart.
//
// Models arrive as a packed binary blob produced at build time by
// benchmarks/layout/spike/cmd/packmodel. Nothing here parses LightGBM's text
// format, nothing is read from disk at run time, and nothing ships alongside
// the binary.
package gbm

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Magic identifies the blob format and its version. A model packed by an older
// build must fail loudly rather than be misread as the current layout.
const Magic = "DMLM0001"

// Ensemble is a decoded model: one flat node array across all trees, with
// absolute child links. A non-negative child is an internal node index; a
// negative child encodes leaf -(index+1).
type Ensemble struct {
	classes     []string
	numClass    int
	numFeatures int
	treeRoot    []int32
	nodeFeature []int32
	threshold   []float64
	left        []int32
	right       []int32
	leafValue   []float64
}

// Decode reads a packed model.
func Decode(blob []byte) (*Ensemble, error) {
	const header = len(Magic) + 6*4
	if len(blob) < header {
		return nil, fmt.Errorf("gbm: blob is %d bytes, shorter than its header", len(blob))
	}
	if string(blob[:len(Magic)]) != Magic {
		return nil, fmt.Errorf("gbm: bad magic %q", blob[:len(Magic)])
	}

	cursor := len(Magic)
	next32 := func() int {
		v := int(binary.LittleEndian.Uint32(blob[cursor:]))
		cursor += 4
		return v
	}
	model := &Ensemble{}
	model.numClass = next32()
	model.numFeatures = next32()
	trees := next32()
	nodes := next32()
	leaves := next32()
	nameBytes := next32()

	if cursor+nameBytes > len(blob) {
		return nil, fmt.Errorf("gbm: truncated class names")
	}
	if nameBytes > 0 {
		model.classes = strings.Split(string(blob[cursor:cursor+nameBytes]), "\x00")
	}
	cursor += nameBytes

	// Checked up front so a truncated artefact fails here with a clear message
	// rather than indexing out of bounds mid-decode.
	need := trees*4 + nodes*4 + nodes*8 + nodes*4 + nodes*4 + leaves*8
	if len(blob)-cursor != need {
		return nil, fmt.Errorf("gbm: expected %d bytes of arrays, found %d", need, len(blob)-cursor)
	}

	readInt32s := func(n int) []int32 {
		out := make([]int32, n)
		for i := range out {
			out[i] = int32(binary.LittleEndian.Uint32(blob[cursor:]))
			cursor += 4
		}
		return out
	}
	readFloats := func(n int) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = math.Float64frombits(binary.LittleEndian.Uint64(blob[cursor:]))
			cursor += 8
		}
		return out
	}

	model.treeRoot = readInt32s(trees)
	model.nodeFeature = readInt32s(nodes)
	model.threshold = readFloats(nodes)
	model.left = readInt32s(nodes)
	model.right = readInt32s(nodes)
	model.leafValue = readFloats(leaves)

	if model.numClass < 1 || trees%model.numClass != 0 {
		return nil, fmt.Errorf("gbm: %d trees for %d classes", trees, model.numClass)
	}
	if len(model.classes) != 0 && len(model.classes) != model.numClass {
		return nil, fmt.Errorf("gbm: %d class names for %d classes", len(model.classes), model.numClass)
	}
	return model, nil
}

// NumClasses, NumFeatures and Classes describe the decoded model.
func (e *Ensemble) NumClasses() int  { return e.numClass }
func (e *Ensemble) NumFeatures() int { return e.numFeatures }
func (e *Ensemble) Classes() []string {
	return append([]string(nil), e.classes...)
}

// RawScores accumulates each class's raw score into out, which must have
// NumClasses entries. LightGBM emits one tree per class per boosting round,
// interleaved, so tree i contributes to class i%numClass.
func (e *Ensemble) RawScores(features []float64, out []float64) {
	for i := range out {
		out[i] = 0
	}
	for tree, root := range e.treeRoot {
		node := root
		for {
			value := features[e.nodeFeature[node]]
			// LightGBM compares with `<=`, and with missing_type None — which
			// packmodel verifies for every split — a NaN feature is treated as
			// 0.0 rather than sent down the default branch.
			if math.IsNaN(value) {
				value = 0
			}
			next := e.right[node]
			if value <= e.threshold[node] {
				next = e.left[node]
			}
			if next < 0 {
				out[tree%e.numClass] += e.leafValue[-next-1]
				break
			}
			node = next
		}
	}
}

// PredictClass returns the winning class index and label, plus a softmax
// probability. The decision is a pure argmax over raw scores: softmax is
// monotonic so it cannot change the winner, and no per-class threshold enters
// anywhere. Cost asymmetry belongs in training weights, never in inference.
func (e *Ensemble) PredictClass(features []float64) (int, string, float64) {
	scores := make([]float64, e.numClass)
	e.RawScores(features, scores)

	best := 0
	for i := 1; i < len(scores); i++ {
		if scores[i] > scores[best] {
			best = i
		}
	}
	sum := 0.0
	for _, s := range scores {
		sum += math.Exp(s - scores[best])
	}
	probability := 1.0
	if sum > 0 {
		probability = 1 / sum
	}

	label := fmt.Sprintf("class_%d", best)
	if best < len(e.classes) {
		label = e.classes[best]
	}
	return best, label, probability
}

// PredictProbabilities returns the softmax over all classes.
//
// PredictClass returns only the winner's probability, which is enough for a
// routing decision but not for comparing candidates: non-max suppression needs
// every candidate scored on the same scale, and a caller that wants to trade
// recall for precision needs the losing classes too.
func (e *Ensemble) PredictProbabilities(features []float64) []float64 {
	scores := make([]float64, e.numClass)
	e.RawScores(features, scores)

	// Softmax shifted by the maximum. Exponentiating raw scores directly
	// overflows for confident predictions, and the shift is exact rather than
	// an approximation: it cancels in the ratio.
	best := 0
	for i := 1; i < len(scores); i++ {
		if scores[i] > scores[best] {
			best = i
		}
	}
	sum := 0.0
	for i := range scores {
		scores[i] = math.Exp(scores[i] - scores[best])
		sum += scores[i]
	}
	if sum <= 0 {
		return scores
	}
	for i := range scores {
		scores[i] /= sum
	}
	return scores
}

// PredictBinary returns P(positive) for a single-output model.
//
// The caller compares it against 0.5, which is argmax over the two classes
// rather than a tuned cutoff — the sigmoid is monotonic, so "score >= 0.5" and
// "positive outscores negative" are the same statement.
func (e *Ensemble) PredictBinary(features []float64) float64 {
	scores := make([]float64, e.numClass)
	e.RawScores(features, scores)
	return 1 / (1 + math.Exp(-scores[0]))
}

// Explain returns a human-readable decision path for one feature vector.
//
// AGENTS.md requires detection to be "deterministic, repeatable, and
// explainable". A learned model satisfies the first two by construction; this
// is what satisfies the third. The full path across thousands of trees is
// unreadable, so it reports the trees that moved the winning class most, plus
// which features the vector was tested against most often.
func (e *Ensemble) Explain(features []float64, names []string, class, topTrees int) string {
	type contribution struct {
		tree int
		leaf float64
		path []string
	}
	var contributions []contribution
	uses := map[int]int{}

	for tree, root := range e.treeRoot {
		node := root
		var path []string
		for {
			feature := int(e.nodeFeature[node])
			value := features[feature]
			uses[feature]++
			if math.IsNaN(value) {
				value = 0
			}
			goLeft := value <= e.threshold[node]
			comparison := ">"
			if goLeft {
				comparison = "<="
			}
			name := fmt.Sprintf("f%d", feature)
			if feature < len(names) {
				name = names[feature]
			}
			path = append(path, fmt.Sprintf("%s=%.4g %s %.4g", name, value, comparison, e.threshold[node]))
			next := e.right[node]
			if goLeft {
				next = e.left[node]
			}
			if next < 0 {
				if tree%e.numClass == class {
					contributions = append(contributions, contribution{tree, e.leafValue[-next-1], path})
				}
				break
			}
			node = next
		}
	}
	sort.SliceStable(contributions, func(i, j int) bool {
		return math.Abs(contributions[i].leaf) > math.Abs(contributions[j].leaf)
	})

	var out strings.Builder
	for i, c := range contributions {
		if i >= topTrees {
			break
		}
		fmt.Fprintf(&out, "  tree %4d  leaf %+.5f  %s\n", c.tree, c.leaf, strings.Join(c.path, "  |  "))
	}
	ranked := make([]int, 0, len(uses))
	for feature := range uses {
		ranked = append(ranked, feature)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if uses[ranked[i]] != uses[ranked[j]] {
			return uses[ranked[i]] > uses[ranked[j]]
		}
		return ranked[i] < ranked[j]
	})
	out.WriteString("most-tested features:\n")
	for i, feature := range ranked {
		if i >= 5 {
			break
		}
		name := fmt.Sprintf("f%d", feature)
		if feature < len(names) {
			name = names[feature]
		}
		fmt.Fprintf(&out, "  %-22s tested %d times, value %.4g\n", name, uses[feature], features[feature])
	}
	return out.String()
}
