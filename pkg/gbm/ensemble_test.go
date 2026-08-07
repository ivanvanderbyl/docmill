package gbm

import (
	"encoding/binary"
	"math"
	"testing"
)

// build packs a tiny model by hand, in the same layout Decode expects, so a
// crash can be reproduced without a 5 MB artefact.
func build(numClass int, roots []int32, feature []int32, threshold []float64, left, right []int32, leaves []float64) []byte {
	buf := append([]byte(nil), Magic...)
	put := func(v int) { buf = binary.LittleEndian.AppendUint32(buf, uint32(v)) }
	put(numClass)
	put(1) // one feature
	put(len(roots))
	put(len(feature))
	put(len(leaves))
	put(0) // no class names
	for _, v := range roots {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(v))
	}
	for _, v := range feature {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(v))
	}
	for _, v := range threshold {
		buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(v))
	}
	for _, v := range left {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(v))
	}
	for _, v := range right {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(v))
	}
	for _, v := range leaves {
		buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(v))
	}
	return buf
}

func TestRawScoresHandlesASplitlessTree(t *testing.T) {
	// LightGBM emits a tree with no splits whenever a class is best predicted
	// by a constant, which happens as soon as a class is rare enough. Such a
	// tree contributes no nodes, so its root is encoded as the leaf itself.
	// Descending from it walked off the end of the node array and panicked —
	// a crash rather than a wrong answer, and only on models that contain one.
	blob := build(1,
		[]int32{-1},        // one tree, root IS leaf 0
		nil, nil, nil, nil, // no internal nodes at all
		[]float64{0.25},
	)
	model, err := Decode(blob)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	scores := make([]float64, 1)
	model.RawScores([]float64{0}, scores)
	if scores[0] != 0.25 {
		t.Errorf("score = %v, want the leaf value 0.25", scores[0])
	}
}

func TestRawScoresMixesSplitlessAndNormalTrees(t *testing.T) {
	// The real artefact interleaves both kinds, one tree per class per round.
	// Leaf indices must stay correct across the mix.
	blob := build(1,
		[]int32{-1, 1}, // tree 0 is a bare leaf, tree 1 has a node
		[]int32{0, 0},  // node 0 unused, node 1 is tree 1's root
		[]float64{0, 5},
		[]int32{-2, -2}, // tree 1 left  -> leaf 1
		[]int32{-3, -3}, // tree 1 right -> leaf 2
		[]float64{0.25, 1.0, 2.0},
	)
	model, err := Decode(blob)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	scores := make([]float64, 1)
	model.RawScores([]float64{1}, scores) // 1 <= 5, so tree 1 goes left
	if scores[0] != 1.25 {
		t.Errorf("score = %v, want 0.25 + 1.0", scores[0])
	}
	model.RawScores([]float64{9}, scores) // 9 > 5, so tree 1 goes right
	if scores[0] != 2.25 {
		t.Errorf("score = %v, want 0.25 + 2.0", scores[0])
	}
}
