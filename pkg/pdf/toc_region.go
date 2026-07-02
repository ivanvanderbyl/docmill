package pdf

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/ivanvanderbyl/docmill/pkg/geom"
)

// Table-of-contents detection — a geometry-only (no literal-text) detector for
// contents / list-of-figures pages. Such pages render as a column of entries,
// each "<entry text> <leader> <page number>" with the page numbers right-aligned
// down a shared margin. Ground truth (and docling) represent these as a TABLE,
// not flowing prose; emitting them as prose interleaves the page numbers into the
// narrative and reads poorly. Detecting the run and emitting it as a 2-column
// pipe table is both better Markdown (a contents page genuinely is tabular) and
// aligns with how the corpus annotates them.
//
// The signal is purely geometric, so it carries no character-pattern overfitting
// (AGENTS.md): a trailing page number (arabic or roman), a leader (dot run OR a
// gap far wider than the line's word spacing), word-like entry text, page numbers
// sharing a right margin and running monotonically down a run of >= minTocRows
// lines. The leader requirement is the load-bearing false-positive guard — body
// prose never has a wide gap before its last token.

const (
	// minTocRows is the minimum aligned entries to call a run a contents region;
	// three dot-leader entries distinguishes a ToC from an incidental pair.
	minTocRows = 3
	// tocRightAlignFactor bounds how far an entry's page-number right edge may
	// deviate from the run's shared right margin, relative to font size.
	tocRightAlignFactor = 0.6
	// tocLeaderGapFactor: a leader gap before the page number is at least this
	// multiple of the line's median inter-word gap.
	tocLeaderGapFactor = 2.5
)

type tocEntry struct {
	rightX float64 // right edge of the page number (the shared margin)
	value  int     // page-number value (for monotonicity)
	roman  bool    // numbering system, so an arabic restart after roman is allowed
	ok     bool
}

// detectTocLines marks which lines belong to a maximal table-of-contents run.
func detectTocLines(lines []ParagraphTextLine) []bool {
	n := len(lines)
	marked := make([]bool, n)
	if n < minTocRows {
		return marked
	}
	entries := make([]tocEntry, n)
	for i := range lines {
		entries[i] = classifyTocLine(lines[i])
	}

	i := 0
	for i < n {
		if !entries[i].ok {
			i++
			continue
		}
		runRight := entries[i].rightX
		count := 1
		prevVal, prevRoman := entries[i].value, entries[i].roman
		j := i
		for j+1 < n && entries[j+1].ok {
			next := entries[j+1]
			tol := math.Max(3, tocRightAlignFactor*lines[j+1].FontSize)
			if math.Abs(next.rightX-runRight) > tol {
				break
			}
			// Page numbers run monotonically down a ToC; allow a reset when the
			// numbering system changes (roman front-matter -> arabic body).
			if next.roman == prevRoman && next.value < prevVal {
				break
			}
			j++
			runRight = (runRight*float64(count) + next.rightX) / float64(count+1)
			count++
			prevVal, prevRoman = next.value, next.roman
		}
		if count >= minTocRows {
			for k := i; k <= j; k++ {
				marked[k] = true
			}
		}
		i = j + 1
	}
	return marked
}

// assembleWithToc assembles a column group's lines into blocks, emitting any
// contents (table-of-contents) run as a pipe table and the remaining lines as
// ordinary paragraphs, preserving order. With no ToC run it is identical to
// assembleParagraphs.
func assembleWithToc(lines []ParagraphTextLine, options ParagraphOptions) []markdownBlock {
	marked := detectTocLines(lines)
	hasToc := false
	for _, m := range marked {
		if m {
			hasToc = true
			break
		}
	}
	if !hasToc {
		return assembleParagraphs(lines, options)
	}
	var blocks []markdownBlock
	for i := 0; i < len(lines); {
		if marked[i] {
			j := i
			for j < len(lines) && marked[j] {
				j++
			}
			blocks = append(blocks, renderTocTable(lines[i:j]))
			i = j
			continue
		}
		j := i
		for j < len(lines) && !marked[j] {
			j++
		}
		blocks = append(blocks, assembleParagraphs(lines[i:j], options)...)
		i = j
	}
	return blocks
}

// classifyTocLine reports whether a single line is a contents entry and returns
// its page-number right edge and value.
func classifyTocLine(line ParagraphTextLine) tocEntry {
	words := line.Words
	if len(words) < 2 {
		return tocEntry{}
	}
	last := words[len(words)-1]
	value, roman, ok := parsePageNumber(last.Value)
	if !ok {
		return tocEntry{}
	}
	// Entry words = everything before the page number, minus a trailing dot leader.
	end := len(words) - 1
	for end > 0 && isDotLeaderWord(words[end-1].Value) {
		end--
	}
	if end == 0 {
		return tocEntry{}
	}
	entryWords := words[:end]
	hasAlpha := false
	for _, w := range entryWords {
		if strings.IndexFunc(w.Value, unicode.IsLetter) >= 0 {
			hasAlpha = true
			break
		}
	}
	if !hasAlpha {
		return tocEntry{}
	}
	// Leader: a dot run between the entry text and the page number, OR a gap
	// before the page number far wider than the line's normal word spacing.
	leader := false
	for k := end; k < len(words)-1; k++ {
		if isDotLeaderWord(words[k].Value) {
			leader = true
			break
		}
	}
	if !leader {
		gap := last.BBox.L - entryWords[len(entryWords)-1].BBox.R
		med := medianWordGap(words)
		if gap > tocLeaderGapFactor*med && gap > 0.5*line.FontSize {
			leader = true
		}
	}
	if !leader {
		return tocEntry{}
	}
	return tocEntry{rightX: last.BBox.R, value: value, roman: roman, ok: true}
}

// tocEntryText returns the entry's text (without the trailing dot leader and
// page number) and the page-number text, for table rendering.
func tocEntryText(line ParagraphTextLine) (entry, page string) {
	words := line.Words
	page = strings.TrimSpace(words[len(words)-1].Value)
	end := len(words) - 1
	for end > 0 && isDotLeaderWord(words[end-1].Value) {
		end--
	}
	parts := make([]string, 0, end)
	for _, w := range words[:end] {
		t := strings.TrimSpace(w.Value)
		if t != "" {
			parts = append(parts, t)
		}
	}
	return collapseSpaces(strings.Join(parts, " ")), page
}

// renderTocTable renders a contents run as a 2-column pipe table (entry | page).
func renderTocTable(run []ParagraphTextLine) markdownBlock {
	minIndex := run[0].MinIndex
	rows := make([]string, 0, len(run)+2)
	rows = append(rows, "| | |", "| --- | --- |")
	bbox := run[0].BBox
	for _, line := range run {
		if line.MinIndex < minIndex {
			minIndex = line.MinIndex
		}
		bbox = geom.EnclosingBox(bbox, line.BBox)
		entry, page := tocEntryText(line)
		rows = append(rows, "| "+escapeTableCell(entry)+" | "+escapeTableCell(page)+" |")
	}
	return markdownBlock{
		Index:     minIndex,
		Text:      strings.Join(rows, "\n"),
		Box:       bbox,
		FontSize:  run[0].FontSize,
		LineCount: len(run),
	}
}

func escapeTableCell(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

// parsePageNumber parses an arabic or canonical lowercase roman page number.
func parsePageNumber(s string) (value int, roman, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false, false
	}
	allDigit := true
	for _, r := range s {
		if !unicode.IsDigit(r) {
			allDigit = false
			break
		}
	}
	if allDigit {
		n, err := strconv.Atoi(s)
		if err != nil || n <= 0 {
			return 0, false, false
		}
		return n, false, true
	}
	if v, ok := romanValue(s); ok {
		return v, true, true
	}
	return 0, false, false
}

// romanValue parses a lowercase roman numeral, requiring canonical form (it must
// re-encode to the same string) so ordinary words ("did", "mill") are rejected.
func romanValue(s string) (int, bool) {
	if len(s) == 0 || len(s) > 7 {
		return 0, false
	}
	vals := map[rune]int{'i': 1, 'v': 5, 'x': 10, 'l': 50, 'c': 100, 'd': 500, 'm': 1000}
	runes := []rune(s)
	total := 0
	for i, r := range runes {
		v, exist := vals[r]
		if !exist {
			return 0, false
		}
		if i+1 < len(runes) && vals[runes[i+1]] > v {
			total -= v
		} else {
			total += v
		}
	}
	if total <= 0 || total >= 4000 {
		return 0, false
	}
	if encodeRoman(total) != s {
		return 0, false
	}
	return total, true
}

func encodeRoman(n int) string {
	type rv struct {
		v int
		s string
	}
	table := []rv{{1000, "m"}, {900, "cm"}, {500, "d"}, {400, "cd"}, {100, "c"}, {90, "xc"},
		{50, "l"}, {40, "xl"}, {10, "x"}, {9, "ix"}, {5, "v"}, {4, "iv"}, {1, "i"}}
	var b strings.Builder
	for _, e := range table {
		for n >= e.v {
			b.WriteString(e.s)
			n -= e.v
		}
	}
	return b.String()
}

func isDotLeaderWord(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		switch r {
		case '.', '·', '•', '…', '․', '‥', ' ':
		default:
			return false
		}
	}
	return strings.ContainsAny(s, ".·•…")
}

func medianWordGap(words []TextLineWord) float64 {
	if len(words) < 2 {
		return 0
	}
	gaps := make([]float64, 0, len(words)-1)
	for i := 1; i < len(words); i++ {
		g := words[i].BBox.L - words[i-1].BBox.R
		if g > 0 {
			gaps = append(gaps, g)
		}
	}
	if len(gaps) == 0 {
		return 0
	}
	sort.Float64s(gaps)
	return gaps[len(gaps)/2]
}
