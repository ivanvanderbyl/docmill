package pdf

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// The Task 4 fixture test: feature vectors plus the scores the Python trainer
// produced for them, replayed through the Go predictor.
//
// The plan calls this "the test proving the Python and Go sides agree", aimed at
// the two realistic porting failures — an index shift in the feature vector and
// float drift in the tree walk. The fixture is stratified across all twelve
// classes so every softmax branch is exercised, not just the common ones.

type layoutFixture struct {
	Features []string `json:"features"`
	Classes  []string `json:"classes"`
	Cases    []struct {
		F     []float64 `json:"f"`
		Label string    `json:"label"`
		Prob  float64   `json:"prob"`
		Raw   []float64 `json:"raw"`
	} `json:"cases"`
}

func loadLayoutFixture(t testing.TB) layoutFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/layoutmodel_fixture.json")
	if err != nil {
		t.Skipf("fixture not present: %v", err)
	}
	var fixture layoutFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return fixture
}

func TestLayoutModelDecodes(t *testing.T) {
	ok, err := LayoutModelAvailable()
	if err != nil {
		t.Fatalf("embedded layout model failed to decode: %v", err)
	}
	if !ok {
		t.Fatal("embedded layout model unavailable")
	}
	if classes := LayoutModelClasses(); len(classes) == 0 {
		t.Fatal("model reports no classes")
	}
}

// TestLayoutModelFeatureContract is the guard against the silent failure: a
// model trained on a different vector must be rejected, not scored.
func TestLayoutModelFeatureContract(t *testing.T) {
	fixture := loadLayoutFixture(t)
	if len(fixture.Features) != len(LayoutFeatureNames) {
		t.Fatalf("fixture declares %d features, pkg/pdf defines %d", len(fixture.Features), len(LayoutFeatureNames))
	}
	for i, name := range fixture.Features {
		if name != LayoutFeatureNames[i] {
			t.Errorf("feature %d: fixture %q, pkg/pdf %q", i, name, LayoutFeatureNames[i])
		}
	}
	model, err := layoutModel()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if model.numFeatures != len(LayoutFeatureNames) {
		t.Errorf("model expects %d features, contract defines %d", model.numFeatures, len(LayoutFeatureNames))
	}
}

// TestLayoutModelMatchesPython replays LightGBM's own raw scores. Raw scores
// rather than probabilities: they are what the tree walk actually produces, so
// a mismatch points straight at the walk instead of being blurred by softmax.
func TestLayoutModelMatchesPython(t *testing.T) {
	fixture := loadLayoutFixture(t)
	model, err := layoutModel()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Sums of many float64 leaf values in the same order should agree to well
	// under this; the tolerance is double-rounding slack, not a quality bar.
	const tolerance = 1e-9

	scores := make([]float64, model.numClass)
	worst := 0.0
	for i, c := range fixture.Cases {
		if len(c.Raw) != model.numClass {
			t.Fatalf("case %d: fixture has %d raw scores, model has %d classes", i, len(c.Raw), model.numClass)
		}
		model.rawScores(c.F, scores)
		for class, want := range c.Raw {
			if delta := math.Abs(scores[class] - want); delta > worst {
				worst = delta
				if delta > tolerance {
					t.Fatalf("case %d class %d (%s): go=%.17g python=%.17g (delta %.3g)",
						i, class, fixture.Classes[class], scores[class], want, delta)
				}
			}
		}

		label, probability := model.PredictLineClass(c.F)
		if label != c.Label {
			t.Errorf("case %d: go predicts %q, python %q", i, label, c.Label)
		}
		if math.Abs(probability-c.Prob) > 1e-6 {
			t.Errorf("case %d: probability go=%.6f python=%.6f", i, probability, c.Prob)
		}
	}
	t.Logf("%d cases across %d classes, worst raw-score delta %.3g", len(fixture.Cases), model.numClass, worst)
}

// TestLayoutModelRejectsCorruptBlob covers the failure the loader exists to make
// loud: a stale or truncated artefact must be reported, never half-decoded.
func TestLayoutModelRejectsCorruptBlob(t *testing.T) {
	for name, blob := range map[string][]byte{
		"empty":        nil,
		"bad magic":    append([]byte("XXXXXXXX"), make([]byte, 64)...),
		"truncated":    layoutModelBlob[:len(layoutModelBlob)/2],
		"short header": []byte("DMLM0001"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeLayoutModel(blob); err == nil {
				t.Error("expected an error, got none")
			}
		})
	}
}

func BenchmarkLayoutModelPredict(b *testing.B) {
	fixture := loadLayoutFixture(b)
	model, err := layoutModel()
	if err != nil {
		b.Fatalf("decode: %v", err)
	}
	vectors := make([][]float64, 0, len(fixture.Cases))
	for _, c := range fixture.Cases {
		vectors = append(vectors, c.F)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.PredictLineClass(vectors[i%len(vectors)])
	}
}

func BenchmarkLayoutModelDecode(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := decodeLayoutModel(layoutModelBlob); err != nil {
			b.Fatal(err)
		}
	}
}
