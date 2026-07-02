package table

import (
	"strings"
	"testing"
)

// longText builds an n-word prose cell.
func longText(n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = "word"
	}
	return strings.Join(words, " ")
}

func TestTableValidityScoreFloorsAllColumnsProse(t *testing.T) {
	// Every content column is prose (a body-paragraph slab gridded into a fake
	// table): score must be 0 (the non-table floor the backend gate rejects).
	data := FromGrid([][]string{
		{longText(30), longText(40), longText(35)},
		{longText(50), longText(45), longText(60)},
	})
	if got := ValidityScore(data); got != 0 {
		t.Fatalf("ValidityScore=%.3f want 0 for all-columns-prose", got)
	}
}

func TestTableValidityScorePositiveForRealTable(t *testing.T) {
	// A genuine table with a short key column and a long description column is
	// NOT all-prose, so it scores positive (kept). This is the 646f shape that a
	// >=2/3 prose threshold over-suppressed.
	data := FromGrid([][]string{
		{"Stage", "Function", "Explanation"},
		{"1. Setup", "init", longText(30)},
		{"2. Run", "exec", longText(40)},
	})
	if got := ValidityScore(data); got <= 0 {
		t.Fatalf("ValidityScore=%.3f want >0 for a real key+description table", got)
	}
}

func TestTableValidityScoreCredits(t *testing.T) {
	// A clean key/value table: short header, short key column, regular structure
	// -> each positive credit fires.
	data := FromGrid([][]string{
		{"Name", "Detail"},
		{"A", longText(8)},
		{"B", longText(9)},
	})
	if s := tableStdStructureScore(data); s < 0.99 {
		t.Errorf("stdStructureScore=%.3f want ~1 (every row has 2 cells)", s)
	}
	if s := tableHeaderDistinctScore(data); s <= 0 {
		t.Errorf("headerDistinctScore=%.3f want >0 (short header over long body)", s)
	}
	if s := tableKeyValueScore(data); s < 0.99 {
		t.Errorf("keyValueScore=%.3f want ~1 (first column all short keys)", s)
	}
	if s := tableProseColumnFraction(data); s >= 1.0 {
		t.Errorf("proseColumnFraction=%.3f want <1 (key column is not prose)", s)
	}
}
