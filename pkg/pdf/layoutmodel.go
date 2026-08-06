package pdf

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
)

// The learned LINE model at inference (Task 4 of the layout classifier plan).
//
// Storage is a packed binary blob rather than generated Go source, and that is a
// measured choice, not a preference: the two produce an identical binary size
// and identical prediction speed — the trees are the same and the walk is the
// same code — but at this model's scale (3,600 trees, 108,000 nodes) Go source
// costs 6.9 s to recompile after every retrain against the blob's 0.35 s, for a
// one-off 4.4 ms decode at start-up. See
// docs/research/2026-08-06-formula-classifier-spike.md.
//
// Nothing is read from disk at run time and nothing ships alongside the binary.
// The artefact is produced by benchmarks/layout/spike/cmd/packmodel; no
// LightGBM text parsing exists at run time.

//go:embed layoutmodel.bin
var layoutModelBlob []byte

const layoutModelMagic = "DMLM0001"

// layoutEnsemble is the decoded model: one flat node array across all trees,
// with absolute child links. A non-negative child is an internal node index; a
// negative child encodes leaf -(index+1).
type layoutEnsemble struct {
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

// layoutModel decodes the embedded blob once. Failure is carried rather than
// panicked: a corrupt or stale artefact must disable the learned path, not take
// down extraction.
var layoutModel = sync.OnceValues(func() (*layoutEnsemble, error) {
	return decodeLayoutModel(layoutModelBlob)
})

func decodeLayoutModel(blob []byte) (*layoutEnsemble, error) {
	const header = len(layoutModelMagic) + 6*4
	if len(blob) < header {
		return nil, fmt.Errorf("layout model: blob is %d bytes, shorter than its header", len(blob))
	}
	if string(blob[:len(layoutModelMagic)]) != layoutModelMagic {
		return nil, fmt.Errorf("layout model: bad magic %q", blob[:len(layoutModelMagic)])
	}

	cursor := len(layoutModelMagic)
	next32 := func() int {
		v := int(binary.LittleEndian.Uint32(blob[cursor:]))
		cursor += 4
		return v
	}
	model := &layoutEnsemble{}
	model.numClass = next32()
	model.numFeatures = next32()
	trees := next32()
	nodes := next32()
	leaves := next32()
	nameBytes := next32()

	if cursor+nameBytes > len(blob) {
		return nil, fmt.Errorf("layout model: truncated class names")
	}
	if nameBytes > 0 {
		model.classes = strings.Split(string(blob[cursor:cursor+nameBytes]), "\x00")
	}
	cursor += nameBytes

	// Total size is checked up front so a truncated artefact fails here with a
	// clear message rather than indexing out of bounds mid-decode.
	need := trees*4 + nodes*4 + nodes*8 + nodes*4 + nodes*4 + leaves*8
	if len(blob)-cursor != need {
		return nil, fmt.Errorf("layout model: expected %d bytes of arrays, found %d", need, len(blob)-cursor)
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
		return nil, fmt.Errorf("layout model: %d trees for %d classes", trees, model.numClass)
	}
	if len(model.classes) != 0 && len(model.classes) != model.numClass {
		return nil, fmt.Errorf("layout model: %d class names for %d classes", len(model.classes), model.numClass)
	}
	// The contract check that matters: a model trained against a different
	// feature vector must fail loudly instead of scoring confidently wrong.
	if model.numFeatures != len(LayoutFeatureNames) {
		return nil, fmt.Errorf("layout model expects %d features, pkg/pdf defines %d — the feature contract has drifted",
			model.numFeatures, len(LayoutFeatureNames))
	}
	return model, nil
}

// rawScores accumulates each class's raw score. LightGBM emits one tree per
// class per boosting round, interleaved, so tree i contributes to class
// i%numClass.
func (m *layoutEnsemble) rawScores(features []float64, out []float64) {
	for i := range out {
		out[i] = 0
	}
	for tree, root := range m.treeRoot {
		node := root
		for {
			value := features[m.nodeFeature[node]]
			// LightGBM compares with `<=`, and with missing_type None — which
			// packmodel verifies for every split — a NaN feature is treated as
			// 0.0 rather than sent down the default branch.
			if math.IsNaN(value) {
				value = 0
			}
			next := m.right[node]
			if value <= m.threshold[node] {
				next = m.left[node]
			}
			if next < 0 {
				out[tree%m.numClass] += m.leafValue[-next-1]
				break
			}
			node = next
		}
	}
}

// PredictLineClass returns the model's label for one feature vector, plus its
// probability. The decision is a pure argmax over the raw scores — softmax is
// monotonic, so it cannot change which class wins, and no per-class threshold
// enters anywhere. The plan requires exactly that: cost asymmetry lives in
// training weights, never in inference cutoffs.
func (m *layoutEnsemble) PredictLineClass(features []float64) (string, float64) {
	scores := make([]float64, m.numClass)
	m.rawScores(features, scores)

	best := 0
	for i := 1; i < len(scores); i++ {
		if scores[i] > scores[best] {
			best = i
		}
	}

	// Softmax only to report a calibrated confidence for logging and shadow
	// mode; it plays no part in the decision.
	maxScore := scores[best]
	sum := 0.0
	for _, s := range scores {
		sum += math.Exp(s - maxScore)
	}
	probability := 1.0
	if sum > 0 {
		probability = 1 / sum
	}

	label := fmt.Sprintf("class_%d", best)
	if best < len(m.classes) {
		label = m.classes[best]
	}
	return label, probability
}

// LayoutModelAvailable reports whether the embedded model decoded, and why not
// if it did not. Callers use it to decide whether the learned path can run.
func LayoutModelAvailable() (bool, error) {
	model, err := layoutModel()
	return model != nil && err == nil, err
}

// LayoutModelClasses returns the model's label set.
func LayoutModelClasses() []string {
	model, err := layoutModel()
	if err != nil || model == nil {
		return nil
	}
	return append([]string(nil), model.classes...)
}
