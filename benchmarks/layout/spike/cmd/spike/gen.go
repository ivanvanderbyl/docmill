package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// The codegen path: turn the LightGBM text artefact into flat Go arrays.
//
// Task 4 of the plan holds this in reserve — "Codegen is deferred, not
// rejected" — for three reasons it lists: it drops the leaves dependency, it
// moves model parsing from start-up to compile time, and it makes a decision
// path trivial to print, which is the AGENTS.md explainability requirement the
// plan flags as a known gap against leaves.
//
// The artefact is identical either way, so the two paths coexist here and
// predict_test.go asserts they agree exactly. That equality test is the point:
// it is what lets the choice be made on measurements rather than on taste.

// lgbTree is one parsed tree from the text model.
type lgbTree struct {
	splitFeature []int
	threshold    []float64
	leftChild    []int
	rightChild   []int
	leafValue    []float64
}

// parseLGBModel reads the LightGBM text format. It is deliberately strict: it
// rejects anything whose semantics the generated walker does not reproduce,
// rather than emitting Go that silently scores differently from the trainer.
func parseLGBModel(data []byte) (trees []lgbTree, nFeatures int, err error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)

	var current *lgbTree
	nFeatures = -1
	flush := func() {
		if current != nil {
			trees = append(trees, *current)
			current = nil
		}
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		switch key {
		case "max_feature_idx":
			n, convErr := strconv.Atoi(value)
			if convErr != nil {
				return nil, 0, fmt.Errorf("max_feature_idx: %w", convErr)
			}
			nFeatures = n + 1
		case "objective":
			// The generated code hard-codes the logistic transform, so anything
			// else must stop the build rather than score wrongly.
			if value != "binary sigmoid:1" {
				return nil, 0, fmt.Errorf("unsupported objective %q: generated code applies binary sigmoid:1", value)
			}
		case "Tree":
			flush()
			current = &lgbTree{}
		case "num_cat":
			if current != nil && value != "0" {
				return nil, 0, fmt.Errorf("tree %d has categorical splits (num_cat=%s); the generated walker handles numerical splits only", len(trees), value)
			}
		case "decision_type":
			// decision_type bit 0 = categorical, bit 1 = default_left,
			// bits 2-3 = missing_type. Every split in this model is 2:
			// numerical, default-left, missing_type None. The generated
			// walker reproduces exactly that, so reject anything else.
			for _, field := range strings.Fields(value) {
				if field != "2" {
					return nil, 0, fmt.Errorf("tree %d has decision_type %s; the generated walker handles only 2 (numerical, default-left, missing_type None)", len(trees), field)
				}
			}
		case "split_feature":
			if current != nil {
				current.splitFeature, err = parseInts(value)
			}
		case "threshold":
			if current != nil {
				current.threshold, err = parseFloats(value)
			}
		case "left_child":
			if current != nil {
				current.leftChild, err = parseInts(value)
			}
		case "right_child":
			if current != nil {
				current.rightChild, err = parseInts(value)
			}
		case "leaf_value":
			if current != nil {
				current.leafValue, err = parseFloats(value)
			}
			// NOTE: `shrinkage` is deliberately ignored. LightGBM bakes the
			// learning rate into leaf_value in the text format, and leaves
			// ignores it too (lgensemble_io.go leaves the field commented out).
			// Applying it here would double-count it.
		}
		if err != nil {
			return nil, 0, fmt.Errorf("%s: %w", key, err)
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if nFeatures < 0 {
		return nil, 0, fmt.Errorf("no max_feature_idx in model")
	}
	if len(trees) == 0 {
		return nil, 0, fmt.Errorf("no trees in model")
	}
	for i, tree := range trees {
		if len(tree.splitFeature) != len(tree.threshold) ||
			len(tree.splitFeature) != len(tree.leftChild) ||
			len(tree.splitFeature) != len(tree.rightChild) {
			return nil, 0, fmt.Errorf("tree %d: ragged node arrays", i)
		}
	}
	return trees, nFeatures, nil
}

func parseInts(value string) ([]int, error) {
	fields := strings.Fields(value)
	out := make([]int, len(fields))
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil, err
		}
		out[i] = n
	}
	return out, nil
}

func parseFloats(value string) ([]float64, error) {
	fields := strings.Fields(value)
	out := make([]float64, len(fields))
	for i, field := range fields {
		f, err := strconv.ParseFloat(field, 64)
		if err != nil {
			return nil, err
		}
		out[i] = f
	}
	return out, nil
}

// runGen writes layoutmodel_gen.go from the embedded artefact.
//
// Layout: every tree's nodes are concatenated into one flat array, and child
// links are rewritten as absolute indices into that array — a non-negative
// child is an internal node, a negative child encodes a leaf as -(leafIndex+1).
// One array walk per tree, no pointer chasing, no per-tree bounds juggling.
func runGen(args []string) error {
	out := "benchmarks/layout/spike/cmd/spike/layoutmodel_gen.go"
	if len(args) > 0 {
		out = args[0]
	}
	trees, nFeatures, err := parseLGBModel(layoutModelBytes)
	if err != nil {
		return err
	}

	var (
		feature   []int
		threshold []float64
		left      []int
		right     []int
		leaf      []float64
		roots     []int
	)
	for _, tree := range trees {
		nodeBase, leafBase := len(feature), len(leaf)
		roots = append(roots, nodeBase)
		leaf = append(leaf, tree.leafValue...)
		link := func(child int) int {
			if child >= 0 {
				return nodeBase + child
			}
			// LightGBM encodes a leaf as ~index; re-encode as -(index+1)
			// against the flat leaf array.
			return -(leafBase + (-child - 1)) - 1
		}
		for i := range tree.splitFeature {
			feature = append(feature, tree.splitFeature[i])
			threshold = append(threshold, tree.threshold[i])
			left = append(left, link(tree.leftChild[i]))
			right = append(right, link(tree.rightChild[i]))
		}
	}

	maxFeature := 0
	for _, f := range feature {
		if f > maxFeature {
			maxFeature = f
		}
	}
	if maxFeature >= nFeatures {
		return fmt.Errorf("split references feature %d but model declares %d", maxFeature, nFeatures)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, `// Code generated by "spike gen" from layoutmodel.txt. DO NOT EDIT.
//
// Flat-array form of the LightGBM ensemble: %d trees, %d internal nodes,
// %d leaves, over %d features. Child links are absolute indices into
// genNodeFeature; a negative child encodes leaf -(index+1) in genLeafValue.
//
// Regenerate with: spike gen

package main

const (
	genTreeCount   = %d
	genFeatureCount = %d
)

`, len(trees), len(feature), len(leaf), nFeatures, len(trees), nFeatures)

	writeInts(&buf, "genTreeRoot", roots)
	writeInts(&buf, "genNodeFeature", feature)
	writeFloats(&buf, "genNodeThreshold", threshold)
	writeInts(&buf, "genNodeLeft", left)
	writeInts(&buf, "genNodeRight", right)
	writeFloats(&buf, "genLeafValue", leaf)

	// The same nodes interleaved. Four parallel arrays cost up to four cache
	// lines per node visited; one 24-byte record costs one, and two nodes share
	// a line. predict_test.go benchmarks both layouts.
	fmt.Fprintf(&buf, "var genNodes = [...]genNode{")
	for i := range feature {
		fmt.Fprintf(&buf, "\n\t{%s, %d, %d, %d},",
			strconv.FormatFloat(threshold[i], 'g', -1, 64), left[i], right[i], feature[i])
	}
	buf.WriteString("\n}\n")

	if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d trees, %d nodes, %d leaves, %d features\n",
		out, len(trees), len(feature), len(leaf), nFeatures)
	return nil
}

func writeInts(buf *bytes.Buffer, name string, values []int) {
	fmt.Fprintf(buf, "var %s = [...]int32{", name)
	for i, v := range values {
		if i%16 == 0 {
			buf.WriteString("\n\t")
		}
		fmt.Fprintf(buf, "%d, ", v)
	}
	buf.WriteString("\n}\n\n")
}

// writeFloats formats with 'g' and precision -1: the shortest decimal that
// round-trips to the identical float64. Anything lossier would make the
// generated model score differently from the artefact it came from.
func writeFloats(buf *bytes.Buffer, name string, values []float64) {
	fmt.Fprintf(buf, "var %s = [...]float64{", name)
	for i, v := range values {
		if i%8 == 0 {
			buf.WriteString("\n\t")
		}
		buf.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		buf.WriteString(", ")
	}
	buf.WriteString("\n}\n\n")
}

// sortedFeatureIndexes is a helper for the explain path: features ordered by
// how often they appear on a decision path.
func sortedFeatureIndexes(counts map[int]int) []int {
	out := make([]int, 0, len(counts))
	for k := range counts {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if counts[out[i]] != counts[out[j]] {
			return counts[out[i]] > counts[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}
