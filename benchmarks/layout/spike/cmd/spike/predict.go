package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/dmitryikh/leaves"
)

// layoutModel is the LightGBM artefact trained by train.py, compiled into the
// binary. Task 4 of the plan settles on exactly this shape for the real thing —
// github.com/dmitryikh/leaves plus go:embed — so the spike exercises the same
// path rather than a stand-in: nothing is read from disk at run time and no
// model file ships alongside the binary.
//
//go:embed layoutmodel.txt
var layoutModelBytes []byte

// loadLayoutModel parses the artefact into trees. This is the work codegen
// moves from start-up to compile time, so it is a named function to let
// BenchmarkLeavesLoad measure exactly that cost.
func loadLayoutModel() (*leaves.Ensemble, error) {
	return leaves.LGEnsembleFromReader(bufio.NewReader(bytes.NewReader(layoutModelBytes)), true)
}

var layoutModel = sync.OnceValues(loadLayoutModel)

// predictionRow is one classified line, written as JSONL for eval.py.
type predictionRow struct {
	Doc     string  `json:"doc"`
	Page    int     `json:"page"`
	Line    int     `json:"line"`
	L       float64 `json:"l"`
	T       float64 `json:"t"`
	R       float64 `json:"r"`
	B       float64 `json:"b"`
	Text    string  `json:"text"`
	Score   float64 `json:"score"`
	Formula bool    `json:"formula"`
}

func runPredict(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: spike predict <input.pdf>...")
	}
	model, err := layoutModel()
	if err != nil {
		return fmt.Errorf("load embedded model: %w", err)
	}
	if got, want := model.NFeatures(), len(featureNames); got != want {
		return fmt.Errorf("model expects %d features, emitter produces %d — the feature contract has drifted", got, want)
	}

	encoder := json.NewEncoder(os.Stdout)
	for _, path := range args {
		rows, err := emitDocument(context.Background(), path, false)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		formulas := 0
		for _, row := range rows {
			// Binary argmax: the sigmoid-transformed score is P(formula), so
			// the larger of the two class probabilities is formula exactly when
			// the score clears 0.5. No tuned cutoff enters here — the plan
			// requires inference to stay pure argmax.
			score := model.PredictSingle(row.Features, 0)
			formula := score >= 0.5
			if formula {
				formulas++
			}
			if err := encoder.Encode(predictionRow{
				Doc: row.Doc, Page: row.Page, Line: row.Line,
				L: row.L, T: row.T, R: row.R, B: row.B,
				Text: row.Text, Score: score, Formula: formula,
			}); err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "%s: %d/%d lines predicted Formula\n", path, formulas, len(rows))
	}
	return nil
}
