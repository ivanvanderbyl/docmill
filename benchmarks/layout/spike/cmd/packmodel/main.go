// Command packmodel converts a LightGBM text model into the compact binary
// form that pkg/pdf embeds.
//
// This is the storage decision the codegen note measured
// (docs/research/2026-08-06-formula-classifier-spike.md): generated Go source
// and an embedded blob produce an identical binary size and identical
// prediction speed, but at multiclass scale the blob rebuilds in 0.35 s against
// 6.9 s — a 20x difference paid on every retrain, for 4.4 ms once at start-up.
// The 12-class LINE model is 3,600 trees and 108,000 nodes, well past the point
// where that trade favours the blob.
//
// It is build-time tooling, deliberately outside pkg/pdf: nothing at runtime
// parses LightGBM text.
package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// blobMagic identifies the format and its version. A model packed by an older
// build must fail loudly rather than be misread as the current layout.
const blobMagic = "DMLM0001"

type tree struct {
	feature   []int32
	threshold []float64
	left      []int32
	right     []int32
	leaf      []float64
}

func main() {
	in := flag.String("in", "", "LightGBM text model")
	out := flag.String("out", "", "output blob")
	classesIn := flag.String("classes", "", "JSON array of class names")
	flag.Parse()
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: packmodel -in model.txt -out model.bin [-classes classes.json]")
		os.Exit(2)
	}

	trees, meta, err := parse(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "packmodel:", err)
		os.Exit(1)
	}

	var classes []string
	if *classesIn != "" {
		data, err := os.ReadFile(*classesIn)
		if err == nil {
			_ = json.Unmarshal(data, &classes)
		}
	}
	if len(classes) != 0 && len(classes) != meta.numClass {
		fmt.Fprintf(os.Stderr, "packmodel: %d class names for a %d-class model\n", len(classes), meta.numClass)
		os.Exit(1)
	}

	blob, err := pack(trees, meta, classes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "packmodel:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, blob, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "packmodel:", err)
		os.Exit(1)
	}

	nodes, leaves := 0, 0
	for _, t := range trees {
		nodes += len(t.feature)
		leaves += len(t.leaf)
	}
	fmt.Printf("wrote %s: %d trees (%d classes), %d nodes, %d leaves, %d features, %.2f MB\n",
		*out, len(trees), meta.numClass, nodes, leaves, meta.numFeatures, float64(len(blob))/(1<<20))
}

type modelMeta struct {
	numClass    int
	numFeatures int
}

// parse reads the LightGBM text format, strictly. It rejects anything whose
// semantics the Go walker does not reproduce rather than emitting a blob that
// scores differently from the trainer.
func parse(path string) ([]tree, modelMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, modelMeta{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)

	var trees []tree
	var current *tree
	meta := modelMeta{numFeatures: -1}
	flush := func() {
		if current != nil {
			trees = append(trees, *current)
			current = nil
		}
	}

	for scanner.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !found {
			continue
		}
		switch key {
		case "num_class":
			if meta.numClass, err = strconv.Atoi(value); err != nil {
				return nil, meta, fmt.Errorf("num_class: %w", err)
			}
		case "max_feature_idx":
			n, convErr := strconv.Atoi(value)
			if convErr != nil {
				return nil, meta, fmt.Errorf("max_feature_idx: %w", convErr)
			}
			meta.numFeatures = n + 1
		case "objective":
			// The Go side applies softmax for multiclass and a logistic for
			// binary; anything else must stop the build.
			if !strings.HasPrefix(value, "multiclass ") && value != "binary sigmoid:1" {
				return nil, meta, fmt.Errorf("unsupported objective %q", value)
			}
		case "Tree":
			flush()
			current = &tree{}
		case "num_cat":
			if current != nil && value != "0" {
				return nil, meta, fmt.Errorf("tree %d has categorical splits; the walker handles numerical splits only", len(trees))
			}
		case "decision_type":
			// bit 0 categorical, bit 1 default_left, bits 2-3 missing_type.
			// 2 means numerical, default-left, missing_type None — the only
			// shape the walker reproduces.
			for _, field := range strings.Fields(value) {
				if field != "2" {
					return nil, meta, fmt.Errorf("tree %d has decision_type %s; only 2 is supported", len(trees), field)
				}
			}
		case "split_feature":
			if current != nil {
				current.feature, err = parseInt32s(value)
			}
		case "threshold":
			if current != nil {
				current.threshold, err = parseFloats(value)
			}
		case "left_child":
			if current != nil {
				current.left, err = parseInt32s(value)
			}
		case "right_child":
			if current != nil {
				current.right, err = parseInt32s(value)
			}
		case "leaf_value":
			if current != nil {
				current.leaf, err = parseFloats(value)
			}
			// `shrinkage` is deliberately ignored: LightGBM bakes the learning
			// rate into leaf_value in the text format. Applying it here would
			// double-count it.
		}
		if err != nil {
			return nil, meta, fmt.Errorf("%s: %w", key, err)
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, meta, err
	}
	if meta.numClass < 1 || meta.numFeatures < 1 || len(trees) == 0 {
		return nil, meta, fmt.Errorf("incomplete model: %d classes, %d features, %d trees", meta.numClass, meta.numFeatures, len(trees))
	}
	if len(trees)%meta.numClass != 0 {
		return nil, meta, fmt.Errorf("%d trees is not a multiple of %d classes", len(trees), meta.numClass)
	}
	return trees, meta, nil
}

func parseInt32s(value string) ([]int32, error) {
	fields := strings.Fields(value)
	out := make([]int32, len(fields))
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil, err
		}
		out[i] = int32(n)
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

// pack flattens every tree into one node array with absolute child links, then
// writes it little-endian. A non-negative child is an internal node index; a
// negative child encodes leaf -(index+1) in the leaf array.
//
// Layout: magic, then counts, then class names, then the arrays. Decoding is a
// handful of typed copies — no parsing, no allocation per node.
func pack(trees []tree, meta modelMeta, classes []string) ([]byte, error) {
	var (
		feature   []int32
		threshold []float64
		left      []int32
		right     []int32
		leaf      []float64
		roots     []int32
	)
	for _, t := range trees {
		if len(t.feature) != len(t.threshold) || len(t.feature) != len(t.left) || len(t.feature) != len(t.right) {
			return nil, fmt.Errorf("ragged node arrays")
		}
		nodeBase, leafBase := int32(len(feature)), int32(len(leaf))
		roots = append(roots, nodeBase)
		leaf = append(leaf, t.leaf...)
		link := func(child int32) int32 {
			if child >= 0 {
				return nodeBase + child
			}
			return -(leafBase + (-child - 1)) - 1
		}
		for i := range t.feature {
			if int(t.feature[i]) >= meta.numFeatures {
				return nil, fmt.Errorf("split references feature %d of %d", t.feature[i], meta.numFeatures)
			}
			feature = append(feature, t.feature[i])
			threshold = append(threshold, t.threshold[i])
			left = append(left, link(t.left[i]))
			right = append(right, link(t.right[i]))
		}
	}

	names := strings.Join(classes, "\x00")
	buf := make([]byte, 0, 8+7*4+len(names)+len(feature)*16+len(leaf)*8+len(roots)*4)
	buf = append(buf, blobMagic...)
	put32 := func(v int) { buf = binary.LittleEndian.AppendUint32(buf, uint32(v)) }
	put32(meta.numClass)
	put32(meta.numFeatures)
	put32(len(roots))
	put32(len(feature))
	put32(len(leaf))
	put32(len(names))
	buf = append(buf, names...)

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
	for _, v := range leaf {
		buf = binary.LittleEndian.AppendUint64(buf, math.Float64bits(v))
	}
	return buf, nil
}
