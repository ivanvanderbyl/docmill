package main

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"testing"
)

// The generated model and the leaves model must be the same function. These
// tests are what let Task 4 choose between them on evidence rather than taste:
// if they ever disagree, the codegen path is wrong and the choice is moot.

func loadFixture(t testing.TB) fixture {
	t.Helper()
	data, err := os.ReadFile("../../out/fixture.json")
	if err != nil {
		t.Skipf("fixture not generated (run train.py): %v", err)
	}
	var fx fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return fx
}

// TestGeneratedMatchesLightGBM is the same check `spike verify` runs against
// leaves, aimed at the generated code: LightGBM's own scores, reproduced.
func TestGeneratedMatchesLightGBM(t *testing.T) {
	fx := loadFixture(t)
	if genFeatureCount != len(featureNames) {
		t.Fatalf("generated model expects %d features, emitter declares %d", genFeatureCount, len(featureNames))
	}
	for i, c := range fx.Cases {
		got := genPredict(c.F)
		if delta := math.Abs(got - c.Score); delta > scoreTolerance {
			t.Errorf("case %d: generated=%.17g lightgbm=%.17g (delta %.3g)", i, got, c.Score, delta)
		}
	}
}

// TestGeneratedMatchesLeavesOnRandomVectors is the stronger check. The fixture
// is 20 hand-picked vectors; this drives both implementations with 200k random
// ones drawn across each feature's observed range, so any split boundary,
// default-branch or NaN disagreement between them has somewhere to show up.
func TestGeneratedMatchesLeavesOnRandomVectors(t *testing.T) {
	model, err := layoutModel()
	if err != nil {
		t.Fatalf("load embedded model: %v", err)
	}
	random := rand.New(rand.NewSource(20260806))

	// Ranges roughly covering the feature_infos in the artefact, deliberately
	// overshooting so vectors land on both sides of extreme thresholds.
	const iterations = 200_000
	worst := 0.0
	features := make([]float64, len(featureNames))
	for i := 0; i < iterations; i++ {
		for j := range features {
			switch {
			case i%97 == 0 && j == i%len(features):
				features[j] = math.NaN() // exercise the missing-value path
			default:
				features[j] = random.Float64() * 50
			}
		}
		want := model.PredictSingle(features, 0)
		got := genPredict(features)
		if delta := math.Abs(got - want); delta > worst {
			worst = delta
			if delta > scoreTolerance {
				t.Fatalf("iteration %d: generated=%.17g leaves=%.17g (delta %.3g)\nfeatures=%v", i, got, want, delta, features)
			}
		}
	}
	t.Logf("%d random vectors, worst delta %.3g", iterations, worst)
}

func benchmarkVectors(t testing.TB) [][]float64 {
	fx := loadFixture(t)
	vectors := make([][]float64, 0, len(fx.Cases))
	for _, c := range fx.Cases {
		vectors = append(vectors, c.F)
	}
	return vectors
}

func BenchmarkLeavesPredict(b *testing.B) {
	model, err := layoutModel()
	if err != nil {
		b.Fatalf("load embedded model: %v", err)
	}
	vectors := benchmarkVectors(b)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := 0; i < b.N; i++ {
		sink += model.PredictSingle(vectors[i%len(vectors)], 0)
	}
	_ = sink
}

func BenchmarkGeneratedPredict(b *testing.B) {
	vectors := benchmarkVectors(b)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := 0; i < b.N; i++ {
		sink += genPredict(vectors[i%len(vectors)])
	}
	_ = sink
}

// BenchmarkLeavesLoad measures what codegen removes from start-up: parsing the
// ~1 MB text artefact into trees on first use.
func BenchmarkLeavesLoad(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := loadLayoutModel(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGeneratedPredictPacked(b *testing.B) {
	vectors := benchmarkVectors(b)
	b.ReportAllocs()
	b.ResetTimer()
	var sink float64
	for i := 0; i < b.N; i++ {
		sink += 1 / (1 + math.Exp(-genPredictRawPacked(vectors[i%len(vectors)])))
	}
	_ = sink
}

// TestPackedMatchesFlat pins the two generated layouts to each other: same
// trees, same arithmetic, different memory layout.
func TestPackedMatchesFlat(t *testing.T) {
	random := rand.New(rand.NewSource(4242))
	features := make([]float64, len(featureNames))
	for i := 0; i < 50_000; i++ {
		for j := range features {
			features[j] = random.Float64() * 50
		}
		if got, want := genPredictRawPacked(features), genPredictRaw(features); got != want {
			t.Fatalf("iteration %d: packed=%.17g flat=%.17g", i, got, want)
		}
	}
}
